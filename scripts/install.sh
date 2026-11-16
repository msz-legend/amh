#!/bin/sh
# amh installer: curl -fsSL https://raw.githubusercontent.com/<owner>/amh/main/scripts/install.sh | sh
set -eu
REPO="${AMH_REPO:-msz-legend/amh}"
VERSION="${AMH_VERSION:-latest}"
INSTALL_DIR="${AMH_INSTALL_DIR:-$HOME/.amh/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "amh install: unsupported arch $arch" >&2; exit 1 ;;
esac

mkdir -p "$INSTALL_DIR"
if command -v go >/dev/null 2>&1 && [ "${AMH_BUILD_FROM_SOURCE:-0}" = "1" ]; then
  echo "amh install: building from source..."
  tmp=$(mktemp -d)
  git clone --depth 1 "https://github.com/$REPO" "$tmp/amh"
  (cd "$tmp/amh" && go build -o "$INSTALL_DIR/amh" ./cmd/amh)
  rm -rf "$tmp"
else
  base="https://github.com/$REPO/releases/download"
  if [ "$VERSION" = "latest" ]; then
    VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
    [ -n "$VERSION" ] || { echo "amh install: no release found; set AMH_BUILD_FROM_SOURCE=1 to build"; exit 1; }
  fi
  url="$base/$VERSION/amh_${VERSION}_${os}_${arch}.tar.gz"
  echo "amh install: fetching $url"
  curl -fsSL "$url" | tar -xz -C "$INSTALL_DIR" amh
fi
chmod +x "$INSTALL_DIR/amh"
echo "amh installed: $INSTALL_DIR/amh"
echo "next: $INSTALL_DIR/amh connect --agent claude-code && $INSTALL_DIR/amh connect --agent codex"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "note: add $INSTALL_DIR to PATH" ;;
esac
