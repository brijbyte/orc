CC ?= cc
STRIP ?= strip
CPPFLAGS += -D_POSIX_C_SOURCE=200809L -Ivendor -Isrc
CFLAGS ?= -O2
CFLAGS += -std=c11 -Wall -Wextra

# Version from the latest tag (falls back to orc.h default outside a git repo)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
ifneq ($(VERSION),)
CPPFLAGS += -DORC_VERSION='"$(VERSION)"'
endif

# STATIC=1: fully static binary (use on Alpine/musl; glibc static is broken)
ifeq ($(STATIC),1)
LDFLAGS += -static
endif

# Keep macOS releases runnable on supported Apple Silicon systems. These flags
# are Darwin-only so the same Makefile remains usable on Linux and other POSIX
# platforms.
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
MACOSX_DEPLOYMENT_TARGET ?= 13.0
CFLAGS += -mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET)
LDFLAGS += -mmacosx-version-min=$(MACOSX_DEPLOYMENT_TARGET)
export MACOSX_DEPLOYMENT_TARGET  # vendor sub-builds (mbedTLS, curl) honor it
endif

OBJS = src/main.o src/agent.o src/provider.o src/providers/codex.o src/http.o \
       src/tools.o src/session.o src/render.o src/input.o src/event.o src/ui.o \
       src/commands.o src/util.o src/auth.o \
       vendor/cJSON.o vendor/md4c.o vendor/timestamp_parse.o \
       vendor/timestamp_format.o vendor/timestamp_valid.o vendor/linenoise.o \
       vendor/utf8proc.o

# libcurl is embedded statically (with mbedTLS) so the binary has no deps
# beyond libc/pthread. `make SYSTEM_CURL=1` links the system libcurl instead.
CURL_VER = 8.11.1
MBEDTLS_VER = 3.6.2
VENDOR := $(abspath vendor)
MBED_A = vendor/mbedtls/lib/libmbedtls.a vendor/mbedtls/lib/libmbedx509.a \
         vendor/mbedtls/lib/libmbedcrypto.a
CPPFLAGS += -I$(VENDOR)/mbedtls/include
ifeq ($(SYSTEM_CURL),1)
LDLIBS += -lcurl $(MBED_A) -lpthread
else
CURL_A = vendor/curl/lib/libcurl.a
CPPFLAGS += -I$(VENDOR)/curl/include
LDLIBS += $(CURL_A) $(MBED_A) -lpthread
ifeq ($(UNAME_S),Darwin)
LDLIBS += -framework CoreFoundation -framework SystemConfiguration
endif
endif

DEBUG_BIN = bin/orc-debug
RELEASE_BIN = bin/orc

all: $(DEBUG_BIN)

$(DEBUG_BIN): $(OBJS)
	mkdir -p bin
	$(CC) $(LDFLAGS) -o $@ $(OBJS) $(LDLIBS)

release: $(RELEASE_BIN)

$(RELEASE_BIN): $(DEBUG_BIN)
	cp $< $@
	$(STRIP) $@

# vendor/ is gitignored; fetch pinned deps on first build
CJSON_URL = https://cdn.jsdelivr.net/gh/DaveGamble/cJSON@v1.7.18
vendor/cJSON.c vendor/cJSON.h: vendor/.cjson.stamp
vendor/.cjson.stamp:
	mkdir -p vendor
	curl -fsSL -o vendor/cJSON.c $(CJSON_URL)/cJSON.c
	curl -fsSL -o vendor/cJSON.h $(CJSON_URL)/cJSON.h
	touch $@

MD4C_URL = https://cdn.jsdelivr.net/gh/mity/md4c@release-0.5.2/src
vendor/md4c.c vendor/md4c.h: vendor/.md4c.stamp
vendor/.md4c.stamp:
	mkdir -p vendor
	curl -fsSL -o vendor/md4c.c $(MD4C_URL)/md4c.c
	curl -fsSL -o vendor/md4c.h $(MD4C_URL)/md4c.h
	touch $@

UTF8PROC_URL = https://cdn.jsdelivr.net/gh/JuliaStrings/utf8proc@v2.11.3
vendor/utf8proc.c:
	mkdir -p vendor
	curl -fsSL -o $@ $(UTF8PROC_URL)/utf8proc.c
vendor/utf8proc.h:
	mkdir -p vendor
	curl -fsSL -o $@ $(UTF8PROC_URL)/utf8proc.h
vendor/utf8proc_data.c:
	mkdir -p vendor
	curl -fsSL -o $@ $(UTF8PROC_URL)/utf8proc_data.c

