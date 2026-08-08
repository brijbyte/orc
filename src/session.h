#ifndef ORC_SESSION_H
#define ORC_SESSION_H

#include <cJSON.h>
#include <stdio.h>
#include "orc.h"

typedef struct {
    FILE *f;
    char path[4096];
} orc_session;

/* Start a new session file under <orc home>/sessions/. Returns 0 on success. */
int session_new(orc_session *s, const orc_cfg *cfg);

/* Resume a session: ref is a session id (prefix ok), a file path, or NULL for
 * the most recent. Appends the stored items into history, restores the
 * session id into cfg, and reopens the file for appending. Returns 0 on
 * success. */
int session_resume(orc_session *s, const char *ref, cJSON *history, orc_cfg *cfg);

void session_append(orc_session *s, cJSON *item);
void session_close(orc_session *s);

#endif
