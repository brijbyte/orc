#ifndef ORC_AGENT_H
#define ORC_AGENT_H

#include <cJSON.h>
#include "orc.h"
#include "provider.h"
#include "session.h"

typedef struct {
    cJSON *history;   /* array of Responses-API input items (owned) */
    cJSON *tools;     /* array of tool definitions (owned) */
    orc_cfg *cfg;
    orc_session *sess;
    const provider *prov;
} agent;

int agent_init(agent *ag, orc_cfg *cfg, const provider *prov,
               orc_session *sess, cJSON *resumed_history);
/* Run one user turn to completion (including tool rounds).
 * Returns 0 done, 1 interrupted, -1 error. */
int agent_turn(agent *ag, const char *user_text);
void agent_free(agent *ag);

#endif
