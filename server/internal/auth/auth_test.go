package auth

import (
	"strings"
	"testing"
	"time"
)

func newTestSigner(t *testing.T) *Signer {
	t.Helper()
	return NewSigner([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
}

func TestIssueVerifyRoundTrip(t *testing.T) {
	s := newTestSigner(t)
	tok, exp, err := s.Issue("u-123", "Alice|♠️", "🦁")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if time.Until(exp) <= 0 {
		t.Fatalf("expiry should be in the future")
	}
	c, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if c.UserID != "u-123" || c.Nickname != "Alice|♠️" || c.Avatar != "🦁" {
		t.Fatalf("unexpected claims: %+v", c)
	}
	// Expires inside Claims is stored as Unix seconds, so compare at that resolution.
	if c.Expires.Unix() != exp.Unix() {
		t.Fatalf("expires mismatch: got %d want %d", c.Expires.Unix(), exp.Unix())
	}
}

func TestVerifyRejectsTamper(t *testing.T) {
	s := newTestSigner(t)
	tok, _, err := s.Issue("u-1", "alice", "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 5 {
		t.Fatalf("expected 5 parts, got %d", len(parts))
	}
	// flip a byte in user id
	flipped := "AA" + parts[0][2:] + "." + parts[1] + "." + parts[2] + "." + parts[3] + "." + parts[4]
	if _, err := s.Verify(flipped); err == nil {
		t.Fatalf("expected error on tampered token")
	}
}

func TestVerifyRejectsForeignKey(t *testing.T) {
	a := NewSigner([]byte("0123456789abcdef0123456789abcdef"), time.Hour)
	b := NewSigner([]byte("feedfacefeedfacefeedfacefeedface"), time.Hour)
	tok, _, err := a.Issue("x", "y", "z")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := b.Verify(tok); err == nil {
		t.Fatalf("expected verify with foreign key to fail")
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	s := newTestSigner(t)
	now := time.Now()
	s.SetClock(func() time.Time { return now })
	tok, _, err := s.Issue("u", "n", "a")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	s.SetClock(func() time.Time { return now.Add(2 * time.Hour) })
	if _, err := s.Verify(tok); err != ErrExpired {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	s := newTestSigner(t)
	cases := []string{"", "a.b.c", ".....", "only-one-piece"}
	for _, c := range cases {
		if _, err := s.Verify(c); err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}

func TestNewSignerStretchesShortSecret(t *testing.T) {
	// Short keys should be accepted (stretched via SHA-256), but tokens issued
	// with a different short key must NOT verify.
	a := NewSigner([]byte("k"), time.Hour)
	b := NewSigner([]byte("other"), time.Hour)
	tok, _, err := a.Issue("u", "n", "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := a.Verify(tok); err != nil {
		t.Fatalf("self-verify: %v", err)
	}
	if _, err := b.Verify(tok); err == nil {
		t.Fatalf("expected cross-key verify to fail")
	}
}

func TestIssueRejectsEmptyUserID(t *testing.T) {
	s := newTestSigner(t)
	if _, _, err := s.Issue("", "x", ""); err == nil {
		t.Fatalf("expected error on empty userID")
	}
}

func TestDecodeSecret(t *testing.T) {
	hex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	b := DecodeSecret(hex)
	if len(b) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(b))
	}
	b = DecodeSecret("plain-passphrase")
	if string(b) != "plain-passphrase" {
		t.Fatalf("non-hex should pass through")
	}
}
