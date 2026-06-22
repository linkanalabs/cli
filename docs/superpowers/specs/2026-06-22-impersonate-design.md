# Design — `lk impersonate` (impersonação de suporte via Access Token)

Data: 2026-06-22
Status: aprovado (brainstorming), pronto pra plano de implementação
Linear: LIN-5921 (branch de trabalho `djalma/lin-5921`)

## Objetivo

A maioria das operações SRM no backend Linkana é **buyer-scoped**: dependem de
`current_user.buyer`. O agente (Cowork/Claude) opera em nome do CS/Onboarding e
precisa **agir dentro de um buyer** sem ter, ele mesmo, uma sessão de buyer.

Hoje, no web, isso é resolvido por **impersonação via Devise** (`impersonates :user`,
sessão/cookie), gated por `check_linkana_admin` para iniciar e por
`SrmPolicy#enforce_impersonation_rules` (exige `buyer.allow_linkana_support?`) para
escrever. Isso **não funciona** no CLI: o CLI é stateless e autentica por PAT
(`Authorization: Bearer lkn_...`), sem cookie.

Este design migra a impersonação para o CLI **cunhando um Access Token real** no
buyer+user de destino. O token impersonado passa a ser uma credencial legítima
daquele user/buyer — os endpoints SRM "só funcionam", sem header novo — e a
impersonação fica **auditável e revogável** (logs nativos de `api_token.*`).

## Contexto do backend (referências exatas — repo `../linkana`)

- **Impersonação web atual:** `app/controllers/impersonates_controller.rb`
  (`< LoginsController`, `before_action :check_linkana_admin`, usa Devise
  `impersonate_user`/`stop_impersonating_user`). Rota:
  `resources "impersonates"` em `config/routes.rb`.
- **Gate de escrita:** `app/policies/srm_policy.rb` →
  `pre_check :enforce_impersonation_rules`; nega se user impersonado é `@linkana`
  e o buyer **não** tem `allow_linkana_support?`.
- **Flag do buyer:** coluna `buyers.allow_linkana_support` (+ `_at`, `_by`);
  schema em `db/schema.rb`; toggle em
  `app/controllers/srm_settings/access_configurations_controller.rb`.
- **"Support user" = email `@linkana`** (sem flag em DB).
  `current_user.email.include?("@linkana")` (ex.
  `app/views/layouts/impersonate_header.rb`).
- **PAT → current_user:** `lib/warden/pat_bearer_strategy.rb` →
  `success!(api_token.user)`. O `current_user` de uma request PAT é o `user` do
  Access Token. Endpoints SRM já resolvem `current_user.buyer`.
- **Access Token (produto) = model `ApiToken`:** criado via
  `ApiTokens::Build.call(key_name:, buyer_id:, user_id:, expires_at:)` (retorna
  registro **não salvo**); verificado por `ApiTokens::Verify`; formato
  `lkn_<short>_<long>`. CRUD web em
  `app/controllers/srm_settings/access_tokens_controller.rb` (usa
  `create_audit_log("api_token.created"/"api_token.revoked", token)` e
  `token.revoke!`).
- **Staff:** `User#linkana_admin?` = `buyer_id == Buyer::ADMIN_ID ||
  Rails.env.development?`.
- **Users:** `index_users_on_email unique: true` (email **global único**);
  `belongs_to :buyer, optional: true` → **1 user = no máx 1 buyer**; id é uuid.
  Logo, **o user de destino determina o buyer** sozinho.

## Decisões (brainstorming)

1. **Token-minting**, não header `X-Impersonate-Buyer`. Cunhar Access Token real
   no buyer+user alvo. Motivo: audit nativo, zero mudança nos endpoints SRM,
   revogável.
2. **Alvo = user** (email **ou** uuid). O user carrega `buyer_id`, então define o
   buyer. Sem necessidade de informar buyer separado.
3. **TTL = 24h**, ajustável por flag `--ttl`.
4. **Sem fallback silencioso.** Enquanto existir contexto de impersonação gravado
   no CLI, o token original fica inacessível até `stop`/re-impersonar. Estado
   expirado é barreira "pegajosa".
5. **Servidor é autoridade sobre expiração.** 401 numa request impersonada =
   erro duro, nunca retry com token original.
6. **Escopo: backend + CLI juntos, v1 direto** (sem v0).

