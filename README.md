# lk — Linkana CLI

CLI interna da Linkana. Consumida pelo Cowork (Claude) em nome dos times de
CS/Onboarding para parametrizar buyers em massa, sem UI e sem pedir scripts à
engenharia. Fala com o backend Rails via `format.json`.

> Inspiração: [`fizzy-cli`](https://github.com/basecamp/fizzy-cli) /
> [`fizzy-sdk`](https://github.com/basecamp/fizzy-sdk). Ver `docs/references/fizzy-reference.md`.

## Status

Esqueleto + `doctor` + autenticação via PAT (`auth`, `whoami`) + suppliers
(`supplier list|show`). Mais comandos de recurso vêm depois.

## Instalação

**macOS** — via [Homebrew](https://brew.sh) (cask no tap da linkanalabs):

```bash
brew install linkanalabs/tap/lk
lk doctor
```

O cask é atualizado automaticamente pelo [GoReleaser](https://goreleaser.com) a
cada release. Para atualizar: `lk update` (ou `brew install linkanalabs/tap/lk`,
que funciona em qualquer estado — `brew upgrade lk` só funciona se o tap já
tiver sido adicionado).

**Linux** — via script instalador (detecta OS/arch, baixa do último release,
verifica checksum, instala em `~/.local/bin/lk`):

```bash
curl -fsSL https://raw.githubusercontent.com/linkanalabs/cli/main/scripts/install.sh | sh
lk doctor
```

Override o diretório com `LK_BIN_DIR` e a versão com `LK_VERSION` (ex:
`LK_VERSION=v0.6.0`). Para atualizar, rode o mesmo comando. O script também
funciona em macOS (fallback sem brew).

<details>
<summary>Outras formas</summary>

```bash
# Build a partir do código (precisa de Go 1.26+)
git clone https://github.com/linkanalabs/cli && cd cli && make build && ./lk doctor

# Ou baixe o tar.gz da sua plataforma direto dos releases:
# https://github.com/linkanalabs/cli/releases
```

</details>

## Atualização

```bash
lk update --check   # o que existe publicado, sem instalar nada
lk update           # atualiza
```

Instalado via Homebrew, `lk update` roda `brew upgrade --cask lk`. Ele **nunca**
roda um `brew update` pelado: isso não tem escopo por tap e atualizaria todos os
taps da máquina. Quando a metadata do brew está atrasada, o `lk` mostra o comando
a rodar em vez de rodar por você.

Fora do Homebrew, o `lk` não consegue se substituir e imprime o comando de
reinstalação.

Códigos de saída de `lk update` (sem `--check`): `0` já está atualizado ou
atualizou; `1` a atualização foi tentada e não aconteceu — a saída diz por quê e
como. **`lk update --check` é leitura e sempre sai `0` quando a consulta
funcionou**: a resposta está no campo `update_available`, não no código de
saída. Isso é deliberado — usar o código como resposta impediria distinguir
"tem atualização" de "a rede caiu".

O `lk` também checa sozinho, **no máximo uma vez a cada 24h**, depois que o
comando já produziu a saída. Quando encontra versão nova, dispara o
`brew upgrade` em segundo plano e avisa em uma linha no **stderr** (stdout
continua sendo só o contrato JSON); a versão nova vale a partir da execução
seguinte. Nunca checa em CI, nem em build local (`dev`), nem quando
`LK_NO_AUTO_UPDATE` está setado:

```bash
export LK_NO_AUTO_UPDATE=1   # desliga a checagem automática
```

O `lk doctor` também reporta se há versão nova (como `warn`, nunca `fail`).

## Requisitos

- Go 1.26+ (apenas para build a partir do código)

## Build & uso

```bash
make build        # gera ./lk
./lk version
./lk doctor
```

Saída é JSON por padrão fora de terminal (machine-readable) e texto legível em
TTY. O `--format` força o formato em qualquer comando, dinâmico ou não, e um
valor desconhecido é rejeitado no parse (nenhuma requisição é gasta num typo):

| `--format` | Saída |
|---|---|
| `auto` | styled em TTY, `json` em pipe (default) |
| `json` | o contrato estável: a resposta, indentada |
| `styled` | tabela alinhada ou bloco chave/valor, para humano |
| `markdown` | tabela GFM ou lista `- **chave:** valor`, para colar em documento |
| `ids` | um `id` por linha, para encadear no próximo comando |
| `count` | quantos registros **esta resposta** carrega |

`styled` e `markdown` são **genéricos**, derivados do shape da resposta: array
de objetos vira tabela (colunas na ordem em que o backend emite as chaves),
objeto vira chave/valor, e shapes sem forma caem no JSON. Comandos de
diagnóstico (`doctor`, `version`, …) mantêm styled próprio — `markdown` sempre
usa o genérico, porque `Styled()` é saída de terminal, não documento.

Duas ressalvas que valem para quem automatiza:

- `count` conta o que voltou, **não o total no buyer**. `supplier list` é
  paginado (10 por página, e o JSON não traz metadado de paginação), então
  `count` ali nunca passa de 10.
- `ids` exige que o recurso seja chaveado por `id`. Num recurso que não é
  (as mensagens de e-mail do SRM são chaveadas por `template`), o comando
  **falha com exit 1** apontando o `--format json` — em vez de imprimir nada e
  deixar o agente concluir que a lista está vazia. Lista de verdade vazia
  imprime nada e sai 0.

## Configuração

| Fonte | Como |
|---|---|
| Env | `LK_API_URL` sobrepõe o arquivo (maior precedência) |
| Arquivo | `~/.config/lk/config.yml` (`base_url: ...`), respeita `XDG_CONFIG_HOME` |
| Default | `https://app.linkana.com` (produção) |

`lk config` mostra o `base_url` efetivo e a origem (env/arquivo/default);
`lk config set-url <url>` grava o `base_url` no arquivo. Em desenvolvimento,
aponte para o backend local com `LK_API_URL=http://localhost:3000` (ver `make dev`).

## Comandos

| Comando | O que faz |
|---|---|
| `lk version` | versão do binário |
| `lk doctor` | checks: version, runtime, config, filesystem, reachability (`GET /up`) e autenticação (`GET /my/identity.json`, pula se sem token ou backend inalcançável) |
| `lk auth login` | guarda um PAT (`--token`, env `LK_TOKEN` ou prompt no stdin) para o `base_url` ativo |
| `lk auth status` | mostra se há token guardado e a origem (env/keychain/arquivo), sem revelar o segredo |
| `lk auth logout` | apaga o token guardado do `base_url` ativo |
| `lk whoami` | mostra a identidade autenticada (`GET /my/identity.json`) |
| `lk supplier list` | lista suppliers (`GET /srm/suppliers`); JSON é um array bare |
| `lk supplier show <id>` | mostra um supplier (`GET /srm/suppliers/<id>/panel`) |
| `lk --help` | ajuda |

## Autenticação

```bash
lk auth login --token lkn_xxx_yyy   # ou: LK_TOKEN=lkn_... lk auth login
lk whoami                            # confirma a identidade
lk auth status
lk auth logout
```

O token é guardado no keychain do SO (via `go-keyring`), com fallback para um
arquivo atômico `0600` em `~/.config/lk/tokens/` (respeita `XDG_CONFIG_HOME`),
sempre por `base_url`. A env `LK_TOKEN` sobrepõe o que estiver guardado;
`LK_NO_KEYRING` força o fallback de arquivo (útil em CI/headless).

## Desenvolvimento

```bash
make test    # testes com -race
make cover   # gate de cobertura ≥95%
make lint    # golangci-lint
make dev     # lk doctor contra localhost:3000
```

Regras do repo em `CLAUDE.md`. **Não abra PR sem `make test` verde e cobertura ≥95%.**
