#!/bin/sh
set -eu

# giro installer — works on Linux and macOS (amd64/arm64).
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/miltonparedes/giro/main/install.sh | sh
#   curl -fsSL ... | INSTALL_DIR=~/.local/bin sh

REPO="miltonparedes/giro"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

fail() { printf '\033[1;31merror:\033[0m %s\n' "$1" >&2; exit 1; }
info() { printf '\033[1;34m==>\033[0m %s\n' "$1"; }

# --- detect platform ---

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Linux)
    DISPLAY_OS="Linux"
    ASSET_OS="linux"
    ;;
  Darwin)
    DISPLAY_OS="Darwin"
    ASSET_OS="darwin"
    ;;
  *)
    fail "unsupported OS: $OS"
    ;;
esac

case "$ARCH" in
  x86_64|amd64)  ARCH="x86_64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)             fail "unsupported architecture: $ARCH" ;;
esac

# --- resolve latest version ---

info "detecting latest release…"

if command -v curl >/dev/null 2>&1; then
  LATEST="$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest" 2>/dev/null || true)"
elif command -v wget >/dev/null 2>&1; then
  LATEST="$(wget --spider --max-redirect=0 "https://github.com/$REPO/releases/latest" 2>&1 | grep -i 'Location:' | awk '{print $2}' | tr -d '\r' || true)"
else
  fail "neither curl nor wget found"
fi

VERSION="${LATEST##*/}"
[ -n "$VERSION" ] || fail "could not determine latest version"

info "installing giro $VERSION (${DISPLAY_OS}/${ARCH})"

# --- download & extract ---

ARCHIVE="giro_${VERSION#v}_${ASSET_OS}_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/${VERSION}/${ARCHIVE}"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$URL" -o "$TMPDIR/$ARCHIVE" || fail "download failed from $URL"
else
  wget -q "$URL" -O "$TMPDIR/$ARCHIVE" || fail "download failed from $URL"
fi

tar xzf "$TMPDIR/$ARCHIVE" -C "$TMPDIR"
[ -f "$TMPDIR/giro" ] || fail "archive did not contain a 'giro' binary"

# --- install ---

if [ -w "$INSTALL_DIR" ]; then
  mv "$TMPDIR/giro" "$INSTALL_DIR/giro"
else
  info "writing to $INSTALL_DIR requires elevated permissions"
  sudo mv "$TMPDIR/giro" "$INSTALL_DIR/giro"
fi

chmod +x "$INSTALL_DIR/giro"

info "giro $VERSION installed to $INSTALL_DIR/giro"

# --- verify ---

if command -v giro >/dev/null 2>&1; then
  info "run 'giro' to start the server"
else
  printf '\033[1;33mwarn:\033[0m %s is not in your PATH\n' "$INSTALL_DIR" >&2
  info "add it with: export PATH=\"$INSTALL_DIR:\$PATH\""
fi
