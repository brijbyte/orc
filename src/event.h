#ifndef ORC_EVENT_H
#define ORC_EVENT_H

typedef int (*event_fd_fn)(void);
typedef void (*event_drain_fn)(void);

void event_source_set(event_fd_fn fd, event_drain_fn drain);
int event_source_fd(void);
void event_source_drain(void);

#endif
