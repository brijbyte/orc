/* OpenAI Codex backend: ChatGPT-subscription auth via ~/.codex/auth.json
 * (token refresh included) + Responses-API requests over SSE. */

#include "provider.h"
#include "http.h"
#include "util.h"

#include <cJSON.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <timestamp.h>
#include <unistd.h>

#ifndef CODEX_URL /* overridable for transport tests */
#define CODEX_URL "https://chatgpt.com/backend-api/codex/responses"
#endif
#define ORIGINATOR "orc"

#define AUTH_PATH "~/.codex/auth.json"
#define REFRESH_URL "https://auth.openai.com/oauth/token"
#define CODEX_CLIENT_ID "app_EMoamEEZ73f0CkXaXp7hrann"
#define REFRESH_WINDOW_S (5 * 60)
#define STALE_AFTER_S (8 * 24 * 3600)

/* ---------- auth ---------- */

typedef struct {
    char *access_token;
    char *account_id;
} codex_auth;

static cJSON *load_auth_json(char **out_path) {
    char *path = expand_home(AUTH_PATH);
    char *text = read_file(path, NULL);
    if (!text) {
        fprintf(stderr, "orc: cannot read %s — run `codex login` first\n", path);
        free(path);
        return NULL;
    }
    cJSON *root = cJSON_Parse(text);
    free(text);
    if (!root) {
        fprintf(stderr, "orc: %s is not valid JSON\n", path);
        free(path);
        return NULL;
    }
    if (out_path) *out_path = path; else free(path);
    return root;
}

static const char *jstr(cJSON *obj, const char *key) {
    cJSON *v = cJSON_GetObjectItem(obj, key);
    return cJSON_IsString(v) ? v->valuestring : NULL;
}

/* exp claim from a JWT, or 0. */
static long long jwt_exp(const char *jwt) {
    const char *p1 = strchr(jwt, '.');
    if (!p1) return 0;
    const char *p2 = strchr(p1 + 1, '.');
    if (!p2) return 0;
    char *payload = strndup(p1 + 1, (size_t)(p2 - p1 - 1));
    char *json = base64url_decode(payload, NULL);
    free(payload);
    if (!json) return 0;
    cJSON *root = cJSON_Parse(json);
    free(json);
    if (!root) return 0;
    cJSON *e = cJSON_GetObjectItem(root, "exp");
    long long exp = cJSON_IsNumber(e) ? (long long)e->valuedouble : 0;
    cJSON_Delete(root);
    return exp;
}

/* Refresh tokens in-place inside root (the parsed auth.json). Returns 0 on success. */
static int do_refresh(cJSON *root, const char *path) {
    cJSON *tokens = cJSON_GetObjectItem(root, "tokens");
    const char *rt = jstr(tokens, "refresh_token");
    if (!rt) {
        fprintf(stderr, "orc: no refresh_token in auth.json\n");
        return -1;
    }

    cJSON *req = cJSON_CreateObject();
    cJSON_AddStringToObject(req, "client_id", CODEX_CLIENT_ID);
    cJSON_AddStringToObject(req, "grant_type", "refresh_token");
    cJSON_AddStringToObject(req, "refresh_token", rt);
    char *body = cJSON_PrintUnformatted(req);
    cJSON_Delete(req);

    const char *headers[] = {"Content-Type: application/json", NULL};
    strbuf resp;
    sb_init(&resp);
    long status = http_post(REFRESH_URL, headers, body, &resp);
    free(body);

    if (status != 200) {
        fprintf(stderr, "orc: token refresh failed (HTTP %ld): %.300s\n",
                status, resp.data ? resp.data : "");
        sb_free(&resp);
        return -1;
    }

    cJSON *nt = cJSON_Parse(resp.data);
    sb_free(&resp);
    if (!nt) {
        fprintf(stderr, "orc: token refresh: bad response JSON\n");
        return -1;
    }
    const char *keys[] = {"access_token", "id_token", "refresh_token"};
    for (int i = 0; i < 3; i++) {
        const char *v = jstr(nt, keys[i]);
        if (v) {
            cJSON_DeleteItemFromObject(tokens, keys[i]);
            cJSON_AddStringToObject(tokens, keys[i], v);
        }
    }
    cJSON_Delete(nt);

    char now[40];
    now_rfc3339(now, sizeof now);
    cJSON_DeleteItemFromObject(root, "last_refresh");
    cJSON_AddStringToObject(root, "last_refresh", now);

    char *out = cJSON_Print(root);
    int rc = write_file_atomic(path, out, strlen(out));
    free(out);
    if (rc != 0) {
        fprintf(stderr, "orc: failed to write %s\n", path);
        return -1;
    }
    fprintf(stderr, "orc: refreshed tokens\n");
    return 0;
}

