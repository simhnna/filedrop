#!/bin/sh
# Installs the filedrop CLI from the latest GitHub release.
#   curl -fsSL https://filedrop.hannaweb.eu/install.sh | sh
# Set INSTALL_DIR to change the destination (default: ~/.local/bin).
set -eu

REPO="simhnna/filedrop"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  *) echo "Unsupported OS: $os — download a binary from https://github.com/$REPO/releases/latest" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
esac

asset="filedrop-$os-$arch"
base="https://github.com/$REPO/releases/latest/download"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading ${asset}..."
curl -fsSL "$base/$asset" -o "$tmp/$asset"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

expected=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
else
  actual=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)
fi
if [ -z "$expected" ] || [ "$expected" != "$actual" ]; then
  echo "Checksum verification failed for $asset" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
chmod +x "$tmp/$asset"
mv "$tmp/$asset" "$INSTALL_DIR/filedrop"
echo "Installed filedrop to $INSTALL_DIR/filedrop"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "Note: $INSTALL_DIR is not on your PATH. Add it, e.g.: export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac
