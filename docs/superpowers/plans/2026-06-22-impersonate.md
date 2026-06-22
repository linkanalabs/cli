# `lk impersonate` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Permitir que o agente impersone o user `@linkana` de um buyer via CLI, cunhando um Access Token real no buyer+user alvo, com auth de duas camadas, estado pegajoso e sem fallback silencioso.

**Architecture:** Backend Rails ganha `POST/DELETE /impersonation.json` que valida o gate de suporte e usa `ApiTokens::Build` para cunhar/revogar um Access Token no buyer alvo. O CLI persiste um contexto de impersonação por origin; enquanto ele existir, o token original fica inacessível (expirado → erro duro; 401 server-side → erro duro). Endpoints SRM não mudam: o token impersonado autentica via `PatBearerStrategy` como o user alvo.

**Tech Stack:** Ruby on Rails (minitest, FactoryBot), Go (cobra, net/http, httptest, go-keyring).

## Global Constraints

- CLI: cobertura de testes ≥ 95% total (`make cover`). PR abaixo é barrado.
- CLI: `make test` (com `-race`) verde, `make lint` (golangci-lint v2) limpo, `gofmt`/`goimports` aplicados, antes de qualquer commit.
- TDD obrigatório: teste antes da implementação.
- stdout = dados; stderr = diagnóstico. JSON é o contrato default; `--format styled` é bônus.
- Sem `os.Exit` fora de `main`/`Execute`; comandos retornam erro. Erros embrulhados com `fmt.Errorf("... %w", err)`.
- Segredos (`lkn_..._...`) nunca impressos.
- Backend: dois repos. CLI em `/Users/cooper/dev/linkana/cli`; Rails em `/Users/cooper/dev/linkana/linkana`. Commits são por-repo.
- Credencial format: `lkn_<short>_<long>`; header `Authorization: Bearer <cred>`.
- Default TTL impersonação: 24h; cap de segurança: 7 dias.

---

### Task B1: Backend — endpoint `POST/DELETE /impersonation.json`

Repo: `/Users/cooper/dev/linkana/linkana`

**Files:**
- Modify: `config/routes.rb` (adicionar a rota; perto de `resources "impersonates"`, linha ~20)
- Create: `app/controllers/impersonations_controller.rb`
- Test: `test/controllers/impersonations_controller_test.rb`

**Interfaces:**
- Consumes: `ApiTokens::Build.call(key_name:, buyer_id:, user_id:, expires_at:)` (retorna `ApiToken` não salvo); `ApiTokens::Verify.call(token:)` (retorna `ApiToken` ou nil/false); `ApiToken#one_time_display_api_key`, `#revoke!`; `User#linkana_admin?`; `Buyer#allow_linkana_support?`; `create_audit_log(action, target)` (de `ApplicationController`).
- Produces: respostas JSON consumidas pelo CLI client (Task C1):
  - `POST` 201 → `{ "token": "lkn_..._...", "identity": { "user_id", "email", "buyer_id" }, "expires_at": "<rfc3339>" }`
  - `DELETE` → 204 sem corpo.
  - Erros → status `403|404|422` com `{ "error": "<mensagem>" }`.

- [ ] **Step 1: Write the failing test**

Criar `test/controllers/impersonations_controller_test.rb`:

```ruby
require "test_helper"

class ImpersonationsControllerTest < ActionDispatch::IntegrationTest
  # Helper: PAT header for a given user.
  def pat_header(user, buyer_id: user.buyer_id)
    token = ApiTokens::Build.call(key_name: "CLI", buyer_id:, user_id: user.id).tap(&:save!)
    {"Authorization" => "Bearer #{token.one_time_display_api_key}"}
  end

  test "#create mints an access token for a linkana support user in an allowed buyer" do
    staff = create(:user, :linkana_admin)
    buyer = create(:buyer, allow_linkana_support: true)
    target = create(:user, buyer:, email: "suporte+acme@linkana.com")

    assert_difference -> { ApiToken.count }, 1 do
      post(impersonation_path(format: :json),
        params: {user: target.email},
        headers: pat_header(staff))
    end

    assert_response :created
    body = response.parsed_body
    assert_match(/\Alkn_/, body["token"])
    assert_equal target.id, body.dig("identity", "user_id")
    assert_equal target.email, body.dig("identity", "email")
    assert_equal buyer.id, body.dig("identity", "buyer_id")
    assert body["expires_at"].present?

    minted = ApiToken.order(:created_at).last
    assert_equal target.id, minted.user_id
    assert_equal buyer.id, minted.buyer_id
    assert_equal "cli-impersonation:#{staff.email}", minted.key_name
    assert minted.expires_at > Time.current
  end

  test "#create resolves the target by uuid" do
    staff = create(:user, :linkana_admin)
    buyer = create(:buyer, allow_linkana_support: true)
    target = create(:user, buyer:, email: "suporte+acme@linkana.com")

    post(impersonation_path(format: :json),
      params: {user: target.id},
      headers: pat_header(staff))

    assert_response :created
    assert_equal target.id, response.parsed_body.dig("identity", "user_id")
  end

  test "#create honors ttl_seconds within the cap" do
    staff = create(:user, :linkana_admin)
    buyer = create(:buyer, allow_linkana_support: true)
    target = create(:user, buyer:, email: "suporte+acme@linkana.com")

    post(impersonation_path(format: :json),
      params: {user: target.email, ttl_seconds: 3600},
      headers: pat_header(staff))

    assert_response :created
    minted = ApiToken.order(:created_at).last
    assert_in_delta 3600, (minted.expires_at - Time.current), 30
  end

  test "#create rejects a non-staff caller with 403" do
    caller_buyer = create(:buyer)
    non_staff = create(:user, buyer: caller_buyer, email: "ana@cliente.com")
    target_buyer = create(:buyer, allow_linkana_support: true)
    target = create(:user, buyer: target_buyer, email: "suporte+acme@linkana.com")

    post(impersonation_path(format: :json),
      params: {user: target.email},
      headers: pat_header(non_staff))

    assert_response :forbidden
    assert_equal 0, ApiToken.where(key_name: /cli-impersonation/).count
  end

  test "#create rejects a non-linkana target with 422" do
    staff = create(:user, :linkana_admin)
    buyer = create(:buyer, allow_linkana_support: true)
    target = create(:user, buyer:, email: "ana@cliente.com")

    post(impersonation_path(format: :json),
      params: {user: target.email},
      headers: pat_header(staff))

    assert_response :unprocessable_content
    assert response.parsed_body["error"].present?
  end

  test "#create rejects a buyer without allow_linkana_support with 422" do
    staff = create(:user, :linkana_admin)
    buyer = create(:buyer, allow_linkana_support: false)
    target = create(:user, buyer:, email: "suporte+acme@linkana.com")

    post(impersonation_path(format: :json),
      params: {user: target.email},
      headers: pat_header(staff))

    assert_response :unprocessable_content
  end

  test "#create rejects a target without a buyer with 422" do
    staff = create(:user, :linkana_admin)
    target = create(:user, buyer: nil, email: "suporte+orfao@linkana.com")

    post(impersonation_path(format: :json),
      params: {user: target.email},
      headers: pat_header(staff))

    assert_response :unprocessable_content
  end

  test "#create returns 404 for an unknown target" do
    staff = create(:user, :linkana_admin)

    post(impersonation_path(format: :json),
      params: {user: "naoexiste@linkana.com"},
      headers: pat_header(staff))

    assert_response :not_found
  end

  test "#create returns 401 without a token" do
    post(impersonation_path(format: :json), params: {user: "x@linkana.com"})
    assert_response :unauthorized
  end

  test "#destroy revokes the presented impersonation token" do
    buyer = create(:buyer, allow_linkana_support: true)
    target = create(:user, buyer:, email: "suporte+acme@linkana.com")
    token = ApiTokens::Build.call(key_name: "cli-impersonation:staff@linkana.com",
      buyer_id: buyer.id, user_id: target.id).tap(&:save!)

    delete(impersonation_path(format: :json),
      headers: {"Authorization" => "Bearer #{token.one_time_display_api_key}"})

    assert_response :no_content
    assert token.reload.revoked?
  end

  test "#destroy is idempotent on an already revoked token" do
    buyer = create(:buyer, allow_linkana_support: true)
    target = create(:user, buyer:, email: "suporte+acme@linkana.com")
    token = ApiTokens::Build.call(key_name: "cli-impersonation:staff@linkana.com",
      buyer_id: buyer.id, user_id: target.id).tap { |t| t.save!; t.revoke! }

    delete(impersonation_path(format: :json),
      headers: {"Authorization" => "Bearer #{token.one_time_display_api_key}"})

    # Revoked token no longer authenticates → Devise returns 401; that is acceptable
    # for stop (the CLI clears local state regardless). Accept 204 or 401.
    assert_includes [204, 401], response.status
  end
end
```

