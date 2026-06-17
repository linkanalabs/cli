# Design — Setup da CLI `lk`

Data: 2026-06-17
Status: aprovado (incremental), em implementação

## Objetivo

Bootstrap da CLI interna da Linkana em Go (binário `lk`), inspirada no
`fizzy-cli`/`fizzy-sdk`. Este branch entrega **só o esqueleto + doctor sem auth**.
Auth (PAT) e comandos de recurso (buyer) são fases futuras.

Contexto e referências: ver `docs/references/fizzy-reference.md`.

## Escopo deste branch (setup)

INCLUI:
- Estrutura Go + cobra: `lk`, `lk --version`, `lk --help`/`lk help`, `lk version`, `lk doctor`.
- `internal/client` — HTTP fino com interface mockável (`API`).
- `internal/config` — base_url (default `http://localhost:3000`), via YAML + env `LK_API_URL`.
- `internal/output` — render JSON (default) e styled (lipgloss).
- `lk doctor` SEM checar auth: version, runtime, config, filesystem, reachability (`GET /up`).
- Suite TDD com cobertura ≥95%.
- `Makefile`, `.golangci.yml`, CI GitHub Actions (Go), `CLAUDE.md`, `README.md`.

NÃO INCLUI (fases futuras):
- Auth / PAT / keychain (`lk auth`), mudanças no Rails Linkana.
- Comandos de recurso (`lk buyer ...`), `lk schema`, `SURFACE.txt`, `SKILL.md`.
- Codegen OpenAPI, camada de resiliência (circuit breaker etc.).

## Layout

```
cli/
├── cmd/lk/main.go            # entrypoint trivial → commands.Execute()
├── internal/
│   ├── commands/
│   │   ├── root.go           # cobra root, flag --format json|styled, Execute()
│   │   ├── version.go        # lk version
│   │   └── doctor.go         # checks básicos sem auth
│   ├── client/
│   │   ├── interface.go      # API interface (mockável)
│   │   └── client.go         # HTTP: Get, buildURL(.json), Bearer (placeholder), timeout
│   ├── config/
│   │   └── config.go         # load/save base_url, XDG + LK_API_URL
│   └── output/
│       └── output.go         # render JSON / styled
├── Makefile
├── .golangci.yml
├── .github/workflows/ci.yml
├── go.mod                    # module github.com/linkanalabs/cli, go 1.26
├── CLAUDE.md
├── README.md
└── docs/
```

## Doctor

Cada check → `Check{Name, Status(pass|warn|fail), Message, Hint}`. Agregado em
`Result{Checks, Passed, Failed, Warned}` + `Summary()`. Saída JSON por padrão.

Checks (nesta ordem):
1. **Version** — versão do binário.
2. **Runtime** — Go version, OS/arch.
3. **Config** — arquivo de config legível + base_url setada.
4. **Filesystem** — dir de config gravável.
5. **API Reachability** — `GET {base_url}/up`, timeout 5s. 2xx/3xx = pass; erro de
   rede = fail; status alto = warn.

Struct comporta checks de auth/recurso no futuro sem refactor.

## Client

- `API` interface: `Get(ctx, path) (*Response, error)` (cresce depois).
- `Client` concreto: `buildURL` injeta `.json`, base configurável, header
  `Accept: application/json` e Bearer (vazio por enquanto), timeout.
- `Response{StatusCode, Body, Header}`.
- Mock em teste implementa `API` — comandos testáveis sem rede.

## Config

- YAML em `~/.config/lk/config.yml` (XDG; respeita `XDG_CONFIG_HOME`).
- Campo `base_url`. Env `LK_API_URL` sobrepõe.
- Default `http://localhost:3000` quando nada setado.

## Qualidade (ver CLAUDE.md)

- TDD: teste antes da implementação.
- Cobertura ≥95% (`make cover` falha abaixo). `main.go` trivial fica fora da meta.
- `golangci-lint` limpo. `gofmt`/`goimports`.
- Nunca abrir PR sem `make test` verde.

## CI

`.github/workflows/ci.yml`: trigger push(main) + pull_request. Jobs: lint
(golangci-lint), test (`go test -race -coverprofile` + gate ≥95%), build.
`actions/setup-go` lendo versão do `go.mod`. Runner `ubuntu-latest`.

## Testabilidade ("prático demais")

- `make build|test|cover|lint|run ARGS="..."`.
- `make dev` → `lk doctor` contra `localhost:3000`.
- Client mockável; doctor testado com client fake + config temp.