# c-timestamp: RFC3339 parse/format (no tagged releases; pinned to a commit)
TS_URL = https://cdn.jsdelivr.net/gh/chansen/c-timestamp@b205c407ae6680d23d74359ac00444b80989792f
TS_SRCS = timestamp_parse.c timestamp_format.c timestamp_valid.c
vendor/timestamp.h $(addprefix vendor/,$(TS_SRCS)): vendor/.timestamp.stamp
vendor/.timestamp.stamp:
	mkdir -p vendor
	set -e; for f in timestamp.h $(TS_SRCS); do \
		curl -fsSL -o vendor/$$f $(TS_URL)/$$f; done
	touch $@

MBEDTLS_URL = https://github.com/Mbed-TLS/mbedtls/releases/download/mbedtls-$(MBEDTLS_VER)/mbedtls-$(MBEDTLS_VER).tar.bz2
vendor/mbedtls/lib/libmbedtls.a:
	mkdir -p vendor
	curl -fsSL $(MBEDTLS_URL) | tar xj -C vendor
	$(MAKE) -C vendor/mbedtls-$(MBEDTLS_VER) lib
	mkdir -p vendor/mbedtls/lib
	cp vendor/mbedtls-$(MBEDTLS_VER)/library/*.a vendor/mbedtls/lib/
	cp -R vendor/mbedtls-$(MBEDTLS_VER)/include vendor/mbedtls/

CURL_SRC_URL = https://curl.se/download/curl-$(CURL_VER).tar.gz
vendor/curl/lib/libcurl.a: vendor/mbedtls/lib/libmbedtls.a
	curl -fsSL $(CURL_SRC_URL) | tar xz -C vendor
	cd vendor/curl-$(CURL_VER) && ./configure --prefix=$(VENDOR)/curl \
	  --disable-shared --enable-static --with-mbedtls=$(VENDOR)/mbedtls \
	  --without-libpsl --without-zlib --without-brotli --without-zstd \
	  --without-nghttp2 --without-libidn2 --without-ca-bundle \
	  --without-ca-path --disable-docs --disable-manual --disable-ldap \
	  --disable-ldaps --disable-rtsp --disable-ftp --disable-file \
	  --disable-dict --disable-telnet --disable-tftp --disable-pop3 \
	  --disable-imap --disable-smb --disable-smtp --disable-gopher \
	  --disable-mqtt --disable-ntlm --disable-tls-srp > configure.log
	$(MAKE) -C vendor/curl-$(CURL_VER)/lib install
	$(MAKE) -C vendor/curl-$(CURL_VER)/include install

# linenoise: async line editing (no tagged releases; pinned to a commit)
LN_URL = https://cdn.jsdelivr.net/gh/antirez/linenoise@a473823d74b93eab2ba83480df16ed37617493f2
vendor/linenoise.c vendor/linenoise.h: vendor/.linenoise.stamp
vendor/.linenoise.stamp:
	mkdir -p vendor
	curl -fsSL -o vendor/linenoise.c $(LN_URL)/linenoise.c
	curl -fsSL -o vendor/linenoise.h $(LN_URL)/linenoise.h
	# TCSAFLUSH drops type-ahead, TCSADRAIN can block on unread output
	sed -i.bak 's/TCSAFLUSH/TCSANOW/g' vendor/linenoise.c && rm -f vendor/linenoise.c.bak
	# orc additions: history-navigation hook (menu selection in src/input.c),
	# Shift+Enter/Ctrl-J soft line breaks, full CSI parameter parsing
	patch -p0 < scripts/linenoise-orc.patch
	touch $@

vendor/cJSON.o: vendor/cJSON.c vendor/cJSON.h
vendor/md4c.o: vendor/md4c.c vendor/md4c.h
vendor/utf8proc.o: vendor/utf8proc.c vendor/utf8proc.h vendor/utf8proc_data.c
vendor/timestamp_parse.o vendor/timestamp_format.o vendor/timestamp_valid.o: \
	vendor/timestamp.h
vendor/linenoise.o: CPPFLAGS += -include strings.h  # strcasecmp under POSIX
vendor/linenoise.o: vendor/linenoise.c vendor/linenoise.h
$(OBJS): vendor/cJSON.h vendor/md4c.h vendor/utf8proc.h vendor/timestamp.h \
	vendor/linenoise.h $(firstword $(MBED_A)) $(CURL_A)

PREFIX ?= /usr/local
install: $(DEBUG_BIN)
	mkdir -p $(DESTDIR)$(PREFIX)/bin
	install -m 755 $(DEBUG_BIN) $(DESTDIR)$(PREFIX)/bin/orc

clean:
	rm -f $(DEBUG_BIN) $(RELEASE_BIN) $(OBJS)

.PHONY: all release install clean
