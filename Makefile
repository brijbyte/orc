CC ?= cc
STRIP ?= strip
CPPFLAGS += -D_POSIX_C_SOURCE=200809L -D_XOPEN_SOURCE=700 \
            -Ivendor -Isrc
CFLAGS ?= -O2
CFLAGS += -std=c11 -Wall -Wextra

# Version from the latest tag; CI overrides with `make dist VERSION=x.y`
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
ifeq ($(VERSION),)
VERSION = dev
endif
CPPFLAGS += -DORC_VERSION='"$(VERSION)"'

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
       src/tools.o src/process.o src/skills.o src/session.o src/render.o src/event.o src/loop.o src/ui.o \
       src/commands.o src/util.o src/auth.o src/instructions.o \
       vendor/cJSON.o vendor/md4c.o vendor/timestamp_parse.o \
       vendor/timestamp_format.o vendor/timestamp_valid.o \
       vendor/utf8proc.o

# libcurl is embedded statically (with mbedTLS) so the binary has no deps
# beyond libc/pthread. `make SYSTEM_CURL=1` links the system libcurl instead.
CURL_VER = 8.11.1
MBEDTLS_VER = 3.6.2
LIBUV_VER = 1.50.0
VENDOR := $(abspath vendor)
MBED_A = vendor/mbedtls/lib/libmbedtls.a vendor/mbedtls/lib/libmbedx509.a \
         vendor/mbedtls/lib/libmbedcrypto.a
UV_A = vendor/libuv/lib/libuv.a
CPPFLAGS += -I$(VENDOR)/mbedtls/include -I$(VENDOR)/libuv/include
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
NCURSES_CFLAGS := $(shell pkg-config --cflags ncursesw 2>/dev/null)
NCURSES_LIBS := $(shell pkg-config --libs ncursesw 2>/dev/null || echo -lncurses)
CPPFLAGS += $(NCURSES_CFLAGS) -D_XOPEN_SOURCE_EXTENDED=1
LDLIBS += $(UV_A) $(NCURSES_LIBS)
ifneq ($(UNAME_S),Darwin)
LDLIBS += -ldl -lrt -lpthread
endif

DEBUG_BIN = bin/orc-debug
RELEASE_BIN = bin/orc

# Primary build: the Go implementation. The C targets below are legacy.
GO_LDFLAGS = -X github.com/brijbyte/orc/internal/config.Version=$(VERSION)
.PHONY: go release dist
go:
	go build -mod=mod -ldflags '$(GO_LDFLAGS)' -o bin/orc ./cmd/orc

release:
	go build -mod=mod -trimpath -ldflags '-s -w $(GO_LDFLAGS)' -o bin/orc ./cmd/orc

# Cross-compile every release target into dist/ tarballs (pure Go, CGO off).
DIST_TARGETS = darwin-arm64 darwin-x86_64 linux-x86_64 linux-arm64
dist:
	@mkdir -p dist
	@for target in $(DIST_TARGETS); do \
		goos=$${target%%-*}; arch=$${target#*-}; \
		case $$arch in x86_64) goarch=amd64 ;; *) goarch=$$arch ;; esac; \
		echo "  GO    $$target"; \
		CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch \
		go build -mod=mod -trimpath -ldflags '-s -w $(GO_LDFLAGS)' \
			-o dist/$$target/orc ./cmd/orc || exit 1; \
		tar -C dist/$$target -czf dist/orc-$$target.tar.gz orc; \
	done

all: go

c: $(DEBUG_BIN)

$(DEBUG_BIN): $(OBJS)
	mkdir -p bin
	$(CC) $(LDFLAGS) -o $@ $(OBJS) $(LDLIBS)

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

LIBUV_URL = https://dist.libuv.org/dist/v$(LIBUV_VER)/libuv-v$(LIBUV_VER).tar.gz
$(UV_A):
	mkdir -p vendor
	curl -fsSL $(LIBUV_URL) | tar xz -C vendor
	cmake -S vendor/libuv-v$(LIBUV_VER) -B vendor/libuv-build \
	  -DBUILD_TESTING=OFF -DLIBUV_BUILD_SHARED=OFF \
	  -DCMAKE_INSTALL_PREFIX=$(VENDOR)/libuv
	cmake --build vendor/libuv-build --target install -j

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

vendor/cJSON.o: vendor/cJSON.c vendor/cJSON.h
vendor/md4c.o: vendor/md4c.c vendor/md4c.h
vendor/utf8proc.o: vendor/utf8proc.c vendor/utf8proc.h vendor/utf8proc_data.c
vendor/timestamp_parse.o vendor/timestamp_format.o vendor/timestamp_valid.o: \
	vendor/timestamp.h
$(OBJS): vendor/cJSON.h vendor/md4c.h vendor/utf8proc.h vendor/timestamp.h \
	$(firstword $(MBED_A)) $(CURL_A) $(UV_A)

PREFIX ?= /usr/local
install: release
	mkdir -p $(DESTDIR)$(PREFIX)/bin
	install -m 755 bin/orc $(DESTDIR)$(PREFIX)/bin/orc

clean:
	rm -f $(DEBUG_BIN) $(RELEASE_BIN) $(OBJS)
	rm -rf dist

.PHONY: all release install clean
