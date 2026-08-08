#include "ui.h"

#include "ansi.h"
#include "commands.h"
#include "event.h"
#include "input.h"
#include "render.h"
#include "util.h"

#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

typedef struct {
    md_render md;
    strbuf think;
    int thinking_open;
    int tty;
    struct timespec started;
} ui_turn;

struct ui {
    ui_turn *turn;
};

static const char *item_str(cJSON *item, const char *key) {
    cJSON *v = cJSON_GetObjectItem(item, key);
    return cJSON_IsString(v) ? v->valuestring : NULL;
}

static void put_think_line(ui_turn *turn, const char *s, size_t n) {
    input_erase();
    if (turn->tty) fputs(ANSI_DIM, stdout);
    fwrite(s, 1, n, stdout);
    if (turn->tty) fputs(ANSI_RESET, stdout);
    fputc('\n', stdout);
    fflush(stdout);
    input_redraw();
}

static void think_flush(ui_turn *turn) {
    if (turn->think.len)
        put_think_line(turn, turn->think.data, turn->think.len);
    turn->think.len = 0;
}

static void think_done(ui_turn *turn) {
    if (!turn->thinking_open) return;
    turn->thinking_open = 0;
    think_flush(turn);
    struct timespec now;
    clock_gettime(CLOCK_MONOTONIC, &now);
    long secs = now.tv_sec - turn->started.tv_sec;
    if (secs < 1) secs = 1;
    input_erase();
    printf(turn->tty ? DIM("✻ thought for %lds") "\n" : "\n✻ thought for %lds\n",
           secs);
    fflush(stdout);
    input_redraw();
}

static ui_turn *ui_turn_begin(void) {
    ui_turn *turn = calloc(1, sizeof *turn);
    if (!turn) return NULL;
    md_init(&turn->md);
    md_set_lead(&turn->md, BOLD("●") " ");
    sb_init(&turn->think);
    turn->tty = isatty(1);
    clock_gettime(CLOCK_MONOTONIC, &turn->started);
    return turn;
}

static void ui_turn_text(ui_turn *turn, const char *text) {
    if (!turn) return;
    if (turn->thinking_open) {
        think_done(turn);
        input_erase();
        fputs("\n", stdout);
        input_redraw();
    }
    input_erase();
    md_delta(&turn->md, text);
    input_redraw();
}

static void ui_turn_thinking(ui_turn *turn, const char *text) {
    if (!turn) return;
    turn->thinking_open = 1;
    if (!turn->tty) {
        fputs(text, stdout);
        fflush(stdout);
        return;
    }
    sb_append(&turn->think, text, strlen(text));
    char *nl;
    while (turn->think.len &&
           (nl = memchr(turn->think.data, '\n', turn->think.len))) {
        size_t n = (size_t)(nl - turn->think.data);
        put_think_line(turn, turn->think.data, n);
        memmove(turn->think.data, nl + 1, turn->think.len - n - 1);
        turn->think.len -= n + 1;
    }
}

static void ui_turn_end(ui_turn *turn) {
    if (!turn) return;
    think_done(turn);
    think_flush(turn);
    sb_free(&turn->think);
    input_erase();
    md_flush(&turn->md);
    md_free(&turn->md);
    fputs("\n", stdout);
    fflush(stdout);
    input_redraw();
    free(turn);
}

static const char *tool_icon(const char *name) {
    if (strcmp(name, "bash") == 0) return "💻";
    if (strcmp(name, "process") == 0) return "⚙️";
    if (strcmp(name, "read") == 0) return "📖";
    if (strcmp(name, "write") == 0) return "📝";
    if (strcmp(name, "edit") == 0) return "✏️";
    if (strcmp(name, "skill") == 0) return "🧠";
    return "🔧";
}

static const char *display_path(const char *path, char resolved[PATH_MAX]) {
    if (path[0] != '/') return path;
    const char *full = realpath(path, resolved);
    if (!full) full = path;
    char cwd[PATH_MAX];
    if (!getcwd(cwd, sizeof cwd)) return path;
    size_t n = strlen(cwd);
    if (strcmp(full, cwd) == 0) return ".";
    if (n == 1 || (strncmp(full, cwd, n) == 0 && full[n] == '/'))
        return full + n + (n > 1);
    return path;
}

static void ui_tool_call(cJSON *call) {
    const char *name = item_str(call, "name");
    const char *arguments = item_str(call, "arguments");
    if (!name) return;
    cJSON *args = arguments ? cJSON_Parse(arguments) : NULL;
    const char *desc = "";
    char resolved[PATH_MAX];
    if (args) {
        cJSON *arg = cJSON_GetObjectItem(args, "cmd");
        if (cJSON_IsString(arg)) {
            desc = arg->valuestring;
        } else {
            arg = cJSON_GetObjectItem(args, "path");
            if (cJSON_IsString(arg))
                desc = display_path(arg->valuestring, resolved);
        }
    }
    input_erase();
    const char *icon = tool_icon(name);
    printf(isatty(1) ? DIM("%s %s %.100s") "\n" : "%s %s %.100s\n",
           icon, name, desc);
    fflush(stdout);
    input_redraw();
    cJSON_Delete(args);
}

