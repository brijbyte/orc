#include "commands.h"
#include "ansi.h"
#include "ui.h"
#include "session.h"
#include "util.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

const orc_cmd orc_cmds[] = {
    {"/model", "[slug]", "set or show the model"},
    {"/effort", "low|medium|high", "set reasoning effort"},
    {"/new", "", "start a fresh session"},
    {"/help", "", "list commands"},
    {"/quit", "", "exit orc"},
    {NULL, NULL, NULL},
};

/* cfg->model / cfg->effort normally point at argv or string literals; /model
 * and /effort repoint them here. */
static char model_buf[128], effort_buf[16];

static const provider *cur_prov;
static const orc_cfg *cur_cfg;
static cJSON *models_cache;

void commands_init(const provider *prov, const orc_cfg *cfg) {
    cur_prov = prov;
    cur_cfg = cfg;
}

const char *commands_current_model(void) {
    return cur_cfg && cur_cfg->model ? cur_cfg->model : "";
}

cJSON *commands_models(void) {
    if (!models_cache && cur_prov && cur_prov->models)
        models_cache = cur_prov->models();
    return models_cache;
}

static const char *model_field(cJSON *m, const char *key) {
    cJSON *v = cJSON_GetObjectItem(m, key);
    return cJSON_IsString(v) ? v->valuestring : "";
}

static long long ctx_used; /* tokens in the context after the last request */

/* Context window of the current model, 0 when unknown. */
static long long model_ctx_window(void) {
    cJSON *m;
    cJSON_ArrayForEach(m, commands_models()) {
        if (strcmp(model_field(m, "slug"), commands_current_model()) != 0)
            continue;
        cJSON *w = cJSON_GetObjectItem(m, "context_window");
        return cJSON_IsNumber(w) ? (long long)w->valuedouble : 0;
    }
    return 0;
}

/* Branch name (or short detached SHA) from the nearest .git/HEAD upward. */
static void git_branch(char *out, size_t outsz) {
    out[0] = '\0';
    char dir[1024];
    if (!getcwd(dir, sizeof dir)) return;
    for (;;) {
        char path[1200];
        snprintf(path, sizeof path, "%s/.git/HEAD", dir);
        char *head = read_file(path, NULL);
        if (head) {
            head[strcspn(head, "\n")] = '\0';
            if (strncmp(head, "ref: refs/heads/", 16) == 0)
                snprintf(out, outsz, "%s", head + 16);
            else
                snprintf(out, outsz, "%.8s", head);
            free(head);
            return;
        }
        char *slash = strrchr(dir, '/');
        if (!slash || slash == dir) return;
        *slash = '\0';
    }
}

void commands_status_update(void) {
    if (!cur_cfg) return;
    char cwd[1024] = "", branch[128];
    if (!getcwd(cwd, sizeof cwd)) cwd[0] = '\0';
    const char *base = strrchr(cwd, '/');
    base = base && base[1] ? base + 1 : cwd;
    git_branch(branch, sizeof branch);

    char s[256];
    int n = snprintf(s, sizeof s, "%s · %s · %s",
                     cur_cfg->model ? cur_cfg->model : "?", cur_cfg->effort, base);
    if (branch[0] && n > 0 && n < (int)sizeof s)
        n += snprintf(s + n, sizeof s - (size_t)n, " (%s)", branch);
    long long win = model_ctx_window();
    if (ctx_used > 0 && n > 0 && n < (int)sizeof s) {
        n += ctx_used < 10000
                 ? snprintf(s + n, sizeof s - (size_t)n, " · ctx %.1fk",
                            (double)ctx_used / 1000.0)
                 : snprintf(s + n, sizeof s - (size_t)n, " · ctx %lldk",
                            ctx_used / 1000);
        if (win > 0 && n > 0 && n < (int)sizeof s)
            snprintf(s + n, sizeof s - (size_t)n, " (%lld%%)",
                     (ctx_used * 100 + win - 1) / win); /* ceil: never 0% */
    }
    ui_status_set(s);
}

