#ifndef ORC_AUTH_H
#define ORC_AUTH_H

#include <cJSON.h>

/* Shared credentials store: auth.json in orc_home, one section per provider. */

/* Parsed store root, or NULL. If out_path, receives the malloc'd path. */
cJSON *auth_store_load(char **out_path);

/* Replace provider's section (ownership passes) and rewrite the store.
 * Returns 0 on success. */
int auth_store_put(const char *provider, cJSON *section);

#endif
