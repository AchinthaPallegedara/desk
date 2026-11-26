// Package mcpserver exposes Claviq Desk to AI clients over MCP (/mcp).
//
// Tool handlers are thin adapters: they convert AI-friendly inputs (plain
// numbers and string IDs) into domain inputs, call the shared domain service
// (the same one the REST API uses), and return its views. No business rules here.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/time/rate"

	"github.com/claviq/desk/backend/internal/auth"
	"github.com/claviq/desk/backend/internal/domain"
	"github.com/claviq/desk/backend/internal/render"
	"github.com/claviq/desk/backend/internal/reqctx"
)

const (
	serverName    = "claviq-desk"
	serverVersion = "1.0.0"
)

const instructions = `Claviq Desk runs the owner's business: customers, estimates, invoices and their PDF templates.

Typical flow for a new requirement:
1. find_customers by name/phone/company. If there is no match, create_customer.
2. create_estimate with line items (and optional add-ons and milestones). It uses the default template unless template_id is given.
3. Share the returned preview_url with the owner so they can review it on their phone.
4. Apply the owner's changes with update_estimate (items and milestones replace the whole list), then share the preview again.
5. Only when the owner explicitly says it is final, call finalise_document with owner_confirmed=true. Then share pdf_url.

Rules:
- Never finalise, and never claim a document was sent: Claviq Desk does not send anything; the owner sends PDFs manually.
- Final documents are locked. To change one, use revise_document to create a new draft revision.
- Amounts are in the document currency (LKR by default). Unit prices exclude tax; tax comes from the tenant's tax rates (get_business_settings).
- Milestone percentages must total 100 before an estimate can be finalised.
- Before designing a template, call get_template_guide and list_fonts.`

type Server struct {
	svc     *domain.Service
	log     *slog.Logger
	mu      sync.Mutex
	perUser map[string]*rate.Limiter
}

func New(svc *domain.Service, log *slog.Logger) *mcp.Server {
	s := &Server{svc: svc, log: log, perUser: map[string]*rate.Limiter{}}
	srv := mcp.NewServer(&mcp.Implementation{Name: serverName, Title: "Claviq Desk", Version: serverVersion},
		&mcp.ServerOptions{Instructions: instructions})
	s.register(srv)
	return srv
}

// Handler serves MCP over Streamable HTTP behind OAuth bearer-token auth.
func Handler(srv *mcp.Server, oauth *auth.OAuth) http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	return mcpauth.RequireBearerToken(oauth.VerifyAccessToken, &mcpauth.RequireBearerTokenOptions{
		ResourceMetadataURL: oauth.ResourceMetadataURL(),
		Scopes:              []string{auth.ScopeDesk},
	})(h)
}

// actor resolves the authenticated principal and applies a per-user rate limit.
func (s *Server) actor(ctx context.Context) (domain.Actor, error) {
	p, err := auth.PrincipalFromContext(ctx)
	if err != nil {
		return domain.Actor{}, errors.New("not authenticated")
	}
	s.mu.Lock()
	l, ok := s.perUser[p.UserID.String()]
	if !ok {
		l = rate.NewLimiter(rate.Every(time.Second/2), 60) // 120/min, burst 60
		s.perUser[p.UserID.String()] = l
	}
	s.mu.Unlock()
	if !l.Allow() {
		return domain.Actor{}, errors.New("rate limit exceeded; wait a moment and try again")
	}
	uid, cid := p.UserID, p.ClientID
	return domain.Actor{TenantID: p.TenantID, UserID: &uid, Via: "ai", ClientID: &cid, RequestID: reqctx.RequestID(ctx)}, nil
}

// toolErr converts domain errors into messages the AI can act on.
func (s *Server) toolErr(err error) error {
	var ve *domain.ValidationError
	var ce *domain.ConflictError
	var le *domain.LockedError
	var re *render.CompileError
	switch {
	case errors.As(err, &ve):
		return errors.New(ve.Message)
	case errors.As(err, &re):
		return fmt.Errorf("template error: %s", re.Message)
	case errors.As(err, &le):
		return errors.New(le.Message)
	case errors.As(err, &ce):
		return errors.New(ce.Message)
	case errors.Is(err, domain.ErrNotFound):
		return errors.New("not found (check the id; ids come from list/find tools)")
	default:
		s.log.Error("mcp tool failed", "error", err)
		return errors.New("internal error; try again")
	}
}

func boolPtr(b bool) *bool { return &b }

func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
}

func writes(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: boolPtr(false), OpenWorldHint: boolPtr(false)}
}

func destructive(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: boolPtr(true), OpenWorldHint: boolPtr(false)}
}

func trimLower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