void commands_ctx_used(long long tokens) {
    ctx_used = tokens;
    commands_status_update();
}

static void cmd_new(agent *ag) {
    orc_session *s = ag->sess;
    session_close(s);
    if (s->items == 0) unlink(s->path); /* nothing was said; drop the file */
    s->items = 0;
    cJSON_Delete(ag->history);
    ag->history = cJSON_CreateArray();
    uuid4(ag->cfg->session_id);
    if (session_new(s, ag->cfg) != 0) {
        fprintf(stderr, "❌ orc: cannot create session file\n");
        return;
    }
    printf("✨ new session %.8s\n", ag->cfg->session_id);
    commands_ctx_used(0); /* empty history: reset the context gauge */
}

int command_dispatch(agent *ag, const char *line) {
    if (line[0] != '/') return 0;
    size_t n = strcspn(line, " \t");
    const char *arg = line + n;
    while (*arg == ' ' || *arg == '\t') arg++;

    const orc_cmd *cmd = NULL;
    for (const orc_cmd *c = orc_cmds; c->name; c++)
        if (strlen(c->name) == n && strncmp(line, c->name, n) == 0) cmd = c;
    if (!cmd) {
        /* "/tmp/x ..." is a path, not a typo'd command; hand it to the model */
        if (memchr(line + 1, '/', n - 1)) return 0;
        printf("⚠️  unknown command %.*s (try /help)\n", (int)n, line);
        return 1;
    }

    if (strcmp(cmd->name, "/quit") == 0) return 2;
    if (strcmp(cmd->name, "/help") == 0) {
        int tty = isatty(1);
        for (const orc_cmd *c = orc_cmds; c->name; c++)
            printf(tty ? "  " BOLD("%-8s") " %-16s " DIM("%s") "\n"
                       : "  %-8s %-16s %s\n",
                   c->name, c->args, c->desc);
        return 1;
    }
    if (strcmp(cmd->name, "/new") == 0) {
        cmd_new(ag);
        return 1;
    }
    if (strcmp(cmd->name, "/model") == 0) {
        cJSON *models = commands_models();
        cJSON *m;
        if (!*arg) {
            printf("🤖 model %s (%s effort)\n", ag->cfg->model, ag->cfg->effort);
            int tty = isatty(1);
            cJSON_ArrayForEach(m, models) {
                const char *slug = model_field(m, "slug");
                int cur = strcmp(slug, ag->cfg->model) == 0;
                printf("  %s%c %-22s %s%s\n", cur && tty ? ANSI_BOLD : "",
                       cur ? '*' : ' ', slug, model_field(m, "description"),
                       cur && tty ? ANSI_RESET : "");
            }
            return 1;
        }
        int known = models == NULL; /* no list: trust the user */
        cJSON_ArrayForEach(m, models)
            if (strcmp(model_field(m, "slug"), arg) == 0) known = 1;
        snprintf(model_buf, sizeof model_buf, "%s", arg);
        ag->cfg->model = model_buf;
        printf("✅ model set to %s%s\n", model_buf,
               known ? "" : " (not in provider's model list)");
        session_set_cfg(ag->sess, ag->cfg);
        commands_status_update();
        return 1;
    }
    if (strcmp(cmd->name, "/effort") == 0) {
        if (!*arg) {
            printf("🧠 effort %s\n", ag->cfg->effort);
            return 1;
        }
        if (strcmp(arg, "low") != 0 && strcmp(arg, "medium") != 0 &&
            strcmp(arg, "high") != 0) {
            printf("⚠️  effort must be low, medium, or high\n");
            return 1;
        }
        snprintf(effort_buf, sizeof effort_buf, "%s", arg);
        ag->cfg->effort = effort_buf;
        printf("✅ effort set to %s\n", effort_buf);
        session_set_cfg(ag->sess, ag->cfg);
        commands_status_update();
        return 1;
    }
    return 1;
}