> Nota sobre `:linkana_admin` factory trait: confirme em `test/factories/users.rb` que existe `trait :linkana_admin` que põe o user no buyer admin (`Buyer::ADMIN_ID`). Se não existir, crie-o nesse mesmo passo:
> ```ruby
> trait :linkana_admin do
>   buyer { Buyer.find_by(id: Buyer::ADMIN_ID) || create(:buyer, id: Buyer::ADMIN_ID) }
>   email { "staff-#{SecureRandom.hex(3)}@linkana.com" }
> end
> ```
> Ajuste à convenção real do repo (`grep -n "linkana_admin" test/factories/users.rb`).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/cooper/dev/linkana/linkana && bin/rails test test/controllers/impersonations_controller_test.rb`
Expected: FAIL — rota `impersonation_path` indefinida / controller ausente.

- [ ] **Step 3: Add the route**

Em `config/routes.rb`, logo após a linha `resources "impersonates", only: %i[new create destroy]` (~linha 20), adicionar:

```ruby
  # JSON-only impersonation for the Linkana CLI: mints/revokes a real Access Token
  # in the target buyer+user. Auth via PAT Bearer. See ImpersonationsController.
  resource :impersonation, only: %i[create destroy]
```

- [ ] **Step 4: Write the controller**

Criar `app/controllers/impersonations_controller.rb`:

```ruby
# frozen_string_literal: true

