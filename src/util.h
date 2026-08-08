#ifndef ORC_UTIL_H
#define ORC_UTIL_H

#include <stddef.h>

/* Growable string buffer. */
typedef struct {
    char *data;
    size_t len;
    size_t cap;
} strbuf;

void sb_init(strbuf *sb);
void sb_append(strbuf *sb, const char *bytes, size_t n);
void sb_append_str(strbuf *sb, const char *s);
void sb_free(strbuf *sb);

/* Read entire file. Returns malloc'd NUL-terminated buffer or NULL. */
char *read_file(const char *path, size_t *out_len);

/* Write file atomically (temp + rename). Returns 0 on success. */
int write_file_atomic(const char *path, const char *data, size_t len);

/* mkdir -p for the given directory path. Returns 0 on success. */
int mkdirs(const char *path);

/* Decode base64url (no padding required). Returns malloc'd buffer, NUL-terminated. */
char *base64url_decode(const char *in, size_t *out_len);

/* Write a random UUIDv4 string into out[37]. */
void uuid4(char out[37]);

/* Current UTC time as RFC3339 with milliseconds, into out (needs >= 32 bytes). */
void now_rfc3339(char *out, size_t cap);

/* Expand leading ~/ using $HOME. Returns malloc'd path. */
char *expand_home(const char *path);

#endif
