#!/usr/bin/env bash
# Verifica uma release do `lk` já publicada: os artefatos, o cask no tap da
# Homebrew e os dois caminhos de instalação.
#
# Existe por causa de um modo de falha silencioso: se o GoReleaser publica a
# release mas o commit do cask no tap falha (token expirado, por exemplo), o
# workflow fica verde e o `brew` continua servindo a versão antiga sem erro.
#
# Uso:
#   scripts/release-verify.sh vX.Y.Z [--local]
#       --local acrescenta a checagem via Homebrew, que mexe no `lk` instalado
#       na máquina. Sem ela, nada é instalado no sistema.
#
# Exit 0 = tudo verificado. Exit 1 = alguma checagem falhou. Exit 2 = erro de uso.
set -euo pipefail

cd "$(dirname "$0")/.."

# Dono/nome do tap: espelha o bloco homebrew_casks do .goreleaser.yaml.
TAP_REPO="linkanalabs/homebrew-tap"
CASK_PATH="Casks/lk.rb"

FAILURES=0

pass() { printf '  PASS  %s\n' "$*"; }
fail() { printf '  FAIL  %s\n' "$*"; FAILURES=$((FAILURES + 1)); }
skip() { printf '  SKIP  %s\n' "$*"; }
step() { printf '\n%s\n' "$*"; }
die() { printf 'erro: %s\n' "$*" >&2; exit 2; }
usage() { awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"; }

VERSION=""
LOCAL=0
while [ $# -gt 0 ]; do
  case "$1" in
    --local) LOCAL=1 ;;
    -h | --help)
      usage
      exit 0
      ;;
    -*) die "flag desconhecida: $1" ;;
    *)
      if [ -n "$VERSION" ]; then
        die "versão informada duas vezes: $VERSION e $1"
      fi
      VERSION=$1
      ;;
  esac
  shift
done

[ -n "$VERSION" ] || die "uso: scripts/release-verify.sh vX.Y.Z [--local]"
printf '%s' "$VERSION" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$' ||
  die "versão fora do padrão vX.Y.Z: $VERSION"
command -v gh >/dev/null 2>&1 || die 'gh é obrigatório para consultar release e tap'

