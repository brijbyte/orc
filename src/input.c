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

static void edit_start(void) {
    linenoiseEditStart(&ls, -1, -1, lbuf, sizeof lbuf, "> ");
    editing = 1;
    hidden = 0;
}

static void shutdown_input(void) {
    if (!active) return;
    if (editing) linenoiseEditStop(&ls);
    editing = 0;
    active = 0;
    if (hist_path) linenoiseHistorySave(hist_path);
}

void input_init(void) {
    if (!isatty(0) || !isatty(1)) return;
    hist_path = orc_path("history");
    linenoiseHistorySetMaxLen(200);
    linenoiseHistoryLoad(hist_path);
    active = 1;
    atexit(shutdown_input);
    edit_start();
}

int input_active(void) { return active; }
int input_fd(void) { return active ? 0 : -1; }
int input_eof(void) { return eof_flag; }
void input_set_idle(int idle) { idle_flag = idle; }

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
    for (;;) {
        struct pollfd p = {.fd = 0, .events = POLLIN};
        if (poll(&p, 1, 0) <= 0 || !editing) break;
        errno = 0;
        char *line = linenoiseEditFeed(&ls);
        if (line == linenoiseEditMore) continue;
        editing = 0;
        linenoiseEditStop(&ls); /* leaves cooked mode, prints \n */
        if (line == NULL) {
            if (errno == EAGAIN) { /* Ctrl-C: discard line; interrupt turn */
                if (!idle_flag) g_interrupt = 1;
                edit_start();
                continue;
            }
            eof_flag = 1; /* Ctrl-D on empty line */
            return;
        }
        if (*line) {
            linenoiseHistoryAdd(line);
            push_line(line);
            if (!idle_flag) {
                fputs(ANSI_DIM "  ↳ queued" ANSI_RESET "\n", stdout);
                fflush(stdout);
            }
        } else {
            free(line);
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
