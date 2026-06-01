#!/bin/sh
# npm-jail installer - downloads the binary from the latest GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/suethttps/npm-jail/master/install.sh | sh
#
# Optional environment variables:
#   NPM_JAIL_VERSION   specific tag (for example: v0.1.0). Default: latest release.
#   NPM_JAIL_BIN_DIR   installation directory. Default: ~/.local/bin.
set -eu

REPO="suethttps/npm-jail"
BIN="npm-jail"
BIN_DIR="${NPM_JAIL_BIN_DIR:-$HOME/.local/bin}"

err() { printf 'npm-jail install: %s\n' "$1" >&2; exit 1; }

# --- prerequisites -----------------------------------------------------------
[ "$(uname -s)" = "Linux" ] || err "npm-jail only runs on Linux (depends on bubblewrap/bwrap)."

if command -v curl >/dev/null 2>&1; then
  dl() { curl -fsSL "$1"; }
  dlo() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  dl() { wget -qO- "$1"; }
  dlo() { wget -qO "$2" "$1"; }
else
  err "curl or wget is required."
fi

command -v tar >/dev/null 2>&1 || err "tar is required."

# --- arch --------------------------------------------------------------------
case "$(uname -m)" in
  x86_64 | amd64) ARCH="x86_64" ;;
  aarch64 | arm64) ARCH="aarch64" ;;
  *) err "unsupported architecture: $(uname -m)" ;;
esac

# --- version -----------------------------------------------------------------
VERSION="${NPM_JAIL_VERSION:-}"
if [ -z "$VERSION" ]; then
  VERSION=$(dl "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
  [ -n "$VERSION" ] || err "could not discover the latest release."
fi

ASSET="${BIN}_Linux_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"

# --- download + extract ------------------------------------------------------
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

printf 'downloading %s %s (%s)...\n' "$BIN" "$VERSION" "$ARCH"
dlo "$URL" "$TMP/$ASSET" || err "failed to download $URL"
tar -xzf "$TMP/$ASSET" -C "$TMP" || err "failed to extract $ASSET"
[ -f "$TMP/$BIN" ] || err "binary $BIN not found in tarball."

mkdir -p "$BIN_DIR"
install -m 0755 "$TMP/$BIN" "$BIN_DIR/$BIN"
printf 'installed at %s\n' "$BIN_DIR/$BIN"

# --- PATH warning ------------------------------------------------------------
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) printf '\nWARNING: %s is not on PATH. Add this to your shell:\n  export PATH="%s:$PATH"\n' "$BIN_DIR" "$BIN_DIR" ;;
esac

command -v bwrap >/dev/null 2>&1 || \
  printf '\nReminder: install bubblewrap (for example: pacman -S bubblewrap / apt install bubblewrap).\n'
