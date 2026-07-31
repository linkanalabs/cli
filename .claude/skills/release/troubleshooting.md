# Recuperação de falhas na release

Referenciado por [SKILL.md](SKILL.md). Ler só quando algo falhou.

## `guard` reprovou (nada foi publicado)

O job roda antes do GoReleaser, então a release não existe e o cask não mudou.
Corrigir a causa na `main` e reaproveitar o mesmo número:

```bash
git tag -d vX.Y.Z                 # apaga local
git push origin :refs/tags/vX.Y.Z # apaga remota
# corrige, mergeia na main, e taguear de novo
```

Reusar a versão só é seguro aqui, justamente porque nada foi publicado.

Motivos comuns: tag fora de `vX.Y.Z`; tag num commit que não está na `origin/main`
(branch não mergeado, ou tag criada antes do merge); `make cover` abaixo do mínimo.

## `verify` reprovou no cask

Sintoma: release publicada, `cask em version "<antiga>"`. É o modo de falha
silencioso que o job existe para pegar.

1. Re-rodar o job antes de concluir qualquer coisa — descarta propagação lenta:
   `gh run rerun <run-id> --job verify`.
2. Persistiu: o suspeito é o secret `HOMEBREW_TAP_GITHUB_TOKEN` (PAT fine-grained
   com Contents **write** só no tap). Expirado ou sem permissão, o GoReleaser não
   consegue commitar. Conferir com `gh secret list` e olhar o log do job
   `goreleaser` procurando o passo do cask.
3. Corrigido o token, re-rodar o job `goreleaser` do mesmo run — ele regenera e
   commita o cask. **Não editar `Casks/lk.rb` à mão**: o arquivo é gerado e a
   próxima release sobrescreve, então o conserto manual esconde o problema real.

## Release já publicada com bug

Tag publicada não se reescreve — alguém já pode ter instalado. Sobe `vX.Y.Z+1` com
a correção. Reaproveitar número publicado deixa `brew` e `install.sh` servindo
binários diferentes com o mesmo nome.

## Prerelease (`-rc`)

`.goreleaser.yaml` tem `release: prerelease: auto`, então uma tag com sufixo
(`v0.8.0-rc1`) entra como prerelease: não vira `latest` e o `install.sh`, que lê
`releases/latest`, não a pega por default (só com `LK_VERSION=v0.8.0-rc1`).

**Antes de usar uma tag `-rc` para testar:** conferir se `homebrew_casks` sobe o
cask em prerelease. O bloco não declara `skip_upload`, então o comportamento
default do GoReleaser decide — verificar na doc da versão em uso, porque um cask
apontando para um rc afeta todo mundo que instala via brew.

## Brew servindo versão antiga mesmo com o cask novo

Cache local do brew:

```bash
brew reinstall linkanalabs/tap/lk
```

Se `lk version` continuar velho, checar se há outro `lk` antes no `PATH`
(`which -a lk`) — por exemplo um instalado via `install.sh` em `~/.local/bin`.

## Release local de contingência (só com aprovação)

Quando o CI está indisponível e a release não pode esperar:

```bash
GITHUB_TOKEN=$(gh auth token) HOMEBREW_TAP_GITHUB_TOKEN=$(gh auth token) \
  goreleaser release --clean
```

Isso **pula os jobs `guard` e `verify`**. Rodar `make release-preflight` antes e
`make release-verify VERSION=vX.Y.Z LOCAL=1` depois, à mão, para não perder os
gates que o workflow daria de graça.

## Limpeza pendente (não bloqueia release)

O repo tem dois secrets de tap: `HOMEBREW_TAP_GITHUB_TOKEN` (o usado) e
`HOMEBREW_TAP_GIHUB_TOKEN` (typo, sem o `T`, nada lê). Apagar o segundo evita que
alguém rode atrás do secret errado num diagnóstico futuro.
