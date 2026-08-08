#include "skills.h"
#include "util.h"

#include <ctype.h>
#include <dirent.h>
#include <errno.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/stat.h>
#include <unistd.h>

typedef struct {
    char *name;
    char *description;
    char *path;
} skill;

typedef struct {
    skill *skills;
    size_t len;
    size_t cap;
    char **visited;
    size_t visited_len;
    size_t visited_cap;
    strbuf warnings;
    int loaded;
} skill_index;

static skill_index index_;

static void *grow(void *items, size_t *cap, size_t size) {
    if (*cap == 0) *cap = 16;
    else *cap *= 2;
    void *out = realloc(items, *cap * size);
    if (!out) { perror("realloc"); exit(1); }
    return out;
}

static char *path_join(const char *left, const char *right) {
    size_t n = strlen(left) + strlen(right) + 2;
    char *out = malloc(n);
    if (!out) { perror("malloc"); exit(1); }
    snprintf(out, n, "%s/%s", left, right);
    return out;
}

static int is_dir(const char *path) {
    struct stat st;
    return stat(path, &st) == 0 && S_ISDIR(st.st_mode);
}

static int seen_path(char ***items, size_t *len, size_t *cap, const char *path) {
    for (size_t i = 0; i < *len; i++)
        if (strcmp((*items)[i], path) == 0) return 1;
    if (*len == *cap) *items = grow(*items, cap, sizeof **items);
    (*items)[(*len)++] = strdup(path);
    return 0;
}

static char *trim(char *s) {
    while (isspace((unsigned char)*s)) s++;
    char *end = s + strlen(s);
    while (end > s && isspace((unsigned char)end[-1])) end--;
    *end = '\0';
    return s;
}

static char *scalar(char *s) {
    s = trim(s);
    size_t n = strlen(s);
    if (n >= 2 && ((s[0] == '"' && s[n - 1] == '"') ||
                   (s[0] == '\'' && s[n - 1] == '\''))) {
        s[n - 1] = '\0';
        s++;
    }
    return strdup(s);
}

static int valid_name(const char *name) {
    size_t n = strlen(name);
    if (n == 0 || n > 64 || name[0] == '-' || name[n - 1] == '-') return 0;
    for (size_t i = 0; i < n; i++) {
        unsigned char c = (unsigned char)name[i];
        if (!(islower(c) || isdigit(c) || c == '-')) return 0;
        if (c == '-' && i > 0 && name[i - 1] == '-') return 0;
    }
    return 1;
}

static void warning(const char *path, const char *reason) {
    sb_append_str(&index_.warnings, "warning: ");
    sb_append_str(&index_.warnings, path);
    sb_append_str(&index_.warnings, ": ");
    sb_append_str(&index_.warnings, reason);
    sb_append_str(&index_.warnings, "\n");
}

static int parse_skill(const char *path, char **name, char **description) {
    FILE *f = fopen(path, "r");
    if (!f) { warning(path, "cannot read SKILL.md"); return -1; }

    char line[2048];
    if (!fgets(line, sizeof line, f) || strcmp(trim(line), "---") != 0) {
        fclose(f);
        warning(path, "missing frontmatter");
        return -1;
    }

    int closed = 0, bad_line = 0;
    while (fgets(line, sizeof line, f)) {
        if (!strchr(line, '\n') && !feof(f)) bad_line = 1;
        char *text = trim(line);
        if (strcmp(text, "---") == 0) { closed = 1; break; }
        char *colon = strchr(text, ':');
        if (!colon) continue;
        *colon = '\0';
        char *key = trim(text);
        if (strcmp(key, "name") == 0) {
            free(*name);
            *name = scalar(colon + 1);
        } else if (strcmp(key, "description") == 0) {
            free(*description);
            *description = scalar(colon + 1);
        }
    }
    fclose(f);

    const char *reason = NULL;
    if (bad_line) reason = "frontmatter line is too long";
    else if (!closed) reason = "unterminated frontmatter";
    else if (!*name || !valid_name(*name)) reason = "invalid name";
    else if (!*description || !**description) reason = "missing description";
    else if (strlen(*description) > 1024) reason = "description is too long";
    if (reason) {
        warning(path, reason);
        free(*name); free(*description);
        *name = NULL; *description = NULL;
        return -1;
    }
    return 0;
}

static int have_name(const char *name) {
    for (size_t i = 0; i < index_.len; i++)
        if (strcmp(index_.skills[i].name, name) == 0) return 1;
    return 0;
}

