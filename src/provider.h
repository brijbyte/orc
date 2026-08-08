#ifndef ORC_PROVIDER_H
#define ORC_PROVIDER_H

#include <cJSON.h>
#include "orc.h"

typedef struct {
    void (*on_text_delta)(const char *s, void *ud);
    void (*on_thinking_delta)(const char *s, void *ud);
    /* Complete output item (message / function_call / reasoning). Ownership passes. */
    void (*on_item_done)(cJSON *item, void *ud);
    /* Optional: tokens now occupying the context (usage total after a request). */
    void (*on_usage)(long long ctx_tokens, void *ud);
} provider_cb;

#define PROVIDER_OK 0
#define PROVIDER_INTERRUPTED 1
#define PROVIDER_ERROR (-1)

/* A provider implements one model backend. History items use the canonical
 * in-memory format (Responses-API input items); a provider that speaks a
 * different wire format translates at request-build/parse time only. */
typedef struct {
    const char *name;
    const char *default_model;
    /* One model request over history (array of input items) and tools
     * (array of tool definitions, may be NULL). Streams via callbacks. */
    int (*turn)(cJSON *history, cJSON *tools, const orc_cfg *cfg,
                const provider_cb *cb, void *ud);
    /* Print auth status for --auth. Returns 0 if usable. */
    int (*auth_status)(void);
    /* Optional (may be NULL): interactive login for --login. Returns 0 on success. */
    int (*login)(void);
    /* Optional (may be NULL): selectable models as a cJSON array of
     * {"slug","description"}, caller owns. NULL when unavailable. */
    cJSON *(*models)(void);
} provider;

/* Look up by name; NULL or "" returns the default provider. NULL if unknown. */
const provider *provider_get(const char *name);
void provider_list(void);

#endif
