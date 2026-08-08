#include "render.h"
#include "ansi.h"
#include "md4c.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

/* ---------------- block renderer: md4c events -> ANSI ---------------- */

enum { S_BOLD = 1, S_ITALIC = 2, S_UNDER = 4, S_STRIKE = 8, S_DIM = 16 };

typedef struct {
    unsigned attrs;
    int fg; /* SGR color code, 0 = default */
} style;

#define MAXSTYLE 32
#define MAXLIST 16

typedef struct {
    style st[MAXSTYLE];
    int nst;
    struct {
        int ordered, next; /* next ordinal for ordered lists */
        int base, cont;    /* marker column; continuation-line indent */
    } list[MAXLIST];
    int nlist;
    int quote;    /* blockquote depth */
    int at_start; /* next output byte begins a visual line */
    /* link capture (to append "(url)" when text differs from href) */
    strbuf href, linktext;
    int in_link, autolink;
    /* table capture: cells buffered, printed aligned on table close */
    int in_table, tcols, trows, tcur, thead;
    strbuf *cells;
    int *cellw;
    char *talign;
} rctx;

/* Re-emit SGR for the merged style stack. */
static void sgr(rctx *c) {
    if (c->in_table) return;
    unsigned a = 0;
    int fg = 0;
    for (int i = 0; i < c->nst; i++) {
        a |= c->st[i].attrs;
        if (c->st[i].fg) fg = c->st[i].fg;
    }
    fputs(ANSI_RESET, stdout);
    if (!a && !fg) return;
    char buf[48];
    int n = snprintf(buf, sizeof buf, "\x1b[");
    if (a & S_BOLD) n += snprintf(buf + n, sizeof buf - n, "%d;", SGR_BOLD);
    if (a & S_DIM) n += snprintf(buf + n, sizeof buf - n, "%d;", SGR_DIM);
    if (a & S_ITALIC) n += snprintf(buf + n, sizeof buf - n, "%d;", SGR_ITALIC);
    if (a & S_UNDER) n += snprintf(buf + n, sizeof buf - n, "%d;", SGR_UNDERLINE);
    if (a & S_STRIKE) n += snprintf(buf + n, sizeof buf - n, "%d;", SGR_STRIKE);
    if (fg) n += snprintf(buf + n, sizeof buf - n, "%d;", fg);
    buf[n - 1] = 'm';
    fputs(buf, stdout);
}

static void push_style(rctx *c, unsigned attrs, int fg) {
    if (c->nst < MAXSTYLE) {
        c->st[c->nst].attrs = attrs;
        c->st[c->nst].fg = fg;
        c->nst++;
    }
    sgr(c);
}

static void pop_style(rctx *c) {
    if (c->nst > 0) c->nst--;
    sgr(c);
}

static void quote_bars(rctx *c) {
    if (!c->quote) return;
    fputs(ANSI_RESET ANSI_DIM, stdout);
    for (int i = 0; i < c->quote; i++) fputs("│ ", stdout);
}

static void spaces(int n) {
    for (int i = 0; i < n; i++) fputc(' ', stdout);
}

/* Prefix printed at the start of each visual line: quote bars + list indent. */
static void line_prefix(rctx *c) {
    quote_bars(c);
    if (c->nlist) spaces(c->list[c->nlist - 1].cont);
    sgr(c);
}

static void newline(rctx *c) {
    fputc('\n', stdout);
    c->at_start = 1;
}

static void break_line(rctx *c) {
    if (!c->at_start) newline(c);
}

/* Count visible columns: skip UTF-8 continuation bytes. */
static int visw(const char *s, size_t n) {
    int w = 0;
    for (size_t i = 0; i < n; i++)
        if (((unsigned char)s[i] & 0xC0) != 0x80) w++;
    return w;
}

