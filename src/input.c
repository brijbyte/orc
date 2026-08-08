#include "input.h"
#include "ansi.h"
#include "commands.h"
#include "orc.h"
#include "util.h"

#include <errno.h>
#include <linenoise.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <termios.h>
#include <time.h>
#include <unistd.h>

typedef struct qnode {
    char *s;
    int queued; /* submitted while the agent was working */
    struct qnode *next;
} qnode;

static struct linenoiseState ls;
static char lbuf[8192];
static char *hist_path;
static int active = 0, editing = 0, idle_flag = 1, eof_flag = 0, hidden = 0;
static qnode *qhead, *qtail;
static struct termios cooked; /* pre-raw state; linenoise's copy gets
                               * clobbered when we restart edits w/o Stop */
static const char *SPIN[] = {"| > ", "/ > ", "- > ", "\\ > "};
static int spin_i;
static struct timespec spin_last;

static const char *cur_prompt(void) {
    return idle_flag ? "> " : SPIN[spin_i];
}

/* --- slash-command autocomplete ------------------------------------------
 * Tab completion + inline hint use linenoise callbacks; the live candidate
 * list is ours: rows drawn below the input line, cursor moved back up, then
 * linenoiseShow() restores the edit position. */

#define MENU_MAX 10
typedef struct {
    const char *c1;   /* command name or model slug */
    const char *c2;   /* command args; NULL marks a model row */
    const char *desc;
    char insert[160]; /* full input line this row stands for */
} menu_item;
static menu_item menu_items[MENU_MAX];
static int menu_n;         /* candidates in menu_items */
static int menu_sel;       /* selected row (moved with Up/Down) */
static int menu_rows;      /* candidate rows currently on screen */
static char menu_key[64];  /* buffer the menu was drawn for; "" = none */
static int hist_nav;       /* last buffer change came from history recall */

/* Partial model slug when buf is "/model <partial>" (no trailing space), else
 * NULL. */
static const char *model_arg(const char *buf) {
    if (strncmp(buf, "/model ", 7) != 0) return NULL;
    const char *p = buf + 7;
    while (*p == ' ') p++;
    return strpbrk(p, " \t") ? NULL : p;
}

static const char *model_str(cJSON *m, const char *key) {
    cJSON *v = cJSON_GetObjectItem(m, key);
    return cJSON_IsString(v) ? v->valuestring : "";
}

static void complete_cb(const char *buf, linenoiseCompletions *lc) {
    if (buf[0] != '/') return;
    const char *marg = model_arg(buf);
    if (marg) {
        cJSON *m;
        cJSON_ArrayForEach(m, commands_models()) {
            const char *slug = model_str(m, "slug");
            if (strncmp(slug, marg, strlen(marg)) != 0) continue;
            char full[160];
            snprintf(full, sizeof full, "%.*s%s", (int)(marg - buf), buf, slug);
            linenoiseAddCompletion(lc, full);
        }
        return;
    }
    if (strpbrk(buf, " \t")) return;
    for (const orc_cmd *c = orc_cmds; c->name; c++)
        if (strncmp(c->name, buf, strlen(buf)) == 0)
            linenoiseAddCompletion(lc, c->name);
}

static char *hints_cb(const char *buf, int *color, int *bold) {
    static char hint[32];
    if (buf[0] != '/') return NULL;
    for (const orc_cmd *c = orc_cmds; c->name; c++)
        if (strcmp(buf, c->name) == 0 && c->args[0]) {
            *color = 90; /* bright black */
            *bold = 0;
            snprintf(hint, sizeof hint, " %s", c->args);
            return hint;
        }
    return NULL;
}

/* Fill menu_items with candidates for the buffer; returns the count. */
static int menu_collect(const char *buf) {
    int n = 0;
    const char *marg = model_arg(buf);
    if (buf[0] == '/' && !strpbrk(buf, " \t")) {
        size_t klen = strlen(buf);
        for (const orc_cmd *c = orc_cmds; c->name && n < MENU_MAX; c++) {
            if (strncmp(c->name, buf, klen) != 0) continue;
            menu_items[n].c1 = c->name;
            menu_items[n].c2 = c->args;
            menu_items[n].desc = c->desc;
            snprintf(menu_items[n].insert, sizeof menu_items[n].insert,
                     "%s", c->name);
            n++;
        }
    } else if (marg) {
        cJSON *m;
        size_t mlen = strlen(marg);
        cJSON_ArrayForEach(m, commands_models()) {
            const char *slug = model_str(m, "slug");
            if (n >= MENU_MAX || strncmp(slug, marg, mlen) != 0) continue;
            menu_items[n].c1 = slug;
            menu_items[n].c2 = NULL;
            menu_items[n].desc = model_str(m, "description");
            snprintf(menu_items[n].insert, sizeof menu_items[n].insert,
                     "/model %s", slug);
            n++;
        }
    }
    return n;
}

