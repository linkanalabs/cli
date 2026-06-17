# CLAUDE.md — lk (Linkana CLI)

CLI interna da Linkana em Go. Binário `lk`. Consumida pelo Cowork (Claude) em nome
do time CS/Onboarding para parametrizar buyers em massa. **Usuário primário é um
agente de IA**, não humano → saída JSON por padrão, machine-readable obrigatório.

Comunica com o backend Rails (`linkana`) via `format.json` (sem API dedicada).
Inspirada no `fizzy-cli` / `fizzy-sdk` da Basecamp — ver `docs/references/fizzy-reference.md`.

## Regras inegociáveis

- **Cobertura de testes ≥ 95% (total).** PR abaixo disso é barrado. Rode `make cover`.
- **Nunca abra PR / suba código sem `make test` verde** localmente.
- **TDD:** escreva o teste antes da implementação.
- **`golangci-lint run` limpo** antes de commitar (`make lint`).
- **`gofmt`/`goimports`** aplicados (`make fmt`).

## Boas práticas Go

- Embrulhe erros com contexto: `fmt.Errorf("fazendo X: %w", err)`.
- Sem `panic` em fluxo normal. Sem `os.Exit` fora de `main`/`Execute` — retorne erro.
- Interfaces pequenas, definidas no consumidor (ex: `client.API`).
- `context.Context` em toda chamada de I/O.
- Sem estado global mutável fora de config/version.
- Injete seams (vars de função) para tornar ramos de erro testáveis, em vez de
  deixar código sem cobertura.

## Boas práticas CLI

- Saída **JSON é contrato**: estável, versionável. `--format styled` é bônus humano.
- **stdout = dados, stderr = diagnóstico.**
- **Exit codes** significativos (0 ok, 1 falha). Comandos renderizam o próprio
  resultado e sinalizam falha via erro; `run()` traduz em exit code.
- `--help` em todo comando. Flags de leitura não têm efeito colateral.
- Comandos dependem de interfaces (mockáveis), não de clients concretos.

## Layout

```
cmd/lk/            entrypoint (trivial; fora da meta de cobertura)
internal/commands/ árvore cobra: root, version, doctor
internal/client/   HTTP fino (format.json) + interface API mockável
internal/config/   base_url via YAML (XDG) + env LK_API_URL
internal/output/   render JSON (default) / styled
```

## Comandos comuns

- `make build` — compila
- `make test` — testes com `-race`
- `make cover` — testes + gate de cobertura ≥95%
- `make lint` — golangci-lint
- `make run ARGS="doctor"` — roda local
- `make dev` — `lk doctor` contra `localhost:3000`

## Estado atual

Branch de setup: esqueleto + `doctor` **sem auth** (version, runtime, config,
filesystem, reachability `GET /up`). Próximas fases: auth via PAT (CLI + Rails),
comandos de recurso (buyer), `lk schema` self-describing, `SKILL.md` embarcado.