# JSON-only impersonation endpoint for the Linkana CLI.
#
# Mints a real Access Token (ApiToken) in the *target* buyer+user so that
# subsequent SRM requests authenticate as that support user via
# Warden::PatBearerStrategy — no per-request impersonation header needed.
#
# Gate (hard): the caller must be Linkana staff (linkana_admin?), the target must
# be a "@linkana" support user that belongs to a buyer, and that buyer must have
# allow_linkana_support enabled. The minted token records the impersonator in its
# key_name for audit.
class ImpersonationsController < ApplicationController
  MAX_TTL = 7.days
  DEFAULT_TTL = 24.hours
  UUID_RE = /\A[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\z/

  skip_before_action :verify_authenticity_token
  before_action :authenticate_user!
  after_action -> { request.session_options[:skip] = true }

  def create
    return render_error(:forbidden, _("Apenas a equipe Linkana pode impersonar.")) unless current_user.linkana_admin?

    target = find_target(params[:user])
    return render_error(:not_found, _("Usuário de destino não encontrado.")) if target.nil?

    unless impersonatable?(target)
      return render_error(
        :unprocessable_content,
        _("Destino inválido: precisa ser usuário @linkana de um buyer com 'Permitir suporte' ativado.")
      )
    end

    token = ApiTokens::Build.call(
      key_name: "cli-impersonation:#{current_user.email}",
      buyer_id: target.buyer_id,
      user_id: target.id,
      expires_at: ttl_from_params
    )
    token.save!
    create_audit_log("api_token.created", token)

    render(json: {
      token: token.one_time_display_api_key,
      identity: {user_id: target.id, email: target.email, buyer_id: target.buyer_id},
      expires_at: token.expires_at
    }, status: :created)
  end

  def destroy
    token = ApiTokens::Verify.call(token: bearer_token)
    if token.is_a?(::ApiToken)
      token.revoke!
      create_audit_log("api_token.revoked", token)
    end
    head(:no_content)
  end

  private

  def find_target(ref)
    return if ref.blank?

    if ref.match?(UUID_RE)
      User.find_by(id: ref)
    else
      User.find_by(email: ref)
    end
  end

  def impersonatable?(user)
    user.email.to_s.include?("@linkana") &&
      user.buyer.present? &&
      user.buyer.allow_linkana_support?
  end

  def ttl_from_params
    seconds = params[:ttl_seconds].presence&.to_i
    duration = seconds&.positive? ? seconds.seconds : DEFAULT_TTL
    [duration, MAX_TTL].min.from_now
  end

  def bearer_token
    request.headers["Authorization"].to_s.split.last
  end

  def render_error(status, message)
    render(json: {error: message}, status:)
  end
end
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/cooper/dev/linkana/linkana && bin/rails test test/controllers/impersonations_controller_test.rb`
Expected: PASS (todas).

Se `bin/rails test` exigir DB de teste, prepare antes: `bin/rails db:test:prepare`.

- [ ] **Step 6: Lint (se o repo usa standard)**

Run: `cd /Users/cooper/dev/linkana/linkana && bundle exec standardrb app/controllers/impersonations_controller.rb`
Expected: sem ofensas (corrija formatação se houver).

- [ ] **Step 7: Commit**

```bash
cd /Users/cooper/dev/linkana/linkana
git add config/routes.rb app/controllers/impersonations_controller.rb test/controllers/impersonations_controller_test.rb test/factories/users.rb
git commit -m "feat(impersonation): JSON endpoint to mint/revoke CLI impersonation tokens (LIN-5921)"
```

---

### Task C1: CLI client — `Post`/`Delete` + impersonation calls

Repo: `/Users/cooper/dev/linkana/cli`

**Files:**
- Modify: `internal/client/client.go` (refatorar para um `do` compartilhado; adicionar `Post`, `Delete`)
- Modify: `internal/client/interface.go` (adicionar métodos à interface `API`)
- Create: `internal/client/impersonation.go`
- Test: `internal/client/impersonation_test.go`

**Interfaces:**
- Consumes: respostas JSON da Task B1.
- Produces:
  - `func (c *Client) Post(ctx context.Context, path string, payload any) (*Response, error)`
  - `func (c *Client) Delete(ctx context.Context, path string) (*Response, error)`
  - `type Impersonation struct { Token string; Identity ImpersonationIdentity; ExpiresAt time.Time }`
  - `type ImpersonationIdentity struct { UserID, Email, BuyerID string }`
  - `func (c *Client) StartImpersonation(ctx context.Context, userRef string, ttl time.Duration) (*Impersonation, error)`
  - `func (c *Client) StopImpersonation(ctx context.Context) error`
  - método `API.StartImpersonation` / `API.StopImpersonation` na interface.

- [ ] **Step 1: Write the failing test**

Criar `internal/client/impersonation_test.go`:

```go
package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStartImpersonationSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/impersonation.json" {
			t.Errorf("method/path = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer lkn_orig_tok" {
			t.Errorf("auth header = %q", got)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["user"] != "suporte@linkana.com" {
			t.Errorf("user = %v", body["user"])
		}
		if body["ttl_seconds"].(float64) != 3600 {
			t.Errorf("ttl_seconds = %v", body["ttl_seconds"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"lkn_imp_tok","identity":{"user_id":"u1","email":"suporte@linkana.com","buyer_id":"b1"},"expires_at":"2026-06-23T14:00:00Z"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.Token = "lkn_orig_tok"
	imp, err := c.StartImpersonation(context.Background(), "suporte@linkana.com", time.Hour)
	if err != nil {
		t.Fatalf("StartImpersonation() error: %v", err)
	}
	if imp.Token != "lkn_imp_tok" || imp.Identity.Email != "suporte@linkana.com" || imp.Identity.BuyerID != "b1" {
		t.Errorf("imp = %+v", imp)
	}
	if !imp.ExpiresAt.Equal(time.Date(2026, 6, 23, 14, 0, 0, 0, time.UTC)) {
		t.Errorf("expires_at = %v", imp.ExpiresAt)
	}
}

func TestStartImpersonationOmitsZeroTTL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, present := body["ttl_seconds"]; present {
			t.Errorf("ttl_seconds should be omitted, got %v", body["ttl_seconds"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"lkn_imp_tok","identity":{"user_id":"u1","email":"x@linkana.com","buyer_id":"b1"},"expires_at":"2026-06-23T14:00:00Z"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	if _, err := c.StartImpersonation(context.Background(), "x@linkana.com", 0); err != nil {
		t.Fatalf("error: %v", err)
	}
}

func TestStartImpersonationUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if _, err := New(srv.URL).StartImpersonation(context.Background(), "x@linkana.com", 0); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("error = %v, want ErrUnauthorized", err)
	}
}

func TestStartImpersonationServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"destino inválido"}`))
	}))
	defer srv.Close()

	_, err := New(srv.URL).StartImpersonation(context.Background(), "x@cliente.com", 0)
	if err == nil {
		t.Fatal("expected error on 422")
	}
	if got := err.Error(); !contains(got, "destino inválido") {
		t.Errorf("error should surface server message, got %q", got)
	}
}

func TestStartImpersonationBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	if _, err := New(srv.URL).StartImpersonation(context.Background(), "x@linkana.com", 0); err == nil {
		t.Error("expected decode error")
	}
}

func TestStopImpersonationSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/impersonation.json" {
			t.Errorf("method/path = %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL)
	c.Token = "lkn_imp_tok"
	if err := c.StopImpersonation(context.Background()); err != nil {
		t.Fatalf("StopImpersonation() error: %v", err)
	}
}

func TestStopImpersonationUnauthorizedIsOK(t *testing.T) {
	// A revoked/expired token returns 401 on DELETE; stop must treat that as success
	// so the CLI can always clear local state.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	if err := New(srv.URL).StopImpersonation(context.Background()); err != nil {
		t.Errorf("StopImpersonation() on 401 should be nil, got %v", err)
	}
}

// contains is a tiny helper to avoid importing strings in this file only for one check.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/cooper/dev/linkana/cli && go test ./internal/client/ -run Impersonation`
Expected: FAIL — `StartImpersonation`/`StopImpersonation`/`Post`/`Delete` indefinidos.

- [ ] **Step 3: Refactor `client.go` to share a `do` helper and add `Post`/`Delete`**

Substituir o corpo de `internal/client/client.go` mantendo `buildURL`/`ensureJSON`/`setHeaders` e o struct, trocando `Get` para usar `do`:

```go
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// Client is an HTTP client for the Linkana backend.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// Ensure Client satisfies the API interface.
var _ API = (*Client)(nil)

// New creates a Client for the given base URL.
func New(baseURL string) *Client {
	return &Client{
		BaseURL:    strings.TrimSuffix(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: defaultTimeout},
	}
}

// buildURL joins the base URL and path, ensuring a leading slash and a .json
// suffix on the path (Rails content negotiation). Absolute URLs pass through.
func (c *Client) buildURL(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.BaseURL + ensureJSON(path)
}

// ensureJSON appends .json to the path (before any query string) when absent.
func ensureJSON(path string) string {
	query := ""
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path, query = path[:i], path[i:]
	}
	if !strings.HasSuffix(path, ".json") {
		path += ".json"
	}
	return path + query
}

// do builds and executes a request, reading the full body into a Response.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.buildURL(path), body)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setHeaders(req)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	return &Response{StatusCode: resp.StatusCode, Body: b, Header: resp.Header}, nil
}

// Get performs a GET request and returns the response.
func (c *Client) Get(ctx context.Context, path string) (*Response, error) {
	return c.do(ctx, http.MethodGet, path, nil)
}

// Post performs a POST request with an optional JSON-encoded payload.
func (c *Client) Post(ctx context.Context, path string, payload any) (*Response, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encoding request body: %w", err)
		}
		body = bytes.NewReader(b)
	}
	return c.do(ctx, http.MethodPost, path, body)
}

// Delete performs a DELETE request and returns the response.
func (c *Client) Delete(ctx context.Context, path string) (*Response, error) {
	return c.do(ctx, http.MethodDelete, path, nil)
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}
```

- [ ] **Step 4: Add interface methods**

Em `internal/client/interface.go`, dentro de `type API interface { ... }`, adicionar:

```go
	StartImpersonation(ctx context.Context, userRef string, ttl time.Duration) (*Impersonation, error)
	StopImpersonation(ctx context.Context) error
```

Garantir o import de `"time"` no arquivo (adicionar se ausente).

- [ ] **Step 5: Implement the impersonation calls**

Criar `internal/client/impersonation.go`:

```go
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Impersonation is the response from POST /impersonation.json.
type Impersonation struct {
	Token     string                `json:"token"`
	Identity  ImpersonationIdentity `json:"identity"`
	ExpiresAt time.Time             `json:"expires_at"`
}

// ImpersonationIdentity is the impersonated principal (the target support user).
type ImpersonationIdentity struct {
	UserID  string `json:"user_id"`
	Email   string `json:"email"`
	BuyerID string `json:"buyer_id"`
}

type startImpersonationRequest struct {
	User       string `json:"user"`
	TTLSeconds int    `json:"ttl_seconds,omitempty"`
}

// StartImpersonation mints an impersonation Access Token for userRef (email or
// uuid). A zero ttl lets the backend apply its default.
func (c *Client) StartImpersonation(ctx context.Context, userRef string, ttl time.Duration) (*Impersonation, error) {
	body := startImpersonationRequest{User: userRef, TTLSeconds: int(ttl.Seconds())}
	resp, err := c.Post(ctx, "/impersonation", body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("impersonation request returned %d: %s", resp.StatusCode, serverError(resp.Body))
	}

	var imp Impersonation
	if err := json.Unmarshal(resp.Body, &imp); err != nil {
		return nil, fmt.Errorf("decoding impersonation: %w", err)
	}
	return &imp, nil
}

// StopImpersonation revokes the impersonation token in use. A 401 (token already
// expired/revoked server-side) is treated as success so the caller can always
// clear local state.
func (c *Client) StopImpersonation(ctx context.Context) error {
	resp, err := c.Delete(ctx, "/impersonation")
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("stop impersonation returned %d", resp.StatusCode)
	}
	return nil
}

// serverError extracts a JSON {"error": "..."} message, falling back to the raw body.
func serverError(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	return string(body)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd /Users/cooper/dev/linkana/cli && go test ./internal/client/`
Expected: PASS.

- [ ] **Step 7: Format, vet, lint**

Run: `cd /Users/cooper/dev/linkana/cli && make fmt && go vet ./... && make lint`
Expected: limpo.

- [ ] **Step 8: Commit**

```bash
cd /Users/cooper/dev/linkana/cli
git add internal/client/client.go internal/client/interface.go internal/client/impersonation.go internal/client/impersonation_test.go
git commit -m "feat(client): Post/Delete + StartImpersonation/StopImpersonation"
```

---

### Task C2: CLI auth — persist impersonation context per origin

Repo: `/Users/cooper/dev/linkana/cli`

**Files:**
- Create: `internal/auth/impersonation.go`
- Test: `internal/auth/impersonation_test.go`

**Interfaces:**
- Consumes: `auth.Save(origin, token string) error`, `auth.Load(origin string) (string, Source, error)`, `auth.Delete(origin string) error` (já existem em `internal/auth/store.go`).
- Produces:
  - `type Impersonation struct { Token, TargetEmail, TargetUserID, BuyerID, ImpersonatorEmail string; ExpiresAt time.Time }`
  - `func (i Impersonation) Expired(now time.Time) bool`
  - `func SaveImpersonation(origin string, imp Impersonation) error`
  - `func LoadImpersonation(origin string) (*Impersonation, error)` — `nil, nil` quando não há contexto.
  - `func DeleteImpersonation(origin string) error`

Estratégia de storage: reutiliza `Save/Load/Delete` com uma chave namespaced `origin + "|impersonation"`, gravando o struct serializado em JSON como se fosse o "token". Zero código novo de keychain/arquivo.

- [ ] **Step 1: Write the failing test**

Criar `internal/auth/impersonation_test.go`:

```go
package auth

import (
	"testing"
	"time"
)

func impersonationEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(EnvNoKeyring, "1")
	t.Setenv(EnvToken, "")
}

func TestSaveLoadImpersonationRoundTrip(t *testing.T) {
	impersonationEnv(t)
	origin := "http://localhost:3000"
	exp := time.Date(2026, 6, 23, 14, 0, 0, 0, time.UTC)
	in := Impersonation{
		Token:             "lkn_imp_tok",
		TargetEmail:       "suporte@linkana.com",
		TargetUserID:      "u1",
		BuyerID:           "b1",
		ImpersonatorEmail: "staff@linkana.com",
		ExpiresAt:         exp,
	}
	if err := SaveImpersonation(origin, in); err != nil {
		t.Fatalf("SaveImpersonation() error: %v", err)
	}
	got, err := LoadImpersonation(origin)
	if err != nil {
		t.Fatalf("LoadImpersonation() error: %v", err)
	}
	if got == nil {
		t.Fatal("LoadImpersonation() = nil, want context")
	}
	if got.Token != in.Token || got.TargetEmail != in.TargetEmail || got.BuyerID != in.BuyerID ||
		got.TargetUserID != in.TargetUserID || got.ImpersonatorEmail != in.ImpersonatorEmail {
		t.Errorf("got = %+v, want %+v", *got, in)
	}
	if !got.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, exp)
	}
}

func TestLoadImpersonationAbsent(t *testing.T) {
	impersonationEnv(t)
	got, err := LoadImpersonation("http://localhost:3000")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if got != nil {
		t.Errorf("got = %+v, want nil", got)
	}
}

func TestDeleteImpersonation(t *testing.T) {
	impersonationEnv(t)
	origin := "http://localhost:3000"
	_ = SaveImpersonation(origin, Impersonation{Token: "lkn_imp_tok", ExpiresAt: time.Now()})
	if err := DeleteImpersonation(origin); err != nil {
		t.Fatalf("DeleteImpersonation() error: %v", err)
	}
	got, _ := LoadImpersonation(origin)
	if got != nil {
		t.Errorf("after delete got = %+v, want nil", got)
	}
}

func TestImpersonationExpired(t *testing.T) {
	base := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	imp := Impersonation{ExpiresAt: base}
	if imp.Expired(base.Add(-time.Second)) {
		t.Error("should not be expired one second before ExpiresAt")
	}
	if !imp.Expired(base.Add(time.Second)) {
		t.Error("should be expired one second after ExpiresAt")
	}
}

func TestImpersonationIsolatedFromToken(t *testing.T) {
	impersonationEnv(t)
	origin := "http://localhost:3000"
	if err := Save(origin, "lkn_original"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	_ = SaveImpersonation(origin, Impersonation{Token: "lkn_imp", ExpiresAt: time.Now()})
	// Saving impersonation must not clobber the original token.
	tok, _, err := Load(origin)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if tok != "lkn_original" {
		t.Errorf("original token = %q, want lkn_original", tok)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/cooper/dev/linkana/cli && go test ./internal/auth/ -run Impersonation`
Expected: FAIL — símbolos indefinidos.

- [ ] **Step 3: Implement**

Criar `internal/auth/impersonation.go`:

```go
package auth

import (
	"encoding/json"
	"fmt"
	"time"
)

// Impersonation is the locally-persisted impersonation context for an origin.
// It lives alongside (not replacing) the original token; while present, callers
// must use Token instead of the original credential.
type Impersonation struct {
	Token             string    `json:"token"`
	TargetEmail       string    `json:"target_email"`
	TargetUserID      string    `json:"target_user_id"`
	BuyerID           string    `json:"buyer_id"`
	ImpersonatorEmail string    `json:"impersonator_email"`
	ExpiresAt         time.Time `json:"expires_at"`
}

// Expired reports whether the context is past its expiry at the given instant.
func (i Impersonation) Expired(now time.Time) bool {
	return now.After(i.ExpiresAt)
}

// impersonationOrigin namespaces the impersonation blob so it never collides
// with the origin's original token in the same keychain/file store.
func impersonationOrigin(origin string) string {
	return origin + "|impersonation"
}

// SaveImpersonation persists the impersonation context for origin.
func SaveImpersonation(origin string, imp Impersonation) error {
	blob, err := json.Marshal(imp)
	if err != nil {
		return fmt.Errorf("encoding impersonation: %w", err)
	}
	return Save(impersonationOrigin(origin), string(blob))
}

// LoadImpersonation returns the stored impersonation context, or nil when none.
func LoadImpersonation(origin string) (*Impersonation, error) {
	blob, _, err := Load(impersonationOrigin(origin))
	if err != nil {
		return nil, err
	}
	if blob == "" {
		return nil, nil
	}
	var imp Impersonation
	if err := json.Unmarshal([]byte(blob), &imp); err != nil {
		return nil, fmt.Errorf("decoding impersonation: %w", err)
	}
	return &imp, nil
}

// DeleteImpersonation removes the impersonation context for origin.
func DeleteImpersonation(origin string) error {
	return Delete(impersonationOrigin(origin))
}
```

> ⚠️ Verifique que `EnvToken`/`EnvNoKeyring` não vazem para a chave de impersonação: `Load(impersonationOrigin)` ainda respeita `LK_TOKEN` (env). Como `LK_TOKEN` é destinado ao token original, garanta nos testes do C3 que `LK_TOKEN` vazio não injeta blob. Se `Load` retornar o valor de `LK_TOKEN` para a chave namespaced, isso seria um JSON inválido → `LoadImpersonation` retorna erro de decode. Mitigação: nos comandos, só consideramos impersonação quando o decode é bem-sucedido. Os testes do C2 rodam com `EnvToken=""`, cobrindo o caminho feliz; o C3 cobre o caminho com `LK_TOKEN` setado (original) garantindo que impersonação fica `nil`.

Ajuste defensivo em `LoadImpersonation`: se `Load` veio do env (`Source == SourceEnv`), ignore — env é para o token original, não para o blob:

```go
func LoadImpersonation(origin string) (*Impersonation, error) {
	blob, src, err := Load(impersonationOrigin(origin))
	if err != nil {
		return nil, err
	}
	if blob == "" || src == SourceEnv {
		return nil, nil
	}
	...
}
```

(Use esta versão com `src == SourceEnv` no guard.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/cooper/dev/linkana/cli && go test ./internal/auth/`
Expected: PASS.

- [ ] **Step 5: Format/lint and commit**

```bash
cd /Users/cooper/dev/linkana/cli
make fmt && make lint
git add internal/auth/impersonation.go internal/auth/impersonation_test.go
git commit -m "feat(auth): persist per-origin impersonation context"
```

---

### Task C3: CLI — credential resolution with sticky expiry (no silent fallback)

Repo: `/Users/cooper/dev/linkana/cli`

**Files:**
- Modify: `internal/commands/whoami.go` (refatorar `authedClient`; adicionar helpers `resolveAPI`, `unauthorizedErr`, seam `timeNow`, sentinela `errImpersonationExpired`)
- Modify: `internal/commands/supplier.go` (usar os novos helpers, preservando mensagens atuais)
- Test: `internal/commands/impersonation_resolve_test.go`

**Interfaces:**
- Consumes: `auth.LoadImpersonation`, `auth.Impersonation`, `config.Load`, `authLoad` (seam existente), `client.API`, `newAPI` (seam existente), `client.ErrUnauthorized`.
- Produces:
  - `var timeNow = time.Now`
  - `var errImpersonationExpired = errors.New("impersonation expired")`
  - `func authedClient() (api client.API, baseURL string, imp *auth.Impersonation, err error)` — **nova assinatura (4 retornos)**.
  - `func resolveAPI() (client.API, *auth.Impersonation, error)` — mapeia `errNoToken`/`errImpersonationExpired` em mensagens prontas.
  - `func unauthorizedErr(imp *auth.Impersonation) error` — mensagem de 401 sensível a impersonação.

- [ ] **Step 1: Write the failing test**

Criar `internal/commands/impersonation_resolve_test.go`:

```go
package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/linkanalabs/cli/internal/auth"
)

// NOTE: adjust the module path import above to match go.mod if different.

func TestResolveAPIUsesOriginalWhenNoImpersonation(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_original")
	api, imp, err := resolveAPI()
	if err != nil {
		t.Fatalf("resolveAPI() error: %v", err)
	}
	if imp != nil {
		t.Errorf("imp = %+v, want nil", imp)
	}
	if api == nil {
		t.Fatal("api = nil")
	}
}

func TestResolveAPIUsesImpersonationWhenActive(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_original")
	base := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	swapTimeNow(t, func() time.Time { return base })
	cfg := "http://localhost:3000"
	_ = auth.SaveImpersonation(cfg, auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "s@linkana.com", BuyerID: "b1",
		ExpiresAt: base.Add(time.Hour),
	})
	_, imp, err := resolveAPI()
	if err != nil {
		t.Fatalf("resolveAPI() error: %v", err)
	}
	if imp == nil || imp.TargetEmail != "s@linkana.com" {
		t.Fatalf("imp = %+v", imp)
	}
}

func TestResolveAPIHardErrorsWhenImpersonationExpired(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_TOKEN", "lkn_original")
	base := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	swapTimeNow(t, func() time.Time { return base })
	cfg := "http://localhost:3000"
	_ = auth.SaveImpersonation(cfg, auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "s@linkana.com", BuyerID: "b1",
		ExpiresAt: base.Add(-time.Minute), // already expired
	})
	_, _, err := resolveAPI()
	if err == nil {
		t.Fatal("expected hard error on expired impersonation")
	}
	msg := err.Error()
	for _, want := range []string{"expirou", "lk impersonate stop", "s@linkana.com"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q: %q", want, msg)
		}
	}
}

func TestUnauthorizedErrWithoutImpersonation(t *testing.T) {
	if got := unauthorizedErr(nil).Error(); !strings.Contains(got, "lk auth login") {
		t.Errorf("got %q, want auth login hint", got)
	}
}

func TestUnauthorizedErrWithImpersonation(t *testing.T) {
	err := unauthorizedErr(&auth.Impersonation{TargetEmail: "s@linkana.com", BuyerID: "b1"})
	for _, want := range []string{"impersonação", "lk impersonate stop", "s@linkana.com"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %q", want, err.Error())
		}
	}
}
```

Adicionar o seam de tempo em um helper de teste (`internal/commands/impersonation_seams_test.go`):

```go
package commands