static int needs_refresh(cJSON *root) {
    cJSON *tokens = cJSON_GetObjectItem(root, "tokens");
    const char *at = jstr(tokens, "access_token");
    long long now = (long long)time(NULL);
    if (at) {
        long long exp = jwt_exp(at);
        if (exp && exp < now + REFRESH_WINDOW_S) return 1;
    }
    const char *lr = jstr(root, "last_refresh");
    if (lr) {
        timestamp_t ts;
        if (timestamp_parse(lr, strlen(lr), &ts) == 0 &&
            now - ts.sec > STALE_AFTER_S)
            return 1;
    }
    return 0;
}

static int auth_load(codex_auth *a) {
    char *path = NULL;
    cJSON *root = load_auth_json(&path);
    if (!root) return -1;

    if (needs_refresh(root)) {
        /* Re-read right before refreshing: another codex process may have rotated. */
        cJSON_Delete(root);
        root = load_auth_json(NULL);
        if (!root) { free(path); return -1; }
        if (needs_refresh(root) && do_refresh(root, path) != 0) {
            cJSON_Delete(root);
            free(path);
            return -1;
        }
    }

    cJSON *tokens = cJSON_GetObjectItem(root, "tokens");
    const char *at = jstr(tokens, "access_token");
    const char *acct = jstr(tokens, "account_id");
    if (!at || !acct) {
        fprintf(stderr, "orc: auth.json missing access_token/account_id\n");
        cJSON_Delete(root);
        free(path);
        return -1;
    }
    a->access_token = strdup(at);
    a->account_id = strdup(acct);
    cJSON_Delete(root);
    free(path);
    return 0;
}

static void auth_free(codex_auth *a) {
    free(a->access_token);
    free(a->account_id);
    a->access_token = a->account_id = NULL;
}

static int codex_auth_status(void) {
    cJSON *root = load_auth_json(NULL);
    if (!root) return -1;
    cJSON *tokens = cJSON_GetObjectItem(root, "tokens");
    const char *mode = jstr(root, "auth_mode");
    const char *acct = jstr(tokens, "account_id");
    const char *at = jstr(tokens, "access_token");
    const char *lr = jstr(root, "last_refresh");

    printf("provider:     codex\n");
    printf("auth_mode:    %s\n", mode ? mode : "(none)");
    printf("account_id:   %s\n", acct ? acct : "(none)");
    if (at) {
        long long exp = jwt_exp(at);
        long long now = (long long)time(NULL);
        if (exp)
            printf("token:        %s (expires in %lld min)\n",
                   exp > now ? "valid" : "EXPIRED", (exp - now) / 60);
        else
            printf("token:        present (no exp claim)\n");
    } else {
        printf("token:        MISSING\n");
    }
    printf("last_refresh: %s\n", lr ? lr : "(none)");
    printf("refresh due:  %s\n", needs_refresh(root) ? "yes" : "no");
    int ok = at && acct;
    cJSON_Delete(root);
    return ok ? 0 : -1;
}

/* ---------- request + SSE ---------- */

typedef struct {
    const provider_cb *cb;
    void *ud;
    cJSON *items;          /* completed output items; forwarded to the agent
                            * only after the whole stream succeeds, so a
                            * mid-stream retry cannot duplicate them */
    int failed;            /* response.failed / response.incomplete seen */
    char errmsg[512];
} turn_state;

