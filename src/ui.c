#include "ui.h"

#include "ansi.h"
#include "commands.h"
#include "event.h"
#include "loop.h"
#include "orc.h"
#include "render.h"
#include "util.h"

#include <limits.h>
#include <locale.h>
#include <ncurses.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <wchar.h>
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

typedef struct qnode {
    char *s;
    int queued;
    struct qnode *next;
} qnode;

typedef struct {
    SCREEN *screen;
    char buf[8192];
    size_t len, pos;
    char **history;
    int history_n, history_cap, history_pos;
    char *history_path;
    int active, idle, eof, hidden, spin;
    int menu_n, menu_sel, below_rows;
    char status[256];
    qnode *head, *tail;
} ui_input_state;

static ui_input_state in = {.idle = 1};
static const char *spinner[] = {"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"};

#define MENU_MAX 10
typedef struct {
    const char *name;
    const char *args;
    const char *desc;
    char insert[160];
} menu_item;
static menu_item menu[MENU_MAX];

static const char *model_arg(const char *buf) {
    if (strncmp(buf, "/model ", 7) != 0) return NULL;
    const char *p = buf + 7;
    while (*p == ' ') p++;
    return strpbrk(p, " \t") ? NULL : p;
}

static const char *model_str(cJSON *model, const char *key) {
    cJSON *value = cJSON_GetObjectItem(model, key);
    return cJSON_IsString(value) ? value->valuestring : "";
}

static int menu_collect(void) {
    int n = 0;
    const char *marg = model_arg(in.buf);
    if (in.buf[0] == '/' && !strpbrk(in.buf, " \t")) {
        size_t len = strlen(in.buf);
        for (const orc_cmd *cmd = orc_cmds; cmd->name && n < MENU_MAX; cmd++) {
            if (strncmp(cmd->name, in.buf, len) != 0) continue;
            menu[n].name = cmd->name;
            menu[n].args = cmd->args;
            menu[n].desc = cmd->desc;
            snprintf(menu[n].insert, sizeof menu[n].insert, "%s", cmd->name);
            n++;
        }
    } else if (marg) {
        cJSON *model;
        size_t len = strlen(marg);
        cJSON_ArrayForEach(model, commands_models()) {
            const char *slug = model_str(model, "slug");
            if (n >= MENU_MAX || strncmp(slug, marg, len) != 0) continue;
            menu[n].name = slug;
            menu[n].args = NULL;
            menu[n].desc = model_str(model, "description");
            snprintf(menu[n].insert, sizeof menu[n].insert, "/model %s", slug);
            n++;
        }
    }
    return n;
}

static int terminal_cols(void) {
    int rows, cols;
    getmaxyx(stdscr, rows, cols);
    (void)rows;
    return cols > 0 ? cols : 80;
}

static int clip_bytes(const char *s, int max) {
    int len = (int)strlen(s);
    if (len <= max) return len;
    while (max > 0 && ((unsigned char)s[max] & 0xc0) == 0x80) max--;
    return max;
}

static void print_rule(int cols) {
    fputs(ANSI_DIM, stdout);
    for (int i = 1; i < cols; i++) fputs("─", stdout);
    fputs(ANSI_RESET, stdout);
}

static void input_draw(void) {
    if (!in.active || in.hidden) return;
    int cols = terminal_cols();
    print_rule(cols);
    fputs("\n\r\x1b[2K", stdout);
    const char *prompt = in.idle ? ">" : spinner[in.spin];
    printf(ANSI_BOLD_CYAN "%s" ANSI_RESET " ", prompt);

    int room = cols - 3;
    size_t start = 0;
    if ((int)in.pos > room) start = in.pos - (size_t)room;
    while (start < in.len && ((unsigned char)in.buf[start] & 0xc0) == 0x80) start++;
    int shown = clip_bytes(in.buf + start, room);
    fwrite(in.buf + start, 1, (size_t)shown, stdout);
    fputs("\x1b[K", stdout);

    int rows = 0;
    int desc_room = cols - 30;
    for (int i = 0; i < in.menu_n; i++, rows++) {
        fputs("\n\r\x1b[2K", stdout);
        if (i == in.menu_sel) fputs(ANSI_REVERSE, stdout);
        if (menu[i].args)
            printf("  %-8s %-16s", menu[i].name, menu[i].args);
        else
            printf("%c %-25s", strcmp(menu[i].name, commands_current_model()) == 0 ? '*' : ' ', menu[i].name);
        if (desc_room > 0)
            printf(ANSI_DIM "%.*s", clip_bytes(menu[i].desc, desc_room), menu[i].desc);
        fputs(ANSI_RESET, stdout);
    }
    if (in.status[0]) {
        fputs("\n\r\x1b[2K", stdout);
        print_rule(cols);
        fputs("\n\r\x1b[2K\n\r\x1b[2K" ANSI_DIM, stdout);
        printf("%.*s" ANSI_RESET, clip_bytes(in.status, cols - 1), in.status);
        rows += 3;
    }
    if (rows) printf("\x1b[%dA", rows);
    fputc('\r', stdout);
    int cursor = 2 + (int)(in.pos - start);
    if (cursor > 0) printf("\x1b[%dC", cursor);
    fflush(stdout);
    in.below_rows = rows;
}

