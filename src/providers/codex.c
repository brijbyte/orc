/* OpenAI Codex backend: ChatGPT-subscription auth (own OAuth login with
 * token refresh; falls back to ~/.codex/auth.json) + Responses-API over SSE. */

#include "provider.h"
#include "auth.h"
#include "http.h"
#include "util.h"

#include <cJSON.h>
#include <curl/curl.h>
#include <errno.h>
#include <limits.h>
#include <netinet/in.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <time.h>
#include <timestamp.h>
#include <unistd.h>

#ifndef CODEX_URL /* overridable for transport tests */
#define CODEX_URL "https://chatgpt.com/backend-api/codex/responses"
#endif
#define ORIGINATOR "orc"

#define CODEX_CLI_AUTH_PATH "~/.codex/auth.json"
#define AUTH_BASE "https://auth.openai.com"
#define AUTHORIZE_URL AUTH_BASE "/oauth/authorize"
#define REFRESH_URL AUTH_BASE "/oauth/token"
#define CODEX_CLIENT_ID "app_EMoamEEZ73f0CkXaXp7hrann"
#define LOGIN_PORT 1455 /* the redirect URI registered for the client */
#define REDIRECT_URI "http://localhost:1455/auth/callback"
#define REFRESH_WINDOW_S (5 * 60)
#define STALE_AFTER_S (8 * 24 * 3600)

/* ---------- auth ---------- */

typedef struct {
    char *access_token;
    char *account_id;
} codex_auth;

/* One loaded credentials file: sec is the codex section inside root
 * (sec == root when the file uses the flat Codex CLI schema). */
typedef struct {
    cJSON *root;
    cJSON *sec;
    char *path;
} auth_file;

static void authfile_free(auth_file *af) {
    cJSON_Delete(af->root);
    free(af->path);
    af->root = af->sec = NULL;
    af->path = NULL;
}

/* orc's provider-keyed store first; the Codex CLI's flat file as a fallback. */
static int authfile_load(auth_file *af) {
    af->root = auth_store_load(&af->path);
    if (af->root) {
        af->sec = cJSON_GetObjectItem(af->root, "codex");
        /* migrate the flat pre-store layout written by early orc logins */
        if (!af->sec && cJSON_GetObjectItem(af->root, "tokens")) {
            af->sec = af->root;
            af->root = cJSON_CreateObject();
            cJSON_AddItemToObject(af->root, "codex", af->sec);
            char *out = cJSON_Print(af->root);
            write_file_atomic(af->path, out, strlen(out));
            free(out);
        }
        if (af->sec) return 0;
        authfile_free(af);
    }
    af->path = expand_home(CODEX_CLI_AUTH_PATH);
    char *text = read_file(af->path, NULL);
    if (!text) {
        fprintf(stderr, "orc: no credentials — run `orc --login`\n");
        authfile_free(af);
        return -1;
    }
    af->root = cJSON_Parse(text);
    free(text);
    if (!af->root) {
        fprintf(stderr, "orc: %s is not valid JSON\n", af->path);
        authfile_free(af);
        return -1;
    }
    af->sec = af->root;
    return 0;
}

static const char *jstr(cJSON *obj, const char *key) {
    cJSON *v = cJSON_GetObjectItem(obj, key);
    return cJSON_IsString(v) ? v->valuestring : NULL;
}

/* Decoded JWT payload claims, or NULL. Caller owns. */
static cJSON *jwt_payload(const char *jwt) {
    const char *p1 = strchr(jwt, '.');
    if (!p1) return NULL;
    const char *p2 = strchr(p1 + 1, '.');
    if (!p2) return NULL;
    char *payload = strndup(p1 + 1, (size_t)(p2 - p1 - 1));
    char *json = base64url_decode(payload, NULL);
    free(payload);
    if (!json) return NULL;
    cJSON *root = cJSON_Parse(json);
    free(json);
    return root;
}

/* exp claim from a JWT, or 0. */
static long long jwt_exp(const char *jwt) {
    cJSON *root = jwt_payload(jwt);
    if (!root) return 0;
    cJSON *e = cJSON_GetObjectItem(root, "exp");
    long long exp = cJSON_IsNumber(e) ? (long long)e->valuedouble : 0;
    cJSON_Delete(root);
    return exp;
}