static void on_sse_data(const char *data, void *ud) {
    turn_state *st = ud;
    cJSON *ev = cJSON_Parse(data);
    if (!ev) return;
    cJSON *t = cJSON_GetObjectItem(ev, "type");
    const char *type = cJSON_IsString(t) ? t->valuestring : "";

    if (strcmp(type, "response.output_text.delta") == 0) {
        cJSON *d = cJSON_GetObjectItem(ev, "delta");
        if (cJSON_IsString(d) && st->cb->on_text_delta)
            st->cb->on_text_delta(d->valuestring, st->ud);
    } else if (strcmp(type, "response.reasoning_summary_text.delta") == 0) {
        cJSON *d = cJSON_GetObjectItem(ev, "delta");
        if (cJSON_IsString(d) && st->cb->on_thinking_delta)
            st->cb->on_thinking_delta(d->valuestring, st->ud);
    } else if (strcmp(type, "response.output_item.done") == 0) {
        cJSON *item = cJSON_DetachItemFromObject(ev, "item");
        if (item) cJSON_AddItemToArray(st->items, item);
    } else if (strcmp(type, "response.failed") == 0 ||
               strcmp(type, "response.incomplete") == 0) {
        st->failed = 1;
        cJSON *resp = cJSON_GetObjectItem(ev, "response");
        cJSON *err = resp ? cJSON_GetObjectItem(resp, "error") : NULL;
        cJSON *msg = err ? cJSON_GetObjectItem(err, "message") : NULL;
        snprintf(st->errmsg, sizeof st->errmsg, "%s",
                 cJSON_IsString(msg) ? msg->valuestring : type);
    }
    /* response.completed, output_item.added, content_part.*, etc.: ignored. */
    cJSON_Delete(ev);
}

static char *build_body(cJSON *history, cJSON *tools, const orc_cfg *cfg) {
    cJSON *req = cJSON_CreateObject();
    cJSON_AddStringToObject(req, "model", cfg->model);
    cJSON_AddStringToObject(req, "instructions", cfg->instructions);
    cJSON_AddItemToObject(req, "input", cJSON_Duplicate(history, 1));
    if (tools)
        cJSON_AddItemToObject(req, "tools", cJSON_Duplicate(tools, 1));
    cJSON_AddStringToObject(req, "tool_choice", "auto");
    cJSON_AddBoolToObject(req, "parallel_tool_calls", 1);
    cJSON *reasoning = cJSON_CreateObject();
    cJSON_AddStringToObject(reasoning, "effort", cfg->effort);
    cJSON_AddStringToObject(reasoning, "summary", "auto");
    cJSON_AddItemToObject(req, "reasoning", reasoning);
    cJSON_AddBoolToObject(req, "store", 0);
    cJSON_AddBoolToObject(req, "stream", 1);
    cJSON *inc = cJSON_CreateArray();
    cJSON_AddItemToArray(inc, cJSON_CreateString("reasoning.encrypted_content"));
    cJSON_AddItemToObject(req, "include", inc);
    cJSON_AddStringToObject(req, "prompt_cache_key", cfg->session_id);
    char *body = cJSON_PrintUnformatted(req);
    cJSON_Delete(req);
    return body;
}

