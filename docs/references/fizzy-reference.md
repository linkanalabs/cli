# Referências fizzy (fonte de verdade para espelhar)

A CLI da Linkana (`lk`) se inspira no `fizzy-cli` da Basecamp. Ambos falam com um
backend Rails sem API dedicada, via `format.json` (content negotiation no
controller). Estes são os arquivos-referência a consultar ao construir nossa CLI.

Repositórios:
- `fizzy-cli` — https://github.com/basecamp/fizzy-cli (a CLI em si)
- `fizzy-sdk` — https://github.com/basecamp/fizzy-sdk (SDK Go que a CLI consome)

> O `fizzy-cli/internal/client` é o caminho **legado/deprecado**. O `fizzy-sdk`
> é o substituto e o melhor professor para comunicação + auth.

## Comunicação com o Rails (`fizzy-sdk/go/pkg/fizzy/`)

| Arquivo | O que ensina |
|---|---|
| `client.go` | `Client` + `AccountClient`, recursos HTTP compartilhados, `Response{Data json.RawMessage}` + `UnmarshalData` |
| `http.go` | Get/Post/Patch/Delete cru |
| `*_service.go` | Uma service por recurso; chamam `client.Get(ctx, "/x.json")`. **Padrão `format.json` puro.** Ex: `cards_service.go`, `identity_service.go` (`GetMyIdentity` → `/my/identity.json`) |

## Auth (`fizzy-sdk/go/pkg/fizzy/`)

| Arquivo | O que ensina |
|---|---|
| `auth_strategy.go` | `BearerAuth.Authenticate()` → `Authorization: Bearer <token>`. Interface plugável (tem `CookieAuth` p/ `session_token` cookie) |
| `auth.go` | `TokenProvider`, `StaticTokenProvider` (token de env), `AuthManager` + `CredentialStore` = **keychain com fallback p/ arquivo**, escrita atômica (temp+rename, chmod 0600), credencial **por origin** (base_url). **Blueprint quase 1:1 do nosso storage de token.** |

## Catálogo de endpoints + params (gerado de `openapi.json`)

O SDK é um espelho **gerado** dos endpoints + params do Rails fizzy. Três lugares:

| Onde | Conteúdo |
|---|---|
| `url-routes.json` (embarcado via `//go:embed` em `url_routes.go`) | Tabela de rotas: `Pattern`, `APIPath`, `Resource`, `Operations` (operationID→verbo), `Params` (params de path com role+type) |
| `generated/types.gen.go` | Structs de request/response. json tag = nome exato do param no Rails (snake_case). Ex: `CreateCardRequest`, `AssignCardRequest` |
| `*_service.go` | Operações: hardcode do path `.json` + tipo do body que cada uma recebe |

Gerado por `fizzy-sdk/go/cmd/generate-services` a partir de `openapi.json`.

## O que NÃO copiamos (no setup / v0)

- **Codegen de OpenAPI.** Fizzy mantém `openapi.json` → gera SDK. Nós escrevemos
  um `internal/client` fino à mão. Lição que herdamos: centralizar paths num só
  lugar e ter structs de request tipadas com os nomes de param do Rails Linkana.
- **Camada pesada de resiliência:** `circuit_breaker.go`, `bulkhead.go`,
  `rate_limiter.go`, `cache.go`, `hooks.go`. Talvez só retry simples no começo.

## Decisões a implementar (backlog — pós-setup)

### `lk schema` — CLI self-describing (melhoria sobre fizzy)
Fizzy expõe a **superfície de comandos** via `SURFACE.txt` (gerado/testado, ver
`internal/commands/gen_surface_test.go`) e um **`SKILL.md`** embarcado
(`skills/embed.go`, comandos `fizzy skill` / `fizzy skill install`). Mas **não**
expõe os types de dados (`types.gen.go`) ao agente.

Como usuário primário é agente, queremos ir além: comando `lk schema` que descreve
as entidades para o agente consultar.
- `lk schema` → lista entidades (buyer, setting, category...)
- `lk schema buyer` → JSON Schema: campos, tipos, required
- `lk schema buyer --create` → params aceitos no create
- **Fonte única = os structs Go** (via reflection em runtime). Schema nunca
  dessincroniza do client — diferente do fizzy, que mantém Smithy à parte.

Casa com o resto do agent-readable do PRD: envelope JSON + breadcrumbs, exit codes
tipados, e erro de payload ruim devolvendo o schema esperado no próprio erro.

Também portar para nós: `SURFACE.txt` (enumeração de comandos, gerável do cobra) e
um `SKILL.md` embarcado com `lk skill` / `lk skill install`.

**Depende de:** entidades de recurso definidas (buyer/setting). Implementar quando
os comandos de buyer entrarem — NÃO no branch de setup.

## Diferença de auth Linkana

Fizzy usa session token / access token próprios. Na Linkana a decisão é
**Personal Access Token (PAT)** — modelo `ApiToken` (`lkn_<short>_<long>`,
`has_secure_password`) já existente no Rails. Auth via WorkOS foi descartada para
a CLI (usuário primário é agente, não humano). O storage no CLI espelha o
`CredentialStore` do `auth.go`.