static void emit(rctx *c, const char *s, size_t n) {
    if (c->in_table) {
        int idx = c->tcur;
        if (idx >= 0 && idx < c->trows * c->tcols) {
            sb_append(&c->cells[idx], s, n);
            c->cellw[idx] += visw(s, n);
        }
        return;
    }
    if (c->in_link) sb_append(&c->linktext, s, n);
    for (size_t i = 0; i < n; i++) {
        if (c->at_start) {
            line_prefix(c);
            c->at_start = 0;
        }
        if (s[i] == '\n')
            newline(c);
        else
            fputc(s[i], stdout);
    }
}

/* ---- tables ---- */

static void table_enter(rctx *c, const MD_BLOCK_TABLE_DETAIL *d) {
    c->tcols = (int)d->col_count;
    c->trows = (int)(d->head_row_count + d->body_row_count);
    c->thead = (int)d->head_row_count;
    int n = c->tcols * c->trows;
    if (n <= 0) return;
    c->cells = calloc((size_t)n, sizeof(strbuf));
    c->cellw = calloc((size_t)n, sizeof(int));
    c->talign = calloc((size_t)c->tcols, 1);
    if (!c->cells || !c->cellw || !c->talign) { perror("calloc"); exit(1); }
    c->tcur = -1;
    c->in_table = 1;
}

static void table_free(rctx *c) {
    if (c->cells) {
        for (int i = 0; i < c->tcols * c->trows; i++) sb_free(&c->cells[i]);
    }
    free(c->cells);
    free(c->cellw);
    free(c->talign);
    c->cells = NULL;
    c->cellw = NULL;
    c->talign = NULL;
}

static void pad_cell(const strbuf *cell, int w, int colw, char align) {
    int pad = colw - w, left = 0;
    if (align == MD_ALIGN_RIGHT) left = pad;
    else if (align == MD_ALIGN_CENTER) left = pad / 2;
    spaces(left);
    if (cell->data) fwrite(cell->data, 1, cell->len, stdout);
    spaces(pad - left);
}

static void table_leave(rctx *c) {
    c->in_table = 0;
    if (!c->cells) return;
    int cols = c->tcols, rows = c->trows;
    int *colw = calloc((size_t)cols, sizeof(int));
    if (!colw) { perror("calloc"); exit(1); }
    for (int r = 0; r < rows; r++)
        for (int j = 0; j < cols; j++)
            if (c->cellw[r * cols + j] > colw[j]) colw[j] = c->cellw[r * cols + j];

    for (int r = 0; r < rows; r++) {
        break_line(c);
        quote_bars(c);
        fputs(r < c->thead ? ANSI_RESET ANSI_BOLD : ANSI_RESET, stdout);
        for (int j = 0; j < cols; j++) {
            if (j) fputs(ANSI_RESET ANSI_DIM " │ " ANSI_RESET, stdout);
            if (j && r < c->thead) fputs(ANSI_BOLD, stdout);
            pad_cell(&c->cells[r * cols + j], c->cellw[r * cols + j], colw[j],
                     c->talign[j]);
        }
        newline(c);
        if (r == c->thead - 1) {
            quote_bars(c);
            fputs(ANSI_RESET ANSI_DIM, stdout);
            for (int j = 0; j < cols; j++) {
                if (j) fputs("─┼─", stdout);
                for (int k = 0; k < colw[j]; k++) fputs("─", stdout);
            }
            newline(c);
        }
    }
    fputs(ANSI_RESET, stdout);
    table_free(c);
    free(colw);
}

/* ---- md4c callbacks ---- */

