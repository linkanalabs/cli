# lk — Linkana CLI

CLI interna da Linkana. Consumida pelo Cowork (Claude) em nome dos times de
CS/Onboarding para parametrizar buyers em massa, sem UI e sem pedir scripts à
engenharia. Fala com o backend Rails via `format.json`.

> Inspiração: [`fizzy-cli`](https://github.com/basecamp/fizzy-cli) /
> [`fizzy-sdk`](https://github.com/basecamp/fizzy-sdk). Ver `docs/references/fizzy-reference.md`.

## Status

Branch de setup — esqueleto + `doctor` sem auth. Auth (PAT) e comandos de recurso
vêm depois.

## Requisitos

- Go 1.26+

## Build & uso

```bash
make build
go run ./cmd/lk version
go run ./cmd/lk doctor
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
| `lk doctor` | checks básicos: version, runtime, config, filesystem, reachability (`GET /up`) |
| `lk --help` | ajuda |

## Desenvolvimento

```bash
make test    # testes com -race
make cover   # gate de cobertura ≥95%
make lint    # golangci-lint
make dev     # lk doctor contra localhost:3000
```

Regras do repo em `CLAUDE.md`. **Não abra PR sem `make test` verde e cobertura ≥95%.**
