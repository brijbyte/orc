#include "agent.h"
#include "provider.h"
#include "render.h"
#include "tools.h"
#include "util.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define A_DIM "\x1b[2m"
#define A_RESET "\x1b[0m"

typedef struct {
    md_render md;
    cJSON *pending;   /* items received this request */
    int thinking_open;
    int tty;
} turn_ui;

static void ui_text_delta(const char *s, void *ud) {
    turn_ui *ui = ud;
    if (ui->thinking_open) {
        if (ui->tty) fputs(A_RESET, stdout);
        fputs("\n", stdout);
        ui->thinking_open = 0;
    }
    md_delta(&ui->md, s);
}

static void ui_thinking_delta(const char *s, void *ud) {
    turn_ui *ui = ud;
    if (!ui->thinking_open) {
        if (ui->tty) fputs(A_DIM, stdout);
        ui->thinking_open = 1;
    }
    fputs(s, stdout);
    fflush(stdout);
}

static void ui_item_done(cJSON *item, void *ud) {
    turn_ui *ui = ud;
    cJSON_AddItemToArray(ui->pending, item);
}

int agent_init(agent *ag, orc_cfg *cfg, const provider *prov,
               orc_session *sess, cJSON *resumed_history) {
    ag->cfg = cfg;
    ag->prov = prov;
    ag->sess = sess;
    ag->history = resumed_history ? resumed_history : cJSON_CreateArray();
    ag->tools = cJSON_Parse(tools_schema_json());
    if (!ag->tools) {
        fprintf(stderr, "orc: internal: bad tools schema\n");
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

static void run_call(agent *ag, cJSON *call, int tty) {
    const char *name = item_str(call, "name");
    const char *arguments = item_str(call, "arguments");
    const char *call_id = item_str(call, "call_id");
    if (!name || !call_id) return;

    cJSON *args = arguments ? cJSON_Parse(arguments) : NULL;

    /* Show what's running: tool name + first line of the key argument. */
    const char *desc = "";
    if (args) {
        cJSON *a = cJSON_GetObjectItem(args, "cmd");
        if (!a) a = cJSON_GetObjectItem(args, "path");
        if (cJSON_IsString(a)) desc = a->valuestring;
    }
    if (tty)
        printf(A_DIM "→ %s %.100s" A_RESET "\n", name, desc);
    else
        printf("→ %s %.100s\n", name, desc);
    fflush(stdout);

    char *output = tool_run(name, args);
    if (args) cJSON_Delete(args);

    cJSON *out = cJSON_CreateObject();
    cJSON_AddStringToObject(out, "type", "function_call_output");
    cJSON_AddStringToObject(out, "call_id", call_id);
    cJSON_AddStringToObject(out, "output", output);
    free(output);
    commit(ag, out);
}

int agent_turn(agent *ag, const char *user_text) {
    commit(ag, user_message(user_text));

    for (;;) {
        turn_ui ui;
        md_init(&ui.md);
        ui.pending = cJSON_CreateArray();
        ui.thinking_open = 0;
        ui.tty = isatty(1);

        provider_cb cb = {
            .on_text_delta = ui_text_delta,
            .on_thinking_delta = ui_thinking_delta,
            .on_item_done = ui_item_done,
        };
        int rc = ag->prov->turn(ag->history, ag->tools, ag->cfg, &cb, &ui);

        if (ui.thinking_open && ui.tty) fputs(A_RESET, stdout);
        md_flush(&ui.md);
        md_free(&ui.md);
        fputs("\n", stdout);

        if (rc != PROVIDER_OK) {
            cJSON_Delete(ui.pending);  /* discard uncommitted turn */
            return rc == PROVIDER_INTERRUPTED ? 1 : -1;
        }

        /* Commit items in order; collect indices of function calls. */
        int ncalls = 0;
        cJSON *calls[64];
        while (cJSON_GetArraySize(ui.pending) > 0) {
            cJSON *item = cJSON_DetachItemFromArray(ui.pending, 0);
            commit(ag, item);
            const char *type = item_str(item, "type");
            if (type && strcmp(type, "function_call") == 0 && ncalls < 64)
                calls[ncalls++] = item;
        }
        cJSON_Delete(ui.pending);

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
                run_call(ag, calls[i], isatty(1));
            }
        }
        if (interrupted) return 1;
    }
}