static int cb_enter_block(MD_BLOCKTYPE type, void *detail, void *ud) {
    rctx *c = ud;
    switch (type) {
    case MD_BLOCK_QUOTE:
        c->quote++;
        break;
    case MD_BLOCK_UL:
    case MD_BLOCK_OL:
        if (c->nlist < MAXLIST) {
            int base = c->nlist ? c->list[c->nlist - 1].cont : 0;
            c->list[c->nlist].ordered = (type == MD_BLOCK_OL);
            c->list[c->nlist].next =
                type == MD_BLOCK_OL ? (int)((MD_BLOCK_OL_DETAIL *)detail)->start : 0;
            c->list[c->nlist].base = base;
            c->list[c->nlist].cont = base + 2;
            c->nlist++;
        }
        break;
    case MD_BLOCK_LI: {
        MD_BLOCK_LI_DETAIL *d = detail;
        break_line(c);
        quote_bars(c);
        if (c->nlist) {
            int fi = c->nlist - 1;
            spaces(c->list[fi].base);
            char mark[16];
            int w = 2;
            if (c->list[fi].ordered)
                w = snprintf(mark, sizeof mark, "%d. ", c->list[fi].next++);
            else if (d->is_task)
                strcpy(mark, d->task_mark == ' ' ? "☐ " : "☑ ");
            else
                strcpy(mark, "• ");
            fputs(ANSI_RESET ANSI_DIM, stdout);
            fputs(mark, stdout);
            c->list[fi].cont = c->list[fi].base + w;
        }
        sgr(c);
        c->at_start = 0;
        break;
    }
    case MD_BLOCK_H: {
        static const int hfg[6] = {SGR_FG_BRIGHT_MAGENTA, SGR_FG_BRIGHT_CYAN,
                                   SGR_FG_BRIGHT_BLUE, 0, 0, 0};
        int lv = (int)((MD_BLOCK_H_DETAIL *)detail)->level;
        break_line(c);
        push_style(c, S_BOLD, hfg[lv - 1]);
        break;
    }
    case MD_BLOCK_CODE:
        break_line(c);
        push_style(c, S_DIM, 0);
        break;
    case MD_BLOCK_HR: {
        break_line(c);
        line_prefix(c);
        fputs(ANSI_RESET ANSI_DIM, stdout);
        for (int i = 0; i < 40; i++) fputs("─", stdout);
        newline(c);
        break;
    }
    case MD_BLOCK_TABLE:
        table_enter(c, detail);
        break;
    case MD_BLOCK_TH:
    case MD_BLOCK_TD:
        if (c->in_table) {
            c->tcur++;
            int col = c->tcols ? c->tcur % c->tcols : 0;
            if (type == MD_BLOCK_TH)
                c->talign[col] = (char)((MD_BLOCK_TD_DETAIL *)detail)->align;
        }
        break;
    default:
        break;
    }
    return 0;
}

static int cb_leave_block(MD_BLOCKTYPE type, void *detail, void *ud) {
    (void)detail;
    rctx *c = ud;
    switch (type) {
    case MD_BLOCK_QUOTE:
        if (c->quote) c->quote--;
        break;
    case MD_BLOCK_UL:
    case MD_BLOCK_OL:
        if (c->nlist) c->nlist--;
        break;
    case MD_BLOCK_LI:
    case MD_BLOCK_P:
        break_line(c);
        break;
    case MD_BLOCK_H:
        pop_style(c);
        break_line(c);
        break;
    case MD_BLOCK_CODE:
        pop_style(c);
        break_line(c);
        break;
    case MD_BLOCK_TABLE:
        table_leave(c);
        break;
    default:
        break;
    }
    return 0;
}

