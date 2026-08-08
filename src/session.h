#ifndef ORC_SESSION_H
#define ORC_SESSION_H

#include <cJSON.h>
#include <stdio.h>
#include "orc.h"

typedef struct {
    FILE *f;
    char path[4096];
    int items;     /* conversation items in the file (excludes _meta) */
    long long ctx; /* tokens in the context at the last request (from _meta) */
    char model[128]; /* last model recorded in the file ("" if none) */
    char effort[16]; /* last effort recorded in the file ("" if none) */
} orc_session;

typedef struct {
    char id[9];
    char when[17];
    char title[73];
} orc_session_info;

/* Start a new session file under <orc home>/sessions/. Returns 0 on success. */
int session_new(orc_session *s, const orc_cfg *cfg);

/* Resume a session: ref is a session id (prefix ok), a file path, or NULL for
 * the most recent. Appends the stored items into history, restores the
 * session id into cfg, and reopens the file for appending. Returns 0 on
 * success. */
int session_resume(orc_session *s, const char *ref, cJSON *history, orc_cfg *cfg);

void session_append(orc_session *s, cJSON *item);

/* Record the current context size as a _meta line, so resume restores it. */
void session_set_ctx(orc_session *s, long long tokens);

/* Record the current model + effort as a _meta line, so resume restores them. */
void session_set_cfg(orc_session *s, const orc_cfg *cfg);

void session_close(orc_session *s);

/* Load session summaries, newest first. The caller frees them. */
int session_list(orc_session_info **items, size_t *count);
const char *session_error(void);

#endif
