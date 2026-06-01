#!/bin/sh
# npm-jail installer — baixa o binario da ultima release do GitHub.
#
#   curl -fsSL https://raw.githubusercontent.com/suethttps/npm-jail/master/install.sh | sh
#
# Variaveis de ambiente opcionais:
#   NPM_JAIL_VERSION   tag especifica (ex.: v0.1.0). Padrao: ultima release.
#   NPM_JAIL_BIN_DIR   diretorio de instalacao. Padrao: ~/.local/bin.
set -eu

REPO="suethttps/npm-jail"
BIN="npm-jail"
BIN_DIR="${NPM_JAIL_BIN_DIR:-$HOME/.local/bin}"

err() { printf 'npm-jail install: %s\n' "$1" >&2; exit 1; }

# --- pre-requisitos ----------------------------------------------------------
[ "$(uname -s)" = "Linux" ] || err "npm-jail so roda no Linux (depende de bubblewrap/bwrap)."

if command -v curl >/dev/null 2>&1; then
  dl() { curl -fsSL "$1"; }
  dlo() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  dl() { wget -qO- "$1"; }
  dlo() { wget -qO "$2" "$1"; }
else
  err "preciso de curl ou wget."
fi

command -v tar >/dev/null 2>&1 || err "preciso de tar."

# --- arch --------------------------------------------------------------------
case "$(uname -m)" in
  x86_64 | amd64) ARCH="x86_64" ;;
  aarch64 | arm64) ARCH="aarch64" ;;
  *) err "arquitetura nao suportada: $(uname -m)" ;;
esac

# --- versao ------------------------------------------------------------------
VERSION="${NPM_JAIL_VERSION:-}"
if [ -z "$VERSION" ]; then
  VERSION=$(dl "https://api.github.com/repos/$REPO/releases/latest" \
    | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
  [ -n "$VERSION" ] || err "nao consegui descobrir a ultima release."
fi

ASSET="${BIN}_Linux_${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"

# --- download + extract ------------------------------------------------------
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

printf 'baixando %s %s (%s)...\n' "$BIN" "$VERSION" "$ARCH"
dlo "$URL" "$TMP/$ASSET" || err "falha ao baixar $URL"
tar -xzf "$TMP/$ASSET" -C "$TMP" || err "falha ao extrair $ASSET"
[ -f "$TMP/$BIN" ] || err "binario $BIN nao encontrado no tarball."

mkdir -p "$BIN_DIR"
install -m 0755 "$TMP/$BIN" "$BIN_DIR/$BIN"
printf 'instalado em %s\n' "$BIN_DIR/$BIN"

# --- aviso de PATH -----------------------------------------------------------
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) printf '\nAVISO: %s nao esta no PATH. Adicione ao seu shell:\n  export PATH="%s:$PATH"\n' "$BIN_DIR" "$BIN_DIR" ;;
esac

command -v bwrap >/dev/null 2>&1 || \
  printf '\nLembrete: instale o bubblewrap (ex.: pacman -S bubblewrap / apt install bubblewrap).\n'
