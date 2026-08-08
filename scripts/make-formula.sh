#!/bin/sh
# Emit the Homebrew formula for a release: make-formula.sh <version> <checksums.txt>
set -eu

VERSION="$1"
CHECKSUMS="$2"
BASE="https://github.com/brijbyte/orc/releases/download/v${VERSION}"

sha() { grep " orc-$1.tar.gz\$" "$CHECKSUMS" | cut -d' ' -f1; }

cat <<EOF
class Orc < Formula
  desc "Minimal coding-agent harness"
  homepage "https://github.com/brijbyte/orc"
  version "${VERSION}"

  on_macos do
    if Hardware::CPU.arm?
      url "${BASE}/orc-darwin-arm64.tar.gz"
      sha256 "$(sha darwin-arm64)"
    else
      url "${BASE}/orc-darwin-x86_64.tar.gz"
      sha256 "$(sha darwin-x86_64)"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "${BASE}/orc-linux-arm64.tar.gz"
      sha256 "$(sha linux-arm64)"
    else
      url "${BASE}/orc-linux-x86_64.tar.gz"
      sha256 "$(sha linux-x86_64)"
    end
  end

  def install
    bin.install "orc"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/orc --version")
  end
end
EOF