import "testing"

func swapTimeNow(t *testing.T, fn func() time.Time) {
	t.Helper()
	orig := timeNow
	t.Cleanup(func() { timeNow = orig })
	timeNow = fn
}
```

(Adicione `import "time"` nesse arquivo.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/cooper/dev/linkana/cli && go test ./internal/commands/ -run 'ResolveAPI|UnauthorizedErr'`
Expected: FAIL — `resolveAPI`/`unauthorizedErr`/`timeNow`/`errImpersonationExpired` indefinidos e `authedClient` com assinatura antiga.

- [ ] **Step 3: Refactor `whoami.go`**

Em `internal/commands/whoami.go`, substituir a função `authedClient` e adicionar helpers. O novo conteúdo das partes relevantes:

```go
// errNoToken signals that no token is configured for the active backend.
var errNoToken = errors.New("no token configured")

// errImpersonationExpired signals a stored-but-expired impersonation context.
// Resolution must NOT fall back to the original token in this state.
var errImpersonationExpired = errors.New("impersonation expired")

// timeNow is a seam so tests can control expiry evaluation.
var timeNow = time.Now

// newAPI is a seam so tests/commands can substitute the backend client.
var newAPI = func(baseURL, token string) client.API {
	c := client.New(baseURL)
	c.Token = token
	return c
}

// authedClient resolves the active credential for the configured backend.
//
//   - impersonation context present & not expired → use the impersonation token.
//   - impersonation context present & expired      → errImpersonationExpired
//     (sticky; never falls back to the original token).
//   - no impersonation context                     → use the original token.
func authedClient() (client.API, string, *auth.Impersonation, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", nil, err
	}
	imp, err := auth.LoadImpersonation(cfg.BaseURL)
	if err != nil {
		return nil, cfg.BaseURL, nil, err
	}
	if imp != nil {
		if imp.Expired(timeNow()) {
			return nil, cfg.BaseURL, imp, errImpersonationExpired
		}
		return newAPI(cfg.BaseURL, imp.Token), cfg.BaseURL, imp, nil
	}
	token, _, err := authLoad(cfg.BaseURL)
	if err != nil {
		return nil, cfg.BaseURL, nil, err
	}
	if token == "" {
		return nil, cfg.BaseURL, nil, errNoToken
	}
	return newAPI(cfg.BaseURL, token), cfg.BaseURL, nil, nil
}

// resolveAPI wraps authedClient and maps known errors to user-facing messages.
func resolveAPI() (client.API, *auth.Impersonation, error) {
	api, _, imp, err := authedClient()
	if err == nil {
		return api, imp, nil
	}
	switch {
	case errors.Is(err, errNoToken):
		return nil, nil, fmt.Errorf("not authenticated; run `lk auth login`")
	case errors.Is(err, errImpersonationExpired):
		return nil, imp, impersonationExpiredErr(imp)
	default:
		return nil, imp, err
	}
}

// impersonationExpiredErr renders the sticky-expiry guidance.
func impersonationExpiredErr(imp *auth.Impersonation) error {
	return fmt.Errorf(
		"impersonação de %s (buyer %s) expirou em %s.\n"+
			"rode `lk impersonate %s` pra renovar, ou `lk impersonate stop` pra voltar ao usuário original",
		imp.TargetEmail, imp.BuyerID, imp.ExpiresAt.Format(time.RFC3339), imp.TargetEmail,
	)
}

// unauthorizedErr renders a 401 message, aware of an active impersonation.
func unauthorizedErr(imp *auth.Impersonation) error {
	if imp != nil {
		return fmt.Errorf(
			"token de impersonação rejeitado (expirou ou foi revogado no servidor).\n"+
				"você está impersonando %s (buyer %s).\n"+
				"  • lk impersonate stop      → voltar ao usuário original\n"+
				"  • lk impersonate %s        → impersonar de novo (renova o token)",
			imp.TargetEmail, imp.BuyerID, imp.TargetEmail,
		)
	}
	return fmt.Errorf("token rejected (401); run `lk auth login` to re-authenticate")
}
```

