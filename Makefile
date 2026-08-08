CC ?= cc
CFLAGS = -std=c11 -Wall -Wextra -O2 -Ivendor -Isrc
LDLIBS = -lcurl

OBJS = src/main.o src/agent.o src/provider.o src/providers/codex.o src/http.o \
       src/tools.o src/session.o src/render.o src/util.o vendor/cJSON.o \
       vendor/md4c.o

bin/orc: $(OBJS)
	mkdir -p bin
	$(CC) -o $@ $(OBJS) $(LDLIBS)

vendor/cJSON.o vendor/md4c.o: CFLAGS = -std=c11 -O2

# vendor/ is gitignored; fetch pinned deps on first build
CJSON_URL = https://cdn.jsdelivr.net/gh/DaveGamble/cJSON@v1.7.18
vendor/cJSON.c vendor/cJSON.h:
	mkdir -p vendor
	curl -fsSL -o vendor/cJSON.c $(CJSON_URL)/cJSON.c
	curl -fsSL -o vendor/cJSON.h $(CJSON_URL)/cJSON.h

MD4C_URL = https://cdn.jsdelivr.net/gh/mity/md4c@release-0.5.2/src
vendor/md4c.c vendor/md4c.h:
	mkdir -p vendor
	curl -fsSL -o vendor/md4c.c $(MD4C_URL)/md4c.c
	curl -fsSL -o vendor/md4c.h $(MD4C_URL)/md4c.h

vendor/cJSON.o: vendor/cJSON.c vendor/cJSON.h
vendor/md4c.o: vendor/md4c.c vendor/md4c.h
$(OBJS): vendor/cJSON.h vendor/md4c.h

clean:
	rm -f bin/orc $(OBJS)

.PHONY: clean
