#include "util.h"

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <time.h>
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

static int b64val(char c) {
    if (c >= 'A' && c <= 'Z') return c - 'A';
    if (c >= 'a' && c <= 'z') return c - 'a' + 26;
    if (c >= '0' && c <= '9') return c - '0' + 52;
    if (c == '-' || c == '+') return 62;
    if (c == '_' || c == '/') return 63;
    return -1;
}

char *base64url_decode(const char *in, size_t *out_len) {
    size_t n = strlen(in);
    while (n > 0 && in[n - 1] == '=') n--;
    char *out = malloc(n * 3 / 4 + 4);
    if (!out) return NULL;
    size_t o = 0;
    int acc = 0, bits = 0;
    for (size_t i = 0; i < n; i++) {
        int v = b64val(in[i]);
        if (v < 0) continue;
        acc = (acc << 6) | v;
        bits += 6;
        if (bits >= 8) {
            bits -= 8;
            out[o++] = (char)((acc >> bits) & 0xFF);
        }
    }
    out[o] = '\0';
    if (out_len) *out_len = o;
    return out;
}

void uuid4(char out[37]) {
    unsigned char b[16];
    FILE *f = fopen("/dev/urandom", "rb");
    if (!f || fread(b, 1, 16, f) != 16) {
        srand((unsigned)time(NULL) ^ (unsigned)getpid());
        for (int i = 0; i < 16; i++) b[i] = (unsigned char)rand();
    }
    if (f) fclose(f);
    b[6] = (b[6] & 0x0F) | 0x40;
    b[8] = (b[8] & 0x3F) | 0x80;
    snprintf(out, 37,
             "%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x",
             b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
             b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15]);
}

void now_rfc3339(char *out, size_t cap) {
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    struct tm tm;
    gmtime_r(&ts.tv_sec, &tm);
    snprintf(out, cap, "%04d-%02d-%02dT%02d:%02d:%02d.%03ldZ",
             tm.tm_year + 1900, tm.tm_mon + 1, tm.tm_mday,
             tm.tm_hour, tm.tm_min, tm.tm_sec, ts.tv_nsec / 1000000);
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