/* Render menu_items (selected row in reverse video) under the input line;
 * SGR 22 ends bold/dim without dropping the reverse. */
static void menu_draw(void) {
    int old = menu_rows;
    int avail = (int)ls.cols - 28; /* room left for the dim description */
    for (int i = 0; i < menu_n; i++) {
        menu_item *it = &menu_items[i];
        fputs("\n\r\x1b[2K", stdout);
        if (i == menu_sel) fputs("\x1b[7m", stdout);
        if (it->c2) {
            printf("  %-8s %-16s", it->c1, it->c2);
        } else {
            int cur = strcmp(it->c1, commands_current_model()) == 0;
            printf("%c %s%-25s%s", cur ? '*' : ' ', cur ? ANSI_BOLD : "",
                   it->c1, cur ? "\x1b[22m" : "");
        }
        if (avail > 0)
            printf(ANSI_DIM "%.*s\x1b[22m", avail, it->desc);
        fputs(ANSI_RESET, stdout);
    }
    for (int i = menu_n; i < old; i++) fputs("\n\r\x1b[2K", stdout);
    int total = menu_n > old ? menu_n : old;
    menu_rows = menu_n;
    if (!total) return;
    printf("\x1b[%dA", total);
    fflush(stdout); /* raw writes from linenoiseShow must land after this */
    linenoiseShow(&ls);
}

/* Recompute candidates when the buffer changed; redraw as needed. Buffer
 * changes from history recall never open a menu, only typing does. */
static void menu_update(void) {
    int from_hist = hist_nav;
    hist_nav = 0;
    if (!active || !editing || hidden) return;
    const char *key =
        ((ls.buf[0] == '/' && !strpbrk(ls.buf, " \t")) || model_arg(ls.buf))
            ? ls.buf : "";
    if (strcmp(key, menu_key) == 0) return;
    snprintf(menu_key, sizeof menu_key, "%s", key);
    menu_n = from_hist ? 0 : (*key ? menu_collect(ls.buf) : 0);
    menu_sel = 0;
    if (menu_n || menu_rows) menu_draw();
}

/* Up/Down while the menu is active move the selection, not history. */
static int history_hook(struct linenoiseState *l, int dir) {
    (void)l;
    if (!menu_n) {
        hist_nav = 1;
        return 0;
    }
    menu_sel = (menu_sel + dir + menu_n) % menu_n;
    if (!hidden) menu_draw();
    return 1;
}

/* Bare ESC closes the menu; the unchanged buffer must not reopen it, so
 * menu_key stays. */
static void esc_hook(struct linenoiseState *l) {
    (void)l;
    if (!menu_n) return;
    menu_n = menu_sel = 0;
    if (!hidden && menu_rows) {
        fputs("\x1b[J", stdout); /* clips the input line at the cursor too */
        fflush(stdout);
        menu_rows = 0;
        linenoiseShow(&ls); /* repaint the clipped line */
    }
}

/* Erase leftover menu rows once the cursor sits below the input line
 * (after linenoiseEditStop) or on it (after linenoiseHide). */
static void menu_discard(void) {
    if (menu_rows) {
        fputs("\x1b[J", stdout);
        fflush(stdout);
    }
    menu_rows = menu_n = menu_sel = 0;
    menu_key[0] = '\0';
}

static void edit_start(void) {
    linenoiseEditStart(&ls, -1, -1, lbuf, sizeof lbuf, cur_prompt());
    /* linenoise raw mode clears OPOST; restore it so "\n" still returns the
     * carriage — without this, multi-line agent output staircases. */
    struct termios t;
    if (tcgetattr(0, &t) == 0) {
        t.c_oflag = cooked.c_oflag;
        tcsetattr(0, TCSANOW, &t);
    }
    editing = 1;
    hidden = 0;
}

static void shutdown_input(void) {
    if (!active) return;
    if (editing) {
        linenoiseEditStop(&ls);
        menu_discard();
    }
    editing = 0;
    active = 0;
    tcsetattr(0, TCSANOW, &cooked);
    if (hist_path) linenoiseHistorySave(hist_path);
}

void input_init(void) {
    if (!isatty(0) || !isatty(1)) return;
    if (tcgetattr(0, &cooked) != 0) return;
    hist_path = orc_path("history");
    linenoiseHistorySetMaxLen(200);
    linenoiseHistoryLoad(hist_path);
    linenoiseSetCompletionCallback(complete_cb);
    linenoiseSetHintsCallback(hints_cb);
    linenoiseSetHistoryHook(history_hook);
    linenoiseSetEscHook(esc_hook);
    active = 1;
    edit_start();
    /* Register after edit_start: linenoise's own atexit (registered inside
     * the first edit_start) frees history, so ours must run first (LIFO)
     * to save history and restore the terminal before that. */
    atexit(shutdown_input);
}

