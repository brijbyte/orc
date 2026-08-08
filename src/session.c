#include "session.h"
#include "util.h"

#include <dirent.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

int session_new(orc_session *s, const orc_cfg *cfg) {
    char *dir = orc_path("sessions");
    if (mkdirs(dir) != 0) { free(dir); return -1; }
    snprintf(s->path, sizeof s->path, "%s/%ld-%.8s.jsonl",
             dir, (long)time(NULL), cfg->session_id);
    free(dir);
    s->f = fopen(s->path, "w");
    if (!s->f) return -1;

    char cwd[2048];
    if (!getcwd(cwd, sizeof cwd)) cwd[0] = '\0';
    cJSON *meta = cJSON_CreateObject();
    cJSON *m = cJSON_CreateObject();
    cJSON_AddStringToObject(m, "id", cfg->session_id);
    cJSON_AddStringToObject(m, "model", cfg->model);
    cJSON_AddStringToObject(m, "cwd", cwd);
    char now[40];
    now_rfc3339(now, sizeof now);
    cJSON_AddStringToObject(m, "t", now);
    cJSON_AddItemToObject(meta, "_meta", m);
    char *line = cJSON_PrintUnformatted(meta);
    fprintf(s->f, "%s\n", line);
    fflush(s->f);
    free(line);
    cJSON_Delete(meta);
    return 0;
}

/* Does filename "<ts>-<id8>.jsonl" match session id (prefix, min 1 char)? */
static int name_matches_id(const char *name, const char *id) {
    const char *dash = strchr(name, '-');
    size_t n = strlen(name);
    if (!dash || n < 7 || strcmp(name + n - 6, ".jsonl") != 0) return 0;
    size_t idlen = strlen(id), part = (size_t)(name + n - 6 - (dash + 1));
    return idlen > 0 && strncmp(dash + 1, id, idlen < part ? idlen : part) == 0;
}

/* Most recent session file, optionally restricted to an id prefix. The ts
 * filename prefix makes "lexically greatest" mean "most recent". */
static char *find_session(const char *id) {
    char *dir = orc_path("sessions");
    DIR *d = opendir(dir);
    if (!d) { free(dir); return NULL; }
    char best[512] = "";
    struct dirent *e;
    while ((e = readdir(d))) {
        size_t n = strlen(e->d_name);
        if (n > 6 && strcmp(e->d_name + n - 6, ".jsonl") == 0 &&
            (!id || name_matches_id(e->d_name, id)) &&
            strcmp(e->d_name, best) > 0)
            snprintf(best, sizeof best, "%s", e->d_name);
    }
    closedir(d);
    if (!best[0]) { free(dir); return NULL; }
    char *out = malloc(strlen(dir) + strlen(best) + 2);
    sprintf(out, "%s/%s", dir, best);
    free(dir);
    return out;
}

int session_resume(orc_session *s, const char *ref, cJSON *history, orc_cfg *cfg) {
    char *resolved;
    if (!ref)
        resolved = find_session(NULL);
    else if (strchr(ref, '/') || access(ref, R_OK) == 0)
        resolved = strdup(ref);  /* explicit file path */
    else
        resolved = find_session(ref);
    if (!resolved) {
        fprintf(stderr, "orc: no session%s%s to resume\n",
                ref ? " matching " : "", ref ? ref : "");
        return -1;
    }
    size_t len;
    char *text = read_file(resolved, &len);
    if (!text) {
        fprintf(stderr, "orc: cannot read %s\n", resolved);
        free(resolved);
        return -1;
    }

    char *line = text;
    int items = 0;
    while (line && *line) {
        char *nl = strchr(line, '\n');
        if (nl) *nl = '\0';
        if (*line) {
            cJSON *item = cJSON_Parse(line);
            if (item) {
                cJSON *meta = cJSON_GetObjectItem(item, "_meta");
                if (meta) {
                    /* Restore the id so prompt_cache_key survives resumes. */
                    cJSON *id = cJSON_GetObjectItem(meta, "id");
                    if (cJSON_IsString(id))
                        snprintf(cfg->session_id, sizeof cfg->session_id, "%s",
                                 id->valuestring);
                    cJSON_Delete(item);
                } else {
                    cJSON_AddItemToArray(history, item);
                    items++;
                }
            }
        }
        line = nl ? nl + 1 : NULL;
    }
    free(text);

    snprintf(s->path, sizeof s->path, "%s", resolved);
    free(resolved);
    s->f = fopen(s->path, "a");
    if (!s->f) return -1;
    s->items = items;
    fprintf(stderr, "orc: resumed %.8s (%d items) %s\n",
            cfg->session_id, items, s->path);
    return 0;
}

void session_append(orc_session *s, cJSON *item) {
    if (!s->f) return;
    char *line = cJSON_PrintUnformatted(item);
    if (line) {
        fprintf(s->f, "%s\n", line);
        fflush(s->f);
        free(line);
        s->items++;
    }
}

void session_close(orc_session *s) {
    if (s->f) fclose(s->f);
    s->f = NULL;
}
