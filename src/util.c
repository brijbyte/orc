#include "util.h"

#include <errno.h>
#include <fcntl.h>
#include <mbedtls/base64.h>
#include <mbedtls/sha256.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <time.h>
#include <timestamp.h>
#include <unistd.h>

void sb_init(strbuf *sb) {
    sb->data = NULL;
    sb->len = 0;
    sb->cap = 0;
}

void sb_append(strbuf *sb, const char *bytes, size_t n) {
    if (sb->len + n + 1 > sb->cap) {
        size_t cap = sb->cap ? sb->cap : 256;
        while (cap < sb->len + n + 1) cap *= 2;
        sb->data = realloc(sb->data, cap);
        if (!sb->data) { perror("realloc"); exit(1); }
        sb->cap = cap;
    }
    memcpy(sb->data + sb->len, bytes, n);
    sb->len += n;
    sb->data[sb->len] = '\0';
}

void sb_append_str(strbuf *sb, const char *s) {
    sb_append(sb, s, strlen(s));
}

void sb_free(strbuf *sb) {
    free(sb->data);
    sb_init(sb);
}

char *read_file(const char *path, size_t *out_len) {
    FILE *f = fopen(path, "rb");
    if (!f) return NULL;
    if (fseek(f, 0, SEEK_END) != 0) { fclose(f); return NULL; }
    long sz = ftell(f);
    if (sz < 0) { fclose(f); return NULL; }
    rewind(f);
    char *buf = malloc((size_t)sz + 1);
    if (!buf) { fclose(f); return NULL; }
    size_t n = fread(buf, 1, (size_t)sz, f);
    fclose(f);
    buf[n] = '\0';
    if (out_len) *out_len = n;
    return buf;
}

int write_file_atomic(const char *path, const char *data, size_t len) {
    char tmp[4096];
    snprintf(tmp, sizeof tmp, "%s.tmp.%d", path, getpid());
    int fd = open(tmp, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if (fd < 0) return -1;
    size_t off = 0;
    while (off < len) {
        ssize_t w = write(fd, data + off, len - off);
        if (w < 0 && errno == EINTR) continue;
        if (w <= 0) { close(fd); unlink(tmp); return -1; }
        off += (size_t)w;
    }
    if (close(fd) != 0) { unlink(tmp); return -1; }
    if (rename(tmp, path) != 0) { unlink(tmp); return -1; }
    return 0;
}

int mkdirs(const char *path) {
    char buf[4096];
    snprintf(buf, sizeof buf, "%s", path);
    for (char *p = buf + 1; *p; p++) {
        if (*p == '/') {
            *p = '\0';
            if (mkdir(buf, 0755) != 0 && errno != EEXIST) return -1;
            *p = '/';
        }
    }
    if (mkdir(buf, 0755) != 0 && errno != EEXIST) return -1;
    return 0;
}

char *base64url_decode(const char *in, size_t *out_len) {
    size_t n = strlen(in);
    while (n > 0 && in[n - 1] == '=') n--;
    if (n % 4 == 1) return NULL;

    size_t padded = (n + 3) / 4 * 4;
    unsigned char *src = malloc(padded ? padded : 1);
    unsigned char *out = malloc(n * 3 / 4 + 4);
    if (!src || !out) { free(src); free(out); return NULL; }
    for (size_t i = 0; i < n; i++)
        src[i] = in[i] == '-' ? '+' : in[i] == '_' ? '/' : (unsigned char)in[i];
    for (size_t i = n; i < padded; i++) src[i] = '=';

    size_t olen = 0;
    int rc = mbedtls_base64_decode(out, n * 3 / 4 + 3, &olen, src, padded);
    free(src);
    if (rc != 0) { free(out); return NULL; }
    out[olen] = '\0';
    if (out_len) *out_len = olen;
    return (char *)out;
}

char *base64url_encode(const unsigned char *in, size_t len) {
    size_t cap = 4 * ((len + 2) / 3) + 1;
    unsigned char *out = malloc(cap);
    if (!out) return NULL;
    out[0] = '\0';
    size_t olen = 0;
    if (mbedtls_base64_encode(out, cap, &olen, in, len) != 0) {
        free(out);
        return NULL;
    }
    for (size_t i = 0; i < olen; i++) {
        if (out[i] == '+') out[i] = '-';
        else if (out[i] == '/') out[i] = '_';
    }
    while (olen > 0 && out[olen - 1] == '=') olen--;
    out[olen] = '\0';
    return (char *)out;
}

int rand_bytes(unsigned char *buf, size_t n) {
    FILE *f = fopen("/dev/urandom", "rb");
    if (!f) return -1;
    size_t r = fread(buf, 1, n, f);
    fclose(f);
    return r == n ? 0 : -1;
}

void sha256(const void *data, size_t len, unsigned char out[32]) {
    if (mbedtls_sha256(data, len, out, 0) != 0) memset(out, 0, 32);
}

void uuid4(char out[37]) {
    unsigned char b[16];
    if (rand_bytes(b, 16) != 0) {
        srand((unsigned)time(NULL) ^ (unsigned)getpid());
        for (int i = 0; i < 16; i++) b[i] = (unsigned char)rand();
    }
    b[6] = (b[6] & 0x0F) | 0x40;
    b[8] = (b[8] & 0x3F) | 0x80;
    snprintf(out, 37,
             "%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
             b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
             b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15]);
}

void now_rfc3339(char *out, size_t cap) {
    struct timespec now;
    clock_gettime(CLOCK_REALTIME, &now);
    timestamp_t ts = {.sec = now.tv_sec, .nsec = (int32_t)now.tv_nsec, .offset = 0};
    if (timestamp_format_precision(out, cap, &ts, 3) == 0 && cap > 0)
        out[0] = '\0';
}

char *expand_home(const char *path) {
    if (path[0] == '~' && path[1] == '/') {
        const char *home = getenv("HOME");
        if (!home) home = "";
        size_t n = strlen(home) + strlen(path);
        char *out = malloc(n);
        snprintf(out, n, "%s%s", home, path + 1);
        return out;
    }
    return strdup(path);
}

char *orc_home(void) {
    const char *xdg = getenv("XDG_CONFIG_HOME");
    if (xdg && xdg[0] == '/') {
        size_t n = strlen(xdg) + 5;
        char *out = malloc(n);
        snprintf(out, n, "%s/orc", xdg);
        return out;
    }
    char *cfg = expand_home("~/.config");
    struct stat st;
    int have_cfg = stat(cfg, &st) == 0 && S_ISDIR(st.st_mode);
    free(cfg);
    return expand_home(have_cfg ? "~/.config/orc" : "~/.orc");
}

char *orc_path(const char *rel) {
    char *home = orc_home();
    size_t n = strlen(home) + strlen(rel) + 2;
    char *out = malloc(n);
    snprintf(out, n, "%s/%s", home, rel);
    free(home);
    return out;
}