/* Animate the busy prompt (spinner) while an agent turn runs. */
static void spin_tick(void) {
    if (idle_flag || !editing || hidden) return;
    struct timespec now;
    clock_gettime(CLOCK_MONOTONIC, &now);
    long ms = (now.tv_sec - spin_last.tv_sec) * 1000 +
              (now.tv_nsec - spin_last.tv_nsec) / 1000000;
    if (ms < 120) return;
    spin_last = now;
    spin_i = (spin_i + 1) % 4;
    ls.prompt = cur_prompt();
    ls.plen = strlen(ls.prompt);
    linenoiseShow(&ls); /* full in-place line redraw */
}

int input_active(void) { return active; }
int input_fd(void) { return active ? 0 : -1; }
int input_eof(void) { return eof_flag; }

void input_set_idle(int idle) {
    if (idle_flag == idle) return;
    idle_flag = idle;
    spin_i = 0;
    clock_gettime(CLOCK_MONOTONIC, &spin_last);
    if (active && editing) {
        ls.prompt = cur_prompt();
        ls.plen = strlen(ls.prompt);
        if (!hidden) linenoiseShow(&ls);
    }
}

void input_erase(void) {
    if (active && editing && !hidden) {
        linenoiseHide(&ls);
        if (menu_rows) { /* hide the rows; keep candidates + selection */
            fputs("\x1b[J", stdout);
            fflush(stdout);
            menu_rows = 0;
        }
        hidden = 1;
    }
}

void input_redraw(void) {
    if (active && editing && hidden) {
        linenoiseShow(&ls);
        hidden = 0;
        if (menu_n) menu_draw(); /* restore the menu the erase hid */
    }
}

static void push_line(char *line) {
    qnode *n = malloc(sizeof *n);
    if (!n) { free(line); return; }
    n->s = line;
    n->queued = !idle_flag;
    n->next = NULL;
    if (qtail) qtail->next = n;
    else qhead = n;
    qtail = n;
}

void input_drain(void) {
    if (!active || eof_flag) return;
    spin_tick();
    for (;;) {
        struct pollfd p = {.fd = 0, .events = POLLIN};
        if (poll(&p, 1, 0) <= 0 || !editing) break;
        errno = 0;
        char *line = linenoiseEditFeed(&ls);
        if (line == linenoiseEditMore) {
            menu_update();
            continue;
        }
        editing = 0;
        if (line == NULL) {
            linenoiseEditStop(&ls); /* leaves cooked mode, prints \n */
            menu_discard();
            if (errno == EAGAIN) { /* Ctrl-C: discard line; interrupt turn */
                if (!idle_flag) g_interrupt = 1;
                edit_start();
                continue;
            }
            eof_flag = 1; /* Ctrl-D on empty line */
            return;
        }
        if (!*line) { /* empty Enter: redraw in place, no newline scroll */
            free(line);
            fputs("\r\x1b[2K", stdout);
            fflush(stdout);
            edit_start();
            continue;
        }
        if (menu_n) { /* Enter runs the selected menu row */
            char *repl = strdup(menu_items[menu_sel].insert);
            if (repl && strcmp(line, repl) != 0) {
                free(line);
                line = repl;
                snprintf(lbuf, sizeof lbuf, "%s", line);
                ls.len = ls.pos = strlen(lbuf);
                linenoiseShow(&ls); /* scrollback shows what actually ran */
            } else {
                free(repl);
            }
        }
        linenoiseEditStop(&ls); /* leaves cooked mode, prints \n */
        menu_discard();
        linenoiseHistoryAdd(line);
        push_line(line);
        if (!idle_flag) {
            fputs(ANSI_DIM "  ↳ queued" ANSI_RESET "\n", stdout);
            fflush(stdout);
        }
        edit_start();
    }
}

void input_wait(void) {
    if (!active) return;
    while (!qhead && !eof_flag && !g_interrupt) {
        struct pollfd p = {.fd = 0, .events = POLLIN};
        if (poll(&p, 1, -1) < 0) {
            if (errno == EINTR) return; /* external SIGINT */
            return;
        }
        input_drain();
    }
}

char *input_take(int *queued) {
    if (!qhead) return NULL;
    qnode *n = qhead;
    qhead = n->next;
    if (!qhead) qtail = NULL;
    char *s = n->s;
    if (queued) *queued = n->queued;
    free(n);
    return s;
}
