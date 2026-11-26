// Package httpapi wires HTTP routes: the REST API (/api), the public preview
// endpoints, OAuth (/oauth, /.well-known) and the MCP endpoint (/mcp).
// Handlers are thin: decode, call the domain service, encode.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"gorm.io/gorm"

	"github.com/claviq/desk/backend/internal/auth"
	"github.com/claviq/desk/backend/internal/domain"
)

type Deps struct {
	DB         *gorm.DB
	Service    *domain.Service
	Sessions   *auth.Sessions
	OAuth      *auth.OAuth
	MCPHandler http.Handler // already wrapped with bearer auth
	PublicURL  string
	Logger     *slog.Logger
}

type api struct {
	Deps
	limits *limiters
}

func NewRouter(d Deps) http.Handler {
	a := &api{Deps: d, limits: newLimiters()}
	mux := http.NewServeMux()

	// Health
	mux.HandleFunc("GET /api/health", a.health)

	// Owner auth
	mux.Handle("POST /api/auth/login", a.limit("login", 5, time.Minute, a.login))
	mux.HandleFunc("POST /api/auth/logout", a.logout)
	mux.Handle("GET /api/auth/me", a.authed(a.me))
	mux.Handle("POST /api/auth/password", a.authed(a.changePassword))
	mux.Handle("GET /api/auth/connections", a.authed(a.connections))
	mux.Handle("DELETE /api/auth/connections/{clientID}", a.authed(a.disconnect))

	// Settings
	mux.Handle("GET /api/settings", a.authed(a.getSettings))
	mux.Handle("PATCH /api/settings", a.authed(a.updateSettings))
	mux.Handle("GET /api/settings/logo", a.authed(a.getLogo))
	mux.Handle("POST /api/settings/logo", a.authed(a.uploadLogo))
	mux.Handle("DELETE /api/settings/logo", a.authed(a.removeLogo))
	mux.Handle("POST /api/tax-rates", a.authed(a.saveTaxRate))
	mux.Handle("PATCH /api/tax-rates/{id}", a.authed(a.saveTaxRate))
	mux.Handle("POST /api/bank-accounts", a.authed(a.saveBankAccount))
	mux.Handle("PATCH /api/bank-accounts/{id}", a.authed(a.saveBankAccount))
	mux.Handle("PATCH /api/number-sequences/{docType}", a.authed(a.updateSequence))
	mux.Handle("GET /api/audit", a.authed(a.listAudit))

	// Customers
	mux.Handle("GET /api/customers", a.authed(a.listCustomers))
	mux.Handle("POST /api/customers", a.authed(a.createCustomer))
	mux.Handle("GET /api/customers/{id}", a.authed(a.getCustomer))
	mux.Handle("PATCH /api/customers/{id}", a.authed(a.updateCustomer))
	mux.Handle("POST /api/customers/{id}/archive", a.authed(a.archiveCustomer(true)))
	mux.Handle("POST /api/customers/{id}/unarchive", a.authed(a.archiveCustomer(false)))

	// Templates & fonts
	mux.Handle("GET /api/templates", a.authed(a.listTemplates))
	mux.Handle("POST /api/templates", a.authed(a.createTemplate))
	mux.Handle("GET /api/templates/{id}", a.authed(a.getTemplate))
	mux.Handle("PATCH /api/templates/{id}", a.authed(a.updateTemplate))
	mux.Handle("POST /api/templates/{id}/archive", a.authed(a.archiveTemplate(true)))
	mux.Handle("POST /api/templates/{id}/unarchive", a.authed(a.archiveTemplate(false)))
	mux.Handle("GET /api/templates/{id}/versions", a.authed(a.listTemplateVersions))
	mux.Handle("GET /api/templates/{id}/versions/{vid}", a.authed(a.getTemplateVersion))
	mux.Handle("POST /api/templates/{id}/versions/{vid}/restore", a.authed(a.restoreTemplateVersion))
	mux.Handle("POST /api/templates/{id}/preview", a.authed(a.previewTemplate))
	mux.Handle("GET /api/fonts", a.authed(a.listFonts))
	mux.Handle("POST /api/fonts/google", a.authed(a.addGoogleFont))
	mux.Handle("POST /api/fonts/upload", a.authed(a.uploadFont))

	// Estimates & invoices (same handlers, kind from the path)
	for _, kind := range []string{domain.KindEstimate, domain.KindInvoice} {
		base := "/api/" + kind + "s"
		mux.Handle("GET "+base, a.authed(a.listDocuments(kind)))
		mux.Handle("POST "+base, a.authed(a.createDocument(kind)))
		mux.Handle("GET "+base+"/{id}", a.authed(a.getDocument(kind)))
		mux.Handle("PATCH "+base+"/{id}", a.authed(a.updateDocument(kind)))
		mux.Handle("DELETE "+base+"/{id}", a.authed(a.deleteDocument(kind)))
		mux.Handle("POST "+base+"/{id}/finalise", a.authed(a.finaliseDocument(kind)))
		mux.Handle("POST "+base+"/{id}/revise", a.authed(a.reviseDocument(kind)))
		mux.Handle("GET "+base+"/{id}/links", a.authed(a.documentLinks(kind)))
		mux.Handle("GET "+base+"/{id}/pdf", a.authed(a.documentPDF(kind)))
	}
	mux.Handle("POST /api/estimates/{id}/convert", a.authed(a.convertEstimate))

	// Public, signed preview links (no login; rate-limited per IP)
	mux.Handle("GET /api/public/p/{token}", a.limit("preview", 60, time.Minute, a.previewInfo))
	mux.Handle("GET /api/public/p/{token}/pages/{n}", a.limit("preview-page", 240, time.Minute, a.previewPage))
	mux.Handle("GET /api/public/p/{token}/pdf", a.limit("preview-pdf", 30, time.Minute, a.previewPDF))

	// OAuth 2.1 authorization server + discovery
	mux.Handle("GET /.well-known/oauth-authorization-server", cors(http.HandlerFunc(d.OAuth.AuthServerMetadata)))
	mux.Handle("GET /.well-known/oauth-protected-resource", cors(http.HandlerFunc(d.OAuth.ProtectedResourceMetadata)))
	mux.Handle("GET /.well-known/oauth-protected-resource/mcp", cors(http.HandlerFunc(d.OAuth.ProtectedResourceMetadata)))
	mux.Handle("POST /oauth/register", cors(a.limit("oauth-register", 20, time.Hour, d.OAuth.Register)))
	mux.Handle("GET /oauth/authorize", a.limit("oauth-authorize", 30, time.Minute, d.OAuth.Authorize))
	mux.Handle("POST /oauth/authorize", a.limit("oauth-authorize", 30, time.Minute, d.OAuth.Authorize))
	mux.Handle("POST /oauth/token", cors(a.limit("oauth-token", 60, time.Minute, d.OAuth.Token)))
	mux.Handle("POST /oauth/revoke", cors(a.limit("oauth-revoke", 60, time.Minute, d.OAuth.Revoke)))
	mux.Handle("OPTIONS /oauth/", cors(http.NotFoundHandler()))
	mux.Handle("OPTIONS /.well-known/", cors(http.NotFoundHandler()))

	// MCP (bearer-token protected; per-IP limit here, per-user limit in the MCP layer)
	mux.Handle("/mcp", cors(a.limitHandler("mcp", 240, time.Minute, d.MCPHandler)))

	return chain(mux, recoverer(d.Logger), securityHeaders, requestLogger(d.Logger), requestID)
}

func (a *api) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status, code := "ok", http.StatusOK
	sqlDB, err := a.DB.DB()
	if err == nil {
		err = sqlDB.PingContext(ctx)
	}
	if err != nil {
		status, code = "database unavailable", http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]string{"status": status})
}