## Arquitetura

### Backend (linkana) — endpoint que cunha/revoga Access Token

Novo controller JSON (auth por PAT do agente), análogo a `My::IdentityController`
e a `SrmSettings::AccessTokensController`. Rota provável:
`resource :impersonation, only: %i[create destroy]` em `config/routes.rb`
(ou `resources :impersonates`), respondendo `format.json`.

**`POST /impersonation.json`** — inicia impersonação
- Auth: `authenticate_user!` (PAT Bearer → `current_user` = user staff do agente).
- **Gate (duro), em ordem:**
  1. `current_user.linkana_admin?` — senão **403**.
  2. Resolve alvo por `params[:user]` (email **ou** uuid) → `User`; senão **404**.
  3. Alvo precisa: email `@linkana` **e** `buyer_id` presente **e**
     `target.buyer.allow_linkana_support?` — senão **422** com motivo legível.
- **Ação:**
  - `token = ApiTokens::Build.call(key_name: "cli-impersonation:#{current_user.email}",
    buyer_id: target.buyer_id, user_id: target.id, expires_at: ttl.from_now)`
  - `token.save!`
  - `create_audit_log("api_token.created", token)`
- **Resposta 201:**
  ```json
  {
    "token": "lkn_<short>_<long>",
    "identity": { "user_id": "<uuid>", "email": "suporte+acme@linkana.com", "buyer_id": "<id>" },
    "expires_at": "2026-06-23T14:00:00Z"
  }
  ```
  (o segredo `lkn_..._...` é exibido **uma única vez**, como no fluxo web.)

**`DELETE /impersonation.json`** — encerra impersonação
- Auth: aceita o **PAT impersonado** (revoga a si mesmo) — caminho primário do
  `lk impersonate stop`. Resolve o `ApiToken` pelo token apresentado e
  `token.revoke!` + `create_audit_log("api_token.revoked", token)`.
- Idempotente: token já revogado/expirado → 200/204 mesmo assim.

**Trilha de auditoria:** `key_name = "cli-impersonation:<email-do-impersonador>"`
registra **quem** impersonou; `created_at`/`expires_at`/`revoked_at` dão **quando**;
`buyer_id`/`user_id` dão **onde/como**. Sem migração nova (reusa `key_name`).

> ⚠️ **Segurança — revisar com calma na implementação.** Este endpoint cunha uma
> credencial que dá acesso aos dados de um buyer. O gate precisa ser estrito
> (linkana_admin + alvo `@linkana` + `allow_linkana_support`), o TTL limitado, e o
> token revogável. Cobrir com testes os ramos de negação (não-staff, alvo não
> `@linkana`, buyer sem flag, alvo inexistente).

### CLI (lk) — auth de duas camadas

`internal/auth` ganha, **por origin** (base_url), além do token original já
existente, um **contexto de impersonação** persistido (mesmo storage:
keychain + fallback arquivo atômico 0600):

```
impersonation = {
  token,                // lkn_..._...  (segredo; nunca impresso)
  target_email,
  target_user_id,
  buyer_id,
  expires_at,           // RFC3339
  impersonator_email,   // quem iniciou (do whoami original)
}
```

**Resolução de credencial (`authedClient()` e equivalentes):**

```
se existe contexto de impersonação:
    se expires_at já passou (relógio local):
        ERRO DURO (exit≠0), NÃO usa token original:
          "impersonação de <email> (buyer <id>) expirou em <ts>.
           rode `lk impersonate <email>` pra renovar, ou
           `lk impersonate stop` pra voltar ao usuário original."
    senão:
        usa token impersonado.
senão:
    usa token original.
```

**Defesa em profundidade — 401 com impersonação ativa:** se o backend rejeitar o
token impersonado (expirou/revogado server-side mesmo o CLI achando válido), a CLI
**não** cai pro original — emite o mesmo erro duro, oferecendo as duas saídas
(`stop` ou re-impersonar). O servidor é a autoridade sobre expiração.

**Comandos (cobra), padrão `supplier`/`auth`:**

- `lk impersonate <email|user_id> [--ttl 24h]`
  → `POST /impersonation.json`; grava contexto; imprime
  `impersonando <email> (buyer <id>), expira <ts>`.
  Substitui contexto anterior (re-impersonar renova).
