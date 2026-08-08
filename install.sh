#!/bin/sh
# orc installer: curl -fsSL https://raw.githubusercontent.com/brijbyte/orc/main/install.sh | sh
# Env: ORC_VERSION (default latest), ORC_INSTALL_DIR (default /usr/local/bin or ~/.local/bin)
set -eu

REPO="brijbyte/orc"

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

if [ -n "${ORC_VERSION:-}" ]; then
    base="https://github.com/${REPO}/releases/download/v${ORC_VERSION#v}"
else
    base="https://github.com/${REPO}/releases/latest/download"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading ${base}/${asset}"
curl -fsSL -o "$tmp/$asset" "${base}/${asset}"
curl -fsSL -o "$tmp/checksums.txt" "${base}/checksums.txt"

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
mkdir -p "$dir"
install -m 755 orc "$dir/orc"

echo "Installed $("$dir/orc" --version) to $dir/orc"
case ":$PATH:" in
    *":$dir:"*) ;;
    *) echo "Note: $dir is not in your PATH" ;;
esac
