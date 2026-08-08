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

/* Encode as base64url without padding. Returns malloc'd string. */
char *base64url_encode(const unsigned char *in, size_t len);

/* Fill buf with n bytes from /dev/urandom. Returns 0 on success. */
int rand_bytes(unsigned char *buf, size_t n);

/* SHA-256 of data into out. */
void sha256(const void *data, size_t len, unsigned char out[32]);

/* Write a random UUIDv4 string into out[37]. */
void uuid4(char out[37]);

/* Current UTC time as RFC3339 with milliseconds, into out (needs >= 32 bytes). */
void now_rfc3339(char *out, size_t cap);

/* Expand leading ~/ using $HOME. Returns malloc'd path. */
char *expand_home(const char *path);

/* orc home: $XDG_CONFIG_HOME/orc if set, ~/.config/orc if ~/.config exists,
 * else ~/.orc. Returns malloc'd path. */
char *orc_home(void);

/* orc_home() + "/" + rel. Returns malloc'd path. */
char *orc_path(const char *rel);

#endif
