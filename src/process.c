#include "process.h"

#include "loop.h"
#include "util.h"

#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

typedef struct job {
    struct job *next;
    uv_process_t process;
    uv_timer_t stop_timer;
    char id[32];
    char *cmd;
    char *log_path;
    int running;
    int timer_open;
    int64_t exit_status;
    int term_signal;
} job;

static job *jobs;
static unsigned next_id = 1;

static const char *arg_str(cJSON *args, const char *key) {
    cJSON *value = cJSON_GetObjectItem(args, key);
    return cJSON_IsString(value) ? value->valuestring : NULL;
}

static int arg_int(cJSON *args, const char *key, int dflt) {
    cJSON *value = cJSON_GetObjectItem(args, key);
    return cJSON_IsNumber(value) ? value->valueint : dflt;
}

static job *find_job(const char *id) {
    for (job *item = jobs; item; item = item->next)
        if (id && strcmp(item->id, id) == 0) return item;
    return NULL;
}

static void closed(uv_handle_t *handle) {
    (void)handle;
}

static void on_exit(uv_process_t *process, int64_t status, int signal) {
    job *item = process->data;
    item->running = 0;
    item->exit_status = status;
    item->term_signal = signal;
    if (item->timer_open) {
        uv_timer_stop(&item->stop_timer);
        uv_close((uv_handle_t *)&item->stop_timer, closed);
        item->timer_open = 0;
    }
    uv_close((uv_handle_t *)process, closed);
}

static void force_stop(uv_timer_t *timer) {
    job *item = timer->data;
    if (item->running) kill(-item->process.pid, SIGKILL);
    uv_timer_stop(timer);
    uv_close((uv_handle_t *)timer, closed);
    item->timer_open = 0;
}

char *process_start(const char *cmd) {
    job *item = calloc(1, sizeof *item);
    if (!item) return strdup("error: out of memory");
    snprintf(item->id, sizeof item->id, "job-%u", next_id++);
    item->cmd = strdup(cmd);

    char *dir = orc_path("processes");
    mkdirs(dir);
    char name[64];
    snprintf(name, sizeof name, "%d-%s.log", getpid(), item->id);
    item->log_path = malloc(strlen(dir) + strlen(name) + 2);
    if (item->log_path)
        sprintf(item->log_path, "%s/%s", dir, name);
    free(dir);
    int log_fd = open(item->log_path, O_CREAT | O_TRUNC | O_WRONLY, 0600);
    if (!item->cmd || !item->log_path || log_fd < 0) {
        if (log_fd >= 0) close(log_fd);
        free(item->cmd);
        free(item->log_path);
        free(item);
        return strdup("error: cannot create process log");
    }

    uv_stdio_container_t stdio[3] = {0};
    stdio[0].flags = UV_IGNORE;
    stdio[1].flags = UV_INHERIT_FD;
    stdio[1].data.fd = log_fd;
    stdio[2].flags = UV_INHERIT_FD;
    stdio[2].data.fd = log_fd;
    char *argv[] = {"sh", "-c", (char *)cmd, NULL};
    uv_process_options_t options = {0};
    options.file = "/bin/sh";
    options.args = argv;
    options.stdio = stdio;
    options.stdio_count = 3;
    options.exit_cb = on_exit;
    options.flags = UV_PROCESS_DETACHED;
    item->process.data = item;
    int rc = uv_spawn(loop_get(), &item->process, &options);
    close(log_fd);
    if (rc != 0) {
        char out[256];
        snprintf(out, sizeof out, "error: spawn failed: %s", uv_strerror(rc));
        free(item->cmd);
        free(item->log_path);
        free(item);
        return strdup(out);
    }

    item->running = 1;
    item->next = jobs;
    jobs = item;
    char *out = malloc(strlen(item->log_path) + 128);
    snprintf(out, strlen(item->log_path) + 128,
             "started %s\npid: %d\nlog: %s", item->id,
             item->process.pid, item->log_path);
    return out;
}

