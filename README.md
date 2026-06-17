# lk — Linkana CLI

CLI interna da Linkana. Consumida pelo Cowork (Claude) em nome dos times de
CS/Onboarding para parametrizar buyers em massa, sem UI e sem pedir scripts à
engenharia. Fala com o backend Rails via `format.json`.

> Inspiração: [`fizzy-cli`](https://github.com/basecamp/fizzy-cli) /
> [`fizzy-sdk`](https://github.com/basecamp/fizzy-sdk). Ver `docs/references/fizzy-reference.md`.

## Status

Esqueleto + `doctor` + autenticação via PAT (`auth`, `whoami`). Comandos de
recurso (buyer) vêm depois.

## Instalação

```bash
curl -fsSL https://raw.githubusercontent.com/linkanalabs/cli/main/scripts/install.sh | bash
```

O instalador detecta OS/arquitetura (Linux/macOS, amd64/arm64), baixa o binário do
último [release](https://github.com/linkanalabs/cli/releases), verifica o checksum
e instala em `~/.local/bin/lk` (override com `LK_BIN_DIR`). Depois:

```bash
lk doctor
```

<details>
<summary>Outras formas</summary>

```bash
# Via Go (precisa de Go 1.26+)
go install github.com/linkanalabs/cli/cmd/lk@latest

# A partir do código
git clone https://github.com/linkanalabs/cli && cd cli && make build && ./lk doctor
```

</details>

## Requisitos

- Go 1.26+ (apenas para build a partir do código)

## Build & uso

```bash
make build        # gera ./lk
./lk version
./lk doctor
```

Saída é JSON por padrão (machine-readable). Use `--format styled` para texto
legível no terminal.

## Configuração

| Fonte | Como |
|---|---|
| Arquivo | `~/.config/lk/config.yml` (`base_url: ...`), respeita `XDG_CONFIG_HOME` |
| Env | `LK_API_URL` sobrepõe o arquivo |
| Default | `http://localhost:3000` |

## Comandos

| Comando | O que faz |
|---|---|
| `lk version` | versão do binário |
| `lk doctor` | checks: version, runtime, config, filesystem, reachability (`GET /up`) e autenticação (`GET /my/identity.json`, pula se sem token ou backend inalcançável) |
| `lk auth login` | guarda um PAT (`--token`, env `LK_TOKEN` ou prompt no stdin) para o `base_url` ativo |
| `lk auth status` | mostra se há token guardado e a origem (env/keychain/arquivo), sem revelar o segredo |
| `lk auth logout` | apaga o token guardado do `base_url` ativo |
| `lk whoami` | mostra a identidade autenticada (`GET /my/identity.json`) |
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
