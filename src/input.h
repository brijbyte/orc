#ifndef ORC_INPUT_H
#define ORC_INPUT_H

/* Async stdin line editor (linenoise multiplexed API): the user can type and
 * queue lines with Enter while the agent streams output; the "> " edit line
 * stays below the output via hide/redraw. Every function no-ops unless
 * input_init() ran on a TTY, so callers may call unconditionally. */

void input_init(void);          /* start editing; raw mode until exit */
int input_active(void);
int input_fd(void);             /* 0 when active, -1 otherwise (for poll) */
void input_drain(void);         /* consume pending keystrokes */
void input_wait(void);          /* block until a line queues, EOF, or SIGINT */
char *input_take(int *queued);  /* pop queued line (malloc'd) or NULL;
                                 * *queued=1 if typed while the agent worked */
const char *input_peek(void);   /* head of the queue without popping, or NULL */
int input_eof(void);            /* Ctrl-D on an empty line */
void input_set_idle(int idle);  /* 0 while an agent turn runs */
void input_erase(void);         /* call before writing agent output */
void input_redraw(void);        /* call after writing agent output */
void input_status_set(const char *s); /* dim status line under the input;
                                       * "" or NULL hides it */

#endif
