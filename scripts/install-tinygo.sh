#!/usr/bin/env bash
# Downloads TinyGo into ./.tools/tinygo so `npm run build` works without a
# global TinyGo install. Runs automatically via package.json's "postinstall".
set -euo pipefail

TINYGO_VERSION="0.42.0"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_DIR="$ROOT_DIR/.tools/tinygo"

if [ -x "$INSTALL_DIR/bin/tinygo" ] && "$INSTALL_DIR/bin/tinygo" version 2>/dev/null | grep -q "$TINYGO_VERSION"; then
  echo "tinygo $TINYGO_VERSION already installed at $INSTALL_DIR"
  exit 0
fi

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Darwin) platform="darwin" ;;
  Linux) platform="linux" ;;
  *)
    echo "install-tinygo.sh: unsupported OS '$os'. Install TinyGo $TINYGO_VERSION manually: https://tinygo.org/getting-started/install/" >&2
    exit 1
    ;;
esac

case "$arch" in
  arm64|aarch64) goarch="arm64" ;;
  x86_64|amd64) goarch="amd64" ;;
  *)
    echo "install-tinygo.sh: unsupported arch '$arch'. Install TinyGo $TINYGO_VERSION manually: https://tinygo.org/getting-started/install/" >&2
    exit 1
    ;;
esac

archive="tinygo${TINYGO_VERSION}.${platform}-${goarch}.tar.gz"
url="https://github.com/tinygo-org/tinygo/releases/download/v${TINYGO_VERSION}/${archive}"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading TinyGo $TINYGO_VERSION for $platform/$goarch..."
curl -sL -o "$tmp/tinygo.tar.gz" "$url"

mkdir -p "$ROOT_DIR/.tools"
rm -rf "$INSTALL_DIR"
tar -xzf "$tmp/tinygo.tar.gz" -C "$ROOT_DIR/.tools"

echo "Installed tinygo $("$INSTALL_DIR/bin/tinygo" version) to $INSTALL_DIR"
