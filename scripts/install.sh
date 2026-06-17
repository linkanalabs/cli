#!/usr/bin/env bash
# Instala o binário `lk` (Linkana CLI) a partir do último GitHub Release.
#
# O repositório é privado, então o download é autenticado via GitHub CLI (gh):
#   gh api repos/linkanalabs/cli/contents/scripts/install.sh \
#     -H "Accept: application/vnd.github.raw" | bash
set -euo pipefail

REPO="linkanalabs/cli"
INSTALL_DIR="${LK_BIN_DIR:-$HOME/.local/bin}"

command -v gh >/dev/null 2>&1 || {
  echo "Requer o GitHub CLI (gh): https://cli.github.com" >&2
  exit 1
}
gh auth status >/dev/null 2>&1 || {
  echo "Autentique primeiro: gh auth login" >&2
  exit 1
}

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64 | amd64) ARCH="amd64" ;;
  aarch64 | arm64) ARCH="arm64" ;;
  *) echo "Arquitetura não suportada: $ARCH" >&2; exit 1 ;;
esac
case "$OS" in
  linux | darwin) ;;
  *) echo "OS não suportado: $OS (use Linux/macOS)" >&2; exit 1 ;;
esac

VERSION=$(gh release view -R "$REPO" --json tagName -q .tagName)
[ -n "$VERSION" ] || { echo "Não consegui determinar a última versão." >&2; exit 1; }
echo "Última versão: $VERSION"

# goreleaser usa a versão sem o "v" no nome do arquivo (ex: lk_0.1.0_darwin_arm64.tar.gz).
ARCHIVE="lk_${VERSION#v}_${OS}_${ARCH}.tar.gz"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "Baixando ${ARCHIVE}..."
gh release download "$VERSION" -R "$REPO" -p "$ARCHIVE" -p checksums.txt -D "$TMPDIR" --clobber

echo "Verificando checksum..."
EXPECTED=$(grep " ${ARCHIVE}\$" "$TMPDIR/checksums.txt" | awk '{print $1}')
[ -n "$EXPECTED" ] || { echo "ERRO: checksum de ${ARCHIVE} não encontrado." >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL=$(sha256sum "$TMPDIR/$ARCHIVE" | awk '{print $1}')
else
  ACTUAL=$(shasum -a 256 "$TMPDIR/$ARCHIVE" | awk '{print $1}')
fi
if [ "$EXPECTED" != "$ACTUAL" ]; then
  echo "ERRO: checksum não confere!" >&2
  echo "  esperado: $EXPECTED" >&2
  echo "  obtido:   $ACTUAL" >&2
  exit 1
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
  SHELL_NAME=$(basename "${SHELL:-bash}")
  case "$SHELL_NAME" in
    zsh) echo "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zshrc && source ~/.zshrc" ;;
    fish) echo "  fish_add_path $INSTALL_DIR" ;;
    *) echo "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.bashrc && source ~/.bashrc" ;;
  esac
fi

echo ""
echo "Confirme com: lk doctor"
