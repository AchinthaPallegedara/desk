package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/claviq/desk/backend/internal/models"
	"github.com/claviq/desk/backend/internal/signing"
)

const (
	SessionCookie = "cd_session"
	sessionTTL    = 14 * 24 * time.Hour
)

var ErrInvalidCredentials = errors.New("invalid email or password")

type Session struct {
	UserID   uuid.UUID `json:"u"`
	TenantID uuid.UUID `json:"t"`
	// PasswordStamp invalidates sessions when the password changes.
	Stamp string `json:"s"`
}

type Sessions struct {
	db     *gorm.DB
	signer *signing.Signer
	secure bool
}

func NewSessions(db *gorm.DB, appSecret string, secureCookies bool) *Sessions {
	return &Sessions{db: db, signer: signing.NewSigner(appSecret, "web-session/v1"), secure: secureCookies}
}

func stamp(hash string) string {
	if len(hash) < 12 {
		return hash
	}
	return hash[len(hash)-12:]
}

// Login verifies credentials and returns the user.
func (s *Sessions) Login(ctx context.Context, email, password string) (*models.User, error) {
	var u models.User
	err := s.db.WithContext(ctx).Where("email = ?", strings.ToLower(strings.TrimSpace(email))).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		VerifyPassword(dummyHash, password)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if !VerifyPassword(u.PasswordHash, password) {
		return nil, ErrInvalidCredentials
	}
	now := time.Now()
	s.db.WithContext(ctx).Model(&u).Update("last_login_at", now)
	return &u, nil
}

func (s *Sessions) Issue(w http.ResponseWriter, u *models.User) error {
	tok, err := signing.Sign(s.signer, Session{UserID: u.ID, TenantID: u.TenantID, Stamp: stamp(u.PasswordHash)}, sessionTTL)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: tok, Path: "/", HttpOnly: true, Secure: s.secure,
		SameSite: http.SameSiteLaxMode, MaxAge: int(sessionTTL.Seconds()),
	})
	return nil
}

func (s *Sessions) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", HttpOnly: true, Secure: s.secure,
		SameSite: http.SameSiteLaxMode, MaxAge: -1})
}

// FromRequest validates the session cookie and that the user still exists
// with the same password.
func (s *Sessions) FromRequest(r *http.Request) (*models.User, error) {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return nil, err
	}
	sess, err := signing.Verify[Session](s.signer, c.Value)
	if err != nil {
		return nil, err
	}
	var u models.User
	if err := s.db.WithContext(r.Context()).First(&u, "id = ? AND tenant_id = ?", sess.UserID, sess.TenantID).Error; err != nil {
		return nil, err
	}
	if stamp(u.PasswordHash) != sess.Stamp {
		return nil, errors.New("session revoked")
	}
	return &u, nil
}

// ChangePassword re-hashes the password (which invalidates other sessions).
func (s *Sessions) ChangePassword(ctx context.Context, userID uuid.UUID, current, next string) (*models.User, error) {
	var u models.User
	if err := s.db.WithContext(ctx).First(&u, "id = ?", userID).Error; err != nil {
		return nil, err
	}
	if !VerifyPassword(u.PasswordHash, current) {
		return nil, ErrInvalidCredentials
	}
	h, err := HashPassword(next)
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&u).Update("password_hash", h).Error; err != nil {
		return nil, err
	}
	u.PasswordHash = h
	return &u, nil
}
