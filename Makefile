CC ?= cc
CFLAGS = -std=c11 -Wall -Wextra -O2 -Ivendor -Isrc
LDLIBS = -lcurl

OBJS = src/main.o src/agent.o src/provider.o src/providers/codex.o src/http.o \
       src/tools.o src/session.o src/render.o src/util.o vendor/cJSON.o

orc: $(OBJS)
	$(CC) -o $@ $(OBJS) $(LDLIBS)

vendor/cJSON.o: CFLAGS = -std=c11 -O2

# vendor/ is gitignored; fetch pinned cJSON on first build
CJSON_URL = https://cdn.jsdelivr.net/gh/DaveGamble/cJSON@v1.7.18
vendor/cJSON.c vendor/cJSON.h:
	mkdir -p vendor
	curl -fsSL -o vendor/cJSON.c $(CJSON_URL)/cJSON.c
	curl -fsSL -o vendor/cJSON.h $(CJSON_URL)/cJSON.h

vendor/cJSON.o: vendor/cJSON.c vendor/cJSON.h
$(OBJS): vendor/cJSON.h

clean:
	rm -f orc $(OBJS)

.PHONY: clean
