package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/ory/fosite"
	"gorm.io/gorm"

	"github.com/claviq/desk/backend/internal/models"
)

// Store implements fosite's storage interfaces on Postgres (oauth_clients,
// oauth_tokens). Only signature hashes are stored, never raw tokens.
type Store struct {
	db *gorm.DB

	mu        sync.Mutex
	clientIDs map[string]uuid.UUID // client_id -> oauth_clients.id
}

func NewStore(db *gorm.DB) *Store { return &Store{db: db, clientIDs: map[string]uuid.UUID{}} }

const (
	kindCode    = "authorization_code"
	kindAccess  = "access_token"
	kindRefresh = "refresh_token"
	kindPKCE    = "pkce"
)

type storedRequest struct {
	ID                string          `json:"id"`
	RequestedAt       time.Time       `json:"requested_at"`
	ClientID          string          `json:"client_id"`
	RequestedScope    []string        `json:"requested_scope"`
	GrantedScope      []string        `json:"granted_scope"`
	RequestedAudience []string        `json:"requested_audience"`
	GrantedAudience   []string        `json:"granted_audience"`
	Form              url.Values      `json:"form"`
	Session           json.RawMessage `json:"session"`
}

func hashSig(sig string) string {
	h := sha256.Sum256([]byte(sig))
	return hex.EncodeToString(h[:])
}

// ---------- ClientManager ----------

func (s *Store) GetClient(ctx context.Context, id string) (fosite.Client, error) {
	var c models.OAuthClient
	if err := s.db.WithContext(ctx).Where("client_id = ?", id).First(&c).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fosite.ErrNotFound
		}
		return nil, err
	}
	s.mu.Lock()
	s.clientIDs[c.ClientID] = c.ID
	s.mu.Unlock()
	dc := &fosite.DefaultClient{
		ID:            c.ClientID,
		RedirectURIs:  c.RedirectURIs.V,
		GrantTypes:    c.GrantTypes.V,
		ResponseTypes: c.ResponseTypes.V,
		Scopes:        c.Scopes.V,
		Public:        c.ClientSecretHash == nil,
	}
	if c.ClientSecretHash != nil {
		dc.Secret = []byte(*c.ClientSecretHash)
	}
	return dc, nil
}