static void add_skill(const char *file) {
    char resolved[PATH_MAX];
    if (!realpath(file, resolved)) return;
    char *name = NULL, *description = NULL;
    if (parse_skill(resolved, &name, &description) != 0) return;
    if (have_name(name)) { free(name); free(description); return; }
    if (index_.len == index_.cap)
        index_.skills = grow(index_.skills, &index_.cap, sizeof *index_.skills);
    index_.skills[index_.len++] = (skill){name, description, strdup(resolved)};
}

static int by_name(const struct dirent **left, const struct dirent **right) {
    return strcmp((*left)->d_name, (*right)->d_name);
}

static void discover_dir(const char *path) {
    char resolved[PATH_MAX];
    if (!realpath(path, resolved) ||
        seen_path(&index_.visited, &index_.visited_len, &index_.visited_cap, resolved))
        return;

    char *file = path_join(resolved, "SKILL.md");
    struct stat st;
    if (stat(file, &st) == 0 && S_ISREG(st.st_mode)) {
        add_skill(file);
        free(file);
        return;
    }
    free(file);

    struct dirent **entries = NULL;
    int count = scandir(resolved, &entries, NULL, by_name);
    if (count < 0) return;
    for (int i = 0; i < count; i++) {
        const char *name = entries[i]->d_name;
        if (name[0] != '.' && strcmp(name, "node_modules") != 0) {
            char *child = path_join(resolved, name);
            if (is_dir(child)) discover_dir(child);
            free(child);
        }
        free(entries[i]);
    }
    free(entries);
}

static void discover_root(const char *root, char ***roots, size_t *len, size_t *cap) {
    char resolved[PATH_MAX];
    if (!realpath(root, resolved) ||
        seen_path(roots, len, cap, resolved)) return;

    struct dirent **entries = NULL;
    int count = scandir(resolved, &entries, NULL, by_name);
    if (count < 0) return;
    for (int i = 0; i < count; i++) {
        const char *name = entries[i]->d_name;
        if (name[0] != '.' && strcmp(name, "node_modules") != 0) {
            char *child = path_join(resolved, name);
            if (is_dir(child)) discover_dir(child);
            free(child);
        }
        free(entries[i]);
    }
    free(entries);
}

static void load_index(void) {
    if (index_.loaded) return;
    index_.loaded = 1;
    sb_init(&index_.warnings);

    char cwd[PATH_MAX], current[PATH_MAX];
    if (!getcwd(cwd, sizeof cwd) || !realpath(cwd, current)) return;
    char **roots = NULL;
    size_t roots_len = 0, roots_cap = 0;

    for (;;) {
        char *root = path_join(current, ".agents/skills");
        discover_root(root, &roots, &roots_len, &roots_cap);
        free(root);

        char *git = path_join(current, ".git");
        struct stat st;
        int at_git_root = lstat(git, &st) == 0;
        free(git);
        if (at_git_root || strcmp(current, "/") == 0) break;
        char *slash = strrchr(current, '/');
        if (slash == current) current[1] = '\0';
        else *slash = '\0';
    }

    const char *home = getenv("HOME");
    if (home && *home) {
        char *global = path_join(home, ".agents/skills");
        discover_root(global, &roots, &roots_len, &roots_cap);
        free(global);
    }
    for (size_t i = 0; i < roots_len; i++) free(roots[i]);
    free(roots);
}

static int contains_case(const char *text, const char *query) {
    size_t n = strlen(query);
    if (n == 0) return 1;
    for (; *text; text++)
        if (strncasecmp(text, query, n) == 0) return 1;
    return 0;
}

static void append_result(strbuf *out, const skill *item) {
    sb_append_str(out, item->name);
    sb_append_str(out, " — ");
    sb_append_str(out, item->description);
    sb_append_str(out, "\n");
    sb_append_str(out, item->path);
    sb_append_str(out, "\n");
}

char *skills_query(const char *query) {
    load_index();
    if (!query || strcmp(query, "*") == 0) query = "";
    strbuf out;
    sb_init(&out);
    if (index_.warnings.len) sb_append(&out, index_.warnings.data, index_.warnings.len);

    int found = 0;
    for (size_t i = 0; i < index_.len; i++) {
        if (strcmp(index_.skills[i].name, query) == 0) {
            append_result(&out, &index_.skills[i]);
            found = 1;
            break;
        }
    }
    if (!found) {
        for (size_t i = 0; i < index_.len; i++) {
            skill *item = &index_.skills[i];
            if (contains_case(item->name, query) || contains_case(item->description, query)) {
                append_result(&out, item);
                found = 1;
            }
        }
    }
    if (!found) sb_append_str(&out, query[0] ? "No matching skills found.\n" : "No skills found.\n");
    return out.data;
}
