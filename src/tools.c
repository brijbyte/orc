#include "tools.h"
#include "orc.h"
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
      "\"description\":\"Run shell command. Returns stdout+stderr.\","
      "\"parameters\":{\"type\":\"object\",\"properties\":{"
        "\"cmd\":{\"type\":\"string\"},"
        "\"timeout_s\":{\"type\":\"integer\"}},\"required\":[\"cmd\"]}},"
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
        "\"new\":{\"type\":\"string\"}},\"required\":[\"path\",\"old\",\"new\"]}}]";
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

static char *tool_bash(cJSON *args) {
    const char *cmd = astr(args, "cmd");
    if (!cmd) return strdup("error: missing cmd");
    int timeout_s = aint(args, "timeout_s", BASH_TIMEOUT_S);

    int pfd[2];
    if (pipe(pfd) != 0) return strdup("error: pipe failed");

    pid_t pid = fork();
    if (pid < 0) {
        close(pfd[0]); close(pfd[1]);
        return strdup("error: fork failed");
    }
    if (pid == 0) {
        setpgid(0, 0);
        int devnull = open("/dev/null", O_RDONLY);
        if (devnull >= 0) {
            dup2(devnull, STDIN_FILENO);
            if (devnull != STDIN_FILENO) close(devnull);
        }
        dup2(pfd[1], 1);
        dup2(pfd[1], 2);
        close(pfd[0]); close(pfd[1]);
        execl("/bin/sh", "sh", "-c", cmd, (char *)NULL);
        _exit(127);
    }
    close(pfd[1]);
    fcntl(pfd[0], F_SETFL, O_NONBLOCK);

    strbuf out;
    sb_init(&out);
    time_t deadline = time(NULL) + timeout_s;
    int timed_out = 0, interrupted = 0;

    for (;;) {
        if (g_interrupt) { interrupted = 1; break; }
        if (time(NULL) > deadline) { timed_out = 1; break; }
        struct pollfd p = {.fd = pfd[0], .events = POLLIN};
        int pr = poll(&p, 1, 200);
        if (pr > 0) {
            char buf[8192];
            ssize_t n = read(pfd[0], buf, sizeof buf);
            if (n > 0) sb_append(&out, buf, (size_t)n);
            else if (n == 0) break;                 /* EOF */
            else if (errno != EAGAIN && errno != EINTR) break;
        }
    }
    if (timed_out || interrupted) kill(-pid, SIGKILL);
    /* Drain whatever remains after exit/kill. */
    for (;;) {
        char buf[8192];
        ssize_t n = read(pfd[0], buf, sizeof buf);
        if (n > 0) sb_append(&out, buf, (size_t)n);
        else break;
    }
    close(pfd[0]);
    int wstatus = 0;
    waitpid(pid, &wstatus, 0);

    char tail[64] = "";
    if (timed_out)
        snprintf(tail, sizeof tail, "\n[timed out after %ds]", timeout_s);
    else if (interrupted)
        snprintf(tail, sizeof tail, "\n[interrupted]");
    else if (WIFEXITED(wstatus) && WEXITSTATUS(wstatus) != 0)
        snprintf(tail, sizeof tail, "\n[exit %d]", WEXITSTATUS(wstatus));
    if (tail[0]) sb_append_str(&out, tail);

    char *result = out.data ? out.data : strdup("");
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
    if (strcmp(name, "read") == 0) return tool_read(args);
    if (strcmp(name, "write") == 0) return tool_write(args);
    if (strcmp(name, "edit") == 0) return tool_edit(args);
    return errf("error: unknown tool %s", name);
}