static int codex_turn(cJSON *history, cJSON *tools, const orc_cfg *cfg,
                      const provider_cb *cb, void *ud) {
    codex_auth auth;
    if (auth_load(&auth) != 0) return PROVIDER_ERROR;

    char h_auth[8192], h_acct[256], h_sess[128], h_reqid[128];
    snprintf(h_auth, sizeof h_auth, "Authorization: Bearer %s", auth.access_token);
    snprintf(h_acct, sizeof h_acct, "chatgpt-account-id: %s", auth.account_id);
    snprintf(h_sess, sizeof h_sess, "session_id: %s", cfg->session_id);
    snprintf(h_reqid, sizeof h_reqid, "x-client-request-id: %s", cfg->session_id);
    const char *headers[] = {
        h_auth, h_acct, h_sess, h_reqid,
        "Content-Type: application/json",
        "Accept: text/event-stream",
        "OpenAI-Beta: responses=experimental",
        "originator: " ORIGINATOR,
        "User-Agent: orc/" ORC_VERSION,
        NULL,
    };

    char *body = build_body(history, tools, cfg);
    int result = PROVIDER_ERROR;

    for (int attempt = 0; attempt < 3; attempt++) {
        turn_state st = {0};
        st.cb = cb;
        st.ud = ud;
        st.items = cJSON_CreateArray();
        strbuf err;
        sb_init(&err);

        long status = http_post_sse(CODEX_URL, headers, body, on_sse_data, &st, &err);

        if (status == -2 || g_interrupt) {
            result = PROVIDER_INTERRUPTED;
        } else if (status >= 200 && status < 300) {
            if (st.failed) {
                fprintf(stderr, "\norc: model error: %s\n", st.errmsg);
                result = PROVIDER_ERROR;
            } else {
                while (cJSON_GetArraySize(st.items) > 0) {
                    cJSON *item = cJSON_DetachItemFromArray(st.items, 0);
                    if (cb->on_item_done) cb->on_item_done(item, ud);
                    else cJSON_Delete(item);
                }
                result = PROVIDER_OK;
            }
        } else if ((status == -1 || status == 429 || status >= 500) &&
                   attempt < 2) {
            /* -1: transport died (dropped connection, partial stream). The
             * response regenerates from scratch; buffered items are dropped. */
            int wait = 2 << attempt;
            if (status == -1)
                fprintf(stderr, "\norc: connection dropped, retrying in %ds...\n", wait);
            else
                fprintf(stderr, "\norc: HTTP %ld, retrying in %ds...\n", status, wait);
            cJSON_Delete(st.items);
            sb_free(&err);
            sleep((unsigned)wait);
            if (!g_interrupt) continue;
            result = PROVIDER_INTERRUPTED;
            break;
        } else if (status == -1 || status == 429 || status >= 500) {
            fprintf(stderr, "\norc: giving up after %d attempts\n", attempt + 1);
        } else {
            fprintf(stderr, "\norc: HTTP %ld: %.500s\n", status, err.data ? err.data : "");
        }
        cJSON_Delete(st.items);
        sb_free(&err);
        break;
    }

    free(body);
    auth_free(&auth);
    return result;
}

/* Selectable models from the Codex CLI's cache; best effort, NULL if absent. */
static cJSON *codex_models(void) {
    char *path = expand_home("~/.codex/models_cache.json");
    char *text = read_file(path, NULL);
    free(path);
    if (!text) return NULL;
    cJSON *root = cJSON_Parse(text);
    free(text);
    if (!root) return NULL;
    cJSON *out = cJSON_CreateArray();
    cJSON *m;
    cJSON_ArrayForEach(m, cJSON_GetObjectItem(root, "models")) {
        cJSON *slug = cJSON_GetObjectItem(m, "slug");
        cJSON *vis = cJSON_GetObjectItem(m, "visibility");
        if (!cJSON_IsString(slug)) continue;
        if (cJSON_IsString(vis) && strcmp(vis->valuestring, "list") != 0)
            continue;
        cJSON *desc = cJSON_GetObjectItem(m, "description");
        cJSON *e = cJSON_CreateObject();
        cJSON_AddStringToObject(e, "slug", slug->valuestring);
        cJSON_AddStringToObject(e, "description",
                                cJSON_IsString(desc) ? desc->valuestring : "");
        cJSON_AddItemToArray(out, e);
    }
    cJSON_Delete(root);
    return out;
}

const provider provider_codex = {
    .name = "codex",
    .default_model = "gpt-5.6-sol",
    .turn = codex_turn,
    .auth_status = codex_auth_status,
    .models = codex_models,
};
