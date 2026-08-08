#ifndef ORC_UI_H
#define ORC_UI_H

#include <stddef.h>

#include "agent.h"
#include "session.h"

typedef struct ui ui;

ui *ui_create(void);
void ui_free(ui *state);
const agent_io *ui_agent_io(void);

void ui_session_resumed(const char *id, int items, const char *path);
void ui_session_list(const orc_session_info *rows, size_t count);

#endif
