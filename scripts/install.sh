#!/bin/sh
# Instala o binário `lk` (Linkana CLI) a partir do último GitHub Release.
#
# Repositório público — download via curl/wget, sem autenticação. Uso:
#   curl -fsSL https://raw.githubusercontent.com/linkanalabs/cli/main/scripts/install.sh | sh
#
# Variáveis de ambiente:
#   LK_BIN_DIR  diretório de instalação (default: ~/.local/bin)
#   LK_VERSION  versão a instalar, ex: v0.6.0 (default: última release)
set -eu

REPO="linkanalabs/cli"
INSTALL_DIR="${LK_BIN_DIR:-$HOME/.local/bin}"

err() { echo "$@" >&2; }

# --- ferramenta de download (curl ou wget) ---
if command -v curl >/dev/null 2>&1; then
  dl() { curl -fsSL "$1" -o "$2"; }
  fetch() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
  dl() { wget -qO "$2" "$1"; }
  fetch() { wget -qO - "$1"; }
else
  err "Requer curl ou wget."; exit 1
fi

# --- OS / arquitetura ---
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64 | amd64) ARCH="amd64" ;;
  aarch64 | arm64) ARCH="arm64" ;;
  *) err "Arquitetura não suportada: $ARCH"; exit 1 ;;
esac
case "$OS" in
  linux | darwin) ;;
  *) err "OS não suportado: $OS (use Linux/macOS; no Windows baixe o .zip dos releases)"; exit 1 ;;
esac

# --- versão (última release, ou LK_VERSION) ---
VERSION="${LK_VERSION:-}"
if [ -z "$VERSION" ]; then
  # Extrai "tag_name": "vX.Y.Z" da API pública, sem depender de jq.
  VERSION=$(fetch "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | head -n1 | sed -E 's/.*"tag_name" *: *"([^"]+)".*/\1/')
fi
[ -n "$VERSION" ] || { err "Não consegui determinar a versão (defina LK_VERSION=vX.Y.Z)."; exit 1; }
echo "Instalando lk ${VERSION} (${OS}/${ARCH})..."

# goreleaser nomeia o arquivo sem o "v" (ex: lk_0.6.0_linux_amd64.tar.gz).
ARCHIVE="lk_${VERSION#v}_${OS}_${ARCH}.tar.gz"
BASE="https://github.com/${REPO}/releases/download/${VERSION}"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

echo "Baixando ${ARCHIVE}..."
dl "${BASE}/${ARCHIVE}" "$TMPDIR/$ARCHIVE" || { err "Falha ao baixar ${ARCHIVE}."; exit 1; }
dl "${BASE}/checksums.txt" "$TMPDIR/checksums.txt" || { err "Falha ao baixar checksums.txt."; exit 1; }

echo "Verificando checksum..."
EXPECTED=$(grep " ${ARCHIVE}\$" "$TMPDIR/checksums.txt" | awk '{print $1}')
[ -n "$EXPECTED" ] || { err "checksum de ${ARCHIVE} não encontrado."; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "$TMPDIR/$ARCHIVE" | awk '{print $1}')
else
  ACTUAL=$(shasum -a 256 "$TMPDIR/$ARCHIVE" | awk '{print $1}')
fi
if [ "$EXPECTED" != "$ACTUAL" ]; then
  err "checksum não confere!"; err "  esperado: $EXPECTED"; err "  obtido:   $ACTUAL"; exit 1
fi
echo "Checksum OK."

tar -xzf "$TMPDIR/$ARCHIVE" -C "$TMPDIR" lk
mkdir -p "$INSTALL_DIR"
install -m 0755 "$TMPDIR/lk" "$INSTALL_DIR/lk"

echo ""
echo "lk ${VERSION} instalado em $INSTALL_DIR/lk"

if ! printf '%s' "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
  echo ""
  echo "Adicione $INSTALL_DIR ao seu PATH:"
  SHELL_NAME=$(basename "${SHELL:-sh}")
  case "$SHELL_NAME" in
    zsh)  echo "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zshrc && . ~/.zshrc" ;;
    fish) echo "  fish_add_path $INSTALL_DIR" ;;
    *)    echo "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.bashrc && . ~/.bashrc" ;;
  esac
fi

echo ""
echo "Confirme com: lk doctor"
