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

/* Resume from path (or the most recent session if NULL). Appends the stored
 * items into history and reopens the file for appending. Returns 0 on success. */
int session_resume(orc_session *s, const char *path, cJSON *history);

void session_append(orc_session *s, cJSON *item);
void session_close(orc_session *s);

#endif