static int cb_enter_span(MD_SPANTYPE type, void *detail, void *ud) {
    rctx *c = ud;
    if (c->in_table) return 0; /* cells capture plain text only */
    switch (type) {
    case MD_SPAN_STRONG:
        push_style(c, S_BOLD, 0);
        break;
    case MD_SPAN_EM:
        push_style(c, S_ITALIC, 0);
        break;
    case MD_SPAN_DEL:
        push_style(c, S_STRIKE, 0);
        break;
    case MD_SPAN_CODE:
        push_style(c, 0, SGR_FG_CYAN);
        break;
    case MD_SPAN_A:
    case MD_SPAN_IMG: {
        MD_ATTRIBUTE *href = type == MD_SPAN_A
                                 ? &((MD_SPAN_A_DETAIL *)detail)->href
                                 : &((MD_SPAN_IMG_DETAIL *)detail)->src;
        c->autolink =
            type == MD_SPAN_A && ((MD_SPAN_A_DETAIL *)detail)->is_autolink;
        c->href.len = 0;
        c->linktext.len = 0;
        sb_append(&c->href, href->text, href->size);
        if (c->at_start) { /* OSC 8 must land after the line prefix */
            line_prefix(c);
            c->at_start = 0;
        }
        printf(OSC8_OPEN "%s" OSC8_ST, c->href.data ? c->href.data : "");
        push_style(c, S_UNDER, SGR_FG_BRIGHT_BLUE);
        c->in_link = 1;
        break;
    }
    default:
        break;
    }
    return 0;
}

static int cb_leave_span(MD_SPANTYPE type, void *detail, void *ud) {
    (void)detail;
    rctx *c = ud;
    if (c->in_table) return 0;
    switch (type) {
    case MD_SPAN_STRONG:
    case MD_SPAN_EM:
    case MD_SPAN_DEL:
    case MD_SPAN_CODE:
        pop_style(c);
        break;
    case MD_SPAN_A:
    case MD_SPAN_IMG:
        c->in_link = 0;
        pop_style(c);
        fputs(OSC8_CLOSE, stdout);
        if (!c->autolink && c->href.data &&
            (!c->linktext.data || strcmp(c->linktext.data, c->href.data))) {
            fputs(ANSI_RESET ANSI_DIM " (", stdout);
            fputs(c->href.data, stdout);
            fputs(")", stdout);
            sgr(c);
        }
        break;
    default:
        break;
    }
    return 0;
}

static int cb_text(MD_TEXTTYPE type, const MD_CHAR *text, MD_SIZE size, void *ud) {
    rctx *c = ud;
    switch (type) {
    case MD_TEXT_NULLCHAR:
        break;
    case MD_TEXT_BR:
    case MD_TEXT_SOFTBR:
        if (c->in_table) {
            emit(c, " ", 1);
        } else {
            if (c->in_link) sb_append(&c->linktext, " ", 1);
            newline(c);
        }
        break;
    default:
        emit(c, text, size);
        break;
    }
    return 0;
}

static void render_block(const char *src, size_t n) {
    static const MD_PARSER parser = {
        0,
        MD_FLAG_TABLES | MD_FLAG_STRIKETHROUGH | MD_FLAG_TASKLISTS |
            MD_FLAG_PERMISSIVEURLAUTOLINKS | MD_FLAG_NOHTMLBLOCKS |
            MD_FLAG_NOHTMLSPANS,
        cb_enter_block, cb_leave_block, cb_enter_span, cb_leave_span,
        cb_text, NULL, NULL,
    };
    rctx c = {0};
    c.at_start = 1;
    sb_init(&c.href);
    sb_init(&c.linktext);
    md_parse(src, (MD_SIZE)n, &parser, &c);
    fputs(ANSI_RESET, stdout);
    table_free(&c); /* also handles a parser abort before MD_BLOCK_TABLE leave */
    sb_free(&c.href);
    sb_free(&c.linktext);
}

/* ---------------- streaming layer: lines -> blocks ---------------- */

static void flush_block(md_render *r) {
    if (r->block.len) {
        render_block(r->block.data, r->block.len);
        r->block.len = 0;
        r->block.data[0] = '\0';
    }
}

static int is_blank(const char *s) {
    while (*s == ' ' || *s == '\t') s++;
    return *s == '\0';
}

