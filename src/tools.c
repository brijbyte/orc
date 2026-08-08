#include "tools.h"
#include "loop.h"
#include "orc.h"
#include "process.h"
#include "skills.h"
#include "util.h"

#include <errno.h>
#include <fcntl.h>
#include <libgen.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

#define OUT_CAP 20480
#define OUT_HEAD 12288
#define OUT_TAIL 8192
#define BASH_TIMEOUT_S 60
#define READ_LIMIT 1000
#define LINE_MAX_CHARS 500

const char *tools_schema_json(void) {
    return
    "[{\"type\":\"function\",\"name\":\"bash\","
      "\"description\":\"Run a shell command. Set background true for a managed long-running process.\","
      "\"parameters\":{\"type\":\"object\",\"properties\":{"
        "\"cmd\":{\"type\":\"string\"},"
        "\"timeout_s\":{\"type\":\"integer\"},"
        "\"background\":{\"type\":\"boolean\"}},\"required\":[\"cmd\"]}},"
     "{\"type\":\"function\",\"name\":\"process\","
      "\"description\":\"Inspect or stop managed background processes.\","
      "\"parameters\":{\"type\":\"object\",\"properties\":{"
        "\"action\":{\"type\":\"string\",\"enum\":[\"list\",\"status\",\"logs\",\"stop\"]},"
        "\"id\":{\"type\":\"string\"},"
        "\"offset\":{\"type\":\"integer\"}},\"required\":[\"action\"]}},"
     "{\"type\":\"function\",\"name\":\"read\","
      "\"description\":\"Read file with line numbers.\","
      "\"parameters\":{\"type\":\"object\",\"properties\":{"
        "\"path\":{\"type\":\"string\"},"
        "\"offset\":{\"type\":\"integer\"},"
        "\"limit\":{\"type\":\"integer\"}},\"required\":[\"path\"]}},"
     "{\"type\":\"function\",\"name\":\"write\","
      "\"description\":\"Write file, creating dirs.\","
      "\"parameters\":{\"type\":\"object\",\"properties\":{"
        "\"path\":{\"type\":\"string\"},"
        "\"content\":{\"type\":\"string\"}},\"required\":[\"path\",\"content\"]}},"
     "{\"type\":\"function\",\"name\":\"edit\","
      "\"description\":\"Replace old with new in file. old must match exactly once.\","
      "\"parameters\":{\"type\":\"object\",\"properties\":{"
        "\"path\":{\"type\":\"string\"},"
        "\"old\":{\"type\":\"string\"},"
        "\"new\":{\"type\":\"string\"}},\"required\":[\"path\",\"old\",\"new\"]}},"
     "{\"type\":\"function\",\"name\":\"skill\","
      "\"description\":\"Find installed skills. Use when the user asks to find or use a skill.\","
      "\"parameters\":{\"type\":\"object\",\"properties\":{"
        "\"query\":{\"type\":\"string\","
          "\"description\":\"Search by name or description. Omit to list all skills.\"}}}}]";
}

static const char *astr(cJSON *args, const char *key) {
    cJSON *v = cJSON_GetObjectItem(args, key);
    return cJSON_IsString(v) ? v->valuestring : NULL;
}

static int aint(cJSON *args, const char *key, int dflt) {
    cJSON *v = cJSON_GetObjectItem(args, key);
    return cJSON_IsNumber(v) ? v->valueint : dflt;
}

static char *errf(const char *fmt, const char *arg) {
    char *out = malloc(4096);
    snprintf(out, 4096, fmt, arg);
    return out;
}

/* Clamp to OUT_CAP: head + truncation marker + tail. Takes ownership of s. */
static char *clamp_output(char *s) {
    size_t len = strlen(s);
    if (len <= OUT_CAP) return s;
    char *out = malloc(OUT_HEAD + OUT_TAIL + 64);
    size_t o = 0;
    memcpy(out, s, OUT_HEAD);
    o = OUT_HEAD;
    o += (size_t)snprintf(out + o, 64, "\n...[truncated %zu bytes]...\n",
                          len - OUT_HEAD - OUT_TAIL);
    memcpy(out + o, s + len - OUT_TAIL, OUT_TAIL);
    o += OUT_TAIL;
    out[o] = '\0';
    free(s);
    return out;
}

