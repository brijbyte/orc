#ifndef ORC_AGENT_H
#define ORC_AGENT_H

#include <cJSON.h>
#include "orc.h"
#include "provider.h"
#include "session.h"

/* Agent events and queued input. The UI supplies this interface. */
typedef struct {
    int (*turn_begin)(void *ctx);
    void (*text_delta)(void *ctx, const char *text);
    void (*thinking_delta)(void *ctx, const char *text);
    void (*turn_end)(void *ctx);
    void (*tool_call)(void *ctx, cJSON *call);
    void (*user_line)(void *ctx, const char *line);
    void (*replay)(void *ctx, cJSON *history);
    void (*usage)(void *ctx, long long tokens);
    void (*queue_drain)(void *ctx);
    const char *(*queue_peek)(void *ctx);
    char *(*queue_take)(void *ctx);
} agent_io;

typedef struct {
    cJSON *history;   /* array of Responses-API input items (owned) */
    cJSON *tools;     /* array of tool definitions (owned) */
    orc_cfg *cfg;
    orc_session *sess;
    const provider *prov;
    const agent_io *io;
    void *io_ctx;
} agent;

int agent_init(agent *ag, orc_cfg *cfg, const provider *prov,
               orc_session *sess, cJSON *resumed_history,
               const agent_io *io, void *io_ctx);
/* Run one user turn to completion (including tool rounds).
 * Returns 0 done, 1 interrupted, -1 error. */
int agent_turn(agent *ag, const char *user_text);

/* Print the tail of a resumed session as if it had just streamed. */
void agent_replay(agent *ag);
void agent_free(agent *ag);

#endif