static void ui_user_line(const char *line) {
    int tty = isatty(1);
    input_erase();
    printf(tty ? BOLD_CYAN(">") " " ANSI_CYAN "%s" ANSI_RESET "\n"
               : "> %s\n",
           line);
    fflush(stdout);
    input_redraw();
}

static char *message_text(cJSON *item) {
    strbuf sb;
    sb_init(&sb);
    cJSON *part;
    cJSON_ArrayForEach(part, cJSON_GetObjectItem(item, "content")) {
        cJSON *text = cJSON_GetObjectItem(part, "text");
        if (cJSON_IsString(text)) sb_append_str(&sb, text->valuestring);
    }
    return sb.data;
}

#define REPLAY_MAX 30
static void ui_replay(cJSON *history) {
    int n = cJSON_GetArraySize(history);
    if (n == 0) return;
    int start = 0, users = 0;
    for (int i = n - 1; i >= 0 && users < 2; i--) {
        cJSON *item = cJSON_GetArrayItem(history, i);
        const char *type = item_str(item, "type");
        const char *role = item_str(item, "role");
        if (type && strcmp(type, "message") == 0 && role &&
            strcmp(role, "user") == 0) {
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
        cJSON *item = cJSON_GetArrayItem(history, i);
        const char *type = item_str(item, "type");
        if (!type) continue;
        if (strcmp(type, "function_call") == 0) {
            ui_tool_call(item);
        } else if (strcmp(type, "message") == 0) {
            char *text = message_text(item);
            if (!text) continue;
            const char *role = item_str(item, "role");
            if (role && strcmp(role, "user") == 0) {
                ui_user_line(text);
            } else {
                md_render md;
                md_init(&md);
                md_set_lead(&md, BOLD("●") " ");
                md_delta(&md, text);
                md_flush(&md);
                md_free(&md);
                fputs("\n", stdout);
            }
            free(text);
        }
    }
    fflush(stdout);
}

void ui_session_resumed(const char *id, int items, const char *path) {
    if (isatty(2))
        fprintf(stderr, "↩️  orc: resumed " BOLD("%.8s") DIM(" (%d items) %s") "\n",
                id, items, path);
    else
        fprintf(stderr, "↩️  orc: resumed %.8s (%d items) %s\n", id, items, path);
}

void ui_session_list(const orc_session_info *rows, size_t count) {
    if (count == 0) {
        puts("📭 no sessions");
        return;
    }
    int tty = isatty(1);
    for (size_t i = 0; i < count; i++)
        printf(tty ? CYAN("%-8s") "  " DIM("%-16s") "  %s\n"
                   : "%-8s  %-16s  %s\n",
               rows[i].id, rows[i].when, rows[i].title);
}


ui *ui_create(void) {
    ui *state = calloc(1, sizeof *state);
    if (state) event_source_set(input_fd, input_drain);
    return state;
}

void ui_free(ui *state) {
    if (!state) return;
    event_source_set(NULL, NULL);
    ui_turn_end(state->turn);
    free(state);
}

static int agent_turn_begin(void *ctx) {
    ui *state = ctx;
    state->turn = ui_turn_begin();
    return state->turn ? 0 : -1;
}

static void agent_text_delta(void *ctx, const char *text) {
    ui *state = ctx;
    ui_turn_text(state->turn, text);
}

static void agent_thinking_delta(void *ctx, const char *text) {
    ui *state = ctx;
    ui_turn_thinking(state->turn, text);
}

static void agent_turn_end(void *ctx) {
    ui *state = ctx;
    ui_turn_end(state->turn);
    state->turn = NULL;
}

static void agent_tool_call(void *ctx, cJSON *call) {
    (void)ctx;
    ui_tool_call(call);
}

static void agent_user_line(void *ctx, const char *line) {
    (void)ctx;
    ui_user_line(line);
}

static void agent_history_replay(void *ctx, cJSON *history) {
    (void)ctx;
    ui_replay(history);
}

static void agent_usage(void *ctx, long long tokens) {
    (void)ctx;
    commands_ctx_used(tokens);
}

static void agent_queue_drain(void *ctx) {
    (void)ctx;
    input_drain();
}

static const char *agent_queue_peek(void *ctx) {
    (void)ctx;
    return input_peek();
}

static char *agent_queue_take(void *ctx) {
    (void)ctx;
    return input_take(NULL);
}

const agent_io *ui_agent_io(void) {
    static const agent_io io = {
        .turn_begin = agent_turn_begin,
        .text_delta = agent_text_delta,
        .thinking_delta = agent_thinking_delta,
        .turn_end = agent_turn_end,
        .tool_call = agent_tool_call,
        .user_line = agent_user_line,
        .replay = agent_history_replay,
        .usage = agent_usage,
        .queue_drain = agent_queue_drain,
        .queue_peek = agent_queue_peek,
        .queue_take = agent_queue_take,
    };
    return &io;
}
