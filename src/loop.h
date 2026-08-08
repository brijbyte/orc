#ifndef ORC_LOOP_H
#define ORC_LOOP_H

#include <uv.h>

int loop_init(void);
void loop_input_start(void);
void loop_run_once(void);
uv_loop_t *loop_get(void);
void loop_free(void);

#endif