typedef struct {
    uv_process_t process;
    uv_pipe_t out;
    uv_pipe_t err;
    uv_timer_t timer;
    strbuf output;
    uint64_t started_at;
    int timeout_s;
    int open_pipes;
    int exited;
    int killed;
    int timed_out;
    int interrupted;
    int64_t exit_status;
    int term_signal;
} bash_run;

static void bash_closed(uv_handle_t *handle) {
    (void)handle;
}

static void bash_alloc(uv_handle_t *handle, size_t size, uv_buf_t *buf) {
    (void)handle;
    (void)size;
    buf->base = malloc(8192);
    buf->len = buf->base ? 8192 : 0;
}

static void bash_read(uv_stream_t *stream, ssize_t nread, const uv_buf_t *buf) {
    bash_run *run = stream->data;
    if (nread > 0) sb_append(&run->output, buf->base, (size_t)nread);
    free(buf->base);
    if (nread >= 0) return;
    uv_read_stop(stream);
    uv_close((uv_handle_t *)stream, bash_closed);
    run->open_pipes--;
}

static void bash_exit(uv_process_t *process, int64_t status, int signal) {
    bash_run *run = process->data;
    run->exit_status = status;
    run->term_signal = signal;
    run->exited = 1;
    uv_close((uv_handle_t *)process, bash_closed);
}

static void bash_tick(uv_timer_t *timer) {
    bash_run *run = timer->data;
    if (run->killed) return;
    uint64_t elapsed = uv_now(loop_get()) - run->started_at;
    if (g_interrupt) run->interrupted = 1;
    else if (elapsed >= (uint64_t)run->timeout_s * 1000) run->timed_out = 1;
    else return;
    run->killed = 1;
    kill(-run->process.pid, SIGKILL);
}

static char *tool_bash(cJSON *args) {
    const char *cmd = astr(args, "cmd");
    if (!cmd) return strdup("error: missing cmd");
    if (cJSON_IsTrue(cJSON_GetObjectItem(args, "background")))
        return process_start(cmd);

    bash_run run = {.timeout_s = aint(args, "timeout_s", BASH_TIMEOUT_S),
                    .open_pipes = 2};
    sb_init(&run.output);
    uv_pipe_init(loop_get(), &run.out, 0);
    uv_pipe_init(loop_get(), &run.err, 0);
    run.out.data = run.err.data = &run;
    run.process.data = &run;

    uv_stdio_container_t stdio[3] = {0};
    stdio[0].flags = UV_IGNORE;
    stdio[1].flags = UV_CREATE_PIPE | UV_WRITABLE_PIPE;
    stdio[1].data.stream = (uv_stream_t *)&run.out;
    stdio[2].flags = UV_CREATE_PIPE | UV_WRITABLE_PIPE;
    stdio[2].data.stream = (uv_stream_t *)&run.err;
    char *argv[] = {"sh", "-c", (char *)cmd, NULL};
    uv_process_options_t options = {0};
    options.file = "/bin/sh";
    options.args = argv;
    options.stdio = stdio;
    options.stdio_count = 3;
    options.exit_cb = bash_exit;
    options.flags = UV_PROCESS_DETACHED;

    int rc = uv_spawn(loop_get(), &run.process, &options);
    if (rc != 0) {
        uv_close((uv_handle_t *)&run.out, bash_closed);
        uv_close((uv_handle_t *)&run.err, bash_closed);
        uv_run(loop_get(), UV_RUN_NOWAIT);
        char buf[256];
        snprintf(buf, sizeof buf, "error: spawn failed: %s", uv_strerror(rc));
        return strdup(buf);
    }

    uv_read_start((uv_stream_t *)&run.out, bash_alloc, bash_read);
    uv_read_start((uv_stream_t *)&run.err, bash_alloc, bash_read);
    uv_timer_init(loop_get(), &run.timer);
    run.timer.data = &run;
    uv_update_time(loop_get());
    run.started_at = uv_now(loop_get());
    uv_timer_start(&run.timer, bash_tick, 100, 100);
    while (!run.exited || run.open_pipes > 0) loop_run_once();
    uv_timer_stop(&run.timer);
    uv_close((uv_handle_t *)&run.timer, bash_closed);
    uv_run(loop_get(), UV_RUN_NOWAIT);

    char tail[64] = "";
    if (run.timed_out)
        snprintf(tail, sizeof tail, "\n[timed out after %ds]", run.timeout_s);
    else if (run.interrupted)
        snprintf(tail, sizeof tail, "\n[interrupted]");
    else if (run.exit_status != 0)
        snprintf(tail, sizeof tail, "\n[exit %lld]", (long long)run.exit_status);
    else if (run.term_signal)
        snprintf(tail, sizeof tail, "\n[signal %d]", run.term_signal);
    if (tail[0]) sb_append_str(&run.output, tail);

    char *result = run.output.data ? run.output.data : strdup("");
    return clamp_output(result);
}

