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
**impersonação (LIN-5921)** + **modo read/write (LIN-5985)** + **comandos
dinâmicos por manifest (LIN-6332)** — ver seção própria. Comandos manuais:
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

Distribuição: repo **público**, release via GoReleaser + Homebrew tap próprio
(`linkanalabs/homebrew-tap`) — ver seção "Release / Homebrew (LIN-6287)".

Próximas fases: comandos de recurso (buyer), `lk schema` self-describing,
`SKILL.md` embarcado.

## Comandos dinâmicos por manifest (LIN-6332)

O backend Rails gera um `cli-manifest.json` descrevendo endpoints expostos; a
CLI vendora esse arquivo em `internal/manifest/cli-manifest.json` (go:embed) e
monta comandos Cobra em runtime com um executor REST genérico. Hoje o manifest
expõe `identity show` e `settings email-message list|show|update`.

- `internal/manifest/` — types do schema + `Load()`/`Parse()` com validação
  (command/method/path obrigatórios, tipos e `in` fechados, path_param tem que
  existir no path). `make update-manifest` baixa a cópia nova do repo Rails
  (falha limpa em 404, nunca sobrescreve com lixo).
- `internal/commands/dynamic.go` — `registerDynamic(root, m)` roda no FIM de
  `newRootCmd()`: **manuais registram antes e vencem colisão de nome no mesmo
  nível** (dinâmico colidente é pulado em silêncio). Grupos intermediários
  ganham Short derivado. `path_params` viram args posicionais (ExactArgs);
  `params` viram flags (string/integer/boolean nativos; date/datetime/decimal
  como string; array de scalar repete a flag; object e array de object são
  flag string JSON). Help LLM-first: description + Endpoint/Auth/Arguments/
  Parameters/Response.
- `internal/commands/dynamic_exec.go` — RunE genérico: `resolveAPI()` →
  substitui `/:param` (PathEscape) → flags alteradas viram query (`in: query`,
  arrays como `name[]`) ou body (`in: body`, embrulhado em `body_root`) →
  `client.Do` (herda gate read/write, Bearer, `.json`). 2xx → JSON cru no
  stdout; 401 → hint de login; não-2xx → body no stderr + exit 1.
- `lk version` mostra `manifest: <generated_at> (<source>)`.
- `SURFACE.txt` na raiz é golden da árvore completa de comandos; o teste
  `TestSurfaceGolden` compara e regenera com
  `go test ./internal/commands -run TestSurfaceGolden -update`. Mudou a
  superfície de comandos → atualizar o golden no mesmo PR.
- Igualdade com o manifest real do backend é sobre `endpoints` apenas —
  `generated_at`/`source` são voláteis.

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

## Release / Homebrew (LIN-6287)

Instalação pública: `brew install linkanalabs/tap/lk`. O cask vive em
`linkanalabs/homebrew-tap` (`Casks/lk.rb`) e é **gerado pelo GoReleaser** a cada
release — nunca editar na mão.

**Nunca publique release sem aprovação explícita.** Publicar = merge na `main` +
tag `vX.Y.Z` (semver). O push da tag dispara `.github/workflows/release.yml`
(GoReleaser), que builda darwin/linux/windows × amd64/arm64, cria o GitHub
Release e commita o cask atualizado no tap.

Pré-requisitos antes de tagear:
- `make test`, `make lint` e `make cover` (≥95%) verdes na `main`.
- `goreleaser check` limpo; mudou o `.goreleaser.yaml` → validar com
  `goreleaser release --snapshot --clean` (build local, não publica).

O CI precisa do secret `HOMEBREW_TAP_GITHUB_TOKEN` (fine-grained PAT com
Contents write **só** no tap) — o `GITHUB_TOKEN` padrão não escreve em outro
repo. **Armadilha:** se o release sair mas a atualização do tap falhar (token
expirado etc.), o brew continua servindo a versão velha silenciosamente. Após
cada release, verificar o commit novo em `linkanalabs/homebrew-tap` e rodar
`brew upgrade lk` (ou `brew install linkanalabs/tap/lk`) + `lk version` pra
confirmar a versão nova de verdade.

Release local (contingência, com aprovação):
`GITHUB_TOKEN=$(gh auth token) HOMEBREW_TAP_GITHUB_TOKEN=$(gh auth token) goreleaser release --clean`.

## Repositório backend (Rails)

O backend Linkana fica em `../linkana` (working dir adicional). Referências da
impersonação:

- `app/controllers/impersonations_controller.rb` — endpoint JSON `POST/DELETE /impersonation`.
- `app/policies/srm_policy.rb` — `enforce_impersonation_rules` (gate de escrita).
- `lib/warden/pat_bearer_strategy.rb` — PAT Bearer → `current_user`.
- `app/models/api_token.rb` + `app/models/api_tokens/build.rb` — Access Token.
- `buyers.allow_linkana_support` — flag que libera suporte (toggle em
  `app/controllers/srm_settings/access_configurations_controller.rb`).
