#ifndef ORC_UI_H
#define ORC_UI_H

#include <stddef.h>

#include "agent.h"
#include "session.h"

typedef struct ui ui;

ui *ui_create(void);
void ui_free(ui *state);
const agent_io *ui_agent_io(void);

void ui_input_init(void);
int ui_input_active(void);
int ui_input_eof(void);
void ui_input_wait(void);
char *ui_input_take(int *queued);
const char *ui_input_peek(void);
void ui_input_set_idle(int idle);
void ui_input_tick(void);
void ui_input_resize(void);
void ui_status_set(const char *s);
void ui_output_suspend(void);
void ui_output_resume(void);

void ui_session_resumed(const char *id, int items, const char *path);
void ui_session_list(const orc_session_info *rows, size_t count);

#endif
