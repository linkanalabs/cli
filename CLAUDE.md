# CLAUDE.md — lk (Linkana CLI)

Linkana's internal CLI in Go. Binary `lk`. Consumed by Cowork (Claude) on behalf
of the CS/Onboarding team to parametrize buyers in bulk. **The primary user is an
AI agent**, not a human → JSON output by default, machine-readable is mandatory.

Talks to the Rails backend (`linkana`) via `format.json` (no dedicated API).
Inspired by Basecamp's `fizzy-cli` / `fizzy-sdk` — see `docs/references/fizzy-reference.md`.

## Non-negotiable rules

- **Test coverage ≥ 95% (total).** A PR below that is blocked. Run `make cover`.
- **Never open a PR / push code without `make test` green** locally.
- **TDD:** write the test before the implementation.
- **`golangci-lint run` clean** before committing (`make lint`).
- **`gofmt`/`goimports`** applied (`make fmt`).

## Go best practices

- Wrap errors with context: `fmt.Errorf("doing X: %w", err)`.
- No `panic` in normal flow. No `os.Exit` outside `main`/`Execute` — return an error.
- Small interfaces, defined at the consumer (e.g. `client.API`).
- `context.Context` on every I/O call.
- No mutable global state outside config/version.
- Inject seams (function vars) to make error branches testable, instead of
  leaving code uncovered.

## CLI best practices

