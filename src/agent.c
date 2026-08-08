#include "agent.h"

#include "instructions.h"
#include "tools.h"

#include <stdlib.h>
#include <string.h>

typedef struct {
    agent *ag;
    cJSON *pending;
} turn_state;

static void on_text_delta(const char *text, void *ud) {
    turn_state *state = ud;
    state->ag->io->text_delta(state->ag->io_ctx, text);
}

static void on_thinking_delta(const char *text, void *ud) {
    turn_state *state = ud;
    state->ag->io->thinking_delta(state->ag->io_ctx, text);
}

static void on_item_done(cJSON *item, void *ud) {
    turn_state *state = ud;
    cJSON_AddItemToArray(state->pending, item);
}

static void on_usage(long long ctx_tokens, void *ud) {
    turn_state *state = ud;
    session_set_ctx(state->ag->sess, ctx_tokens);
    state->ag->io->usage(state->ag->io_ctx, ctx_tokens);
}

int agent_init(agent *ag, orc_cfg *cfg, const provider *prov,
               orc_session *sess, cJSON *resumed_history,
               const agent_io *io, void *io_ctx) {
    memset(ag, 0, sizeof *ag);
    ag->cfg = cfg;
    ag->prov = prov;
    ag->sess = sess;
    ag->io = io;
    ag->io_ctx = io_ctx;
    ag->history = resumed_history ? resumed_history : cJSON_CreateArray();
    ag->tools = cJSON_Parse(tools_schema_json());
    if (!ag->history || !ag->tools) {
        agent_free(ag);
        return -1;
    }
    return 0;
}

void agent_free(agent *ag) {
    cJSON_Delete(ag->history);
    cJSON_Delete(ag->tools);
    ag->history = ag->tools = NULL;
}

static cJSON *user_message(const char *text) {
    cJSON *item = cJSON_CreateObject();
    cJSON_AddStringToObject(item, "type", "message");
    cJSON_AddStringToObject(item, "role", "user");
    cJSON *content = cJSON_CreateArray();
    cJSON *part = cJSON_CreateObject();
    cJSON_AddStringToObject(part, "type", "input_text");
    cJSON_AddStringToObject(part, "text", text);
    cJSON_AddItemToArray(content, part);
    cJSON_AddItemToObject(item, "content", content);
    return item;
}

static void commit(agent *ag, cJSON *item) {
    session_append(ag->sess, item);
    cJSON_AddItemToArray(ag->history, item);
}

static const char *item_str(cJSON *item, const char *key) {
    cJSON *v = cJSON_GetObjectItem(item, key);
    return cJSON_IsString(v) ? v->valuestring : NULL;
}

static void run_call(agent *ag, cJSON *call) {
    const char *name = item_str(call, "name");
    const char *arguments = item_str(call, "arguments");
    const char *call_id = item_str(call, "call_id");
    if (!name || !call_id) return;

    cJSON *args = arguments ? cJSON_Parse(arguments) : NULL;
    ag->io->tool_call(ag->io_ctx, call);
    char *output = tool_run(name, args);
    cJSON_Delete(args);

    cJSON *out = cJSON_CreateObject();
    cJSON_AddStringToObject(out, "type", "function_call_output");
    cJSON_AddStringToObject(out, "call_id", call_id);
    cJSON_AddStringToObject(out, "output", output);
    free(output);
    commit(ag, out);
}

/* Slash command or exit word: runs after the turn, never sent as steering.
 * Mirrors command_dispatch: "/tmp/x" is a path, not a command. */
static int is_control_line(const char *s) {
    if (strcmp(s, "exit") == 0 || strcmp(s, "quit") == 0) return 1;
    if (s[0] != '/') return 0;
    size_t n = strcspn(s, " \t");
    return memchr(s + 1, '/', n - 1) == NULL;
}

/* Inject lines queued during the turn as user messages, so the model sees
 * them at the next request (pi-style steering between tool rounds). */
static void steer(agent *ag) {
    ag->io->queue_drain(ag->io_ctx);
    const char *peek;
    while ((peek = ag->io->queue_peek(ag->io_ctx)) && !is_control_line(peek)) {
        char *line = ag->io->queue_take(ag->io_ctx);
        ag->io->user_line(ag->io_ctx, line);
        commit(ag, user_message(line));
        free(line);
    }
}

void agent_replay(agent *ag) {
    ag->io->replay(ag->io_ctx, ag->history);
}

int agent_turn(agent *ag, const char *user_text) {
    if (!ag->cfg->instructions) {
        ag->cfg->instructions = instructions_build();
        if (!ag->cfg->instructions) return -1;
    }
    commit(ag, user_message(user_text));

    for (;;) {
        turn_state state = {
            .ag = ag,
            .pending = cJSON_CreateArray(),
        };
        if (!state.pending || ag->io->turn_begin(ag->io_ctx) != 0) {
            cJSON_Delete(state.pending);
            return -1;
        }
        provider_cb cb = {
            .on_text_delta = on_text_delta,
            .on_thinking_delta = on_thinking_delta,
            .on_item_done = on_item_done,
            .on_usage = on_usage,
        };
        int rc = ag->prov->turn(ag->history, ag->tools, ag->cfg, &cb, &state);
        ag->io->turn_end(ag->io_ctx);

        if (rc != PROVIDER_OK) {
            cJSON_Delete(state.pending);
            return rc == PROVIDER_INTERRUPTED ? 1 : -1;
        }

        /* Commit items in order; collect indices of function calls. */
        int ncalls = 0;
        cJSON *calls[64];
        while (cJSON_GetArraySize(state.pending) > 0) {
            cJSON *item = cJSON_DetachItemFromArray(state.pending, 0);
            commit(ag, item);
            const char *type = item_str(item, "type");
            if (type && strcmp(type, "function_call") == 0 && ncalls < 64)
                calls[ncalls++] = item;
        }
        cJSON_Delete(state.pending);

        if (ncalls == 0) return 0;

        int interrupted = 0;
        for (int i = 0; i < ncalls; i++) {
            if (g_interrupt) interrupted = 1;
            if (interrupted) {
                /* Committed calls must still get outputs or the next request 400s. */
                const char *call_id = item_str(calls[i], "call_id");
                cJSON *out = cJSON_CreateObject();
                cJSON_AddStringToObject(out, "type", "function_call_output");
                cJSON_AddStringToObject(out, "call_id", call_id ? call_id : "");
                cJSON_AddStringToObject(out, "output", "[interrupted by user]");
                commit(ag, out);
            } else {
                run_call(ag, calls[i]);
            }
        }
        if (interrupted) return 1;
        steer(ag);
    }
}
