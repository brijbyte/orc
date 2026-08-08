#ifndef ORC_PROCESS_H
#define ORC_PROCESS_H

#include <cJSON.h>

char *process_start(const char *cmd);
char *process_tool(cJSON *args);
void process_cleanup(void);

#endif
