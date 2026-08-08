#ifndef ORC_COMMANDS_H
#define ORC_COMMANDS_H

#include "agent.h"

typedef struct {
    const char *name; /* includes the leading slash */
    const char *args; /* "" when the command takes none */
    const char *desc;
} orc_cmd;

extern const orc_cmd orc_cmds[]; /* terminated by name == NULL */

/* Remember the active provider and config (for /model listing/completion). */
void commands_init(const provider *prov, const orc_cfg *cfg);

/* Currently selected model slug ("" before commands_init). */
const char *commands_current_model(void);

/* Provider's selectable models ({"slug","description"} array, cached here)
 * or NULL when the provider cannot list them. */
cJSON *commands_models(void);

/* Handle a REPL line. Returns 0: not a command (send to the model),
 * 1: handled here, 2: quit. */
int command_dispatch(agent *ag, const char *line);

#endif
