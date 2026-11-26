package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/ory/fosite"
	"github.com/ory/fosite/compose"
	"gorm.io/gorm"

	"github.com/claviq/desk/backend/internal/database"
	"github.com/claviq/desk/backend/internal/models"
	"github.com/claviq/desk/backend/internal/signing"
)

const (
	ScopeDesk    = "desk"
	ScopeOffline = "offline_access"
)

var supportedScopes = []string{ScopeDesk, ScopeOffline}

// OAuth is the in-house OAuth 2.1 authorization server (Ory Fosite) that lets
// Claude, ChatGPT and other MCP clients connect to one user's tenant.
type OAuth struct {
	provider fosite.OAuth2Provider
	config   *fosite.Config
	store    *Store
	sessions *Sessions
	db       *gorm.DB
	issuer   string
	csrf     *signing.Signer
	log      *slog.Logger
}

func NewOAuth(db *gorm.DB, sessions *Sessions, issuer, appSecret string, log *slog.Logger) *OAuth {
	cfg := &fosite.Config{
		AccessTokenLifespan:            time.Hour,
		RefreshTokenLifespan:           60 * 24 * time.Hour,
		AuthorizeCodeLifespan:          10 * time.Minute,
		GlobalSecret:                   signing.DeriveKey(appSecret, "oauth-hmac/v1"),
		EnforcePKCE:                    true,
		EnablePKCEPlainChallengeMethod: false,
		RefreshTokenScopes:             []string{}, // always issue refresh tokens to clients allowed the grant
		ScopeStrategy:                  fosite.ExactScopeStrategy,
		AudienceMatchingStrategy:       fosite.DefaultAudienceMatchingStrategy,
		TokenURL:                       issuer + "/oauth/token",
		AccessTokenIssuer:              issuer,
		SendDebugMessagesToClients:     false,
	}
	store := NewStore(db)
	strategy := compose.NewOAuth2HMACStrategy(cfg)
	provider := compose.Compose(cfg, store, strategy,
		compose.OAuth2AuthorizeExplicitFactory,
		compose.OAuth2RefreshTokenGrantFactory,
		compose.OAuth2TokenRevocationFactory,
		compose.OAuth2TokenIntrospectionFactory,
		compose.OAuth2PKCEFactory,
	)
	return &OAuth{provider: provider, config: cfg, store: store, sessions: sessions, db: db, issuer: issuer,
		csrf: signing.NewSigner(appSecret, "oauth-consent/v1"), log: log}
}

func (o *OAuth) Store() *Store { return o.store }

// ResourceMetadataURL is advertised in WWW-Authenticate on 401s from /mcp.
func (o *OAuth) ResourceMetadataURL() string {
	return o.issuer + "/.well-known/oauth-protected-resource/mcp"
}

// ---------- discovery metadata (RFC 8414, RFC 9728) ----------

func (o *OAuth) AuthServerMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                o.issuer,
		"authorization_endpoint":                o.issuer + "/oauth/authorize",
		"token_endpoint":                        o.issuer + "/oauth/token",
		"registration_endpoint":                 o.issuer + "/oauth/register",
		"revocation_endpoint":                   o.issuer + "/oauth/revoke",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none", "client_secret_basic", "client_secret_post"},
		"scopes_supported":                      supportedScopes,
	})
}

func (o *OAuth) ProtectedResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 o.issuer + "/mcp",
		"authorization_servers":    []string{o.issuer},
		"scopes_supported":         []string{ScopeDesk},
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Claviq Desk",
	})
}

// ---------- dynamic client registration (RFC 7591) ----------

type registrationRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	ClientName              string   `json:"client_name"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	Scope                   string   `json:"scope"`
}

func (o *OAuth) Register(w http.ResponseWriter, r *http.Request) {
	var req registrationRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		regError(w, "invalid_client_metadata", "request body must be JSON")
		return
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 10 {
		regError(w, "invalid_redirect_uri", "1–10 redirect_uris are required")
		return
	}
	for _, u := range req.RedirectURIs {
		if !validRedirect(u) {
			regError(w, "invalid_redirect_uri", "redirect URIs must be https (or http on localhost) without fragments: "+u)
			return
		}
	}
	method := req.TokenEndpointAuthMethod
	if method == "" {
		method = "none"
	}
	if !slices.Contains([]string{"none", "client_secret_basic", "client_secret_post"}, method) {
		regError(w, "invalid_client_metadata", "unsupported token_endpoint_auth_method")
		return
	}
	for _, g := range req.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			regError(w, "invalid_client_metadata", "unsupported grant type "+g)
			return
		}
	}
	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "MCP client"
	}
	if len(name) > 100 {
		name = name[:100]
	}

	client := models.OAuthClient{
		ClientID:                randomHex(16),
		Name:                    name,
		RedirectURIs:            models.NewJSONB(req.RedirectURIs),
		GrantTypes:              models.NewJSONB([]string{"authorization_code", "refresh_token"}),
		ResponseTypes:           models.NewJSONB([]string{"code"}),
		Scopes:                  models.NewJSONB(supportedScopes),
		TokenEndpointAuthMethod: method,
		RegisteredVia:           "dynamic",
	}
	var secret string
	if method != "none" {
		secret = randomHex(32)
		hash, err := (&fosite.BCrypt{Config: o.config}).Hash(r.Context(), []byte(secret))
		if err != nil {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		h := string(hash)
		client.ClientSecretHash = &h
	}
	if err := o.db.WithContext(r.Context()).Create(&client).Error; err != nil {
		o.log.Error("register client", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	resp := map[string]any{
		"client_id":                  client.ClientID,
		"client_id_issued_at":        client.CreatedAt.Unix(),
		"client_name":                client.Name,
		"redirect_uris":              req.RedirectURIs,
		"grant_types":                client.GrantTypes.V,
		"response_types":             client.ResponseTypes.V,
		"token_endpoint_auth_method": method,
		"scope":                      strings.Join(supportedScopes, " "),
	}
	if secret != "" {
		resp["client_secret"] = secret
		resp["client_secret_expires_at"] = 0
	}
	writeJSON(w, http.StatusCreated, resp)
}

func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.Host == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	h := u.Hostname()
	return u.Scheme == "http" && (h == "localhost" || h == "127.0.0.1" || h == "::1")
}

func regError(w http.ResponseWriter, code, desc string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": code, "error_description": desc})
}

// ---------- authorize + consent ----------

type consentClaims struct {
	UserID uuid.UUID `json:"u"`
}

func (o *OAuth) Authorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ar, err := o.provider.NewAuthorizeRequest(ctx, r)
	if err != nil {
		o.log.Info("authorize request rejected", "error", err)
		o.provider.WriteAuthorizeError(ctx, w, ar, err)
		return
	}
	// OAuth 2.1: PKCE (S256) on every flow. Fosite checks this when issuing the
	// code; reject earlier so the owner is never shown a consent screen for it.
	if ar.GetRequestForm().Get("code_challenge") == "" || ar.GetRequestForm().Get("code_challenge_method") != "S256" {
		o.provider.WriteAuthorizeError(ctx, w, ar, fosite.ErrInvalidRequest.WithHint("PKCE with code_challenge_method=S256 is required."))
		return
	}

	user, err := o.sessions.FromRequest(r)
	if err != nil {
		// Not logged in: the web app's login page returns here afterwards.
		authorizeURL := "/oauth/authorize?" + r.URL.RawQuery
		http.Redirect(w, r, "/login?next="+url.QueryEscape(authorizeURL), http.StatusFound)
		return
	}

	if r.Method == http.MethodGet {
		o.renderConsent(w, r, ar, user)
		return
	}

	// POST: the owner's decision.
	claims, err := signing.Verify[consentClaims](o.csrf, r.PostFormValue("consent_token"))
	if err != nil || claims.UserID != user.ID {
		http.Error(w, "consent expired, please try again", http.StatusBadRequest)
		return
	}
	if r.PostFormValue("decision") != "allow" {
		o.provider.WriteAuthorizeError(ctx, w, ar, fosite.ErrAccessDenied.WithHint("The owner declined access."))
		return
	}

	for _, sc := range ar.GetRequestedScopes() {
		if slices.Contains(supportedScopes, sc) {
			ar.GrantScope(sc)
		}
	}
	ar.GrantScope(ScopeDesk)
	for _, aud := range ar.GetRequestedAudience() {
		ar.GrantAudience(aud)
	}

	sess := &fosite.DefaultSession{
		Subject:  user.ID.String(),
		Username: user.Email,
		Extra:    map[string]any{"tenant_id": user.TenantID.String(), "user_id": user.ID.String()},
	}
	resp, err := o.provider.NewAuthorizeResponse(ctx, ar, sess)
	if err != nil {
		o.provider.WriteAuthorizeError(ctx, w, ar, err)
		return
	}

	clientUUID, _ := o.store.ClientUUID(ctx, ar.GetClient().GetID())
	_ = database.WithTenant(ctx, o.db, user.TenantID, func(tx *gorm.DB) error {
		cid := clientUUID
		return tx.Create(&models.AuditLog{TenantID: user.TenantID, UserID: &user.ID, Via: "web", OAuthClientID: &cid,
			Action: "connect_ai_client", Entity: "oauth_client", EntityID: ptr(ar.GetClient().GetID())}).Error
	})
	o.provider.WriteAuthorizeResponse(ctx, w, ar, resp)
}

func ptr[T any](v T) *T { return &v }

var consentTmpl = template.Must(template.New("consent").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Connect {{.ClientName}} — Claviq Desk</title>
<style>
:root{--bg:#fafafa;--card:#fff;--ink:#111827;--muted:#6b7280;--line:#e5e7eb;--accent:#1e3a5f}
@media (prefers-color-scheme:dark){:root{--bg:#0a0a0a;--card:#171717;--ink:#f5f5f5;--muted:#a3a3a3;--line:#262626;--accent:#93b4d8}}
*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;background:var(--bg);color:var(--ink);font:15px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif;padding:16px}
.card{width:100%;max-width:400px;background:var(--card);border:1px solid var(--line);border-radius:12px;padding:24px}
h1{font-size:18px;margin:0 0 4px}p{margin:0 0 16px;color:var(--muted)}ul{margin:0 0 20px;padding-left:18px}li{margin:4px 0}
.who{font-size:13px;color:var(--muted);border-top:1px solid var(--line);padding-top:12px;margin-top:4px}
.row{display:flex;gap:8px}button{flex:1;height:44px;border-radius:8px;font:inherit;font-weight:600;cursor:pointer;border:1px solid var(--line);background:transparent;color:var(--ink)}
button.allow{background:var(--accent);border-color:var(--accent);color:var(--card)}
</style></head><body>
<form class="card" method="post" action="{{.Action}}">
<h1>Connect {{.ClientName}}?</h1>
<p>{{.ClientName}} wants to work in <strong>{{.TenantName}}</strong> on Claviq Desk.</p>
<ul><li>Read and create customers</li><li>Create and edit draft estimates and invoices</li><li>Create and edit templates</li><li>Finalise documents when you ask it to</li></ul>
<input type="hidden" name="consent_token" value="{{.Token}}">
<div class="row"><button type="submit" name="decision" value="deny">Cancel</button><button class="allow" type="submit" name="decision" value="allow">Allow</button></div>
<p class="who">Signed in as {{.Email}}. Redirects to {{.RedirectHost}}. You can disconnect it any time in Settings.</p>
</form></body></html>`))

