#include "agent.h"
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

static char *build_instructions(void) {
    char cwd[2048];
    if (!getcwd(cwd, sizeof cwd)) cwd[0] = '\0';
    struct utsname u;
    uname(&u);
    char *out = malloc(4096);
    snprintf(out, 4096,
        "You are orc, a terse coding agent running in a terminal at %s on %s %s. "
        "Use the tools to complete the user's task. Prefer acting over asking. "
        "Read files before editing them. "
        "Keep answers short; no preamble, no summaries of what you did unless asked.",
        cwd, u.sysname, u.machine);
    return out;
}

static void usage(void) {
    puts("usage: orc [options] [-p \"prompt\"]\n"
         "  -p <prompt>       one-shot: run a single task and exit\n"
         "  -m <model>        model (default: provider's; env ORC_MODEL)\n"
         "  -e <effort>       reasoning effort: low|medium|high (default " ORC_DEFAULT_EFFORT ")\n"
         "  --provider <name> provider (default codex; env ORC_PROVIDER)\n"
         "  --resume [path]   resume most recent (or given) session\n"
         "  --auth            show provider auth status\n"
         "  -h                help");
}

int main(int argc, char **argv) {
    const char *prompt = NULL, *resume_path = NULL;
    int do_resume = 0, do_auth = 0;

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
            if (i + 1 < argc && argv[i + 1][0] != '-') resume_path = argv[++i];
        }
        else if (strcmp(argv[i], "--auth") == 0) do_auth = 1;
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

    const provider *prov = provider_get(cfg.provider);
    if (!prov) {
        fprintf(stderr, "orc: unknown provider '%s'; available:\n", cfg.provider);
        provider_list();
        return 2;
    }
    if (!cfg.model) cfg.model = prov->default_model;

    curl_global_init(CURL_GLOBAL_DEFAULT);
    if (do_auth) return prov->auth_status() == 0 ? 0 : 1;

    struct sigaction sa = {0};
    sa.sa_handler = on_sigint;
    sigaction(SIGINT, &sa, NULL);

    cfg.instructions = build_instructions();

    orc_session sess = {0};
    cJSON *resumed = NULL;
    if (do_resume) {
        resumed = cJSON_CreateArray();
        if (session_resume(&sess, resume_path, resumed) != 0) {
            cJSON_Delete(resumed);
            return 1;
        }
    } else if (session_new(&sess, &cfg) != 0) {
        fprintf(stderr, "orc: cannot create session file\n");
        return 1;
    }

    agent ag;
    if (agent_init(&ag, &cfg, prov, &sess, resumed) != 0) return 1;

    int rc = 0;
    if (prompt) {
        int tr = agent_turn(&ag, prompt);
        rc = tr < 0 ? 1 : (tr == 1 ? 130 : 0);
    } else {
        printf("orc %s — %s (%s effort). Ctrl-D or 'exit' to quit.\n",
               ORC_VERSION, cfg.model, cfg.effort);
        char line[65536];
        for (;;) {
            g_interrupt = 0;
            fputs("\x1b[1m> \x1b[0m", stdout);
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
            g_interrupt = 0;
            agent_turn(&ag, line);
        }
        puts("");
    }

    agent_free(&ag);
    session_close(&sess);
    free(cfg.instructions);
    curl_global_cleanup();
    return rc;
}