- **JSON output is a contract**: stable, versioned. The other `--format` values
  are projections of the same data, never a different contract. An unknown
  value is rejected while pflag parses it (`formatValue` in `root.go`), so a
  typo costs no request — do NOT move that check into a `PersistentPreRunE`, a
  hook on a subcommand would shadow it.
  - `styled` — human bonus. Uses the result's own `Styler` when it has one.
  - `markdown` — GFM. **Always** the generic renderer, never `Styler`: a
    `Styled()` paints a terminal, it is not a document. Nested values go into
    a code span so their compact JSON survives; scalars get `\`, `|`, `[`, `]`
    escaped (a supplier-supplied name must not render as a clickable link).
    Markdown carries backend text — review before pasting anywhere public.
  - `ids` — one id per line, for chaining. A response carrying records with no
    usable `id` **fails with exit 1** instead of printing nothing: silence
    would read to an agent as an empty list. Real case today: the SRM e-mail
    messages are keyed by `template`. An actually empty response prints
    nothing and exits 0.
  - `count` — an integer, and always one (a 2xx with an empty body prints
    `0`). On an unpaginated endpoint it counts **what this response carried**.
    On a paginated one it reports the collection's real total, read from the
    `total-count` header (see "Pagination" below) — `lk supplier list --format
    count` is the honest answer to "how many suppliers does this buyer have".
  - `ids` and `count` exist to keep an agent's context cheap. `--jq` was
    deliberately rejected: piping to the shell's `jq` costs the model no
    tokens either, and gojq would buy a dependency plus a flag-conflict matrix.
- **Pagination is a manifest capability, not a param.** An endpoint whose JSON
  is paged declares `pagination: {param}` in its `config/cli/*.yml`; the
  executor derives the page flag from it, so no paged endpoint hand-declares
  one. **The page size and the total are never in the manifest** — they come
  back per response in the pagy headers (`total-count`, `total-pages`,
  `current-page`, `page-items`), so the CLI cannot drift from the backend:
  - `--<param>` (e.g. `--page`) fetches one page; below 1 is rejected locally
    (the backend 500s on it — LIN-6637).
  - `--format count` answers the **collection's total** from the header, in a
    single request — that is what makes "how many are there?" cheap. With an
    explicit `--page`, it counts that page instead (the caller asked for it).
  - When there is more than one page, stderr reports `page X of Y — N records
    in total` plus the next-page hint. stdout stays pure data.
  - No header (unpaged endpoint, older backend) degrades to the previous
    behaviour: count the body, say nothing.
  - Only `supplier list` is paged today: the `srm_settings` index actions run
    `pagy` inside `format.html`, so their JSON returns the full collection.
- **stdout = data, stderr = diagnostics.**
- **Meaningful exit codes** (0 ok, 1 failure). Commands render their own result
  and signal failure via an error; `run()` translates it into an exit code.
- `--help` on every command. Read flags have no side effects.
- Commands depend on interfaces (mockable), not concrete clients.

## Layout

```
cmd/lk/            entrypoint (trivial; outside the coverage target)
internal/commands/ cobra tree: root, version, doctor, auth, whoami
internal/client/   thin HTTP (format.json) + mockable API interface (Get, GetIdentity)
internal/config/   base_url via YAML (XDG) + env LK_API_URL; default = production
internal/auth/     PAT storage: keychain (go-keyring) + atomic file fallback, per origin
internal/output/   render JSON (default) / styled / markdown / ids / count
                   (shape.go = ordered JSON tree shared by the renderers)
```

## Common commands

- `make build` — compile
- `make test` — tests with `-race`
- `make cover` — tests + coverage gate ≥95%
- `make lint` — golangci-lint
- `make run ARGS="doctor"` — run locally
- `make dev` — `lk doctor` against `localhost:3000`

## Current state

Skeleton + `doctor` + **auth via PAT (CLI)** + **suppliers (SRM)** +
**impersonation (LIN-5921)** + **manifest-driven dynamic commands (LIN-6332)**
— see the dedicated section. Manual commands:
`version`, `doctor` (version, runtime, config, filesystem, reachability `GET /up`,
**Authentication** via `GET /my/identity.json` — pass/fail/skip, with a skip-cascade
when the backend is unreachable), `auth login|status|logout`, `whoami`,
`supplier list|show`, `impersonate <ref>|stop|status`, `config`,
`config set-url <url>`, `update [--check]`.

`base_url` resolves in the order `LK_API_URL` (env) → `config.yml` (XDG) →
**default `https://app.linkana.com`** (production — a clean install via brew talks
to production; dev overrides with `LK_API_URL`, see `make dev`). `lk config` shows
the effective value and its source (env|file|default); `lk config set-url` writes
it to the file (and warns if `LK_API_URL` is set, since the env wins at runtime).

`supplier list` → `GET /srm/suppliers` (bare array of suppliers). `supplier show
<id>` → `GET /srm/suppliers/<id>/panel` (a single supplier). Supplier contract:
`{id, name, identifier, legal_entity, state, tags:[{id, display_name}]}`. 401 →
hint `lk auth login`.

Token storage in `internal/auth`: OS keychain (`go-keyring`) with an atomic file
fallback (temp+rename, 0600), per origin (base_url). `LK_TOKEN` replaces the
**original** token — but an active impersonation takes precedence over `LK_TOKEN`
(see the section below). `LK_NO_KEYRING` forces the fallback (used in tests).

Credential: `lkn_<short>_<long>`; header `Authorization: Bearer <cred>`.

Distribution: **public** repo, released via GoReleaser + a dedicated Homebrew tap
(`linkanalabs/homebrew-tap`) — see the "Release / Homebrew (LIN-6287)" section.

Next phases: resource commands (buyer), self-describing `lk schema`, embedded
`SKILL.md`.

## Manifest-driven dynamic commands (LIN-6332)

The Rails backend generates a `cli-manifest.json` describing the exposed
endpoints; the CLI vendors that file in `internal/manifest/cli-manifest.json`
(go:embed) and builds Cobra commands at runtime with a generic REST executor.
Today the manifest exposes `identity show` and `settings email-message list|show|update`.

- `internal/manifest/` — schema types + `Load()`/`Parse()` with validation
  (command/method/path required, closed type and `in` sets, a path_param must
  exist in the path). `make update-manifest` downloads the fresh copy from the
  Rails repo (fails cleanly on 404, never overwrites with garbage).
- `internal/commands/dynamic.go` — `registerDynamic(root, m)` runs at the END of
  `newRootCmd()`: **manual commands register first and win name collisions at the
  same level** (a colliding dynamic command is silently skipped). Intermediate
  groups get a derived Short. `path_params` become positional args (ExactArgs);
  `params` become flags (native string/integer/boolean; date/datetime/decimal
  as string; array of scalar repeats the flag; object and array-of-object are
  JSON string flags). LLM-first help: description + Endpoint/Auth/Arguments/
  Parameters/Response.
- `internal/commands/dynamic_exec.go` — generic RunE: `resolveAPI()` →
  substitutes `/:param` (PathEscape) → changed flags become query (`in: query`,
  arrays as `name[]`) or body (`in: body`, wrapped in `body_root`) →
  `client.Do` (inherits the Bearer token and the `.json` suffix). 2xx → raw JSON on
  stdout; 401 → login hint; non-2xx → body on stderr + exit 1.
- `lk version` shows `manifest: <generated_at> (<source>)`.
- `SURFACE.txt` at the root is the golden of the full command tree; the
  `TestSurfaceGolden` test compares and regenerates with
  `go test ./internal/commands -run TestSurfaceGolden -update`. Changed the
  command surface → update the golden in the same PR.
- Equality with the real backend manifest is over `endpoints` only —
  `generated_at`/`source` are volatile.

### Exposing/changing a command — the deterministic process (mandatory)

**How to request (trigger).** Requests like "expose/add/promote/make-dynamic the
command X on the CLI", "expose `<controller#action>` on lk", "I want `<resource>`
on lk" (PT or EN — "expõe/adiciona/dinamiza o comando X", "quero X no lk") trigger
THIS process in full. The default scope is ALWAYS the three repos (linkana → cli →
lk-stack) — a delivery that stops at Phase 1 or 2 is incomplete. Only do a partial
scope if the request explicitly says so ("just the backend" / "cli only"). When
unsure whether it's one command or several, or about id dependencies
(reference-chaining), ask before starting.

Every new command (or surface/param change) **spans three repos** and follows the
same ordered steps. The reference slice is PR #14103 (`settings email-message`):
`cli_expose` + `config/cli/<x>.yml` + JSON branch/jbuilder + `_cli_test.rb` +
regenerated manifest. **Done = all of the verification gate below is demonstrable
with real output** (not "looks right"): Rails tests green (CLI + web untouched),
`cli:manifest:check` green, `make test|lint|cover` (≥95%) green, `SURFACE.txt`
golden green, **the real `lk` binary exercised end-to-end against a backend
(incl. error paths: 401 hint, 4xx/422 on stderr + exit 1)**, and the lk-stack
skill updated with reference chaining. Nothing merges/publishes without
explicit approval.

**Contract-mapping rules** (the contract lives in the controller's
`params.expect`/`permit`, not the YAML — read the action first):
- `path_params` come from the route (`required_parts`), never from YAML.
- Param nested under a model root (`params.expect(model: [...])`) → `in: body` +
  `body_root: model` (schema requires body_root iff there are body params).
- Param read at the top level (`params[:x]`, `params.dig("search","y")`,
  `params.expect(:x)`) → **`in: query`** (body would need a root the controller
  doesn't read). GET never declares body.
- Reserved param names: `format`, `help`, `h`. Command tokens: lowercase
  `[a-z0-9-]`. Buyer settings group under `settings` (e.g. `settings buyer-document`).
- Manual commands win name collisions — to make a manual command dynamic, remove
  the manual one (and mind Go coverage + loss of `--format styled`).

**Reference/dependency rule:** for every param that is an id/ref of another model
(`*_id`, a template, an identifier), define which `lk` command resolves it first
(`supplier_id` ← `supplier list`; `user_id` ← `settings company-user list`). If no
command resolves it yet, **expose that one first (prerequisite) in an earlier
wave** — never ship a command whose relationship id has no CLI source. This
chaining becomes skill instruction (Phase C).

**Ordered phases (linkana per command → cli+lk-stack once per batch):**
1. **linkana** (source of truth, **one PR per command**): read the action; derive
   the contract by the rules above; TDD `<...>_cli_test.rb` (success +
   401/403/404/422/400, web intact); add `cli_expose`, the `format.json` branch +
   jbuilders, and `config/cli/<x>.yml`; `bin/rails cli:manifest` (commit the
   manifest). **Gate ANTES de abrir o PR (obrigatório, ordenado): `bin/rails test` +
   `cli:manifest:check` verdes E `curl` real contra o backend local de pé (PAT) — os
   dois passando, saída do curl colada na descrição** (ver CLAUDE.local.md). Stack
   dependentes com `gh-stack`.
2. **cli — UM PR consolidado por LOTE, só depois que TODOS os PRs de linkana do lote
   estiverem mergeados na `main`.** NÃO abrir um PR de cli por comando. Quando o
   último backend entrar na `main`, rode `make update-manifest` **uma vez** (ele
   vendoriza o manifest inteiro e atualizado, capturando todos os comandos do lote de
   uma vez); `TestSurfaceGolden -update`; `make test|lint|cover` (≥95%); **prove
   e2e com o binário real** contra o backend local (`make dev` / `LK_API_URL`),
   incluindo os caminhos de erro. `make update-manifest` lê a `main`.
3. **lk-stack — PAREADO com o PR de cli (abrir juntos).** Update the `lk` skill in
   `lk-stack/lk-tools/skills/lk/` — `SKILL.md` and/or `references/command-catalog.md`
   — ensinando todos os comandos novos/alterados do lote (syntax, flags) e o
   **reference chaining** (qual comando rodar antes para obter cada id). O PR de cli e
   o de lk-stack são um par: o cli expõe, a skill ensina; revisar/mergear juntos.

**Ordering & stacking:** ordem cross-repo é sempre linkana (todos os PRs do lote
mergeados na `main`) → cli → lk-stack. Planos dependentes rodam em ondas (um comando
que precisa do id de outro é onda posterior). **No linkana, um PR por comando,
empilhando dependentes com `gh-stack`** (base primeiro), review na ordem do stack.
**As fases cli e lk-stack acontecem UMA vez por lote** (não por comando): um único PR
de cli consolidado + um único PR de lk-stack, abertos juntos depois que o backend do
lote estiver 100% mergeado.

## Impersonation / buyer-scope (LIN-5921)

SRM commands are **buyer-scoped** (they depend on `current_user.buyer`). The agent
has no buyer session of its own; to act on a buyer, **impersonate the `@linkana`
user of that buyer**:

- `lk impersonate <email|user_id>` — mints a real Access Token on the target
  buyer+user (backend gate: caller `linkana_admin?` + target `@linkana` + buyer
  with `allow_linkana_support`). Default TTL 24h, `--ttl` adjusts. Request
  parameter: `target` (not `user`).
- `lk impersonate status` — shows the active impersonation (target, buyer, expiry).
- `lk impersonate stop` — revokes the token on the server and clears local state.

**Sticky state:** while an impersonation is stored, the original token is
inaccessible. Expired (local clock) → the command fails with a hard error. Rejected
by the server (401) → same hard error. It **never** silently falls back to the
original user — always choose `lk impersonate stop` or re-impersonate.

**Credential precedence:** active impersonation context > `LK_TOKEN` >
keychain/file PAT. `LK_TOKEN` overrides only the **original** token; it does not
disable or bypass an active impersonation. This was decided intentionally to close
a security footgun (previously `LK_TOKEN` silently ignored the impersonation).

## Release / Homebrew (LIN-6287)

**Never publish a release without explicit approval.** Publishing = tag `vX.Y.Z` on a
commit that is already on `main`; pushing the tag triggers
`.github/workflows/release.yml` (`guard` → `ci` → `goreleaser` → `verify`, com o `ci`
reusando o `ci.yml` do repositório).

Public installation is `brew install linkanalabs/tap/lk` — **always the
fully-qualified tap name** (`brew upgrade lk` fails on a clean machine). The cask
lives in `linkanalabs/homebrew-tap` (`Casks/lk.rb`) and is **generated by
GoReleaser**; never edit it by hand.

**Pitfall the tooling exists for:** the release can ship while the cask commit to the
tap fails (expired `HOMEBREW_TAP_GITHUB_TOKEN`), leaving brew serving the old version
silently. Two deterministic gates cover the process — do not hand-roll checks around
them:

- `make release-preflight [VERSION=vX.Y.Z]` — gate before tagging. Derives the semver
  bump from `SURFACE.txt` (line removed from the golden = command gone = minor,
  pre-1.0), requires CI green on the exact sha, runs the test gates. Creates no tag.
- `make release-verify VERSION=vX.Y.Z [LOCAL=1]` — asserts a published release:
  assets, cask version, cask sha256 against `checksums.txt`, install paths.

Use `/release` (`.claude/skills/release/`) for the runbook: approval flow, failure
recovery and the local contingency release.

## Self-update (`lk update` + automatic check)

`internal/update/` resolves what is installable; `internal/state/` remembers when
lk last looked. **lk never replaces its own binary** — on a Workbrew fleet the
Caskroom is owned by another user and `/opt/workbrew/bin/brew` is setuid root, so
brew is the only process that can write there. Overwriting it would also desync
`INSTALL_RECEIPT.json`, whose path carries the version (`Caskroom/lk/0.7.0/lk`).

**Four versions, and they are not interchangeable:**

| | Source | Meaning |
|---|---|---|
| `current` | ldflag (bare, `0.7.0`) | this binary |
| `tap_local` | the cask file on disk, from the receipt's `source.path` | what brew can install **now** |
| `tap_remote` | `raw.githubusercontent.com/.../Casks/lk.rb` | what brew installs once its metadata is fresh |
| `release` | `Location` header of `github.com/.../releases/latest` | what is published |

`update_available` compares against **`tap_remote`**, not the release: they
diverge when a release ships and the tap commit fails, and pointing someone at
`brew upgrade` for a version brew does not have only generates support. Either
ordering between the two becomes the `warning` field.

**A prerelease in the tap is never installed on lk's own initiative.**
`.goreleaser.yaml` has `release: prerelease: auto` but no `skip_upload` on
`homebrew_casks`, and the release workflow guard accepts `vX.Y.Z-rc.1` — so
tagging a release candidate commits its cask to the tap, while
`/releases/latest` keeps pointing at the last stable release. Without this rule
a single RC tag would auto-upgrade the whole fleet onto it. `lk update` reports
the version and the command so a person can still opt in; moving between two
prereleases is allowed, since that install already opted in.

- **Never `api.github.com`** — 60 req/h *per IP*, shared behind a customer's NAT.
  Both sources above are CDN reads with no rate limit and no credentials. Never
  route these through `internal/client`: `buildURL` passes absolute URLs through,
  but `do()` always stamps `Authorization: Bearer <PAT>`, which would leak the
  Linkana token to GitHub.
- **`brew update` is never run.** It has no per-tap scope (`brew update --help`:
  "all formulae"), and refreshing the tap clone by hand is impossible on the
  fleet. `tap_local < tap_remote` → lk reports `brew update && brew upgrade
  --cask lk` and leaves the call to the user.
- **Cadence:** the automatic check runs from `Execute()` — the process edge, not
  `runWith()`, which stays the pure testable core so no test reaches the disk or
  network by accident. It runs *after* the command produced its output (the
  running process is the old binary either way), at most once per 24h. The
  timestamp is claimed **before** the request, so a network outage cannot become
  one attempt per command — the cost is that a blip waits for tomorrow. Guards:
  `LK_NO_AUTO_UPDATE`, CI env vars, `dev` build, non-Homebrew install, and
  informational runs (`--help`, `--version`, `lk help ...`), since "read flags
  have no side effects". A command that merely *failed* still checks: gating on
  success would mean a broken install — the one most in need of a fix — never
  updates.
- A command opts out by declaring `Annotations[annotationNoAutoUpdate]`: today
  `lk update`, which already did the job synchronously, and `lk version`, which
  is a read and must behave exactly like the `--version` flag. The guard reads
  the annotation up the command chain rather than matching names — comparing
  against `"lk update"` would break silently on a rename, and `Name() ==
  "update"` would also match `lk settings ... update`.
- **No TTY gate**, unlike gh and fizzy: lk's primary user is an agent, so gating
  on a terminal would disable the feature exactly where it was asked for.
  Isolation comes from the notice being stderr-only.
- The upgrade is spawned **detached** (`Setsid`, hence `spawn_unix.go` /
  `spawn_windows.go`) and logged to `<state dir>/upgrade.log`; lk never waits.
- `internal/update` imports only stdlib. `Detect()` does path arithmetic and no
  file read — it runs on every invocation; Homebrew's receipt is read lazily by
  `Install.CaskPath()`, and the log path belongs to `internal/state`, which owns
  that directory.
- **What authorises brew to replace the binary** is Homebrew's own
  `INSTALL_RECEIPT.json` naming `linkanalabs/tap`, not the path. A path is only
  a hint — any directory can be called `Caskroom` — so `CaskPath()` returns ""
  unless the receipt confirms it, and an unconfirmed install is never
  self-upgraded, only told the command. The check is free because the receipt is
  read after the daily budget is claimed; `brew --prefix` (what gh and flyctl
  use) would spawn a process on every invocation and still get Linux wrong.
- State holds **one** field (`LastCheckAt`). The daily budget is what bounds
  retries, so nothing else needs remembering; what brew did is in the log.
- `lk doctor` reports it as the `Update` check: **warn** when outdated, never
  fail (`res.Failed > 0` exits 1, and being a version behind is not a broken
  install); `skip` on a dev build or when the lookup fails.

## Backend repository (Rails)

The Linkana backend lives in `../linkana` (additional working dir). Impersonation
references:

- `app/controllers/impersonations_controller.rb` — JSON endpoint `POST/DELETE /impersonation`.
- `app/policies/srm_policy.rb` — `enforce_impersonation_rules` (write gate).
- `lib/warden/pat_bearer_strategy.rb` — PAT Bearer → `current_user`.
- `app/models/api_token.rb` + `app/models/api_tokens/build.rb` — Access Token.
- `buyers.allow_linkana_support` — flag that enables support (toggled in
  `app/controllers/srm_settings/access_configurations_controller.rb`).