Garantir imports em `whoami.go`: `errors`, `fmt`, `time`, `github.com/linkanalabs/cli/internal/auth`, `.../internal/client`, `.../internal/config`.

Atualizar o `RunE` do `whoami` para usar os helpers:

```go
		RunE: func(cmd *cobra.Command, _ []string) error {
			api, imp, err := resolveAPI()
			if err != nil {
				return err
			}
			id, err := api.GetIdentity(cmd.Context())
			if err != nil {
				if errors.Is(err, client.ErrUnauthorized) {
					return unauthorizedErr(imp)
				}
				return err
			}
			return output.Render(cmd.OutOrStdout(), formatFlag(cmd), identityView{Identity: id})
		},
```

- [ ] **Step 4: Update `supplier.go` to the new helpers**

Em `internal/commands/supplier.go`, nas duas subcomandos (`list` e `show`), trocar o bloco:

```go
			api, _, err := authedClient()
			if err != nil {
				if errors.Is(err, errNoToken) {
					return fmt.Errorf("not authenticated; run `lk auth login`")
				}
				return err
			}
```

por:

```go
			api, imp, err := resolveAPI()
			if err != nil {
				return err
			}
```

e o bloco de erro `ErrUnauthorized`:

```go
				if errors.Is(err, client.ErrUnauthorized) {
					return fmt.Errorf("token rejected (401); run `lk auth login` to re-authenticate")
				}
```