/* Refresh tokens in-place inside af->sec and rewrite af->path. 0 on success. */
static int do_refresh(auth_file *af) {
    cJSON *tokens = cJSON_GetObjectItem(af->sec, "tokens");
    const char *rt = jstr(tokens, "refresh_token");
    if (!rt) {
        fprintf(stderr, "orc: no refresh_token in %s\n", af->path);
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
    cJSON_DeleteItemFromObject(af->sec, "last_refresh");
    cJSON_AddStringToObject(af->sec, "last_refresh", now);

    char *out = cJSON_Print(af->root);
    int rc = write_file_atomic(af->path, out, strlen(out));
    free(out);
    if (rc != 0) {
        fprintf(stderr, "orc: failed to write %s\n", af->path);
        return -1;
    }
    fprintf(stderr, "orc: refreshed tokens\n");
    return 0;
}

static int needs_refresh(cJSON *sec) {
    cJSON *tokens = cJSON_GetObjectItem(sec, "tokens");
    const char *at = jstr(tokens, "access_token");
    long long now = (long long)time(NULL);
    if (at) {
        long long exp = jwt_exp(at);
        if (exp && exp < now + REFRESH_WINDOW_S) return 1;
    }
    const char *lr = jstr(sec, "last_refresh");
    if (lr) {
        timestamp_t ts;
        if (timestamp_parse(lr, strlen(lr), &ts) == 0 &&
            now - ts.sec > STALE_AFTER_S)
            return 1;
    }
    return 0;
}

static int auth_load(codex_auth *a) {
    auth_file af = {0};
    if (authfile_load(&af) != 0) return -1;

    if (needs_refresh(af.sec)) {
        /* Re-read right before refreshing: another process may have rotated. */
        authfile_free(&af);
        if (authfile_load(&af) != 0) return -1;
        if (needs_refresh(af.sec) && do_refresh(&af) != 0) {
            authfile_free(&af);
            return -1;
        }
    }

    cJSON *tokens = cJSON_GetObjectItem(af.sec, "tokens");
    const char *at = jstr(tokens, "access_token");
    const char *acct = jstr(tokens, "account_id");
    if (!at || !acct) {
        fprintf(stderr, "orc: %s missing access_token/account_id\n", af.path);
        authfile_free(&af);
        return -1;
    }
    a->access_token = strdup(at);
    a->account_id = strdup(acct);
    authfile_free(&af);
    return 0;
}

static void auth_free(codex_auth *a) {
    free(a->access_token);
    free(a->account_id);
    a->access_token = a->account_id = NULL;
}

static int codex_auth_status(void) {
    auth_file af = {0};
    if (authfile_load(&af) != 0) return -1;
    cJSON *tokens = cJSON_GetObjectItem(af.sec, "tokens");
    const char *mode = jstr(af.sec, "auth_mode");
    const char *acct = jstr(tokens, "account_id");
    const char *at = jstr(tokens, "access_token");
    const char *lr = jstr(af.sec, "last_refresh");

    printf("provider:     codex\n");
    printf("auth_file:    %s\n", af.path);
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
    printf("refresh due:  %s\n", needs_refresh(af.sec) ? "yes" : "no");
    int ok = at && acct;
    authfile_free(&af);
    return ok ? 0 : -1;
}

/* ---------- login (OAuth authorization code + PKCE) ---------- */

static const char SUCCESS_HTML[] =
    "<html><body style=\"font-family:sans-serif;margin:4em\">"
    "<h2>orc: login successful</h2><p>You can close this tab.</p></body></html>";
static const char FAIL_HTML[] =
    "<html><body style=\"font-family:sans-serif;margin:4em\">"
    "<h2>orc: login failed</h2><p>Go back to the terminal.</p></body></html>";

static void http_reply(int fd, const char *status, const char *html) {
    char hdr[256];
    int n = snprintf(hdr, sizeof hdr,
                     "HTTP/1.1 %s\r\nContent-Type: text/html\r\n"
                     "Content-Length: %zu\r\nConnection: close\r\n\r\n",
                     status, strlen(html));
    if (write(fd, hdr, (size_t)n) < 0 || write(fd, html, strlen(html)) < 0) {}
}

/* Decoded value of key in a query string. Free with curl_free(). */
static char *query_param(const char *q, const char *key) {
    size_t kl = strlen(key);
    while (*q) {
        const char *end = strchr(q, '&');
        size_t seg = end ? (size_t)(end - q) : strlen(q);
        if (seg > kl + 1 && strncmp(q, key, kl) == 0 && q[kl] == '=') {
            const char *v = q + kl + 1;
            size_t vl = seg - kl - 1;
            if (vl > INT_MAX) return NULL;
            char *encoded = strndup(v, vl);
            if (!encoded) return NULL;
            for (char *p = encoded; *p; p++)
                if (*p == '+') *p = ' ';
            char *out = curl_easy_unescape(NULL, encoded, (int)vl, NULL);
            free(encoded);
            return out;
        }
        q = end ? end + 1 : q + seg;
    }
    return NULL;
}

/* Serve requests until the OAuth callback arrives; return its code. */
static char *wait_for_code(int srv, const char *state) {
    for (;;) {
        int fd = accept(srv, NULL, NULL);
        if (fd < 0) {
            if (errno == EINTR) continue;
            perror("orc: accept");
            return NULL;
        }
        char req[8192];
        size_t got = 0;
        while (got < sizeof req - 1) {
            ssize_t r = read(fd, req + got, sizeof req - 1 - got);
            if (r <= 0) break;
            got += (size_t)r;
            req[got] = '\0';
            if (strstr(req, "\r\n\r\n")) break;
        }
        req[got] = '\0';
        char *path = NULL;
        if (strncmp(req, "GET ", 4) == 0) {
            path = req + 4;
            char *sp = strchr(path, ' ');
            if (sp) *sp = '\0';
        }
        if (!path || strncmp(path, "/auth/callback", 14) != 0) {
            http_reply(fd, "404 Not Found", "");
            close(fd);
            continue; /* favicon and other stray requests */
        }
        char *q = strchr(path, '?');
        char *code = q ? query_param(q + 1, "code") : NULL;
        char *st = q ? query_param(q + 1, "state") : NULL;
        char *err = q ? query_param(q + 1, "error") : NULL;
        int ok = code && st && strcmp(st, state) == 0;
        http_reply(fd, ok ? "200 OK" : "400 Bad Request",
                   ok ? SUCCESS_HTML : FAIL_HTML);
        close(fd);
        curl_free(st);
        if (!ok) {
            fprintf(stderr, "orc: login callback rejected (%s)\n",
                    err ? err : "missing code or bad state");
            curl_free(err);
            curl_free(code);
            return NULL;
        }
        curl_free(err);
        return code;
    }
}

static void open_browser(const char *url) {
    char cmd[2048];
#ifdef __APPLE__
    snprintf(cmd, sizeof cmd, "open '%s' >/dev/null 2>&1", url);
#else
    snprintf(cmd, sizeof cmd, "xdg-open '%s' >/dev/null 2>&1", url);
#endif
    if (system(cmd) != 0) {} /* URL is printed; manual open still works */
}

static int save_auth(cJSON *grant) {
    const char *idt = jstr(grant, "id_token");
    const char *at = jstr(grant, "access_token");
    const char *rt = jstr(grant, "refresh_token");
    if (!idt || !at || !rt) {
        fprintf(stderr, "orc: token response missing fields\n");
        return -1;
    }
    cJSON *claims = jwt_payload(idt);
    const char *acct =
        jstr(cJSON_GetObjectItem(claims, "https://api.openai.com/auth"),
             "chatgpt_account_id");
    if (!acct) {
        fprintf(stderr, "orc: id_token has no chatgpt_account_id\n");
        cJSON_Delete(claims);
        return -1;
    }

    cJSON *sec = cJSON_CreateObject();
    cJSON_AddStringToObject(sec, "auth_mode", "chatgpt");
    cJSON *tokens = cJSON_AddObjectToObject(sec, "tokens");
    cJSON_AddStringToObject(tokens, "id_token", idt);
    cJSON_AddStringToObject(tokens, "access_token", at);
    cJSON_AddStringToObject(tokens, "refresh_token", rt);
    cJSON_AddStringToObject(tokens, "account_id", acct);
    char now[40];
    now_rfc3339(now, sizeof now);
    cJSON_AddStringToObject(sec, "last_refresh", now);
    cJSON_Delete(claims);

    int rc = auth_store_put("codex", sec);
    if (rc == 0) {
        char *path = orc_path("auth.json");
        printf("orc: logged in; credentials saved to %s\n", path);
        free(path);
    }
    return rc;
}

static int codex_login(void) {
    unsigned char rnd[32], dig[32];
    if (rand_bytes(rnd, 32) != 0) {
        fprintf(stderr, "orc: cannot read /dev/urandom\n");
        return -1;
    }
    char *verifier = base64url_encode(rnd, 32); /* 43 chars, PKCE-valid */
    if (!verifier) return -1;
    sha256(verifier, strlen(verifier), dig);
    char *challenge = base64url_encode(dig, 32);
    if (rand_bytes(rnd, 16) != 0) {
        free(verifier); free(challenge);
        return -1;
    }
    char *state = base64url_encode(rnd, 16);
    char *redirect = curl_easy_escape(NULL, REDIRECT_URI, 0);
    char *scope = curl_easy_escape(NULL, "openid profile email offline_access", 0);
    char *challenge_q = challenge ? curl_easy_escape(NULL, challenge, 0) : NULL;
    char *state_q = state ? curl_easy_escape(NULL, state, 0) : NULL;
    if (!challenge || !state || !redirect || !scope || !challenge_q || !state_q) {
        free(verifier); free(challenge); free(state);
        curl_free(redirect); curl_free(scope); curl_free(challenge_q); curl_free(state_q);
        return -1;
    }

    int srv = socket(AF_INET, SOCK_STREAM, 0);
    int one = 1;
    setsockopt(srv, SOL_SOCKET, SO_REUSEADDR, &one, sizeof one);
    struct sockaddr_in addr = {0};
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = htonl(0x7f000001); /* 127.0.0.1 */
    addr.sin_port = htons(LOGIN_PORT);
    if (bind(srv, (struct sockaddr *)&addr, sizeof addr) != 0 ||
        listen(srv, 4) != 0) {
        fprintf(stderr, "orc: cannot listen on localhost:%d (port in use?)\n",
                LOGIN_PORT);
        close(srv);
        free(verifier); free(challenge); free(state);
        curl_free(redirect); curl_free(scope); curl_free(challenge_q); curl_free(state_q);
        return -1;
    }

    char url[1024];
    snprintf(url, sizeof url,
             AUTHORIZE_URL "?response_type=code&client_id=" CODEX_CLIENT_ID
             "&redirect_uri=%s&scope=%s"
             "&code_challenge=%s&code_challenge_method=S256"
             "&id_token_add_organizations=true&codex_cli_simplified_flow=true"
             "&state=%s",
             redirect, scope, challenge_q, state_q);
    printf("Open this URL to sign in with ChatGPT:\n\n  %s\n\n"
           "Waiting for the browser callback on localhost:%d "
           "(Ctrl-C to cancel)...\n", url, LOGIN_PORT);
    fflush(stdout); /* stdout may be a pipe; show the URL before blocking */
    open_browser(url);
    free(challenge);
    curl_free(scope); curl_free(challenge_q); curl_free(state_q);

    char *code = wait_for_code(srv, state);
    close(srv);
    free(state);
    if (!code) {
        free(verifier);
        curl_free(redirect);
        return -1;
    }

    char *code_form = curl_easy_escape(NULL, code, 0);
    char *verifier_form = curl_easy_escape(NULL, verifier, 0);
    curl_free(code);
    free(verifier);
    if (!code_form || !verifier_form) {
        curl_free(code_form); curl_free(verifier_form); curl_free(redirect);
        return -1;
    }
    char body[2048];
    snprintf(body, sizeof body,
             "grant_type=authorization_code&client_id=" CODEX_CLIENT_ID
             "&code=%s&code_verifier=%s&redirect_uri=%s",
             code_form, verifier_form, redirect);
    curl_free(code_form); curl_free(verifier_form); curl_free(redirect);
    const char *headers[] = {"Content-Type: application/x-www-form-urlencoded",
                             NULL};
    strbuf resp;
    sb_init(&resp);
    long status = http_post(REFRESH_URL, headers, body, &resp);
    if (status != 200) {
        fprintf(stderr, "orc: code exchange failed (HTTP %ld): %.300s\n",
                status, resp.data ? resp.data : "");
        sb_free(&resp);
        return -1;
    }
    cJSON *grant = cJSON_Parse(resp.data);
    sb_free(&resp);
    if (!grant) {
        fprintf(stderr, "orc: code exchange: bad response JSON\n");
        return -1;
    }
    int rc = save_auth(grant);
    cJSON_Delete(grant);
    return rc;
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
    .login = codex_login,
    .models = codex_models,
};
