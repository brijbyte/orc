#ifndef ORC_RENDER_H
#define ORC_RENDER_H

#include "util.h"

/* Line-buffered markdown-to-ANSI renderer for streamed model text.
 * Styles headers, **bold**, *italic*, `code`, [links](url), and dims code
 * fences. Passes text through unstyled when stdout is not a TTY. */
typedef struct {
    strbuf line;
    int tty;
    int in_fence;
} md_render;

void md_init(md_render *r);
void md_delta(md_render *r, const char *s);  /* feed streamed text */
void md_flush(md_render *r);                 /* render any partial final line */
void md_free(md_render *r);

#endif