- `lk impersonate stop`
  → `DELETE /impersonation.json` com o token impersonado; limpa contexto local.
  Sem contexto ativo → no-op com aviso.
- `lk impersonate status`
  → mostra contexto (alvo, buyer, expiry, impersonador) ou `nenhuma impersonação ativa`.

**Visibilidade:**
- `lk whoami` e `lk auth status` mostram **ambas** as camadas: "logado como
  ⟨original⟩; impersonando ⟨alvo⟩ (buyer ⟨id⟩, expira ⟨ts⟩)".
- Comandos SRM podem emitir banner em **stderr** (`⚠ impersonando <email>`) —
  stdout continua só dados.

### Camada client (`internal/client`)

- `interface.go`: novos métodos na `API` —
  `StartImpersonation(ctx, userRef string, ttl time.Duration) (*Impersonation, error)`
  e `StopImpersonation(ctx) error`.
- Novo arquivo `impersonation.go`: struct `Impersonation` (espelha a resposta JSON)
  + chamadas `POST`/`DELETE`. Reusa `c.Get`-style helpers (precisa de um `Post`/
  `Delete` fino no `client.go`, hoje só há `Get`). 401 → `ErrUnauthorized`.

## Mudanças no CLAUDE.md / instruções (pedido explícito do usuário)

Para o usuário **não repetir contexto** ("impersonar", "buyer", "outro repo") a cada
sessão, adicionar ao `cli/CLAUDE.md`:

- Seção **"Impersonação / buyer-scope"**: comandos SRM são buyer-scoped; para agir
  num buyer, impersone o user `@linkana` daquele buyer com
  `lk impersonate <email>`; o estado de impersonação é pegajoso (expira → erro
  duro, sem fallback); encerre com `lk impersonate stop`.
- Seção **"Repositório backend"**: o Rails Linkana fica em `../linkana`
  (working dir adicional). Apontar arquivos de referência da impersonação
  (controller, `srm_policy.rb`, `pat_bearer_strategy.rb`, `api_token.rb`,
  `api_tokens/build.rb`) para que o agente não precise redescobrir.
- (Fase 2, fora deste escopo) `SKILL.md` embarcado.

## Testes (TDD, cobertura ≥95%)

**Backend (minitest):**
- `create`: sucesso (staff + alvo `@linkana` + buyer com flag) → 201, token salvo,
  `api_token.created` logado, `user_id`/`buyer_id`/`key_name`/`expires_at` corretos.
- `create` negações: não-staff → 403; alvo não-`@linkana` → 422; buyer sem
  `allow_linkana_support` → 422; alvo inexistente → 404; alvo sem buyer → 422.
- `create` por **email** e por **uuid**.
- `destroy`: revoga + `api_token.revoked` logado; idempotente em token já revogado.
- Token impersonado autentica em endpoint SRM e resolve o buyer alvo (integração).

**CLI (Go, httptest + seams):**
- `client`: `StartImpersonation`/`StopImpersonation` — sucesso, 401→`ErrUnauthorized`,
  5xx, JSON inválido.
- `commands`:
  - `impersonate <email>` grava contexto; stdout/styled corretos.
  - resolução de credencial: original quando sem contexto; impersonado quando ativo;
    **erro duro** quando expirado (sem fallback) — afirmar exit≠0 e mensagem.
  - 401 com impersonação ativa → erro duro, **não** usa original.
  - `stop` chama DELETE e limpa contexto; sem contexto → no-op.
  - `status` com/sem impersonação.
  - `whoami`/`auth status` mostram as duas camadas.

## Fora de escopo (fases futuras)

- Descoberta do email `@linkana` de um buyer (listagem). Por ora o agente
  cola email/uuid manualmente.
- `SKILL.md` embarcado; `lk schema` self-describing.
- Renovação automática / refresh de TTL.

## Critérios de aceite

- `lk impersonate <email>` cunha Access Token no buyer alvo e o CLI passa a agir
  como aquele user em comandos SRM.
- Token expirado/revogado **nunca** resulta em ação no buyer original (erro duro
  nas duas camadas — local e 401).
- `lk impersonate stop` revoga o token server-side e limpa o estado local.
- Trilha de auditoria identifica impersonador, alvo, buyer e janela temporal.
- `make cover` ≥95%, `make lint` limpo, `make test` verde.
