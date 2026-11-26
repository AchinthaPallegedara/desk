// Package signing derives purpose-specific keys from APP_SECRET and issues
// compact HMAC-signed, expiring tokens (preview links, session cookies).
package signing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalid = errors.New("signing: invalid token")
	ErrExpired = errors.New("signing: token expired")
)

// DeriveKey returns HMAC-SHA256(root, purpose): independent keys per use.
func DeriveKey(root, purpose string) []byte {
	m := hmac.New(sha256.New, []byte(root))
	m.Write([]byte(purpose))
	return m.Sum(nil)
}

type Signer struct{ key []byte }

func NewSigner(root, purpose string) *Signer { return &Signer{key: DeriveKey(root, purpose)} }

type envelope[T any] struct {
	Exp int64 `json:"e"`
	V   T     `json:"v"`
}

var b64 = base64.RawURLEncoding

// Sign returns "<payload>.<mac>", both base64url.
func Sign[T any](s *Signer, v T, ttl time.Duration) (string, error) {
	payload, err := json.Marshal(envelope[T]{Exp: time.Now().Add(ttl).Unix(), V: v})
	if err != nil {
		return "", err
	}
	p := b64.EncodeToString(payload)
	return p + "." + b64.EncodeToString(s.mac(p)), nil
}

// Verify checks the MAC and expiry and decodes the value.
func Verify[T any](s *Signer, token string) (T, error) {
	var zero T
	p, sig, ok := strings.Cut(token, ".")
	if !ok {
		return zero, ErrInvalid
	}
	got, err := b64.DecodeString(sig)
	if err != nil || !hmac.Equal(got, s.mac(p)) {
		return zero, ErrInvalid
	}
	raw, err := b64.DecodeString(p)
	if err != nil {
		return zero, ErrInvalid
	}
	var env envelope[T]
	if err := json.Unmarshal(raw, &env); err != nil {
		return zero, ErrInvalid
	}
	if time.Now().Unix() > env.Exp {
		return zero, ErrExpired
	}
	return env.V, nil
}

func (s *Signer) mac(p string) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(p))
	return m.Sum(nil)
}
