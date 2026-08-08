#include "render.h"

#include <stdio.h>
#include <string.h>
#include <unistd.h>

#define A_RESET "\x1b[0m"
#define A_BOLD "\x1b[1m"
#define A_DIM "\x1b[2m"
#define A_ITALIC "\x1b[3m"
#define A_UNDER "\x1b[4m"
#define A_CYAN "\x1b[36m"

void md_init(md_render *r) {
    sb_init(&r->line);
    r->tty = isatty(1);
    r->in_fence = 0;
}

void md_free(md_render *r) {
    sb_free(&r->line);
}

/* Render inline spans: `code`, **bold**, *italic*, [text](url).
 * '_' is deliberately not styled (snake_case is common in code). */
static void render_inline(const char *s) {
    size_t i = 0, n = strlen(s);
    while (i < n) {
        if (s[i] == '`') {
            char *end = strchr(s + i + 1, '`');
            if (end) {
                fputs(A_CYAN, stdout);
                fwrite(s + i + 1, 1, (size_t)(end - s - i - 1), stdout);
                fputs(A_RESET, stdout);
                i = (size_t)(end - s) + 1;
                continue;
            }
        } else if (s[i] == '*' && i + 1 < n && s[i + 1] == '*') {
            char *end = strstr(s + i + 2, "**");
            if (end) {
                fputs(A_BOLD, stdout);
                fwrite(s + i + 2, 1, (size_t)(end - s - i - 2), stdout);
                fputs(A_RESET, stdout);
                i = (size_t)(end - s) + 2;
                continue;
            }
        } else if (s[i] == '*' && i + 1 < n && s[i + 1] != ' ' && s[i + 1] != '*') {
            char *end = strchr(s + i + 1, '*');
            if (end && end != s + i + 1) {
                fputs(A_ITALIC, stdout);
                fwrite(s + i + 1, 1, (size_t)(end - s - i - 1), stdout);
                fputs(A_RESET, stdout);
                i = (size_t)(end - s) + 1;
                continue;
            }
        } else if (s[i] == '[') {
            char *close = strchr(s + i + 1, ']');
            if (close && close[1] == '(') {
                char *paren = strchr(close + 2, ')');
                if (paren) {
                    fputs(A_UNDER, stdout);
                    fwrite(s + i + 1, 1, (size_t)(close - s - i - 1), stdout);
                    fputs(A_RESET A_DIM " (", stdout);
                    fwrite(close + 2, 1, (size_t)(paren - close - 2), stdout);
                    fputs(")" A_RESET, stdout);
                    i = (size_t)(paren - s) + 1;
                    continue;
                }
            }
        }
        fputc(s[i], stdout);
        i++;
    }
}

static void render_line(md_render *r, const char *s, int newline) {
    if (!r->tty) {
        fputs(s, stdout);
        if (newline) fputc('\n', stdout);
        fflush(stdout);
        return;
    }

    const char *trim = s;
    while (*trim == ' ') trim++;

    if (strncmp(trim, "```", 3) == 0) {
        r->in_fence = !r->in_fence;
        printf(A_DIM "%s" A_RESET, s);
    } else if (r->in_fence) {
        printf(A_DIM "%s" A_RESET, s);
    } else if (trim[0] == '#') {
        int level = 0;
        while (trim[level] == '#') level++;
        if (level <= 6 && (trim[level] == ' ' || trim[level] == '\0')) {
            printf(A_BOLD "%s" A_RESET, s);
        } else {
            render_inline(s);
        }
    } else if ((trim[0] == '-' || trim[0] == '*') && trim[1] == ' ') {
        /* bullet: print indent + bullet char, style the rest */
        fwrite(s, 1, (size_t)(trim - s), stdout);
        fputs("• ", stdout);
        render_inline(trim + 2);
    } else {
        render_inline(s);
    }
    if (newline) fputc('\n', stdout);
    fflush(stdout);
}

void md_delta(md_render *r, const char *s) {
    for (const char *p = s; *p; p++) {
        if (*p == '\n') {
            render_line(r, r->line.data ? r->line.data : "", 1);
            r->line.len = 0;
            if (r->line.data) r->line.data[0] = '\0';
        } else {
            sb_append(&r->line, p, 1);
        }
    }
}

void md_flush(md_render *r) {
    if (r->line.len > 0) {
        render_line(r, r->line.data, 0);
        r->line.len = 0;
        r->line.data[0] = '\0';
    }
}
