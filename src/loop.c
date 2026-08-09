#if defined(__APPLE__) && !defined(_DARWIN_C_SOURCE)
#define _DARWIN_C_SOURCE 1
#endif

#include "loop.h"
#include "event.h"
#include "ui.h"
#include "orc.h"

#include <fcntl.h>
#include <signal.h>
#include <stdlib.h>

static uv_loop_t *loop;
static uv_signal_t sigint, sigwinch;
static uv_poll_t input;
static uv_timer_t animation;
static int sigint_open, sigwinch_open, input_open, animation_open;

static void on_sigint(uv_signal_t *handle, int signum) {
    (void)handle;
    (void)signum;
    g_interrupt = 1;
}

static void on_sigwinch(uv_signal_t *handle, int signum) {
    (void)handle;
    (void)signum;
    ui_input_resize();
}

static void on_input(uv_poll_t *handle, int status, int events) {
    (void)handle;
    if (status == 0 && (events & UV_READABLE)) event_source_drain();
}

static void on_animation(uv_timer_t *handle) {
    (void)handle;
    ui_input_tick();
}

int loop_init(void) {
    loop = uv_default_loop();
    if (!loop || uv_signal_init(loop, &sigint) != 0) return -1;
    sigint_open = 1;
    if (uv_signal_start(&sigint, on_sigint, SIGINT) != 0) return -1;
    if (uv_signal_init(loop, &sigwinch) != 0) return -1;
    sigwinch_open = 1;
    return uv_signal_start(&sigwinch, on_sigwinch, SIGWINCH);
}

void loop_input_start(void) {
    int fd = event_source_fd();
    if (input_open || fd < 0) return;
    if (uv_poll_init(loop, &input, fd) != 0) return;
    input_open = 1;
    /* uv_poll_init makes the fd nonblocking; stdin shares the tty file
     * description with stdout, so screen writes would drop under load. */
    int fl = fcntl(fd, F_GETFL);
    if (fl != -1) fcntl(fd, F_SETFL, fl & ~O_NONBLOCK);
    uv_poll_start(&input, UV_READABLE, on_input);
    if (uv_timer_init(loop, &animation) != 0) return;
    animation_open = 1;
    uv_timer_start(&animation, on_animation, 120, 120);
}

void loop_run_once(void) {
    uv_run(loop, UV_RUN_ONCE);
}

uv_loop_t *loop_get(void) {
    return loop;
}

static void closed(uv_handle_t *handle) {
    (void)handle;
}

void loop_free(void) {
    if (!loop) return;
    if (input_open) {
        uv_poll_stop(&input);
        uv_close((uv_handle_t *)&input, closed);
        input_open = 0;
    }
    if (animation_open) {
        uv_timer_stop(&animation);
        uv_close((uv_handle_t *)&animation, closed);
        animation_open = 0;
    }
    if (sigint_open) {
        uv_signal_stop(&sigint);
        uv_close((uv_handle_t *)&sigint, closed);
        sigint_open = 0;
    }
    if (sigwinch_open) {
        uv_signal_stop(&sigwinch);
        uv_close((uv_handle_t *)&sigwinch, closed);
        sigwinch_open = 0;
    }
    uv_run(loop, UV_RUN_DEFAULT);
    uv_loop_close(loop);
    loop = NULL;
}
