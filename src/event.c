#include "event.h"

static event_fd_fn source_fd;
static event_drain_fn source_drain;

void event_source_set(event_fd_fn fd, event_drain_fn drain) {
    source_fd = fd;
    source_drain = drain;
}

int event_source_fd(void) {
    return source_fd ? source_fd() : -1;
}

void event_source_drain(void) {
    if (source_drain) source_drain();
}
