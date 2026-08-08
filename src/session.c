#include "session.h"
#include "util.h"

#include <dirent.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

static char last_error[4096];

const char *session_error(void) { return last_error; }

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
    cJSON_AddStringToObject(m, "effort", cfg->effort);
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
        snprintf(last_error, sizeof last_error, "no session%s%s to resume",
                 ref ? " matching " : "", ref ? ref : "");
        return -1;
    }
    size_t len;
    char *text = read_file(resolved, &len);
    if (!text) {
        snprintf(last_error, sizeof last_error, "cannot read %s", resolved);
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
                    cJSON *ctx = cJSON_GetObjectItem(meta, "ctx");
                    if (cJSON_IsNumber(ctx)) s->ctx = (long long)ctx->valuedouble;
                    cJSON *v = cJSON_GetObjectItem(meta, "model");
                    if (cJSON_IsString(v))
                        snprintf(s->model, sizeof s->model, "%s", v->valuestring);
                    v = cJSON_GetObjectItem(meta, "effort");
                    if (cJSON_IsString(v))
                        snprintf(s->effort, sizeof s->effort, "%s", v->valuestring);
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
    if (!s->f) {
        snprintf(last_error, sizeof last_error, "cannot open %s", s->path);
        return -1;
    }
    s->items = items;
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

void session_set_ctx(orc_session *s, long long tokens) {
    if (!s->f) return;
    s->ctx = tokens;
    fprintf(s->f, "{\"_meta\":{\"ctx\":%lld}}\n", tokens);
    fflush(s->f);
}

void session_set_cfg(orc_session *s, const orc_cfg *cfg) {
    if (!s->f) return;
    cJSON *root = cJSON_CreateObject();
    cJSON *m = cJSON_AddObjectToObject(root, "_meta");
    cJSON_AddStringToObject(m, "model", cfg->model);
    cJSON_AddStringToObject(m, "effort", cfg->effort);
    char *line = cJSON_PrintUnformatted(root);
    if (line) {
        fprintf(s->f, "%s\n", line);
        fflush(s->f);
        free(line);
    }
    cJSON_Delete(root);
}

void session_close(orc_session *s) {
    if (s->f) fclose(s->f);
    s->f = NULL;
}

/* Read one --list row if it belongs to cwd. */
static int list_one(const char *path, const char *cwd, orc_session_info *info) {
    FILE *f = fopen(path, "r");
    if (!f) return -1;
    memset(info, 0, sizeof *info);
    int cwd_matches = 0;
    char *line = NULL;
    size_t cap = 0;
    for (int ln = 0; ln < 4 && getline(&line, &cap, f) != -1; ln++) {
        cJSON *item = cJSON_Parse(line);
        if (!item) continue;
        cJSON *meta = cJSON_GetObjectItem(item, "_meta");
        if (meta) {
            cJSON *v = cJSON_GetObjectItem(meta, "cwd");
            if (cJSON_IsString(v) && strcmp(v->valuestring, cwd) == 0)
                cwd_matches = 1;
            v = cJSON_GetObjectItem(meta, "id");
            if (cJSON_IsString(v))
                snprintf(info->id, sizeof info->id, "%s", v->valuestring);
            v = cJSON_GetObjectItem(meta, "t");
            if (cJSON_IsString(v))
                snprintf(info->when, sizeof info->when, "%s", v->valuestring);
        } else {
            cJSON *role = cJSON_GetObjectItem(item, "role");
            if (cJSON_IsString(role) && strcmp(role->valuestring, "user") == 0) {
                cJSON *part = cJSON_GetArrayItem(
                    cJSON_GetObjectItem(item, "content"), 0);
                cJSON *text = part ? cJSON_GetObjectItem(part, "text") : NULL;
                if (cJSON_IsString(text)) {
                    snprintf(info->title, sizeof info->title, "%s", text->valuestring);
                    for (char *p = info->title; *p; p++)
                        if (*p == '\n' || *p == '\t') *p = ' ';
                }
                cJSON_Delete(item);
                break;
            }
        }
        cJSON_Delete(item);
    }
    free(line);
    fclose(f);
    char *separator = strchr(info->when, 'T');
    if (separator) *separator = ' ';
    return cwd_matches;
}

static int newest_first(const void *a, const void *b) {
    return strcmp(*(char *const *)b, *(char *const *)a);
}

int session_list(orc_session_info **items, size_t *count) {
    *items = NULL;
    *count = 0;
    char *cwd = getcwd(NULL, 0);
    if (!cwd) {
        snprintf(last_error, sizeof last_error, "cannot get current directory");
        return -1;
    }
    char *dir = orc_path("sessions");
    DIR *d = opendir(dir);
    char **names = NULL;
    size_t n = 0, cap = 0;
    if (d) {
        struct dirent *entry;
        while ((entry = readdir(d))) {
            size_t len = strlen(entry->d_name);
            if (len <= 6 || strcmp(entry->d_name + len - 6, ".jsonl") != 0)
                continue;
            if (n == cap) {
                cap = cap ? cap * 2 : 32;
                char **grown = realloc(names, cap * sizeof *names);
                if (!grown) break;
                names = grown;
            }
            names[n++] = strdup(entry->d_name);
        }
        closedir(d);
    }
    qsort(names, n, sizeof *names, newest_first);
    orc_session_info *rows = calloc(n, sizeof *rows);
    size_t used = 0;
    for (size_t i = 0; i < n; i++) {
        char path[4096];
        snprintf(path, sizeof path, "%s/%s", dir, names[i]);
        if (rows && list_one(path, cwd, &rows[used]) > 0) used++;
        free(names[i]);
    }
    free(names);
    free(dir);
    free(cwd);
    if (n && !rows) {
        snprintf(last_error, sizeof last_error, "out of memory");
        return -1;
    }
    *items = rows;
    *count = used;
    return 0;
}
