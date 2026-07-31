#!/usr/bin/env bash
# Gate determinístico antes de taguear uma release do `lk`.
#
# Uso:
#   scripts/release-preflight.sh [vX.Y.Z]
#       Roda todos os gates. Sem argumento, deriva a versão recomendada; com
#       argumento, valida também a versão pedida contra o bump mínimo derivado.
#   scripts/release-preflight.sh --bump-only [--base <tag>]
#       Só imprime a versão derivada (leitura pura, nenhum gate). `--base` troca
#       a tag de referência — serve para conferir a derivação contra o histórico.
#
# Exit 0 = todos os gates passaram (e só então os comandos de tag são impressos).
# Exit 1 = algum FAIL. Exit 2 = erro de uso.
#
# Nunca cria tag, nunca faz push, nunca altera arquivo: é gate, não ação.
set -euo pipefail

cd "$(dirname "$0")/.."

FAILURES=0

pass() { printf '  PASS  %s\n' "$*"; }
fail() { printf '  FAIL  %s\n' "$*"; FAILURES=$((FAILURES + 1)); }
skip() { printf '  SKIP  %s\n' "$*"; }
warn() { printf '  WARN  %s\n' "$*"; }
info() { printf '  INFO  %s\n' "$*"; }
step() { printf '\n%s\n' "$*"; }
die() { printf 'erro: %s\n' "$*" >&2; exit 2; }
usage() { awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"; }

# --- semver ---
semver_ok() { printf '%s' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; }

# v1.2.3 -> 1002003, para comparação numérica.
ver_num() {
  printf '%s' "${1#v}" | awk -F. '{ printf "%d", $1 * 1000000 + $2 * 1000 + $3 }'
}

# next_version <tag> <minor|patch> -> vX.Y.Z
next_version() {
  local t=${1#v} kind=$2 major minor patch
  major=$(printf '%s' "$t" | cut -d. -f1)
  minor=$(printf '%s' "$t" | cut -d. -f2)
  patch=$(printf '%s' "$t" | cut -d. -f3)
  if [ "$kind" = minor ]; then
    printf 'v%d.%d.0' "$major" $((minor + 1))
  else
    printf 'v%d.%d.%d' "$major" "$minor" $((patch + 1))
  fi
}

# --- derivação do bump a partir do golden da superfície ---
#
# SURFACE.txt é o golden da árvore completa de comandos e é versionado, então a
# diferença entre a tag e a árvore atual diz o que aconteceu com o contrato da
# CLI, sem depender da mensagem de commit:
#   linha removida   -> comando/flag saiu da superfície -> minor (pré-1.0)
#   linha adicionada -> comando novo                    -> minor
#   golden idêntico  -> superfície intacta              -> patch
#
# Escreve nas globais SURFACE_KIND e SURFACE_NOTE (não pode rodar em subshell,
# senão as globais se perdem).
SURFACE_KIND=""
SURFACE_NOTE=""

derive_bump() {
  local base=$1 d removed added
  if ! git cat-file -e "$base:SURFACE.txt" 2>/dev/null; then
    SURFACE_KIND=minor
    SURFACE_NOTE="sem SURFACE.txt em $base, assumindo minor por precaução"
    return 0
  fi
  d=$(diff <(git show "$base:SURFACE.txt") SURFACE.txt || true)
  removed=$(printf '%s\n' "$d" | grep -c '^< ' || true)
  added=$(printf '%s\n' "$d" | grep -c '^> ' || true)
  if [ "$removed" -gt 0 ]; then
    SURFACE_KIND=minor
    SURFACE_NOTE="$removed comando(s) removido(s) da superfície, $added adicionado(s)"
  elif [ "$added" -gt 0 ]; then
    SURFACE_KIND=minor
    SURFACE_NOTE="$added comando(s) adicionado(s)"
  else
    SURFACE_KIND=patch
    SURFACE_NOTE="superfície intacta"
  fi
}

# --- argumentos ---
BUMP_ONLY=0
BASE_TAG=""
WANTED=""

while [ $# -gt 0 ]; do
  case "$1" in
    --bump-only) BUMP_ONLY=1 ;;
    --base)
      shift
      [ $# -gt 0 ] || die "--base exige uma tag"
      BASE_TAG=$1
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    -*) die "flag desconhecida: $1" ;;
    *)
      if [ -n "$WANTED" ]; then
        die "versão informada duas vezes: $WANTED e $1"
      fi
      WANTED=$1
      ;;
  esac
  shift
