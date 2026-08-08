#include "http.h"
#include "orc.h"

#include <curl/curl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

volatile sig_atomic_t g_interrupt = 0;

typedef struct {
    strbuf buf;                       /* unparsed SSE bytes */
    void (*cb)(const char *, void *);
    void *ud;
    FILE *debug;
} sse_state;

/* Extract complete SSE frames (terminated by \n\n) and dispatch data: payloads. */
static void sse_drain(sse_state *st) {
    for (;;) {
        char *frame_end = NULL;
        for (size_t i = 0; i + 1 < st->buf.len; i++) {
            if (st->buf.data[i] == '\n' && st->buf.data[i + 1] == '\n') {
                frame_end = st->buf.data + i;
                break;
            }
        }
        if (!frame_end) return;
        *frame_end = '\0';

        /* Concatenate all data: lines in the frame. */
        strbuf data;
        sb_init(&data);
        char *line = st->buf.data;
        while (line) {
            char *nl = strchr(line, '\n');
            if (nl) *nl = '\0';
            if (strncmp(line, "data:", 5) == 0) {
                const char *p = line + 5;
                if (*p == ' ') p++;
                sb_append_str(&data, p);
            }
            line = nl ? nl + 1 : NULL;
        }
        if (data.len > 0 && strcmp(data.data, "[DONE]") != 0)
            st->cb(data.data, st->ud);
        sb_free(&data);

        size_t consumed = (size_t)(frame_end - st->buf.data) + 2;
        memmove(st->buf.data, st->buf.data + consumed, st->buf.len - consumed);
        st->buf.len -= consumed;
        st->buf.data[st->buf.len] = '\0';
    }
}

static size_t sse_write(char *ptr, size_t size, size_t nmemb, void *ud) {
    sse_state *st = ud;
    size_t n = size * nmemb;
    if (st->debug) fwrite(ptr, 1, n, st->debug);
    sb_append(&st->buf, ptr, n);
    sse_drain(st);
    return n;
}

static size_t plain_write(char *ptr, size_t size, size_t nmemb, void *ud) {
    sb_append((strbuf *)ud, ptr, size * nmemb);
    return size * nmemb;
}

static int progress_cb(void *ud, curl_off_t dt, curl_off_t dn, curl_off_t ut, curl_off_t un) {
    (void)ud; (void)dt; (void)dn; (void)ut; (void)un;
    return g_interrupt ? 1 : 0;
}

/* The embedded (static) libcurl has no baked-in CA path; probe at runtime. */
static const char *ca_bundle(void) {
    static const char *paths[] = {
        "/etc/ssl/cert.pem",                  /* macOS, BSD */
        "/etc/ssl/certs/ca-certificates.crt", /* Debian/Ubuntu/Alpine */
        "/etc/pki/tls/certs/ca-bundle.crt",   /* Fedora/RHEL */
        "/etc/ssl/ca-bundle.pem",             /* openSUSE */
        NULL,
    };
    const char *env = getenv("CURL_CA_BUNDLE");
    if (!env || !*env) env = getenv("SSL_CERT_FILE");
    if (env && *env) return env;
    for (int i = 0; paths[i]; i++)
        if (access(paths[i], R_OK) == 0) return paths[i];
    return NULL;
}

static CURL *make_handle(const char *url, const char **headers, const char *body,
                         struct curl_slist **out_list) {
    CURL *h = curl_easy_init();
    if (!h) return NULL;
    const char *ca = ca_bundle();
    if (ca) curl_easy_setopt(h, CURLOPT_CAINFO, ca);
    struct curl_slist *list = NULL;
    for (int i = 0; headers && headers[i]; i++)
        list = curl_slist_append(list, headers[i]);
    curl_easy_setopt(h, CURLOPT_URL, url);
    curl_easy_setopt(h, CURLOPT_HTTPHEADER, list);
    curl_easy_setopt(h, CURLOPT_POSTFIELDS, body);
    curl_easy_setopt(h, CURLOPT_NOPROGRESS, 0L);
    curl_easy_setopt(h, CURLOPT_XFERINFOFUNCTION, progress_cb);
    curl_easy_setopt(h, CURLOPT_LOW_SPEED_LIMIT, 1L);
    curl_easy_setopt(h, CURLOPT_LOW_SPEED_TIME, 120L);
    curl_easy_setopt(h, CURLOPT_FOLLOWLOCATION, 0L);
    *out_list = list;
    return h;
}

long http_post(const char *url, const char **headers, const char *body, strbuf *out) {
    struct curl_slist *list = NULL;
    CURL *h = make_handle(url, headers, body, &list);
    if (!h) return -1;
    curl_easy_setopt(h, CURLOPT_WRITEFUNCTION, plain_write);
    curl_easy_setopt(h, CURLOPT_WRITEDATA, out);
    CURLcode rc = curl_easy_perform(h);
    long status = -1;
    if (rc == CURLE_OK) curl_easy_getinfo(h, CURLINFO_RESPONSE_CODE, &status);
    else if (rc == CURLE_ABORTED_BY_CALLBACK) status = -2;
    else fprintf(stderr, "orc: http: %s\n", curl_easy_strerror(rc));
    curl_slist_free_all(list);
    curl_easy_cleanup(h);
    return status;
}

typedef struct {
    sse_state sse;
    strbuf *err;
    long status;
    CURL *handle;
} sse_router;

/* First bytes decide: 2xx streams to SSE parser, otherwise buffer as error. */
static size_t route_write(char *ptr, size_t size, size_t nmemb, void *ud) {
    sse_router *r = ud;
    if (r->status == 0)
        curl_easy_getinfo(r->handle, CURLINFO_RESPONSE_CODE, &r->status);
    if (r->status >= 200 && r->status < 300)
        return sse_write(ptr, size, nmemb, &r->sse);
    sb_append(r->err, ptr, size * nmemb);
    return size * nmemb;
}

long http_post_sse(const char *url, const char **headers, const char *body,
                   void (*cb)(const char *data, void *ud), void *ud, strbuf *err) {
    struct curl_slist *list = NULL;
    CURL *h = make_handle(url, headers, body, &list);
    if (!h) return -1;

    sse_router r = {0};
    sb_init(&r.sse.buf);
    r.sse.cb = cb;
    r.sse.ud = ud;
    r.err = err;
    r.handle = h;
    if (getenv("ORC_DEBUG")) {
        char *p = orc_path("debug.log");
        char *dir = orc_home();
        mkdirs(dir);
        r.sse.debug = fopen(p, "a");
        free(p);
        free(dir);
    }

    curl_easy_setopt(h, CURLOPT_WRITEFUNCTION, route_write);
    curl_easy_setopt(h, CURLOPT_WRITEDATA, &r);
    CURLcode rc = curl_easy_perform(h);

    long status = -1;
    if (rc == CURLE_OK) status = r.status ? r.status : -1;
    else if (rc == CURLE_ABORTED_BY_CALLBACK) status = -2;
    else fprintf(stderr, "orc: http: %s\n", curl_easy_strerror(rc));

    if (r.sse.debug) fclose(r.sse.debug);
    sb_free(&r.sse.buf);
    curl_slist_free_all(list);
    curl_easy_cleanup(h);
    return status;
}
