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
internal/commands/ árvore cobra: root, version, doctor, auth, whoami
internal/client/   HTTP fino (format.json) + interface API mockável (Get, GetIdentity)
internal/config/   base_url via YAML (XDG) + env LK_API_URL
internal/auth/     storage do PAT: keychain (go-keyring) + fallback arquivo atômico, por origin
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

Esqueleto + `doctor` + **auth via PAT (CLI)** + **suppliers (SRM)** +
**impersonação (LIN-5921)** + **modo read/write (LIN-5985)**. Comandos:
`version`, `doctor` (version, runtime, config, filesystem, reachability `GET /up`,
**Authentication** via `GET /my/identity.json` — pass/fail/skip, com skip-cascade
quando o backend está inalcançável), `auth login|status|logout`, `whoami`,
`supplier list|show`, `impersonate <ref>|stop|status`, `mode`, `mode write`,
`mode read`.

`supplier list` → `GET /srm/suppliers` (array bare de suppliers). `supplier show
<id>` → `GET /srm/suppliers/<id>/panel` (um supplier). Contrato do supplier:
`{id, name, identifier, legal_entity, state, tags:[{id, display_name}]}`. 401 →
hint `lk auth login`.

Storage do token em `internal/auth`: OS keychain (`go-keyring`) com fallback de
arquivo atômico (temp+rename, 0600), por origin (base_url). `LK_TOKEN` substitui
o token **original** — mas uma impersonação ativa tem precedência sobre `LK_TOKEN`
(ver seção abaixo). `LK_NO_KEYRING` força o fallback (usado em testes).

Credencial: `lkn_<short>_<long>`; header `Authorization: Bearer <cred>`.

Próximas fases: comandos de recurso (buyer), `lk schema` self-describing,
`SKILL.md` embarcado.

## Impersonação / buyer-scope (LIN-5921)

Comandos SRM são **buyer-scoped** (dependem de `current_user.buyer`). O agente não
tem sessão de buyer próprio; para agir num buyer, **impersone o usuário `@linkana`
daquele buyer**:

- `lk impersonate <email|user_id>` — cunha um Access Token real no buyer+user alvo
  (gate no backend: caller `linkana_admin?` + alvo `@linkana` + buyer com
  `allow_linkana_support`). Default TTL 24h, `--ttl` ajusta. Parâmetro do request:
  `target` (não `user`).
- `lk impersonate status` — mostra a impersonação ativa (alvo, buyer, expiry).
- `lk impersonate stop` — revoga o token no servidor e limpa o estado local.

**Estado pegajoso:** enquanto há impersonação gravada, o token original fica
inacessível. Expirou (relógio local) → comando falha com erro duro. Rejeitado pelo
servidor (401) → mesmo erro duro. **Nunca** cai silenciosamente pro usuário
original — escolha sempre `lk impersonate stop` ou re-impersonar.

**Precedência de credencial:** contexto de impersonação ativo > `LK_TOKEN` >
PAT do keychain/arquivo. `LK_TOKEN` sobrescreve apenas o token **original**; não
desativa nem bypassa uma impersonação ativa. Isso foi decidido intencionalmente para
fechar um footgun de segurança (antes, `LK_TOKEN` silenciosamente ignorava a
impersonação).

## Modo read/write (LIN-5985)

Cada origin tem um modo independente, persistido em `modes.json` (XDG). **Padrão: read.**

- `lk mode` — exibe o modo atual do origin ativo (JSON ou styled).
- `lk mode write` — habilita escrita; exige TTY interativo + digitação literal de `"write"`. Agente de IA não consegue habilitar write sem humano no terminal.
- `lk mode read` — retorna ao modo read sem confirmação.

**Gate em `client.do`:** qualquer requisição não-GET em modo read retorna `client.ErrReadOnly`
(`CLI is in read mode`). Comandos de leitura (`GET`) sempre passam.

**Credencial:** `authedClient()` injeta o modo no `client.Client`; impersonação ativa
herda o modo — não há bypass silencioso. Os verbos de impersonação (`POST/DELETE
/impersonation`) também passam pelo gate: `lk impersonate <ref>` exige modo write;
`lk impersonate stop` sempre limpa o estado local, mas a revogação remota em modo
read falha com aviso (best-effort).

**Storage:** `internal/mode/` — `Load(origin)` / `Save(origin, m)`. Arquivo atômico
(temp+rename, 0o600). Chave no mapa = `cfg.BaseURL`.

## Repositório backend (Rails)

O backend Linkana fica em `../linkana` (working dir adicional). Referências da
impersonação:

- `app/controllers/impersonations_controller.rb` — endpoint JSON `POST/DELETE /impersonation`.
- `app/policies/srm_policy.rb` — `enforce_impersonation_rules` (gate de escrita).
- `lib/warden/pat_bearer_strategy.rb` — PAT Bearer → `current_user`.
- `app/models/api_token.rb` + `app/models/api_tokens/build.rb` — Access Token.
- `buyers.allow_linkana_support` — flag que libera suporte (toggle em
  `app/controllers/srm_settings/access_configurations_controller.rb`).