// ClientUUID maps a client_id string to oauth_clients.id (for audit logs).
func (s *Store) ClientUUID(ctx context.Context, clientID string) (uuid.UUID, error) {
	s.mu.Lock()
	id, ok := s.clientIDs[clientID]
	s.mu.Unlock()
	if ok {
		return id, nil
	}
	if _, err := s.GetClient(ctx, clientID); err != nil {
		return uuid.Nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clientIDs[clientID], nil
}

// JWT client assertions are not supported; these satisfy the interface.
func (s *Store) ClientAssertionJWTValid(context.Context, string) error            { return nil }
func (s *Store) SetClientAssertionJWT(context.Context, string, time.Time) error { return nil }

// ---------- generic token rows ----------

func (s *Store) create(ctx context.Context, kind, signature string, req fosite.Requester) error {
	sessJSON, err := json.Marshal(req.GetSession())
	if err != nil {
		return err
	}
	sr := storedRequest{
		ID: req.GetID(), RequestedAt: req.GetRequestedAt(), ClientID: req.GetClient().GetID(),
		RequestedScope: req.GetRequestedScopes(), GrantedScope: req.GetGrantedScopes(),
		RequestedAudience: req.GetRequestedAudience(), GrantedAudience: req.GetGrantedAudience(),
		Form: req.GetRequestForm(), Session: sessJSON,
	}
	data, err := json.Marshal(sr)
	if err != nil {
		return err
	}
	tenantID, userID, err := sessionIDs(req.GetSession())
	if err != nil {
		return err
	}
	clientUUID, err := s.ClientUUID(ctx, sr.ClientID)
	if err != nil {
		return err
	}
	row := models.OAuthToken{
		Kind: kind, SignatureHash: hashSig(signature), RequestID: req.GetID(), ClientID: clientUUID,
		TenantID: tenantID, UserID: userID,
		Scopes: models.NewJSONB([]string(req.GetGrantedScopes())), GrantedAudience: models.NewJSONB([]string(req.GetGrantedAudience())),
		SessionData: data,
	}
	if exp := expiryFor(kind, req.GetSession()); !exp.IsZero() {
		row.ExpiresAt = &exp
	}
	return s.db.WithContext(ctx).Create(&row).Error
}

func expiryFor(kind string, sess fosite.Session) time.Time {
	if sess == nil {
		return time.Time{}
	}
	switch kind {
	case kindAccess:
		return sess.GetExpiresAt(fosite.AccessToken)
	case kindRefresh:
		return sess.GetExpiresAt(fosite.RefreshToken)
	case kindCode, kindPKCE:
		return sess.GetExpiresAt(fosite.AuthorizeCode)
	}
	return time.Time{}
}

func sessionIDs(sess fosite.Session) (uuid.UUID, uuid.UUID, error) {
	ds, ok := sess.(*fosite.DefaultSession)
	if !ok || ds.Extra == nil {
		return uuid.Nil, uuid.Nil, errors.New("oauth: session without tenant")
	}
	t, err1 := uuid.Parse(asString(ds.Extra["tenant_id"]))
	u, err2 := uuid.Parse(asString(ds.Extra["user_id"]))
	if err1 != nil || err2 != nil {
		return uuid.Nil, uuid.Nil, errors.New("oauth: session without tenant")
	}
	return t, u, nil
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func (s *Store) get(ctx context.Context, kind, signature string, session fosite.Session) (*fosite.Request, *models.OAuthToken, error) {
	var row models.OAuthToken
	err := s.db.WithContext(ctx).Where("kind = ? AND signature_hash = ?", kind, hashSig(signature)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, fosite.ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	var sr storedRequest
	if err := json.Unmarshal(row.SessionData, &sr); err != nil {
		return nil, nil, err
	}
	client, err := s.GetClient(ctx, sr.ClientID)
	if err != nil {
		return nil, nil, err
	}
	if session == nil {
		session = &fosite.DefaultSession{}
	}
	if err := json.Unmarshal(sr.Session, session); err != nil {
		return nil, nil, err
	}
	req := &fosite.Request{
		ID: sr.ID, RequestedAt: sr.RequestedAt, Client: client,
		RequestedScope: sr.RequestedScope, GrantedScope: sr.GrantedScope,
		RequestedAudience: sr.RequestedAudience, GrantedAudience: sr.GrantedAudience,
		Form: sr.Form, Session: session,
	}
	if req.Form == nil {
		req.Form = url.Values{}
	}
	return req, &row, nil
}

func (s *Store) delete(ctx context.Context, kind, signature string) error {
	return s.db.WithContext(ctx).Where("kind = ? AND signature_hash = ?", kind, hashSig(signature)).
		Delete(&models.OAuthToken{}).Error
}

// ---------- authorize codes ----------

func (s *Store) CreateAuthorizeCodeSession(ctx context.Context, code string, req fosite.Requester) error {
	return s.create(ctx, kindCode, code, req)
}

func (s *Store) GetAuthorizeCodeSession(ctx context.Context, code string, session fosite.Session) (fosite.Requester, error) {
	req, row, err := s.get(ctx, kindCode, code, session)
	if err != nil {
		return nil, err
	}
	if row.UsedAt != nil {
		return req, fosite.ErrInvalidatedAuthorizeCode
	}
	return req, nil
}

func (s *Store) InvalidateAuthorizeCodeSession(ctx context.Context, code string) error {
	return s.db.WithContext(ctx).Model(&models.OAuthToken{}).
		Where("kind = ? AND signature_hash = ?", kindCode, hashSig(code)).Update("used_at", time.Now()).Error
}

// ---------- access tokens ----------

func (s *Store) CreateAccessTokenSession(ctx context.Context, sig string, req fosite.Requester) error {
	return s.create(ctx, kindAccess, sig, req)
}

func (s *Store) GetAccessTokenSession(ctx context.Context, sig string, session fosite.Session) (fosite.Requester, error) {
	req, row, err := s.get(ctx, kindAccess, sig, session)
	if err != nil {
		return nil, err
	}
	if row.RevokedAt != nil {
		return nil, fosite.ErrNotFound
	}
	return req, nil
}

func (s *Store) DeleteAccessTokenSession(ctx context.Context, sig string) error {
	return s.delete(ctx, kindAccess, sig)
}

// ---------- refresh tokens ----------

func (s *Store) CreateRefreshTokenSession(ctx context.Context, sig string, _ string, req fosite.Requester) error {
	return s.create(ctx, kindRefresh, sig, req)
}

func (s *Store) GetRefreshTokenSession(ctx context.Context, sig string, session fosite.Session) (fosite.Requester, error) {
	req, row, err := s.get(ctx, kindRefresh, sig, session)
	if err != nil {
		return nil, err
	}
	if row.RevokedAt != nil {
		// Reuse of a rotated token: fosite revokes the whole grant chain.
		return req, fosite.ErrInactiveToken
	}
	return req, nil
}

func (s *Store) DeleteRefreshTokenSession(ctx context.Context, sig string) error {
	return s.delete(ctx, kindRefresh, sig)
}

func (s *Store) RotateRefreshToken(ctx context.Context, requestID string, _ string) error {
	if err := s.RevokeRefreshToken(ctx, requestID); err != nil {
		return err
	}
	return s.RevokeAccessToken(ctx, requestID)
}

func (s *Store) RevokeRefreshToken(ctx context.Context, requestID string) error {
	return s.db.WithContext(ctx).Model(&models.OAuthToken{}).
		Where("kind = ? AND request_id = ? AND revoked_at IS NULL", kindRefresh, requestID).
		Update("revoked_at", time.Now()).Error
}

func (s *Store) RevokeAccessToken(ctx context.Context, requestID string) error {
	return s.db.WithContext(ctx).Where("kind = ? AND request_id = ?", kindAccess, requestID).
		Delete(&models.OAuthToken{}).Error
}

// ---------- PKCE ----------

func (s *Store) CreatePKCERequestSession(ctx context.Context, sig string, req fosite.Requester) error {
	return s.create(ctx, kindPKCE, sig, req)
}

func (s *Store) GetPKCERequestSession(ctx context.Context, sig string, session fosite.Session) (fosite.Requester, error) {
	req, _, err := s.get(ctx, kindPKCE, sig, session)
	return req, err
}

func (s *Store) DeletePKCERequestSession(ctx context.Context, sig string) error {
	return s.delete(ctx, kindPKCE, sig)
}

// ---------- housekeeping ----------

// Cleanup removes expired tokens and used/expired codes. Run periodically.
func (s *Store) Cleanup(ctx context.Context) (int64, error) {
	res := s.db.WithContext(ctx).Where("expires_at < ?", time.Now().Add(-24*time.Hour)).Delete(&models.OAuthToken{})
	return res.RowsAffected, res.Error
}

// Connections lists the AI clients a user has active grants for.
type Connection struct {
	ClientID   string    `json:"client_id"`
	Name       string    `json:"name"`
	ConnectedAt time.Time `json:"connected_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func (s *Store) Connections(ctx context.Context, userID uuid.UUID) ([]Connection, error) {
	var out []Connection
	err := s.db.WithContext(ctx).Raw(`
		SELECT c.client_id, c.name, min(t.created_at) AS connected_at, c.last_used_at
		FROM oauth_tokens t JOIN oauth_clients c ON c.id = t.client_id
		WHERE t.user_id = ? AND t.kind = 'refresh_token' AND t.revoked_at IS NULL
		GROUP BY c.client_id, c.name, c.last_used_at ORDER BY connected_at`, userID).Scan(&out).Error
	return out, err
}

// Disconnect revokes every token a user granted to a client.
func (s *Store) Disconnect(ctx context.Context, userID uuid.UUID, clientID string) error {
	cid, err := s.ClientUUID(ctx, clientID)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Where("user_id = ? AND client_id = ?", userID, cid).Delete(&models.OAuthToken{}).Error
}

func (s *Store) touchClient(ctx context.Context, clientUUID uuid.UUID) {
	s.db.WithContext(ctx).Model(&models.OAuthClient{}).Where("id = ? AND (last_used_at IS NULL OR last_used_at < ?)",
		clientUUID, time.Now().Add(-10*time.Minute)).Update("last_used_at", time.Now())
}
