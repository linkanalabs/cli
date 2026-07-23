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

- **JSON output is a contract**: stable, versioned. `--format styled` is a human bonus.
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
internal/output/   render JSON (default) / styled
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
**impersonation (LIN-5921)** + **read/write mode (LIN-5985)** + **manifest-driven
dynamic commands (LIN-6332)** — see the dedicated section. Manual commands:
`version`, `doctor` (version, runtime, config, filesystem, reachability `GET /up`,
**Authentication** via `GET /my/identity.json` — pass/fail/skip, with a skip-cascade
when the backend is unreachable), `auth login|status|logout`, `whoami`,
`supplier list|show`, `impersonate <ref>|stop|status`, `mode`, `mode write`,
`mode read`, `config`, `config set-url <url>`.

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
  `client.Do` (inherits the read/write gate, Bearer, `.json`). 2xx → raw JSON on
  stdout; 401 → login hint; non-2xx → body on stderr + exit 1.
- `lk version` shows `manifest: <generated_at> (<source>)`.
- `SURFACE.txt` at the root is the golden of the full command tree; the
  `TestSurfaceGolden` test compares and regenerates with
  `go test ./internal/commands -run TestSurfaceGolden -update`. Changed the
  command surface → update the golden in the same PR.
- Equality with the real backend manifest is over `endpoints` only —
  `generated_at`/`source` are volatile.

### Checklist when exposing/changing a command (mandatory process)

Every new command (or surface/param change) follows this full cycle — it does not
end at the merge of the `linkana`/`cli` repos:

1. **The source of truth is Rails** (`../linkana`): before anything, read the
   exposed controller and confirm the real strong params the action accepts
   (`params.expect`) — the manifest/YAML documents it, but the contract lives there.
2. **Reference params**: identify params that are IDs/refs of other models
   (e.g. `supplier_id`, `template`). For each one, define which `lk` command
   resolves the reference first (e.g. `lk supplier list` → id; `settings
   email-message list` → template) — this becomes skill instruction (step 4).
3. **Refresh here**: `make update-manifest` + regenerated `SURFACE.txt` in the same PR.
4. **Update the `lk` skill in lk-stack** (`lk-stack/lk-tools/skills/lk/` —
   `SKILL.md` and/or `references/command-catalog.md`): every new/changed command
   must be taught to the LLM agent that consumes the CLI — syntax, flags, and
   especially the **reference chaining** from step 2 (which command to call first
   to obtain the ID this command needs). An lk-stack change is ALWAYS a new PR
   created from the latest `main` of lk-stack.

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

## Read/write mode (LIN-5985)

Each origin has an independent mode, persisted in `modes.json` (XDG). **Default: read.**

- `lk mode` — shows the current mode of the active origin (JSON or styled).
- `lk mode write` — enables writes; requires an interactive TTY + literally typing `"write"`. An AI agent cannot enable write without a human at the terminal.
- `lk mode read` — returns to read mode without confirmation.

**Gate in `client.do`:** any non-GET request in read mode returns `client.ErrReadOnly`
(`CLI is in read mode`). Read commands (`GET`) always pass.

**Credential:** `authedClient()` injects the mode into `client.Client`; an active
impersonation inherits the mode — there is no silent bypass. The impersonation verbs
(`POST/DELETE /impersonation`) also go through the gate: `lk impersonate <ref>`
requires write mode; `lk impersonate stop` always clears local state, but the remote
revocation in read mode fails with a warning (best-effort).

**Storage:** `internal/mode/` — `Load(origin)` / `Save(origin, m)`. Atomic file
(temp+rename, 0o600). Map key = `cfg.BaseURL`.

## Release / Homebrew (LIN-6287)

Public installation: `brew install linkanalabs/tap/lk`. The cask lives in
`linkanalabs/homebrew-tap` (`Casks/lk.rb`) and is **generated by GoReleaser** on
every release — never edit it by hand.

**Never publish a release without explicit approval.** Publishing = merge to `main`
+ tag `vX.Y.Z` (semver). Pushing the tag triggers `.github/workflows/release.yml`
(GoReleaser), which builds darwin/linux/windows × amd64/arm64, creates the GitHub
Release and commits the updated cask to the tap.

Prerequisites before tagging:
- `make test`, `make lint` and `make cover` (≥95%) green on `main`.
- `goreleaser check` clean; changed `.goreleaser.yaml` → validate with
  `goreleaser release --snapshot --clean` (local build, does not publish).

CI needs the `HOMEBREW_TAP_GITHUB_TOKEN` secret (fine-grained PAT with Contents
write **only** on the tap) — the default `GITHUB_TOKEN` cannot write to another
repo. **Pitfall:** if the release ships but the tap update fails (expired token,
etc.), brew keeps serving the old version silently. After every release, verify the
new commit in `linkanalabs/homebrew-tap` and run `lk version` to confirm the new
version for real. Install/update command — **always the fully-qualified tap name**:

- `brew install linkanalabs/tap/lk` — installs from scratch (adds the tap) **and**
  updates to the latest version. This is the canonical command; works in any state.
- `brew upgrade lk` — **only** works if the tap was already added and `lk` is
  already installed; on a clean machine it fails with `No available formula with the
  name "lk"` (it is a cask in a dedicated tap, not a core formula). Do not use as the
  first command.
- `brew reinstall linkanalabs/tap/lk` — if brew serves a cached version even after
  the tap updated.

Local release (contingency, with approval):
`GITHUB_TOKEN=$(gh auth token) HOMEBREW_TAP_GITHUB_TOKEN=$(gh auth token) goreleaser release --clean`.

## Backend repository (Rails)

The Linkana backend lives in `../linkana` (additional working dir). Impersonation
references:

- `app/controllers/impersonations_controller.rb` — JSON endpoint `POST/DELETE /impersonation`.
- `app/policies/srm_policy.rb` — `enforce_impersonation_rules` (write gate).
- `lib/warden/pat_bearer_strategy.rb` — PAT Bearer → `current_user`.
- `app/models/api_token.rb` + `app/models/api_tokens/build.rb` — Access Token.
- `buyers.allow_linkana_support` — flag that enables support (toggled in
  `app/controllers/srm_settings/access_configurations_controller.rb`).
