#!/bin/sh
# orc installer: curl -fsSL --connect-timeout 5 https://github.com/brijbyte/orc/releases/latest/download/install.sh | sh
# Env: ORC_VERSION, ORC_INSTALL_DIR, ORC_SERVICE=1, ORC_SERVICE_ADDR, ORC_SERVICE_DOMAIN
set -eu

REPO="brijbyte/orc"
# Some networks blackhole single CDN addresses; a short connect timeout makes
# curl fail over to the next address instead of hanging for minutes.
CURL="curl -fL --connect-timeout 5 --retry 2"

say() { echo "orc: $*"; }

os=$(uname -s)
arch=$(uname -m)
case "$os" in
    Darwin) os=darwin ;;
    Linux)  os=linux ;;
    *) echo "orc: unsupported OS: $os" >&2; exit 1 ;;
esac
case "$arch" in
    x86_64|amd64)  arch=x86_64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) echo "orc: unsupported architecture: $arch" >&2; exit 1 ;;
esac
asset="orc-${os}-${arch}.tar.gz"
say "platform ${os}/${arch}"

if [ -n "${ORC_VERSION:-}" ]; then
    base="https://github.com/${REPO}/releases/download/v${ORC_VERSION#v}"
else
    base="https://github.com/${REPO}/releases/latest/download"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "downloading ${base}/${asset}"
$CURL -# -o "$tmp/$asset" "${base}/${asset}"
say "downloading checksums"
$CURL -s -o "$tmp/checksums.txt" "${base}/checksums.txt"

say "verifying checksum"
cd "$tmp"
expected=$(grep " ${asset}\$" checksums.txt | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then
    got=$(sha256sum "$asset" | cut -d' ' -f1)
else
    got=$(shasum -a 256 "$asset" | cut -d' ' -f1)
fi
if [ "$expected" != "$got" ]; then
    echo "orc: checksum mismatch for $asset" >&2
    exit 1
fi
tar xzf "$asset"

if [ -n "${ORC_INSTALL_DIR:-}" ]; then
    dir="$ORC_INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
    dir=/usr/local/bin
else
    dir="$HOME/.local/bin"
fi
say "installing to $dir/orc"
mkdir -p "$dir"
install -m 755 orc "$dir/orc"

say "installed $("$dir/orc" --version)"
case ":$PATH:" in
    *":$dir:"*) ;;
    *) say "note: $dir is not in your PATH" ;;
esac

if [ "${ORC_SERVICE:-0}" = 1 ]; then
    set -- service install --cwd "$HOME" --serve "${ORC_SERVICE_ADDR:-127.0.0.1:7777}"
    if [ -n "${ORC_SERVICE_DOMAIN:-}" ]; then
        set -- "$@" --domain "$ORC_SERVICE_DOMAIN"
    fi
    "$dir/orc" "$@"
else
    say "run '$dir/orc service install' to start the web UI in the background"
fi