done

if [ -n "$BASE_TAG" ] && [ "$BUMP_ONLY" -eq 0 ]; then
  die "--base só vale com --bump-only (o gate sempre usa a última tag)"
fi

if [ -z "$BASE_TAG" ]; then
  BASE_TAG=$(git describe --tags --abbrev=0 2>/dev/null) || die "repositório sem nenhuma tag"
fi
semver_ok "$BASE_TAG" || die "tag base fora do padrão vX.Y.Z: $BASE_TAG"

derive_bump "$BASE_TAG"
DERIVED=$(next_version "$BASE_TAG" "$SURFACE_KIND")

if [ "$BUMP_ONLY" -eq 1 ]; then
  printf 'base=%s\n' "$BASE_TAG"
  printf 'bump=%s (%s)\n' "$SURFACE_KIND" "$SURFACE_NOTE"
  printf 'recommended_version=%s\n' "$DERIVED"
  exit 0
fi

if [ -n "$WANTED" ]; then
  semver_ok "$WANTED" || die "versão fora do padrão vX.Y.Z: $WANTED (prerelease é decisão manual)"
fi

printf 'preflight de release — base %s\n' "$BASE_TAG"

# --- 1. estado do repositório ---
step 'Estado do repositório'
git fetch --tags --quiet origin

HEAD_SHA=$(git rev-parse HEAD)
SHORT_SHA=$(git rev-parse --short HEAD)

# O que importa não é o nome do branch local, e sim que o commit a ser tagueado
# já esteja na origin/main — a mesma invariante que o job `guard` do release.yml
# aplica no servidor.
if git merge-base --is-ancestor "$HEAD_SHA" origin/main; then
  pass "$SHORT_SHA está contido na origin/main"
else
  fail "$SHORT_SHA não está na origin/main — release só sai de código mergeado"
fi

if [ -z "$(git status --porcelain --untracked-files=no)" ]; then
  pass 'nenhuma alteração pendente em arquivo versionado'
else
  fail 'há alteração não comitada em arquivo versionado (a tag não a incluiria)'
fi

UNTRACKED=$(git ls-files --others --exclude-standard | tr '\n' ' ')
if [ -n "$UNTRACKED" ]; then
  warn "arquivo(s) não versionado(s), fora da tag: $UNTRACKED"
fi

# --- 2. escopo da release ---
step 'Escopo da release'
COMMITS=$(git rev-list --count "$BASE_TAG..HEAD")
if [ "$COMMITS" -gt 0 ]; then
  pass "$COMMITS commit(s) desde $BASE_TAG"
else
  fail "nada para lançar: HEAD é a própria $BASE_TAG"
fi

info "bump derivado: $SURFACE_KIND ($SURFACE_NOTE) -> $DERIVED"

VERSION=$DERIVED
if [ -n "$WANTED" ]; then
  VERSION=$WANTED
  if [ "$(ver_num "$WANTED")" -lt "$(ver_num "$DERIVED")" ]; then
    fail "$WANTED é menor que o mínimo derivado $DERIVED ($SURFACE_NOTE)"
  elif [ "$WANTED" = "$DERIVED" ]; then
    pass "$WANTED bate com o bump derivado"
  else
    pass "$WANTED é um bump maior que o mínimo derivado $DERIVED"
  fi
