#ifndef ORC_HTTP_H
#define ORC_HTTP_H

#include "util.h"

/* headers: NULL-terminated array of "Name: value" strings.
 * Returns HTTP status, or -1 on transport error, or -2 if interrupted. */

/* Non-streaming POST; response body appended to out. */
long http_post(const char *url, const char **headers, const char *body, strbuf *out);

/* Streaming POST. Each complete SSE `data:` payload (JSON text) is passed to cb.
 * On non-2xx the body is appended to err instead of dispatched. */
long http_post_sse(const char *url, const char **headers, const char *body,
                   void (*cb)(const char *data, void *ud), void *ud, strbuf *err);

#endif
