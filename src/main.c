#include "agent.h"
#include "ansi.h"
#include "commands.h"
#include "input.h"
#include "loop.h"
#include "orc.h"
#include "process.h"
#include "provider.h"
#include "session.h"
#include "ui.h"
#include "util.h"

#include <curl/curl.h>
#include <getopt.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

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

    enum { OPT_PROVIDER = 256, OPT_RESUME, OPT_LIST, OPT_LOGIN, OPT_AUTH, OPT_VERSION };
    static const struct option options[] = {
        {"provider", required_argument, NULL, OPT_PROVIDER},
        {"resume", optional_argument, NULL, OPT_RESUME},
        {"list", no_argument, NULL, OPT_LIST},
        {"login", no_argument, NULL, OPT_LOGIN},
        {"auth", no_argument, NULL, OPT_AUTH},
        {"version", no_argument, NULL, OPT_VERSION},
        {"help", no_argument, NULL, 'h'},
        {NULL, 0, NULL, 0},
    };

    int effort_explicit = 0;
    opterr = 0;
    for (;;) {
        int opt = getopt_long(argc, argv, ":p:m:e:h", options, NULL);
        if (opt == -1) break;
        switch (opt) {
        case 'p': prompt = optarg; break;
        case 'm': cfg.model = optarg; break;
        case 'e':
            cfg.effort = optarg;
            effort_explicit = 1;
            break;
        case OPT_PROVIDER: cfg.provider = optarg; break;
        case OPT_RESUME:
            do_resume = 1;
            resume_ref = optarg;
            if (!resume_ref && optind < argc && argv[optind][0] != '-')
                resume_ref = argv[optind++];
            break;
        case OPT_LIST: do_list = 1; break;
        case OPT_LOGIN: do_login = 1; break;
        case OPT_AUTH: do_auth = 1; break;
        case OPT_VERSION:
            puts("orc " ORC_VERSION);
            return 0;
        case 'h':
            usage();
            return 0;
        case ':':
            fprintf(stderr, "❌ orc: option requires an argument: %s\n",
                    argv[optind - 1]);
            usage();
            return 2;
        default:
            fprintf(stderr, "❌ orc: unknown option %s\n", argv[optind - 1]);
            usage();
            return 2;
        }
    }
    if (optind < argc) {
        fprintf(stderr, "❌ orc: unexpected argument %s\n", argv[optind]);
        usage();
        return 2;
    }

    if (do_list) {
        orc_session_info *sessions;
        size_t count;
        if (session_list(&sessions, &count) != 0) {
            fprintf(stderr, "❌ orc: %s\n", session_error());
            return 1;
        }
        ui_session_list(sessions, count);
        free(sessions);
        return 0;
    }

    int model_explicit = cfg.model != NULL; /* -m flag or ORC_MODEL env */
    const provider *prov = provider_get(cfg.provider);
    if (!prov) {
        fprintf(stderr, "❌ orc: unknown provider '%s'; available:\n", cfg.provider);
        provider_list();
        return 2;
    }
    if (!cfg.model) cfg.model = prov->default_model;
    commands_init(prov, &cfg);

    if (curl_global_init(CURL_GLOBAL_DEFAULT) != CURLE_OK || loop_init() != 0) {
        fprintf(stderr, "❌ orc: cannot initialize event loop\n");
        curl_global_cleanup();
        return 1;
    }

    int rc = 1;
    int ready = 0;
    orc_session sess = {0};
    agent ag = {0};
    cJSON *resumed = NULL;
    ui *terminal_ui = NULL;

    if (do_login) {
        if (!prov->login) {
            fprintf(stderr, "❌ orc: provider '%s' has no login\n", prov->name);
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

    terminal_ui = ui_create();
    if (!terminal_ui) {
        fprintf(stderr, "❌ orc: out of memory\n");
        goto cleanup;
    }

    if (do_resume) {
        resumed = cJSON_CreateArray();
        if (!resumed) {
            fprintf(stderr, "❌ orc: out of memory\n");
            goto cleanup;
        }
        if (session_resume(&sess, resume_ref, resumed, &cfg) != 0) {
            fprintf(stderr, "❌ orc: %s\n", session_error());
            goto cleanup;
        }
        ui_session_resumed(cfg.session_id, sess.items, sess.path);
        /* Restore the session's model/effort; explicit flags win. */
        if (!model_explicit && sess.model[0]) cfg.model = sess.model;
        if (!effort_explicit && sess.effort[0]) cfg.effort = sess.effort;
    } else if (session_new(&sess, &cfg) != 0) {
        fprintf(stderr, "❌ orc: cannot create session file\n");
        goto cleanup;
    }

    int init_rc = agent_init(&ag, &cfg, prov, &sess, resumed,
                             ui_agent_io(), terminal_ui);
    resumed = NULL; /* agent_init consumes it on both success and failure */
    if (init_rc != 0) {
        fprintf(stderr, "❌ orc: cannot initialize agent\n");
        goto cleanup;
    }
    ready = 1;
    rc = 0;
    if (prompt) {
        int tr = agent_turn(&ag, prompt);
        rc = tr < 0 ? 1 : (tr == 1 ? 130 : 0);
    } else {
        if (isatty(1))
            printf(BOLD_CYAN("🧌 orc") DIM(" %s") " — " BOLD("%s")
                   DIM(" (%s effort) · %ssession %.8s · Ctrl-D or 'exit' to quit")
                   "\n",
                   ORC_VERSION, cfg.model, cfg.effort,
                   do_resume ? "resumed " : "", cfg.session_id);
        else
            printf("🧌 orc %s — %s (%s effort), %ssession %.8s. Ctrl-D or 'exit' to quit.\n",
                   ORC_VERSION, cfg.model, cfg.effort,
                   do_resume ? "resumed " : "", cfg.session_id);
        if (do_resume) agent_replay(&ag);
        if (sess.ctx > 0) commands_ctx_used(sess.ctx);
        commands_status_update();
        input_init();
        loop_input_start();
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
                    printf(BOLD_CYAN(">") " " ANSI_CYAN "%s" ANSI_RESET "\n",
                           line);
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
    ui_free(terminal_ui);
    session_close(&sess);
    if (ready && sess.items > 0) {
        fflush(stdout);
        fprintf(stderr,
                isatty(2) ? DIM("💡 orc: resume with `orc --resume %.8s`") "\n"
                          : "💡 orc: resume with `orc --resume %.8s`\n",
                cfg.session_id);
    } else if (ready && !do_resume && sess.path[0]) {
        unlink(sess.path); /* nothing was said; drop the empty session file */
    }
    free(cfg.instructions);
    process_cleanup();
    loop_free();
    curl_global_cleanup();
    return rc;
}