static void input_erase(void) {
    if (!in.active || in.hidden) return;
    if (in.below_rows) printf("\x1b[%dB", in.below_rows);
    fputs("\r\x1b[J", stdout);
    printf("\x1b[%dA\r\x1b[J", in.below_rows + 1);
    fflush(stdout);
    in.hidden = 1;
}

static void input_redraw(void) {
    if (!in.active || !in.hidden) return;
    in.hidden = 0;
    input_draw();
}

static void menu_update(void) {
    in.menu_n = menu_collect();
    in.menu_sel = 0;
}

static void history_add(const char *line) {
    if (!*line || (in.history_n && strcmp(in.history[in.history_n - 1], line) == 0)) return;
    if (in.history_n == in.history_cap) {
        int cap = in.history_cap ? in.history_cap * 2 : 64;
        char **next = realloc(in.history, (size_t)cap * sizeof *next);
        if (!next) return;
        in.history = next;
        in.history_cap = cap;
    }
    in.history[in.history_n++] = strdup(line);
    if (in.history_n > 200) {
        free(in.history[0]);
        memmove(in.history, in.history + 1, 199 * sizeof *in.history);
        in.history_n = 200;
    }
    in.history_pos = in.history_n;
}

static void history_load(void) {
    FILE *file = fopen(in.history_path, "r");
    if (!file) return;
    char *line = NULL;
    size_t cap = 0;
    while (getline(&line, &cap, file) >= 0) {
        line[strcspn(line, "\r\n")] = '\0';
        history_add(line);
    }
    free(line);
    fclose(file);
}

static void history_save(void) {
    if (!in.history_path) return;
    FILE *file = fopen(in.history_path, "w");
    if (!file) return;
    for (int i = 0; i < in.history_n; i++) fprintf(file, "%s\n", in.history[i]);
    fclose(file);
}

static void set_buffer(const char *s) {
    snprintf(in.buf, sizeof in.buf, "%s", s);
    in.len = in.pos = strlen(in.buf);
    in.menu_n = 0;
}

static void history_move(int delta) {
    if (!in.history_n) return;
    int pos = in.history_pos + delta;
    if (pos < 0) pos = 0;
    if (pos > in.history_n) pos = in.history_n;
    in.history_pos = pos;
    set_buffer(pos == in.history_n ? "" : in.history[pos]);
}

static void queue_push(char *line) {
    qnode *node = calloc(1, sizeof *node);
    if (!node) { free(line); return; }
    node->s = line;
    node->queued = !in.idle;
    if (in.tail) in.tail->next = node;
    else in.head = node;
    in.tail = node;
}

static void submit(void) {
    if (!in.len) return;
    if (in.menu_n) set_buffer(menu[in.menu_sel].insert);
    char *line = strdup(in.buf);
    if (!line) return;
    input_erase();
    if (in.idle)
        printf(BOLD_CYAN(">") " " ANSI_CYAN "%s" ANSI_RESET "\n", line);
    else
        printf(ANSI_DIM "> %s ⏳" ANSI_RESET "\n", line);
    fflush(stdout);
    history_add(line);
    queue_push(line);
    in.buf[0] = '\0';
    in.len = in.pos = 0;
    in.menu_n = 0;
}

static void insert_bytes(const char *s, size_t n) {
    if (in.len + n >= sizeof in.buf) return;
    memmove(in.buf + in.pos + n, in.buf + in.pos, in.len - in.pos + 1);
    memcpy(in.buf + in.pos, s, n);
    in.pos += n;
    in.len += n;
    menu_update();
}

static size_t prev_char(size_t pos) {
    if (!pos) return 0;
    pos--;
    while (pos && ((unsigned char)in.buf[pos] & 0xc0) == 0x80) pos--;
    return pos;
}

static size_t next_char(size_t pos) {
    if (pos >= in.len) return in.len;
    pos++;
    while (pos < in.len && ((unsigned char)in.buf[pos] & 0xc0) == 0x80) pos++;
    return pos;
}

static void delete_range(size_t start, size_t end) {
    memmove(in.buf + start, in.buf + end, in.len - end + 1);
    in.len -= end - start;
    in.pos = start;
    menu_update();
}