por:

```go
				if errors.Is(err, client.ErrUnauthorized) {
					return unauthorizedErr(imp)
				}
```

(Remova imports agora não usados em `supplier.go`, se `errNoToken` deixou de ser referenciado lá — rode `make fmt`/`goimports`.)

- [ ] **Step 5: Run the full commands suite to verify nothing regressed**

Run: `cd /Users/cooper/dev/linkana/cli && go test ./internal/commands/`
Expected: PASS — incluindo os testes existentes de supplier/whoami (as mensagens default são idênticas: `unauthorizedErr(nil)` e o ramo `errNoToken`).

- [ ] **Step 6: Format/lint and commit**

```bash
cd /Users/cooper/dev/linkana/cli
make fmt && make lint
git add internal/commands/whoami.go internal/commands/supplier.go internal/commands/impersonation_resolve_test.go internal/commands/impersonation_seams_test.go
git commit -m "feat(commands): sticky impersonation-aware credential resolution"
```

---

### Task C4: CLI — `lk impersonate <ref> | stop | status`

Repo: `/Users/cooper/dev/linkana/cli`

**Files:**
- Create: `internal/commands/impersonate.go`
- Modify: `internal/commands/root.go` (registrar `newImpersonateCmd()`)
- Test: `internal/commands/impersonate_test.go`

**Interfaces:**
- Consumes: `config.Load`, `authLoad`, `newAPI`, `client.API.StartImpersonation/StopImpersonation/GetIdentity`, `auth.SaveImpersonation/LoadImpersonation/DeleteImpersonation`, `auth.Impersonation`, `output.Render`, `formatFlag`, `timeNow`, `errNoToken`.
- Produces: comando cobra `impersonate` com subcomandos `start (default arg)`, `stop`, `status`; structs de view para output JSON/styled.

Notas de design dos comandos:
- `lk impersonate <email|user_id> [--ttl 24h]`: usa **sempre o token original** (não a resolução pegajosa) para chamar `StartImpersonation`. Busca o email do impersonador via `GetIdentity` (token original). Grava `auth.Impersonation`. Imprime confirmação.
- `lk impersonate stop`: carrega o contexto direto (mesmo expirado), constrói client com `imp.Token`, chama `StopImpersonation` (best-effort), depois `DeleteImpersonation`. Sem contexto → aviso.
- `lk impersonate status`: imprime o contexto ou "nenhuma impersonação ativa".

- [ ] **Step 1: Write the failing test**

Criar `internal/commands/impersonate_test.go`:

```go
package commands

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/linkanalabs/cli/internal/auth"
)

// impersonateServer mocks the backend: GET /my/identity.json (impersonator),
// POST /impersonation.json (mint), DELETE /impersonation.json (revoke).
func impersonateServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/my/identity.json":
			_, _ = w.Write([]byte(`{"id":"staff1","email":"staff@linkana.com","name":"Staff","role":"admin","buyer_id":"admin","is_staff":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/impersonation.json":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"token":"lkn_imp_tok","identity":{"user_id":"u1","email":"suporte@linkana.com","buyer_id":"b1"},"expires_at":"2026-06-23T14:00:00Z"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/impersonation.json":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestImpersonateStartStoresContext(t *testing.T) {
	authEnv(t)
	srv := impersonateServer(t)
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_original")

	var out, errOut strings.Builder
	code := run([]string{"impersonate", "suporte@linkana.com"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "suporte@linkana.com") || !strings.Contains(out.String(), "b1") {
		t.Errorf("stdout = %q", out.String())
	}
	imp, err := auth.LoadImpersonation(srv.URL)
	if err != nil || imp == nil {
		t.Fatalf("context not stored: imp=%v err=%v", imp, err)
	}
	if imp.Token != "lkn_imp_tok" || imp.ImpersonatorEmail != "staff@linkana.com" {
		t.Errorf("imp = %+v", *imp)
	}
}

func TestImpersonateStartNoToken(t *testing.T) {
	authEnv(t)
	srv := impersonateServer(t)
	t.Setenv("LK_API_URL", srv.URL)
	// no LK_TOKEN

	var out, errOut strings.Builder
	code := run([]string{"impersonate", "suporte@linkana.com"}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "lk auth login") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestImpersonateStatusActive(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "s@linkana.com", BuyerID: "b1",
		ImpersonatorEmail: "staff@linkana.com", ExpiresAt: time.Now().Add(time.Hour),
	})
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "status"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "s@linkana.com") {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestImpersonateStatusNone(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "status"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String(), "nenhuma") {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestImpersonateStopClearsContext(t *testing.T) {
	authEnv(t)
	srv := impersonateServer(t)
	t.Setenv("LK_API_URL", srv.URL)
	_ = auth.SaveImpersonation(srv.URL, auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "s@linkana.com", BuyerID: "b1",
		ExpiresAt: time.Now().Add(time.Hour),
	})
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "stop"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if imp, _ := auth.LoadImpersonation(srv.URL); imp != nil {
		t.Errorf("context still present: %+v", *imp)
	}
}

func TestImpersonateStopWhenNone(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	var out, errOut strings.Builder
	if code := run([]string{"impersonate", "stop"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out.String()+errOut.String(), "nenhuma") {
		t.Errorf("expected 'nenhuma' notice, got out=%q err=%q", out.String(), errOut.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/cooper/dev/linkana/cli && go test ./internal/commands/ -run Impersonate`
Expected: FAIL — comando `impersonate` inexistente.

- [ ] **Step 3: Implement the command**

Criar `internal/commands/impersonate.go`:

```go
package commands

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/linkanalabs/cli/internal/auth"
	"github.com/linkanalabs/cli/internal/client"
	"github.com/linkanalabs/cli/internal/config"
	"github.com/linkanalabs/cli/internal/output"
)

// impersonationView renders an active impersonation context.
type impersonationView struct {
	*auth.Impersonation
}

func (v impersonationView) MarshalJSON() ([]byte, error) {
	return jsonMarshalImpersonation(v.Impersonation)
}

func (v impersonationView) Styled() string {
	return fmt.Sprintf(
		"impersonando %s\n  buyer:        %s\n  user_id:      %s\n  expira:       %s\n  impersonador: %s\n",
		v.TargetEmail, v.BuyerID, v.TargetUserID, v.ExpiresAt.Format(time.RFC3339), v.ImpersonatorEmail,
	)
}

func newImpersonateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "impersonate <email|user_id>",
		Short: "Impersonar o usuário @linkana de um buyer (SRM)",
		Long: "Cunha um Access Token no buyer+user de destino e passa a agir como ele.\n" +
			"O estado é pegajoso: ao expirar, comandos falham até `lk impersonate stop` ou re-impersonar.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return runImpersonateStart(cmd, args[0])
		},
	}
	cmd.Flags().Duration("ttl", 0, "tempo de vida do token (ex: 24h); vazio usa o default do backend")
	cmd.AddCommand(newImpersonateStopCmd())
	cmd.AddCommand(newImpersonateStatusCmd())
	return cmd
}

func runImpersonateStart(cmd *cobra.Command, ref string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	token, _, err := authLoad(cfg.BaseURL)
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("not authenticated; run `lk auth login`")
	}
	api := newAPI(cfg.BaseURL, token)

	ttl, _ := cmd.Flags().GetDuration("ttl")
	imp, err := api.StartImpersonation(cmd.Context(), ref, ttl)
	if err != nil {
		if errors.Is(err, client.ErrUnauthorized) {
			return fmt.Errorf("token rejected (401); run `lk auth login` to re-authenticate")
		}
		return err
	}

	impersonator := ""
	if id, idErr := api.GetIdentity(cmd.Context()); idErr == nil {
		impersonator = id.Email
	}

	ctx := auth.Impersonation{
		Token:             imp.Token,
		TargetEmail:       imp.Identity.Email,
		TargetUserID:      imp.Identity.UserID,
		BuyerID:           imp.Identity.BuyerID,
		ImpersonatorEmail: impersonator,
		ExpiresAt:         imp.ExpiresAt,
	}
	if err := auth.SaveImpersonation(cfg.BaseURL, ctx); err != nil {
		return fmt.Errorf("saving impersonation context: %w", err)
	}
	return output.Render(cmd.OutOrStdout(), formatFlag(cmd), impersonationView{Impersonation: &ctx})
}

func newImpersonateStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Encerrar a impersonação ativa",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			imp, err := auth.LoadImpersonation(cfg.BaseURL)
			if err != nil {
				return err
			}
			if imp == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "nenhuma impersonação ativa")
				return nil
			}
			// Best-effort revoke using the impersonation token itself.
			api := newAPI(cfg.BaseURL, imp.Token)
			if stopErr := api.StopImpersonation(cmd.Context()); stopErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "aviso: revogação remota falhou (%v); limpando estado local mesmo assim\n", stopErr)
			}
			if err := auth.DeleteImpersonation(cfg.BaseURL); err != nil {
				return fmt.Errorf("clearing impersonation context: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "impersonação de %s encerrada\n", imp.TargetEmail)
			return nil
		},
	}
}

func newImpersonateStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Mostrar a impersonação ativa",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			imp, err := auth.LoadImpersonation(cfg.BaseURL)
			if err != nil {
				return err
			}
			if imp == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "nenhuma impersonação ativa")
				return nil
			}
			note := ""
			if imp.Expired(timeNow()) {
				note = " (EXPIRADA — rode `lk impersonate stop` ou re-impersonar)"
			}
			if formatFlag(cmd) == output.FormatJSON {
				return output.Render(cmd.OutOrStdout(), output.FormatJSON, impersonationView{Impersonation: imp})
			}
			out := impersonationView{Impersonation: imp}.Styled()
			fmt.Fprint(cmd.OutOrStdout(), strings.TrimRight(out, "\n")+note+"\n")
			return nil
		},
	}
}

// jsonMarshalImpersonation emits a stable JSON shape (without the secret token).
func jsonMarshalImpersonation(i *auth.Impersonation) ([]byte, error) {
	type public struct {
		TargetEmail       string    `json:"target_email"`
		TargetUserID      string    `json:"target_user_id"`
		BuyerID           string    `json:"buyer_id"`
		ImpersonatorEmail string    `json:"impersonator_email"`
		ExpiresAt         time.Time `json:"expires_at"`
	}
	return jsonMarshal(public{
		TargetEmail:       i.TargetEmail,
		TargetUserID:      i.TargetUserID,
		BuyerID:           i.BuyerID,
		ImpersonatorEmail: i.ImpersonatorEmail,
		ExpiresAt:         i.ExpiresAt,
	})
}
```

> O segredo `Token` **nunca** entra no JSON de output (só é persistido no storage). `jsonMarshal` é um wrapper fino sobre `encoding/json.Marshal`; se já existir um helper no pacote, reutilize. Caso contrário, adicione em `impersonate.go`:
> ```go
> func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
> ```
> e importe `encoding/json`.

- [ ] **Step 4: Register the command**

Em `internal/commands/root.go`, após `root.AddCommand(newSupplierCmd())`:

```go
	root.AddCommand(newImpersonateCmd())
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/cooper/dev/linkana/cli && go test ./internal/commands/ -run Impersonate`
Expected: PASS.

- [ ] **Step 6: Full suite + coverage + lint**

Run: `cd /Users/cooper/dev/linkana/cli && make test && make cover && make lint`
Expected: verde; cobertura ≥95%.

- [ ] **Step 7: Commit**

```bash
cd /Users/cooper/dev/linkana/cli
make fmt
git add internal/commands/impersonate.go internal/commands/root.go internal/commands/impersonate_test.go
git commit -m "feat(commands): lk impersonate start|stop|status"
```

---

### Task C5: CLI — surface impersonation in `whoami` and `auth status`

Repo: `/Users/cooper/dev/linkana/cli`

**Files:**
- Modify: `internal/commands/whoami.go` (banner de impersonação no `whoami`)
- Modify: `internal/commands/auth.go` (bloco de impersonação no `auth status`)
- Test: `internal/commands/impersonation_visibility_test.go`

**Interfaces:**
- Consumes: `auth.LoadImpersonation`, `auth.Impersonation`, `resolveAPI`, `timeNow`.
- Produces: linhas extras em stderr/stdout indicando a impersonação ativa.

> Antes de escrever, leia `internal/commands/auth.go` para casar com o formato do `auth status` atual (`grep -n "func newAuthStatusCmd\|Styled\|status" internal/commands/auth.go`). Ajuste os nomes reais.

- [ ] **Step 1: Write the failing test**

Criar `internal/commands/impersonation_visibility_test.go`:

```go
package commands

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/linkanalabs/cli/internal/auth"
)

