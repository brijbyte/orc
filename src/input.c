#include "input.h"
#include "ansi.h"
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
    if (editing) linenoiseEditStop(&ls);
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
        hidden = 1;
    }
}

void input_redraw(void) {
    if (active && editing && hidden) {
        linenoiseShow(&ls);
        hidden = 0;
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
        if (line == linenoiseEditMore) continue;
        editing = 0;
        if (line == NULL) {
            linenoiseEditStop(&ls); /* leaves cooked mode, prints \n */
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
        linenoiseEditStop(&ls); /* leaves cooked mode, prints \n */
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