static void append_status(strbuf *out, const job *item) {
    char line[256];
    if (item->running)
        snprintf(line, sizeof line, "%s running pid=%d", item->id, item->process.pid);
    else if (item->term_signal)
        snprintf(line, sizeof line, "%s stopped signal=%d", item->id, item->term_signal);
    else
        snprintf(line, sizeof line, "%s exited status=%lld", item->id,
                 (long long)item->exit_status);
    sb_append_str(out, line);
}

static char *list_jobs(void) {
    strbuf out;
    sb_init(&out);
    for (job *item = jobs; item; item = item->next) {
        append_status(&out, item);
        sb_append_str(&out, "\n");
    }
    return out.data ? out.data : strdup("no managed processes");
}

static char *job_status(job *item) {
    strbuf out;
    sb_init(&out);
    append_status(&out, item);
    sb_append_str(&out, "\ncommand: ");
    sb_append_str(&out, item->cmd);
    sb_append_str(&out, "\nlog: ");
    sb_append_str(&out, item->log_path);
    return out.data;
}

static char *job_logs(job *item, int offset) {
    if (offset < 0) offset = 0;
    FILE *file = fopen(item->log_path, "r");
    if (!file) return strdup("error: cannot read process log");
    if (fseek(file, offset, SEEK_SET) != 0) {
        fclose(file);
        return strdup("error: invalid log offset");
    }
    char data[20481];
    size_t n = fread(data, 1, sizeof data - 1, file);
    data[n] = '\0';
    fclose(file);
    char head[64];
    snprintf(head, sizeof head, "offset: %d\nnext_offset: %d\n", offset, offset + (int)n);
    strbuf out;
    sb_init(&out);
    sb_append_str(&out, head);
    sb_append(&out, data, n);
    return out.data;
}

static char *stop_job(job *item) {
    if (!item->running) return strdup("already stopped");
    kill(-item->process.pid, SIGTERM);
    if (!item->timer_open && uv_timer_init(loop_get(), &item->stop_timer) == 0) {
        item->timer_open = 1;
        item->stop_timer.data = item;
        uv_timer_start(&item->stop_timer, force_stop, 2000, 0);
    }
    return strdup("stopping");
}

char *process_tool(cJSON *args) {
    const char *action = arg_str(args, "action");
    if (!action) return strdup("error: missing action");
    if (strcmp(action, "list") == 0) return list_jobs();
    job *item = find_job(arg_str(args, "id"));
    if (!item) return strdup("error: process not found");
    if (strcmp(action, "status") == 0) return job_status(item);
    if (strcmp(action, "logs") == 0) return job_logs(item, arg_int(args, "offset", 0));
    if (strcmp(action, "stop") == 0) return stop_job(item);
    return strdup("error: unknown action");
}

typedef struct {
    int fired;
    int closed;
} cleanup_state;

static void cleanup_wake(uv_timer_t *timer) {
    cleanup_state *state = timer->data;
    state->fired = 1;
}

static void cleanup_timer_closed(uv_handle_t *handle) {
    cleanup_state *state = handle->data;
    state->closed = 1;
}

void process_cleanup(void) {
    int running = 0;
    for (job *item = jobs; item; item = item->next) {
        if (!item->running) continue;
        kill(-item->process.pid, SIGTERM);
        running++;
    }
    uv_timer_t wake;
    cleanup_state wake_state = {0};
    uv_timer_init(loop_get(), &wake);
    wake.data = &wake_state;
    uv_timer_start(&wake, cleanup_wake, 1000, 0);
    while (running && !wake_state.fired) {
        loop_run_once();
        running = 0;
        for (job *item = jobs; item; item = item->next) running += item->running;
    }
    uv_timer_stop(&wake);
    uv_close((uv_handle_t *)&wake, cleanup_timer_closed);
    for (job *item = jobs; item; item = item->next)
        if (item->running) kill(-item->process.pid, SIGKILL);
    while (running) {
        loop_run_once();
        running = 0;
        for (job *item = jobs; item; item = item->next) running += item->running;
    }
    while (!wake_state.closed) uv_run(loop_get(), UV_RUN_NOWAIT);
    uv_run(loop_get(), UV_RUN_NOWAIT);
    while (jobs) {
        job *next = jobs->next;
        free(jobs->cmd);
        free(jobs->log_path);
        free(jobs);
        jobs = next;
    }
}