func TestWhoamiShowsImpersonationBanner(t *testing.T) {
	authEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// impersonation token resolves to the target identity
		_, _ = w.Write([]byte(`{"id":"u1","email":"suporte@linkana.com","name":"Suporte","role":"operator","buyer_id":"b1","is_staff":false}`))
	}))
	defer srv.Close()
	t.Setenv("LK_API_URL", srv.URL)
	t.Setenv("LK_TOKEN", "lkn_original")
	_ = auth.SaveImpersonation(srv.URL, auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "suporte@linkana.com", BuyerID: "b1",
		ImpersonatorEmail: "staff@linkana.com", ExpiresAt: time.Now().Add(time.Hour),
	})

	var out, errOut strings.Builder
	code := run([]string{"whoami", "--format", "styled"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	combined := out.String() + errOut.String()
	if !strings.Contains(combined, "impersonando") || !strings.Contains(combined, "staff@linkana.com") {
		t.Errorf("whoami should surface impersonation: out=%q err=%q", out.String(), errOut.String())
	}
}

func TestAuthStatusShowsImpersonation(t *testing.T) {
	authEnv(t)
	t.Setenv("LK_API_URL", "http://localhost:3000")
	t.Setenv("LK_TOKEN", "lkn_original")
	_ = auth.SaveImpersonation("http://localhost:3000", auth.Impersonation{
		Token: "lkn_imp", TargetEmail: "suporte@linkana.com", BuyerID: "b1",
		ImpersonatorEmail: "staff@linkana.com", ExpiresAt: time.Now().Add(time.Hour),
	})
	var out, errOut strings.Builder
	if code := run([]string{"auth", "status"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "suporte@linkana.com") {
		t.Errorf("auth status should show impersonation: %q", out.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/cooper/dev/linkana/cli && go test ./internal/commands/ -run 'WhoamiShowsImpersonation|AuthStatusShowsImpersonation'`
Expected: FAIL.

- [ ] **Step 3: Add the whoami banner**

No `RunE` do `whoami` (em `whoami.go`), antes do `return output.Render(...)`, inserir banner em stderr quando impersonando:

```go
			if imp != nil {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"⚠ impersonando %s (buyer %s, expira %s); original: %s\n",
					imp.TargetEmail, imp.BuyerID, imp.ExpiresAt.Format(time.RFC3339), imp.ImpersonatorEmail)
			}
```

(`imp` já vem de `resolveAPI()`; stderr mantém stdout limpo para o contrato JSON.)

- [ ] **Step 4: Add the auth status block**

Em `internal/commands/auth.go`, no comando `status` (após renderizar o status normal), carregar e exibir impersonação:

```go
			cfg, _ := config.Load()
			if imp, _ := auth.LoadImpersonation(cfg.BaseURL); imp != nil {
				state := "ativa"
				if imp.Expired(timeNow()) {
					state = "EXPIRADA"
				}
				fmt.Fprintf(cmd.OutOrStdout(),
					"impersonação (%s): %s (buyer %s, expira %s; por %s)\n",
					state, imp.TargetEmail, imp.BuyerID, imp.ExpiresAt.Format(time.RFC3339), imp.ImpersonatorEmail)
			}
```

Ajustar imports (`config`, `auth`, `time`, `fmt`) e casar com a estrutura real do comando `status` (ler o arquivo antes). Se `status` usa `output.Render`, anexe o bloco após o render.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/cooper/dev/linkana/cli && go test ./internal/commands/`
Expected: PASS (incluindo testes existentes de `auth`/`whoami`).

- [ ] **Step 6: Coverage + lint + commit**

```bash
cd /Users/cooper/dev/linkana/cli
make cover && make lint && make fmt
git add internal/commands/whoami.go internal/commands/auth.go internal/commands/impersonation_visibility_test.go
git commit -m "feat(commands): surface active impersonation in whoami and auth status"
```

---

### Task C6: Docs — `CLAUDE.md` (buyer-scope/impersonação + repo backend)

Repo: `/Users/cooper/dev/linkana/cli`

**Files:**
- Modify: `CLAUDE.md`

**Interfaces:** N/A (documentação). Objetivo: o agente não precisar repetir contexto ("impersonar", "buyer", "outro repo") a cada sessão.

- [ ] **Step 1: Update the "Estado atual" + add two sections**

Em `CLAUDE.md`, atualizar a linha de comandos para incluir `impersonate` e adicionar duas seções novas (após "Estado atual"):

```markdown
## Impersonação / buyer-scope (LIN-5921)

Comandos SRM são **buyer-scoped** (dependem de `current_user.buyer`). O agente não
tem sessão de buyer próprio; para agir num buyer, **impersone o usuário `@linkana`
daquele buyer**:

- `lk impersonate <email|user_id>` — cunha um Access Token real no buyer+user alvo
  (gate no backend: caller `linkana_admin?` + alvo `@linkana` + buyer com
  `allow_linkana_support`). Default TTL 24h, `--ttl` ajusta.
- `lk impersonate status` — mostra a impersonação ativa (alvo, buyer, expiry).
- `lk impersonate stop` — revoga o token no servidor e limpa o estado local.

**Estado pegajoso:** enquanto há impersonação gravada, o token original fica
inacessível. Expirou (relógio local) → comando falha com erro duro. Rejeitado pelo
servidor (401) → mesmo erro duro. **Nunca** cai silenciosamente pro usuário
original — escolha sempre `lk impersonate stop` ou re-impersonar.

## Repositório backend (Rails)

O backend Linkana fica em `../linkana` (working dir adicional). Referências da
impersonação:
- `app/controllers/impersonations_controller.rb` — endpoint JSON `POST/DELETE /impersonation`.
- `app/policies/srm_policy.rb` — `enforce_impersonation_rules` (gate de escrita).
- `lib/warden/pat_bearer_strategy.rb` — PAT Bearer → `current_user`.
- `app/models/api_token.rb` + `app/models/api_tokens/build.rb` — Access Token.
- `buyers.allow_linkana_support` — flag que libera suporte (toggle em
  `app/controllers/srm_settings/access_configurations_controller.rb`).
```

Também trocar na linha de comandos existentes `supplier list|show` por
`supplier list|show`, `impersonate <ref>|stop|status`.

- [ ] **Step 2: Commit**

```bash
cd /Users/cooper/dev/linkana/cli
git add CLAUDE.md
git commit -m "docs(cli): document impersonation flow and backend repo pointers"
```

---

## End-to-end verification (após todas as tasks)

Pré-requisito: liberar as portas 3000 (Rails) e 3001 se ocupadas, subir o Rails e
ter um token PAT de um user `@linkana` staff + um buyer com `allow_linkana_support`.

- [ ] Rails em `localhost:3000` (`cd ../linkana && bin/rails s -p 3000`).
- [ ] `LK_API_URL=http://localhost:3000 lk auth login` com PAT staff.
- [ ] `lk impersonate suporte+<buyer>@linkana.com` → confirma alvo/buyer/expiry.
- [ ] `lk supplier list` → retorna suppliers **do buyer impersonado**.
- [ ] `lk whoami` → mostra identidade alvo + banner de impersonação.
- [ ] `lk impersonate stop` → token revogado; `lk supplier list` volta ao contexto original.
- [ ] Forçar expiração (TTL curto `--ttl 5s`, esperar) → `lk supplier list` falha com erro duro, sem agir no buyer original.

## Self-Review (preenchido pelo autor do plano)

- **Cobertura do spec:** endpoint mint/revoke (B1); client Post/Delete + chamadas (C1);
  storage de contexto (C2); resolução pegajosa + 401 (C3); comandos start/stop/status (C4);
  visibilidade whoami/auth status (C5); instruções CLAUDE.md (C6). Gate de segurança e
  ramos de negação cobertos em B1. TTL 24h + cap 7d em B1; flag `--ttl` em C4.
- **Placeholders:** nenhum — todo passo traz código/comando reais.
- **Consistência de tipos:** `client.Impersonation`/`ImpersonationIdentity` (resposta) vs
  `auth.Impersonation` (storage) são tipos distintos em pacotes distintos, mapeados
  explicitamente em `runImpersonateStart`. `authedClient` tem 4 retornos consistentes
  em todos os call sites (whoami, supplier via `resolveAPI`).
- **Ajuste de módulo:** os imports usam `github.com/linkanalabs/cli/...` — confirme o
  módulo real em `go.mod` e ajuste se diferente (1 substituição global nos testes/arquivos novos).
```