func (o *OAuth) renderConsent(w http.ResponseWriter, r *http.Request, ar fosite.AuthorizeRequester, user *models.User) {
	tok, _ := signing.Sign(o.csrf, consentClaims{UserID: user.ID}, 10*time.Minute)
	var tenantName string
	var names []string
	_ = database.WithTenant(r.Context(), o.db, user.TenantID, func(tx *gorm.DB) error {
		return tx.Model(&models.Tenant{}).Where("id = ?", user.TenantID).Pluck("name", &names).Error
	})
	if len(names) == 1 {
		tenantName = names[0]
	}
	var c models.OAuthClient
	o.db.WithContext(r.Context()).Where("client_id = ?", ar.GetClient().GetID()).First(&c)
	redirectHost := ar.GetRedirectURI().Host
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self' https: http://localhost:* http://127.0.0.1:*; frame-ancestors 'none'")
	_ = consentTmpl.Execute(w, map[string]string{
		"ClientName": c.Name, "TenantName": tenantName, "Email": user.Email,
		"RedirectHost": redirectHost, "Token": tok, "Action": "/oauth/authorize?" + r.URL.RawQuery,
	})
}

// ---------- token & revocation ----------

func (o *OAuth) Token(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ar, err := o.provider.NewAccessRequest(ctx, r, &fosite.DefaultSession{})
	if err != nil {
		o.log.Info("token request rejected", "error", err)
		o.provider.WriteAccessError(ctx, w, ar, err)
		return
	}
	resp, err := o.provider.NewAccessResponse(ctx, ar)
	if err != nil {
		o.provider.WriteAccessError(ctx, w, ar, err)
		return
	}
	o.provider.WriteAccessResponse(ctx, w, ar, resp)
}

