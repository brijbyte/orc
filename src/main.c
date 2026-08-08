#include "agent.h"
#include "ansi.h"
#include "commands.h"
#include "input.h"
#include "orc.h"
#include "provider.h"
#include "session.h"
#include "util.h"

#include <curl/curl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/utsname.h>
#include <unistd.h>

static void on_sigint(int sig) {
    (void)sig;
    g_interrupt = 1;
}

/* Append a user AGENTS.md file (global or project) to the instructions. */
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

static char *build_instructions(void) {
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

static void usage(void) {
    puts("usage: orc [options] [-p \"prompt\"]\n"
         "  -p <prompt>       one-shot: run a single task and exit\n"
         "  -m <model>        model (default: provider's; env ORC_MODEL)\n"
         "  -e <effort>       reasoning effort: low|medium|high (default " ORC_DEFAULT_EFFORT ")\n"
         "  --provider <name> provider (default codex; env ORC_PROVIDER)\n"
         "  --resume [id|path] resume most recent (or given) session\n"
         "  --list            list sessions, newest first\n"
         "  --login           sign in to the provider (browser OAuth)\n"
         "  --auth            show provider auth status\n"
         "  --version         print version\n"
         "  -h                help");
}

int main(int argc, char **argv) {
    const char *prompt = NULL, *resume_ref = NULL;
    int do_resume = 0, do_auth = 0, do_list = 0, do_login = 0;

    orc_cfg cfg = {0};
    cfg.provider = getenv("ORC_PROVIDER");
    cfg.model = getenv("ORC_MODEL");
    cfg.effort = ORC_DEFAULT_EFFORT;
    uuid4(cfg.session_id);

    for (int i = 1; i < argc; i++) {
        if (strcmp(argv[i], "-p") == 0 && i + 1 < argc) prompt = argv[++i];
        else if (strcmp(argv[i], "-m") == 0 && i + 1 < argc) cfg.model = argv[++i];
        else if (strcmp(argv[i], "-e") == 0 && i + 1 < argc) cfg.effort = argv[++i];
        else if (strcmp(argv[i], "--provider") == 0 && i + 1 < argc) cfg.provider = argv[++i];
        else if (strcmp(argv[i], "--resume") == 0) {
            do_resume = 1;
            if (i + 1 < argc && argv[i + 1][0] != '-') resume_ref = argv[++i];
        }
        else if (strcmp(argv[i], "--list") == 0) do_list = 1;
        else if (strcmp(argv[i], "--auth") == 0) do_auth = 1;
        else if (strcmp(argv[i], "--login") == 0) do_login = 1;
        else if (strcmp(argv[i], "--version") == 0) {
            puts("orc " ORC_VERSION);
            return 0;
        }
        else if (strcmp(argv[i], "-h") == 0 || strcmp(argv[i], "--help") == 0) {
            usage();
            return 0;
        }
        else {
            fprintf(stderr, "orc: unknown option %s\n", argv[i]);
            usage();
            return 2;
        }
    }

    if (do_list) return session_list();

    const provider *prov = provider_get(cfg.provider);
    if (!prov) {
        fprintf(stderr, "orc: unknown provider '%s'; available:\n", cfg.provider);
        provider_list();
        return 2;
    }
    if (!cfg.model) cfg.model = prov->default_model;
    commands_init(prov, &cfg);

    if (curl_global_init(CURL_GLOBAL_DEFAULT) != CURLE_OK) {
        fprintf(stderr, "orc: cannot initialize HTTP client\n");
        return 1;
    }

    int rc = 1;
    int ready = 0;
    orc_session sess = {0};
    agent ag = {0};
    cJSON *resumed = NULL;

    if (do_login) {
        if (!prov->login) {
            fprintf(stderr, "orc: provider '%s' has no login\n", prov->name);
            rc = 2;
        } else {
            rc = prov->login() == 0 ? 0 : 1;
        }
        goto cleanup;
    }
    if (do_auth) {
        rc = prov->auth_status() == 0 ? 0 : 1;
        goto cleanup;
    }

    struct sigaction sa = {0};
    sa.sa_handler = on_sigint;
    sigaction(SIGINT, &sa, NULL);

    cfg.instructions = build_instructions();
    if (!cfg.instructions) {
        fprintf(stderr, "orc: out of memory\n");
        goto cleanup;
    }

    if (do_resume) {
        resumed = cJSON_CreateArray();
        if (!resumed || session_resume(&sess, resume_ref, resumed, &cfg) != 0)
            goto cleanup;
    } else if (session_new(&sess, &cfg) != 0) {
        fprintf(stderr, "orc: cannot create session file\n");
        goto cleanup;
    }

    int init_rc = agent_init(&ag, &cfg, prov, &sess, resumed);
    resumed = NULL; /* agent_init consumes it on both success and failure */
    if (init_rc != 0) goto cleanup;
    ready = 1;
    rc = 0;
    if (prompt) {
        int tr = agent_turn(&ag, prompt);
        rc = tr < 0 ? 1 : (tr == 1 ? 130 : 0);
    } else {
        if (isatty(1))
            printf(BOLD_CYAN("orc") DIM(" %s") " — " BOLD("%s")
                   DIM(" (%s effort) · %ssession %.8s · Ctrl-D or 'exit' to quit")
                   "\n",
                   ORC_VERSION, cfg.model, cfg.effort,
                   do_resume ? "resumed " : "", cfg.session_id);
        else
            printf("orc %s — %s (%s effort), %ssession %.8s. Ctrl-D or 'exit' to quit.\n",
                   ORC_VERSION, cfg.model, cfg.effort,
                   do_resume ? "resumed " : "", cfg.session_id);
        if (do_resume) agent_replay(&ag);
        input_init();
        if (input_active()) {
            /* Event-loop REPL: lines typed while a turn runs are queued. */
            for (;;) {
                g_interrupt = 0;
                input_wait();
                if (g_interrupt) { g_interrupt = 0; continue; }
                int queued = 0;
                char *line = input_take(&queued);
                if (!line) {
                    if (input_eof()) break;
                    continue;
                }
                if (strcmp(line, "exit") == 0 || strcmp(line, "quit") == 0) {
                    free(line);
                    break;
                }
                if (queued) { /* replay so it's clear what runs now */
                    input_erase();
                    printf(BOLD_CYAN(">") " %s\n", line);
                    input_redraw();
                }
                if (line[0] == '/') {
                    input_erase();
                    int cd = command_dispatch(&ag, line);
                    input_redraw();
                    if (cd) {
                        free(line);
                        if (cd == 2) break;
                        continue;
                    }
                }
                input_set_idle(0);
                agent_turn(&ag, line);
                input_set_idle(1);
                free(line);
            }
        } else { /* stdin is a pipe: plain blocking reads */
            char line[65536];
            int tty = isatty(1);
            for (;;) {
                g_interrupt = 0;
                fputs(tty ? BOLD_CYAN(">") " " : "> ", stdout);
                fflush(stdout);
                if (!fgets(line, sizeof line, stdin)) {
                    if (g_interrupt) {          /* Ctrl-C at prompt */
                        clearerr(stdin);
                        fputs("\n(^D or 'exit' to quit)\n", stdout);
                        continue;
                    }
                    break;                       /* EOF */
                }
                size_t n = strlen(line);
                while (n > 0 && (line[n - 1] == '\n' || line[n - 1] == '\r')) line[--n] = '\0';
                if (n == 0) continue;
                if (strcmp(line, "exit") == 0 || strcmp(line, "quit") == 0) break;
                if (line[0] == '/') {
                    int cd = command_dispatch(&ag, line);
                    if (cd == 2) break;
                    if (cd) continue;
                }
                g_interrupt = 0;
                agent_turn(&ag, line);
            }
        }
        puts("");
    }

cleanup:
    cJSON_Delete(resumed);
    agent_free(&ag);
    session_close(&sess);
    if (ready && sess.items > 0) {
        fflush(stdout);
        fprintf(stderr,
                isatty(2) ? DIM("orc: resume with `orc --resume %.8s`") "\n"
                          : "orc: resume with `orc --resume %.8s`\n",
                cfg.session_id);
    } else if (ready && !do_resume && sess.path[0]) {
        unlink(sess.path); /* nothing was said; drop the empty session file */
    }
    free(cfg.instructions);
    curl_global_cleanup();
    return rc;
}
