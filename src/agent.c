#include "agent.h"
#include "ansi.h"
#include "input.h"
#include "provider.h"
#include "render.h"
#include "tools.h"
#include "util.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

typedef struct {
    md_render md;
    cJSON *pending;   /* items received this request */
    strbuf think;     /* partial thinking line (emitted per whole line) */
    int thinking_open;
    int tty;
} turn_ui;

/* Emit one whole dim thinking line, keeping the input line below it. */
static void put_think_line(turn_ui *ui, const char *s, size_t n) {
    input_erase();
    if (ui->tty) fputs(ANSI_DIM, stdout);
    fwrite(s, 1, n, stdout);
    if (ui->tty) fputs(ANSI_RESET, stdout);
    fputc('\n', stdout);
    fflush(stdout);
    input_redraw();
}

/* Flush any partial thinking line (turn end or thinking -> text switch). */
static void think_flush(turn_ui *ui) {
    if (ui->think.len)
        put_think_line(ui, ui->think.data, ui->think.len);
    ui->think.len = 0;
}

static void ui_text_delta(const char *s, void *ud) {
    turn_ui *ui = ud;
    if (ui->thinking_open) {
        think_flush(ui);
        input_erase();
        fputs("\n", stdout);
        input_redraw();
        ui->thinking_open = 0;
    }
    input_erase();
    md_delta(&ui->md, s);
    input_redraw();
}

static void ui_thinking_delta(const char *s, void *ud) {
    turn_ui *ui = ud;
    ui->thinking_open = 1;
    if (!ui->tty) { /* no line editor to protect; stream as-is */
        fputs(s, stdout);
        fflush(stdout);
        return;
    }
    sb_append(&ui->think, s, strlen(s));
    char *nl;
    while (ui->think.len &&
           (nl = memchr(ui->think.data, '\n', ui->think.len))) {
        size_t n = (size_t)(nl - ui->think.data);
        put_think_line(ui, ui->think.data, n);
        memmove(ui->think.data, nl + 1, ui->think.len - n - 1);
        ui->think.len -= n + 1;
    }
}

static void ui_item_done(cJSON *item, void *ud) {
    turn_ui *ui = ud;
    cJSON_AddItemToArray(ui->pending, item);
}

int agent_init(agent *ag, orc_cfg *cfg, const provider *prov,
               orc_session *sess, cJSON *resumed_history) {
    memset(ag, 0, sizeof *ag);
    ag->cfg = cfg;
    ag->prov = prov;
    ag->sess = sess;
    ag->history = resumed_history ? resumed_history : cJSON_CreateArray();
    ag->tools = cJSON_Parse(tools_schema_json());
    if (!ag->history || !ag->tools) {
        fprintf(stderr, "❌ orc: cannot initialize agent\n");
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

/* Show a tool call: name + first line of the key argument. */
static void print_call(cJSON *call, int tty) {
    const char *name = item_str(call, "name");
    const char *arguments = item_str(call, "arguments");
    if (!name) return;
    cJSON *args = arguments ? cJSON_Parse(arguments) : NULL;
    const char *desc = "";
    if (args) {
        cJSON *a = cJSON_GetObjectItem(args, "cmd");
        if (!a) a = cJSON_GetObjectItem(args, "path");
        if (cJSON_IsString(a)) desc = a->valuestring;
    }
    if (tty)
        printf(DIM("🔧 %s %.100s") "\n", name, desc);
    else
        printf("🔧 %s %.100s\n", name, desc);
    if (args) cJSON_Delete(args);
}

static void run_call(agent *ag, cJSON *call, int tty) {
    const char *name = item_str(call, "name");
    const char *arguments = item_str(call, "arguments");
    const char *call_id = item_str(call, "call_id");
    if (!name || !call_id) return;

    cJSON *args = arguments ? cJSON_Parse(arguments) : NULL;

    input_erase();
    print_call(call, tty);
    fflush(stdout);
    input_redraw();

    char *output = tool_run(name, args);
    if (args) cJSON_Delete(args);

    cJSON *out = cJSON_CreateObject();
    cJSON_AddStringToObject(out, "type", "function_call_output");
    cJSON_AddStringToObject(out, "call_id", call_id);
    cJSON_AddStringToObject(out, "output", output);
    free(output);
    commit(ag, out);
}

/* Concatenated text parts of a message item; malloc'd, NULL if none. */
static char *message_text(cJSON *item) {
    strbuf sb;
    sb_init(&sb);
    cJSON *part;
    cJSON_ArrayForEach(part, cJSON_GetObjectItem(item, "content")) {
        cJSON *t = cJSON_GetObjectItem(part, "text");
        if (cJSON_IsString(t)) sb_append_str(&sb, t->valuestring);
    }
    return sb.data;
}

#define REPLAY_MAX 30
void agent_replay(agent *ag) {
    int n = cJSON_GetArraySize(ag->history);
    if (n == 0) return;

    /* Start at the second-to-last user message: the last full exchange. */
    int start = 0, users = 0;
    for (int i = n - 1; i >= 0 && users < 2; i--) {
        cJSON *it = cJSON_GetArrayItem(ag->history, i);
        const char *type = item_str(it, "type");
        const char *role = item_str(it, "role");
        if (type && strcmp(type, "message") == 0 &&
            role && strcmp(role, "user") == 0) {
            start = i;
            users++;
        }
    }
    if (n - start > REPLAY_MAX) start = n - REPLAY_MAX;

    int tty = isatty(1);
    if (start > 0)
        printf(tty ? DIM("📚 %d earlier items") "\n" : "📚 %d earlier items\n",
               start);
    for (int i = start; i < n; i++) {
        cJSON *it = cJSON_GetArrayItem(ag->history, i);
        const char *type = item_str(it, "type");
        if (!type) continue;
        if (strcmp(type, "function_call") == 0) {
            print_call(it, tty);
        } else if (strcmp(type, "message") == 0) {
            char *text = message_text(it);
            if (!text) continue;
            const char *role = item_str(it, "role");
            if (role && strcmp(role, "user") == 0) {
                printf(tty ? BOLD_CYAN(">") " %s\n" : "> %s\n", text);
            } else {
                md_render md;
                md_init(&md);
                md_delta(&md, text);
                md_flush(&md);
                md_free(&md);
                fputs("\n", stdout);
            }
            free(text);
        } /* reasoning and function_call_output items stay silent, as live */
    }
    fflush(stdout);
}

int agent_turn(agent *ag, const char *user_text) {
    commit(ag, user_message(user_text));

    for (;;) {
        turn_ui ui;
        md_init(&ui.md);
        ui.pending = cJSON_CreateArray();
        sb_init(&ui.think);
        ui.thinking_open = 0;
        ui.tty = isatty(1);

        provider_cb cb = {
            .on_text_delta = ui_text_delta,
            .on_thinking_delta = ui_thinking_delta,
            .on_item_done = ui_item_done,
        };
        int rc = ag->prov->turn(ag->history, ag->tools, ag->cfg, &cb, &ui);

        think_flush(&ui);
        sb_free(&ui.think);
        input_erase();
        md_flush(&ui.md);
        md_free(&ui.md);
        fputs("\n", stdout);
        fflush(stdout);
        input_redraw();

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
