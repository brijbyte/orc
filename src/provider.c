#include "provider.h"

#include <stdio.h>
#include <string.h>

extern const provider provider_codex;

static const provider *providers[] = {
    &provider_codex,
    /* add new providers here */
};

const provider *provider_get(const char *name) {
    if (!name || !*name) return providers[0];
    for (size_t i = 0; i < sizeof providers / sizeof *providers; i++)
        if (strcmp(providers[i]->name, name) == 0) return providers[i];
    return NULL;
}

void provider_list(void) {
    for (size_t i = 0; i < sizeof providers / sizeof *providers; i++)
        fprintf(stderr, "  %s (default model %s)\n",
                providers[i]->name, providers[i]->default_model);
}
