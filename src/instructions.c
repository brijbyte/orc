#include "instructions.h"

#include "util.h"

#include <stdio.h>
#include <stdlib.h>
#include <sys/utsname.h>
#include <unistd.h>

#define AGENTS_MAX 32768

static void append_agents(strbuf *sb, const char *path) {
    size_t len;
    char *text = read_file(path, &len);
    if (!text) return;
    sb_append_str(sb, "\n\n# User instructions (");
    sb_append_str(sb, path);
    sb_append_str(sb, ")\n");
    if (len > AGENTS_MAX) {
        sb_append(sb, text, AGENTS_MAX);
        sb_append_str(sb, "\n[truncated]");
    } else {
        sb_append(sb, text, len);
    }
    free(text);
}

char *instructions_build(void) {
    char cwd[2048];
    if (!getcwd(cwd, sizeof cwd)) cwd[0] = '\0';
    struct utsname u;
    uname(&u);
    char head[4096];
    snprintf(head, sizeof head,
        "You are orc, a terse coding agent running in a terminal at %s on %s %s. "
        "Use the tools to complete the user's task. Prefer acting over asking. "
        "Read files before editing them. "
        "Keep answers short; no preamble, no summaries of what you did unless asked.",
        cwd, u.sysname, u.machine);

    strbuf sb;
    sb_init(&sb);
    sb_append_str(&sb, head);
    char *global = expand_home("~/.agents/AGENTS.md");
    append_agents(&sb, global);
    free(global);
    append_agents(&sb, "AGENTS.md");
    return sb.data;
}
