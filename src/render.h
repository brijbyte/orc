#ifndef ORC_RENDER_H
#define ORC_RENDER_H

#include "util.h"

/* Streaming markdown-to-ANSI renderer for model text (md4c-based).
 * Buffers input per markdown block (flushed on blank lines) so multi-line
 * constructs render correctly; code fences stream through line by line.
 * Styles headers, bold/italic/strikethrough, `code`, lists, quotes, tables,
 * and OSC 8 clickable links. Passthrough when stdout is not a TTY. */
typedef struct {
    strbuf line;   /* current partial line */
    strbuf block;  /* buffered source of the pending block */
    int tty;
    int in_fence;
    int fence_len;
    char fence_ch;
} md_render;

void md_init(md_render *r);
void md_delta(md_render *r, const char *s);  /* feed streamed text */
void md_flush(md_render *r);                 /* render any pending input */
void md_free(md_render *r);

#endif
