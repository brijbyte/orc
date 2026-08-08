#ifndef ORC_H
#define ORC_H

#include <signal.h>

#ifndef ORC_VERSION
#define ORC_VERSION "0.1"
#endif
#define ORC_DEFAULT_EFFORT "medium"

typedef struct {
    const char *provider;   /* provider name; NULL = default */
    const char *model;      /* NULL = provider's default */
    const char *effort;
    char session_id[37];
    char *instructions;
    int debug;
} orc_cfg;

/* Set by SIGINT handler; checked in curl progress callback and tool loops. */
extern volatile sig_atomic_t g_interrupt;

#endif
