// Package auth issues and verifies HMAC-signed session tokens used by the
// WebSocket gateway. Tokens carry the userID, nickname, avatar and expiry,
// signed with HMAC-SHA256 over a base64url-encoded body.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DefaultTTL is the default token lifetime when not overridden.
const DefaultTTL = 7 * 24 * time.Hour

var (
	ErrEmptyToken     = errors.New("auth: empty token")
	ErrMalformedToken = errors.New("auth: malformed token")
	ErrBadSignature   = errors.New("auth: bad signature")
	ErrExpired        = errors.New("auth: token expired")
	ErrInvalidUserID  = errors.New("auth: invalid user id")
)

// Claims is the verified payload of a session token.
type Claims struct {
	UserID   string
	Nickname string
	Avatar   string
	Expires  time.Time
}

// Signer signs and verifies tokens using HMAC-SHA256.
type Signer struct {
	key []byte
	ttl time.Duration
	now func() time.Time
}

// NewSigner creates a Signer from any non-empty secret. Short or unevenly
// sized secrets are stretched via SHA-256 so the resulting MAC key is always
// 32 bytes. ttl <= 0 falls back to DefaultTTL.
func NewSigner(secret []byte, ttl time.Duration) *Signer {
	if len(secret) == 0 {
		secret = []byte("dev-insecure-default")
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	sum := sha256.Sum256(secret)
	return &Signer{
		key: append([]byte(nil), sum[:]...),
		ttl: ttl,
		now: time.Now,
	}
}

// TTL returns the configured token lifetime.
func (s *Signer) TTL() time.Duration { return s.ttl }

// Issue returns a token, the absolute expiry time, and any error.
func (s *Signer) Issue(userID, nickname, avatar string) (string, time.Time, error) {
	if userID == "" {
		return "", time.Time{}, ErrInvalidUserID
	}
	exp := s.now().Add(s.ttl)
	tok, err := s.sign(userID, nickname, avatar, exp)
	if err != nil {
		return "", time.Time{}, err
	}
	return tok, exp, nil
}

func (s *Signer) sign(userID, nickname, avatar string, exp time.Time) (string, error) {
	uid := base64.RawURLEncoding.EncodeToString([]byte(userID))
	nk := base64.RawURLEncoding.EncodeToString([]byte(nickname))
	av := base64.RawURLEncoding.EncodeToString([]byte(avatar))
	body := fmt.Sprintf("%s.%s.%s.%d", uid, nk, av, exp.Unix())
	mac := hmac.New(sha256.New, s.key)
	if _, err := mac.Write([]byte(body)); err != nil {
		return "", err
	}
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return body + "." + sig, nil
}

// Verify validates a token signature and expiry, returning the embedded Claims.
func (s *Signer) Verify(token string) (Claims, error) {
	if token == "" {
		return Claims{}, ErrEmptyToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 5 {
		return Claims{}, ErrMalformedToken
	}
	body := parts[0] + "." + parts[1] + "." + parts[2] + "." + parts[3]
	mac := hmac.New(sha256.New, s.key)
	if _, err := mac.Write([]byte(body)); err != nil {
		return Claims{}, err
	}
	expect := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[4])
	if err != nil {
		return Claims{}, ErrBadSignature
	}
	if !hmac.Equal(expect, got) {
		return Claims{}, ErrBadSignature
	}
	expUnix, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil {
		return Claims{}, ErrMalformedToken
	}
	exp := time.Unix(expUnix, 0)
	if s.now().After(exp) {
		return Claims{}, ErrExpired
	}
	uid, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrMalformedToken
	}
	nk, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrMalformedToken
	}
	av, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrMalformedToken
	}
	return Claims{
		UserID:   string(uid),
		Nickname: string(nk),
		Avatar:   string(av),
		Expires:  exp,
	}, nil
}

// SetClock overrides the time source. Test-only.
func (s *Signer) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

// GenerateSecret returns a new 32-byte hex-encoded random secret string,
// suitable for an environment variable seed.
func GenerateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// DecodeSecret accepts either a hex string or a raw passphrase and returns
// the bytes used for HMAC. Hex must be exactly 64 chars (32 bytes).
func DecodeSecret(s string) []byte {
	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil {
			return b
		}
	}
	return []byte(s)
}
