#include "loop.h"

#include "event.h"
#include "orc.h"

#include <stdlib.h>

static uv_loop_t *loop;
static uv_signal_t sigint;
static uv_poll_t input;
static int signal_open, input_open;

static void on_sigint(uv_signal_t *handle, int signum) {
    (void)handle;
    (void)signum;
    g_interrupt = 1;
}

static void on_input(uv_poll_t *handle, int status, int events) {
    (void)handle;
    if (status == 0 && (events & UV_READABLE)) event_source_drain();
}

int loop_init(void) {
    loop = uv_default_loop();
    if (!loop || uv_signal_init(loop, &sigint) != 0) return -1;
    signal_open = 1;
    return uv_signal_start(&sigint, on_sigint, SIGINT);
}

void loop_input_start(void) {
    int fd = event_source_fd();
    if (input_open || fd < 0) return;
    if (uv_poll_init(loop, &input, fd) != 0) return;
    input_open = 1;
    uv_poll_start(&input, UV_READABLE, on_input);
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
    if (signal_open) {
        uv_signal_stop(&sigint);
        uv_close((uv_handle_t *)&sigint, closed);
        signal_open = 0;
    }
    uv_run(loop, UV_RUN_DEFAULT);
    uv_loop_close(loop);
    loop = NULL;
}