fi

# --- 3. golden da superfície ---
step 'Golden da superfície'
if go test ./internal/commands -run TestSurfaceGolden >/dev/null 2>&1; then
  pass 'SURFACE.txt corresponde à árvore de comandos'
else
  fail 'TestSurfaceGolden falhou — golden desatualizado (go test ./internal/commands -run TestSurfaceGolden -update)'
fi

# --- 4. CI verde no sha exato ---
step 'CI no commit a ser tagueado'
if ! command -v gh >/dev/null 2>&1; then
  fail 'gh não instalado — sem ele não dá para confirmar CI verde no sha'
else
  CHECKS=$(gh api "repos/{owner}/{repo}/commits/$HEAD_SHA/check-runs" \
    --jq '.check_runs[] | "\(.name)\t\(.conclusion)"' 2>/dev/null || true)
  if [ -z "$CHECKS" ]; then
    fail "nenhum check-run em $SHORT_SHA (o commit chegou na origin? a CI rodou?)"
  else
    # Os três jobs do ci.yml. Nome exato, para um job renomeado virar FAIL
    # explícito em vez de sumir da verificação em silêncio.
    for job in 'Lint' 'Test (race + coverage gate)' 'Build'; do
      conclusion=$(printf '%s\n' "$CHECKS" | awk -F'\t' -v j="$job" '$1 == j { print $2; exit }')
      if [ -z "$conclusion" ]; then
        fail "job \"$job\" não rodou em $SHORT_SHA"
      elif [ "$conclusion" = success ]; then
        pass "$job verde em $SHORT_SHA"
      else
        fail "$job em $SHORT_SHA: $conclusion"
      fi
    done
  fi
fi

# --- 5. config do GoReleaser (só quando mudou) ---
step 'Config do GoReleaser'
if git diff --quiet "$BASE_TAG..HEAD" -- .goreleaser.yaml; then
  skip ".goreleaser.yaml inalterado desde $BASE_TAG, check dispensado"
elif ! command -v goreleaser >/dev/null 2>&1; then
  fail '.goreleaser.yaml mudou e goreleaser não está instalado (brew install goreleaser)'
elif goreleaser check >/dev/null 2>&1; then
  pass 'goreleaser check limpo'
else
  fail 'goreleaser check reprovou (rode `goreleaser check` para ver o motivo)'
fi

# --- 6. testes, cobertura e lint ---
# make test e make cover se sobrepõem em parte (cover roda só ./internal/...),
# mas os dois são o gate documentado no CLAUDE.md — rodar ambos é intencional.
step 'Testes'
if make test >/dev/null 2>&1; then
  pass 'make test'
else
  fail 'make test vermelho (rode `make test` para ver o motivo)'
fi

if COVER_OUT=$(make cover 2>&1); then
  pass "make cover — $(printf '%s\n' "$COVER_OUT" | grep 'total coverage' || echo 'gate ok')"
else
  fail 'make cover reprovou (rode `make cover` para ver a cobertura)'
fi

if command -v golangci-lint >/dev/null 2>&1; then
  if make lint >/dev/null 2>&1; then
    pass 'make lint'
  else
    fail 'make lint vermelho (rode `make lint` para ver o motivo)'
  fi
else
  skip 'golangci-lint não instalado (brew install golangci-lint) — quem cobre o lint é o job Lint da CI, checado acima'
fi

# --- veredito ---
if [ "$FAILURES" -gt 0 ]; then
  printf '\npreflight REPROVADO: %d falha(s). Nada de tag até tudo passar.\n' "$FAILURES" >&2
  exit 1
fi

printf '\npreflight ok\n'
printf 'recommended_version=%s\n' "$VERSION"
printf '\nPróximo passo, só após aprovação explícita:\n'
printf '  git tag -a %s -m "%s"\n' "$VERSION" "$VERSION"
printf '  git push origin %s\n' "$VERSION"