static char *tool_read(cJSON *args) {
    const char *path = astr(args, "path");
    if (!path) return strdup("error: missing path");
    int offset = aint(args, "offset", 0);
    int limit = aint(args, "limit", READ_LIMIT);

    FILE *f = fopen(path, "r");
    if (!f) return errf("error: cannot open %s", path);

    strbuf out;
    sb_init(&out);
    char line[16384];
    int lineno = 0, emitted = 0, skipped_tail = 0;
    while (fgets(line, sizeof line, f)) {
        lineno++;
        if (lineno <= offset) continue;
        if (emitted >= limit) { skipped_tail = 1; break; }
        size_t n = strlen(line);
        int had_nl = n > 0 && line[n - 1] == '\n';
        if (had_nl) line[--n] = '\0';
        char prefix[32];
        snprintf(prefix, sizeof prefix, "%6d\t", lineno);
        sb_append_str(&out, prefix);
        if (n > LINE_MAX_CHARS) {
            sb_append(&out, line, LINE_MAX_CHARS);
            sb_append_str(&out, "...");
        } else {
            sb_append(&out, line, n);
        }
        sb_append_str(&out, "\n");
        emitted++;
    }
    fclose(f);
    if (skipped_tail) {
        char note[64];
        snprintf(note, sizeof note, "[more lines after %d]\n", lineno - 1);
        sb_append_str(&out, note);
    }
    if (out.len == 0) sb_append_str(&out, "(empty)");
    return clamp_output(out.data);
}

static char *tool_write(cJSON *args) {
    const char *path = astr(args, "path");
    const char *content = astr(args, "content");
    if (!path || !content) return strdup("error: missing path/content");

    char dirbuf[4096];
    snprintf(dirbuf, sizeof dirbuf, "%s", path);
    char *dir = dirname(dirbuf);
    if (dir && strcmp(dir, ".") != 0) mkdirs(dir);

    if (write_file_atomic(path, content, strlen(content)) != 0)
        return errf("error: cannot write %s", path);
    return strdup("ok");
}

static char *tool_edit(cJSON *args) {
    const char *path = astr(args, "path");
    const char *old = astr(args, "old");
    const char *new_ = astr(args, "new");
    if (!path || !old || !new_) return strdup("error: missing path/old/new");
    if (!*old) return strdup("error: old is empty");

    size_t flen;
    char *text = read_file(path, &flen);
    if (!text) return errf("error: cannot open %s", path);

    size_t oldlen = strlen(old);
    int count = 0;
    for (char *p = text; (p = strstr(p, old)); p += oldlen) count++;

    if (count == 0) { free(text); return strdup("error: old string not found"); }
    if (count > 1) {
        free(text);
        char *out = malloc(80);
        snprintf(out, 80, "error: old matches %d times; add context", count);
        return out;
    }

    char *pos = strstr(text, old);
    strbuf out;
    sb_init(&out);
    sb_append(&out, text, (size_t)(pos - text));
    sb_append_str(&out, new_);
    sb_append_str(&out, pos + oldlen);
    free(text);

    int rc = write_file_atomic(path, out.data, out.len);
    sb_free(&out);
    if (rc != 0) return errf("error: cannot write %s", path);
    return strdup("ok");
}

char *tool_run(const char *name, cJSON *args) {
    if (!args) return strdup("error: bad arguments JSON");
    if (strcmp(name, "bash") == 0) return tool_bash(args);
    if (strcmp(name, "process") == 0) return clamp_output(process_tool(args));
    if (strcmp(name, "read") == 0) return tool_read(args);
    if (strcmp(name, "write") == 0) return tool_write(args);
    if (strcmp(name, "edit") == 0) return tool_edit(args);
    if (strcmp(name, "skill") == 0) return clamp_output(skills_query(astr(args, "query")));
    return errf("error: unknown tool %s", name);
}
