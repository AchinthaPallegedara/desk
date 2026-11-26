// Package domain is the single business-logic layer. The REST API and the MCP
// tools both call this package; neither contains business rules of its own.
//
// Every method takes an Actor (who is acting, for which tenant, through which
// channel) and runs its database work inside database.WithTenant so Postgres
// RLS confines it to that tenant.
package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/claviq/desk/backend/internal/database"
	"github.com/claviq/desk/backend/internal/models"
	"github.com/claviq/desk/backend/internal/render"
	"github.com/claviq/desk/backend/internal/signing"
	"github.com/claviq/desk/backend/internal/storage"
)

// Actor identifies who performs an action. Via is "web", "ai" or "system".
type Actor struct {
	TenantID  uuid.UUID
	UserID    *uuid.UUID
	Via       string
	ClientID  *uuid.UUID // OAuth client when Via == "ai"
	RequestID string
}

func SystemActor(tenantID uuid.UUID) Actor { return Actor{TenantID: tenantID, Via: "system"} }

type Service struct {
	db        *gorm.DB
	renderer  *render.Renderer
	store     storage.Storage
	fonts     *FontLibrary
	links     *signing.Signer
	publicURL string
	log       *slog.Logger
	previews  *previewCache
}

type Options struct {
	DB        *gorm.DB
	Renderer  *render.Renderer
	Storage   storage.Storage
	DataDir   string
	AppSecret string
	PublicURL string
	Logger    *slog.Logger
}

func New(o Options) *Service {
	return &Service{
		db:        o.DB,
		renderer:  o.Renderer,
		store:     o.Storage,
		fonts:     NewFontLibrary(o.DataDir, o.Storage),
		links:     signing.NewSigner(o.AppSecret, "preview-links/v1"),
		publicURL: o.PublicURL,
		log:       o.Logger,
		previews:  newPreviewCache(64, 10*time.Minute),
	}
}

// Fonts exposes the font library (startup sync of bundled fonts).
func (s *Service) Fonts() *FontLibrary { return s.fonts }

func (s *Service) tx(ctx context.Context, a Actor, fn func(tx *gorm.DB) error) error {
	return mapError(database.WithTenant(ctx, s.db, a.TenantID, fn))
}

// ---------- errors ----------

var (
	ErrNotFound = errors.New("not found")
	ErrLocked   = errors.New("document is locked")
)

// ValidationError is a problem with the caller's input; safe to show users and AI.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// ConflictError is a uniqueness or state conflict.
type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }

// LockedError wraps ErrLocked with a human message.
type LockedError struct{ Message string }

func (e *LockedError) Error() string { return e.Message }
func (e *LockedError) Unwrap() error { return ErrLocked }

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch pg.Code {
		case "CQ001":
			return &LockedError{Message: pg.Message}
		case "23505":
			return &ConflictError{Message: uniqueMessage(pg)}
		case "23503":
			return invalid("a referenced record does not exist (%s)", pg.ConstraintName)
		case "23514":
			if pg.ConstraintName == "" {
				return invalid("%s", pg.Message)
			}
			return invalid("value not allowed (%s)", pg.ConstraintName)
		case "22P02", "22007", "22008", "22003":
			return invalid("%s", pg.Message)
		}
	}
	return err
}

func uniqueMessage(pg *pgconn.PgError) string {
	switch {
	case strings.Contains(pg.ConstraintName, "one_default"):
		return "another record is already the default"
	case strings.Contains(pg.ConstraintName, "templates_name"):
		return "a template with this name already exists"
	case strings.Contains(pg.ConstraintName, "tax_rates_tenant_id_name"):
		return "a tax rate with this name already exists"
	case strings.Contains(pg.ConstraintName, "previous_revision"):
		return "a revision of this document already exists"
	case strings.Contains(pg.ConstraintName, "number"):
		return "this document number is already used; check the number sequence settings"
	}
	return "duplicate value (" + pg.ConstraintName + ")"
}

// ---------- audit ----------

func (s *Service) audit(tx *gorm.DB, a Actor, action, entity string, entityID any, changes any) error {
	var idStr *string
	if entityID != nil {
		v := fmt.Sprint(entityID)
		idStr = &v
	}
	var ch *models.JSONB[any]
	if changes != nil {
		// Round-trip through JSON so the stored value is plain data.
		b, err := json.Marshal(changes)
		if err != nil {
			return err
		}
		var v any
		_ = json.Unmarshal(b, &v)
		j := models.NewJSONB(v)
		ch = &j
	}
	var reqID *string
	if a.RequestID != "" {
		reqID = &a.RequestID
	}
	via := a.Via
	if via == "" {
		via = "system"
	}
	return tx.Create(&models.AuditLog{
		TenantID:      a.TenantID,
		UserID:        a.UserID,
		Via:           via,
		OAuthClientID: a.ClientID,
		RequestID:     reqID,
		Action:        action,
		Entity:        entity,
		EntityID:      idStr,
		Changes:       ch,
	}).Error
}

// ListAudit returns recent audit entries, optionally for one entity.
func (s *Service) ListAudit(ctx context.Context, a Actor, entity string, entityID string, limit int) ([]models.AuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []models.AuditLog
	err := s.tx(ctx, a, func(tx *gorm.DB) error {
		q := tx.Order("created_at DESC").Limit(limit)
		if entity != "" {
			q = q.Where("entity = ?", entity)
		}
		if entityID != "" {
			q = q.Where("entity_id = ?", entityID)
		}
		return q.Find(&out).Error
	})
	return out, err
}

// ---------- small helpers ----------

func trimPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