static void handle_key(wint_t key, int code) {
    if (code == KEY_CODE_YES) {
        switch ((int)key) {
        case KEY_LEFT: in.pos = prev_char(in.pos); break;
        case KEY_RIGHT: in.pos = next_char(in.pos); break;
        case KEY_HOME: in.pos = 0; break;
        case KEY_END: in.pos = in.len; break;
        case KEY_BACKSPACE: if (in.pos) delete_range(prev_char(in.pos), in.pos); break;
        case KEY_DC: if (in.pos < in.len) delete_range(in.pos, next_char(in.pos)); break;
        case KEY_UP:
            if (in.menu_n) in.menu_sel = (in.menu_sel + in.menu_n - 1) % in.menu_n;
            else history_move(-1);
            break;
        case KEY_DOWN:
            if (in.menu_n) in.menu_sel = (in.menu_sel + 1) % in.menu_n;
            else history_move(1);
            break;
        case KEY_ENTER: submit(); break;
        }
        return;
    }
    if (key == '\n' || key == '\r') { submit(); return; }
    if (key == 3) {
        in.buf[0] = '\0'; in.len = in.pos = 0; in.menu_n = 0;
        if (!in.idle) g_interrupt = 1;
        return;
    }
    if (key == 4) {
        if (!in.len) { in.eof = 1; if (!in.idle) g_interrupt = 1; }
        else if (in.pos < in.len) delete_range(in.pos, next_char(in.pos));
        return;
    }
    if (key == 27) {
        if (in.menu_n) in.menu_n = 0;
        else if (!in.idle) g_interrupt = 1;
        return;
    }
    if (key == 9) {
        if (in.menu_n) set_buffer(menu[in.menu_sel].insert);
        return;
    }
    if (key == 127 || key == 8) {
        if (in.pos) delete_range(prev_char(in.pos), in.pos);
        return;
    }
    if (key >= 32 && key != 127) {
        char bytes[MB_LEN_MAX];
        mbstate_t state = {0};
        size_t n = wcrtomb(bytes, (wchar_t)key, &state);
        if (n != (size_t)-1) insert_bytes(bytes, n);
    }
}

void ui_input_init(void) {
    if (!isatty(0) || !isatty(1) || in.active) return;
    setlocale(LC_CTYPE, "");
    in.screen = newterm(NULL, stdout, stdin);
    if (!in.screen) return;
    set_term(in.screen);
    raw();
    noecho();
    nonl();
    keypad(stdscr, TRUE);
    nodelay(stdscr, TRUE);
    set_escdelay(25);
    in.history_path = orc_path("history");
    history_load();
    in.active = 1;
    input_draw();
}

static void input_shutdown(void) {
    if (!in.screen) return;
    input_erase();
    history_save();
    endwin();
    delscreen(in.screen);
    in.screen = NULL;
    in.active = 0;
    for (int i = 0; i < in.history_n; i++) free(in.history[i]);
    free(in.history);
    free(in.history_path);
    while (in.head) { qnode *next = in.head->next; free(in.head->s); free(in.head); in.head = next; }
}

int ui_input_active(void) { return in.active; }
static int input_fd(void) { return in.active ? 0 : -1; }
int ui_input_eof(void) { return in.eof; }

static void input_drain(void) {
    if (!in.active || in.eof) return;
    input_erase();
    wint_t key;
    int code;
    while ((code = wget_wch(stdscr, &key)) != ERR) handle_key(key, code);
    input_redraw();
}

void ui_input_tick(void) {
    if (in.idle || !in.active || in.hidden) return;
    input_erase();
    in.spin = (in.spin + 1) % (int)(sizeof spinner / sizeof *spinner);
    input_redraw();
}

void ui_input_resize(void) {
    if (!in.active) return;
    input_erase();
    resize_term(0, 0);
    input_redraw();
}

void ui_status_set(const char *s) {
    if (in.active && !in.hidden) input_erase();
    snprintf(in.status, sizeof in.status, "%s", s ? s : "");
    if (in.active) input_redraw();
}

void ui_input_set_idle(int idle) {
    if (in.active && !in.hidden) input_erase();
    in.idle = idle;
    in.spin = 0;
    if (in.active) input_redraw();
}

void ui_input_wait(void) {
    while (!in.head && !in.eof && !g_interrupt) loop_run_once();
}

const char *ui_input_peek(void) { return in.head ? in.head->s : NULL; }

char *ui_input_take(int *queued) {
    if (!in.head) return NULL;
    qnode *node = in.head;
    in.head = node->next;
    if (!in.head) in.tail = NULL;
    char *line = node->s;
    if (queued) *queued = node->queued;
    free(node);
    return line;
}

void ui_output_suspend(void) { input_erase(); }
void ui_output_resume(void) { input_redraw(); }

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
    input_shutdown();
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
    return ui_input_peek();
}

static char *agent_queue_take(void *ctx) {
    (void)ctx;
    return ui_input_take(NULL);
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