func (o *OAuth) Revoke(w http.ResponseWriter, r *http.Request) {
	err := o.provider.NewRevocationRequest(r.Context(), r)
	o.provider.WriteRevocationResponse(r.Context(), w, err)
}

// ---------- bearer verification for /mcp ----------

// Principal is the authenticated MCP caller.
type Principal struct {
	TenantID uuid.UUID
	UserID   uuid.UUID
	ClientID uuid.UUID
}

// VerifyAccessToken is the TokenVerifier used by the MCP SDK's bearer middleware.
func (o *OAuth) VerifyAccessToken(ctx context.Context, token string, _ *http.Request) (*mcpauth.TokenInfo, error) {
	sess := &fosite.DefaultSession{}
	_, ar, err := o.provider.IntrospectToken(ctx, token, fosite.AccessToken, sess)
	if err != nil {
		return nil, mcpauth.ErrInvalidToken
	}
	tenantID, userID, err := sessionIDs(ar.GetSession())
	if err != nil {
		return nil, mcpauth.ErrInvalidToken
	}
	clientUUID, err := o.store.ClientUUID(ctx, ar.GetClient().GetID())
	if err != nil {
		return nil, mcpauth.ErrInvalidToken
	}
	o.store.touchClient(ctx, clientUUID)
	return &mcpauth.TokenInfo{
		Scopes:     ar.GetGrantedScopes(),
		Expiration: ar.GetSession().GetExpiresAt(fosite.AccessToken),
		UserID:     userID.String(),
		Extra:      map[string]any{"principal": Principal{TenantID: tenantID, UserID: userID, ClientID: clientUUID}},
	}, nil
}

// PrincipalFromContext returns the MCP caller set by the bearer middleware.
func PrincipalFromContext(ctx context.Context) (Principal, error) {
	ti := mcpauth.TokenInfoFromContext(ctx)
	if ti == nil {
		return Principal{}, errors.New("unauthenticated")
	}
	p, ok := ti.Extra["principal"].(Principal)
	if !ok {
		return Principal{}, errors.New("unauthenticated")
	}
	return p, nil
}

// ---------- helpers ----------

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
