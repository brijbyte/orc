#ifndef ORC_TOOLS_H
#define ORC_TOOLS_H

#include <cJSON.h>

/* Static JSON array of tool definitions (Responses API "function" tools). */
const char *tools_schema_json(void);

/* Execute a tool. Returns a malloc'd output string (never NULL);
 * errors are returned as text for the model to read. */
char *tool_run(const char *name, cJSON *args);

#endif