NUM=${VERSION#v} # goreleaser nomeia os arquivos sem o "v"

printf 'verificação da release %s\n' "$VERSION"

# --- 1. release no GitHub ---
step 'Release no GitHub'
RELEASE=$(gh release view "$VERSION" --json tagName,isDraft,isPrerelease,assets 2>/dev/null || true)
if [ -z "$RELEASE" ]; then
  fail "release $VERSION não existe (ou o tag não foi publicado)"
  ASSETS=""
else
  if [ "$(printf '%s' "$RELEASE" | jq -r .isDraft)" = false ]; then
    pass "$VERSION publicada (não é draft)"
  else
    fail "$VERSION está como draft — não chega a ninguém"
  fi
  ASSETS=$(printf '%s' "$RELEASE" | jq -r '.assets[].name')
fi

if [ -n "$ASSETS" ]; then
  # Nome por nome: o name_template do goreleaser é contrato com o install.sh e
  # com o cask, então um asset faltando ou renomeado tem de falhar aqui.
  for want in \
    "checksums.txt" \
    "lk_${NUM}_darwin_amd64.tar.gz" \
    "lk_${NUM}_darwin_arm64.tar.gz" \
    "lk_${NUM}_linux_amd64.tar.gz" \
    "lk_${NUM}_linux_arm64.tar.gz" \
    "lk_${NUM}_windows_amd64.zip" \
    "lk_${NUM}_windows_arm64.zip"; do
    if printf '%s\n' "$ASSETS" | grep -qx "$want"; then
      pass "asset $want"
    else
      fail "asset ausente: $want"
    fi
  done
fi

if [ -n "$RELEASE" ]; then
  if [ "$(printf '%s' "$RELEASE" | jq -r .isPrerelease)" = true ]; then
    skip 'prerelease: não vira "latest", o install.sh não pega por default'
  else
    LATEST=$(gh release view --json tagName --jq .tagName 2>/dev/null || true)
    if [ "$LATEST" = "$VERSION" ]; then
      pass "é a release latest (o que o install.sh instala por default)"
    else
      fail "latest é $LATEST, não $VERSION"
    fi
  fi
fi

# --- 2. cask no tap ---
step "Cask em $TAP_REPO"
# Via API de contents, não pela raw.githubusercontent: a raw passa por CDN e
# poderia devolver o cask antigo em cache logo depois da release, gerando um
# falso negativo justo na janela que mais importa.
CASK=$(gh api "repos/$TAP_REPO/contents/$CASK_PATH" --jq .content 2>/dev/null | base64 -d 2>/dev/null || true)
if [ -z "$CASK" ]; then
  fail "não consegui ler $CASK_PATH em $TAP_REPO"
else
  CASK_VERSION=$(printf '%s\n' "$CASK" | awk -F'"' '/^[[:space:]]*version "/ { print $2; exit }')
  if [ "$CASK_VERSION" = "$NUM" ]; then
    pass "cask em version \"$NUM\""
  else
    fail "cask em version \"$CASK_VERSION\", esperado \"$NUM\" — o commit no tap falhou (HOMEBREW_TAP_GITHUB_TOKEN?)"
  fi

  # Cada par sha256/url do cask contra o checksums.txt da própria release: pega
  # cask gerado de outro build.
  CHECKSUMS=$(gh release download "$VERSION" --pattern checksums.txt --output - 2>/dev/null || true)
  if [ -z "$CHECKSUMS" ]; then
    fail 'não consegui baixar checksums.txt da release'
  else
    PAIRS=$(printf '%s\n' "$CASK" | awk '
      /^[[:space:]]*sha256 "/ { sha = $0; sub(/.*sha256 "/, "", sha); sub(/".*/, "", sha); next }
      /^[[:space:]]*url "/ && sha != "" {
        u = $0; sub(/.*\//, "", u); sub(/".*/, "", u)
        print sha, u; sha = ""
      }')
    if [ -z "$PAIRS" ]; then
      fail 'nenhum par sha256/url encontrado no cask'
    else
      while read -r sha archive; do
        [ -n "$archive" ] || continue
        archive=${archive//'#{version}'/$NUM}
        expected=$(printf '%s\n' "$CHECKSUMS" | awk -v a="$archive" '$2 == a { print $1; exit }')
        if [ -z "$expected" ]; then
          fail "$archive citado no cask não está no checksums.txt"
        elif [ "$expected" = "$sha" ]; then
          pass "sha256 do $archive bate com checksums.txt"
        else
          fail "sha256 do $archive divergente (cask $sha, release $expected)"
        fi
      done <<EOF
$PAIRS
EOF
    fi
  fi
fi

# --- 3. instalação via script público (isolada, não toca no sistema) ---
step 'Instalação via scripts/install.sh'
TMP_BIN=$(mktemp -d)
trap 'rm -rf "$TMP_BIN"' EXIT INT TERM
if LK_VERSION="$VERSION" LK_BIN_DIR="$TMP_BIN" sh scripts/install.sh >/dev/null 2>&1; then
  got=$("$TMP_BIN/lk" version 2>/dev/null | jq -r .version 2>/dev/null || true)
  if [ "$got" = "$NUM" ]; then
    pass "install.sh instalou $NUM (checksum conferido pelo próprio script)"
  else
    fail "install.sh instalou versão \"$got\", esperado \"$NUM\""
  fi
else
  fail 'install.sh falhou (asset ausente para esta plataforma? checksum?)'
fi

# --- 4. instalação via Homebrew (só com --local: mexe no lk da máquina) ---
step 'Instalação via Homebrew'
if [ "$LOCAL" -eq 0 ]; then
  skip 'sem --local: nada instalado no sistema'
elif ! command -v brew >/dev/null 2>&1; then
  skip 'brew não instalado nesta máquina'
else
  brew install linkanalabs/tap/lk >/dev/null 2>&1 || true
  got=$(lk version 2>/dev/null | jq -r .version 2>/dev/null || true)
  if [ "$got" = "$NUM" ]; then
    pass "brew install linkanalabs/tap/lk -> lk $NUM"
  else
    fail "lk instalado está em \"$got\", esperado \"$NUM\" (tente brew reinstall linkanalabs/tap/lk)"
  fi
fi

# --- veredito ---
if [ "$FAILURES" -gt 0 ]; then
  printf '\nverificação REPROVADA: %d falha(s) em %s.\n' "$FAILURES" "$VERSION" >&2
  exit 1
fi

printf '\nrelease %s verificada\n' "$VERSION"