static int fence_open(const char *s, char *ch, int *len) {
    int i = 0;
    while (s[i] == ' ' && i < 3) i++;
    char f = s[i];
    if (f != '`' && f != '~') return 0;
    int n = 0;
    while (s[i + n] == f) n++;
    if (n < 3) return 0;
    if (f == '`' && strchr(s + i + n, '`')) return 0; /* info can't hold ` */
    *ch = f;
    *len = n;
    return 1;
}

static int fence_close(md_render *r, const char *s) {
    int i = 0;
    while (s[i] == ' ' && i < 3) i++;
    int n = 0;
    while (s[i + n] == r->fence_ch) n++;
    if (n < r->fence_len) return 0;
    return is_blank(s + i + n);
}

static int is_atx_heading(const char *s) {
    int i = 0;
    while (s[i] == ' ' && i < 3) i++;
    int n = 0;
    while (s[i + n] == '#') n++;
    return n >= 1 && n <= 6 && (s[i + n] == ' ' || s[i + n] == '\0');
}

static int is_hr(const char *s) {
    char f = 0;
    int n = 0;
    for (; *s; s++) {
        if (*s == ' ' || *s == '\t') continue;
        if (*s != '-' && *s != '_' && *s != '*') return 0;
        if (!f) f = *s;
        if (*s != f) return 0;
        n++;
    }
    return n >= 3;
}

/* Unindented "- ", "* ", "+ ", "1. ", "1) " — starts a new top-level item. */
static int is_toplevel_item(const char *s) {
    if ((s[0] == '-' || s[0] == '*' || s[0] == '+') && s[1] == ' ') return 1;
    int i = 0;
    while (s[i] >= '0' && s[i] <= '9' && i < 9) i++;
    return i > 0 && (s[i] == '.' || s[i] == ')') && s[i + 1] == ' ';
}

static void feed_line(md_render *r, const char *s) {
    if (r->in_fence) {
        if (fence_close(r, s)) {
            printf(ANSI_DIM "%s" ANSI_RESET "\n", s);
            r->in_fence = 0;
        } else {
            fputs(s, stdout);
            fputc('\n', stdout);
        }
        return;
    }
    char ch;
    int len;
    if (fence_open(s, &ch, &len)) {
        flush_block(r);
        printf(ANSI_DIM "%s" ANSI_RESET "\n", s);
        r->in_fence = 1;
        r->fence_ch = ch;
        r->fence_len = len;
        return;
    }
    if (is_blank(s)) {
        flush_block(r);
        fputc('\n', stdout);
        return;
    }
    if (is_atx_heading(s) || (r->block.len == 0 && is_hr(s))) {
        flush_block(r);
        render_block(s, strlen(s));
        return;
    }
    /* New top-level list item: prior block can't be its parent — stream it. */
    if (r->block.len && is_toplevel_item(s)) flush_block(r);
    sb_append_str(&r->block, s);
    sb_append(&r->block, "\n", 1);
}

/* ---------------- public API ---------------- */

void md_init(md_render *r) {
    sb_init(&r->line);
    sb_init(&r->block);
    r->tty = isatty(1);
    r->in_fence = 0;
    r->fence_len = 0;
    r->fence_ch = 0;
}

void md_free(md_render *r) {
    sb_free(&r->line);
    sb_free(&r->block);
}

void md_delta(md_render *r, const char *s) {
    if (!r->tty) {
        fputs(s, stdout);
        fflush(stdout);
        return;
    }
    for (const char *p = s; *p; p++) {
        if (*p == '\n') {
            feed_line(r, r->line.data ? r->line.data : "");
            r->line.len = 0;
            if (r->line.data) r->line.data[0] = '\0';
        } else {
            sb_append(&r->line, p, 1);
        }
    }
    fflush(stdout);
}

void md_flush(md_render *r) {
    if (!r->tty) return;
    if (r->line.len > 0) {
        feed_line(r, r->line.data);
        r->line.len = 0;
        r->line.data[0] = '\0';
    }
    flush_block(r);
    fflush(stdout);
}
