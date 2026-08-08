#include "auth.h"
#include "util.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

cJSON *auth_store_load(char **out_path) {
    char *path = orc_path("auth.json");
    char *text = read_file(path, NULL);
    cJSON *root = text ? cJSON_Parse(text) : NULL;
    free(text);
    if (root && out_path) *out_path = path;
    else free(path);
    return root;
}

int auth_store_put(const char *provider, cJSON *section) {
    cJSON *root = auth_store_load(NULL);
    /* drop legacy flat layout (pre provider-keyed store) */
    if (!cJSON_IsObject(root) || cJSON_GetObjectItem(root, "tokens")) {
        cJSON_Delete(root);
        root = cJSON_CreateObject();
    }
    cJSON_DeleteItemFromObject(root, provider);
    cJSON_AddItemToObject(root, provider, section);

    char *home = orc_home();
    mkdirs(home);
    free(home);
    char *path = orc_path("auth.json");
    char *out = cJSON_Print(root);
    int rc = write_file_atomic(path, out, strlen(out));
    if (rc != 0) fprintf(stderr, "❌ orc: failed to write %s\n", path);
    free(out);
    free(path);
    cJSON_Delete(root);
    return rc;
}
