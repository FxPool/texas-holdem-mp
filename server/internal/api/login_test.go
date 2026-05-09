package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/auth"
)

func postJSON(t *testing.T, h http.Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginDevModeIssuesToken(t *testing.T) {
	signer := auth.NewSigner([]byte("secret-secret-secret-secret"), time.Hour)
	h := LoginHandler(signer, WxConfig{})

	rec := postJSON(t, h, LoginRequest{
		UserID:   "dev-uid-abcd",
		Nickname: "Alice",
		Avatar:   "🦁",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp LoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.UserID != "dev-uid-abcd" || resp.Nickname != "Alice" || resp.Avatar != "🦁" {
		t.Fatalf("unexpected resp: %+v", resp)
	}
	c, err := signer.Verify(resp.Token)
	if err != nil {
		t.Fatalf("verify minted token: %v", err)
	}
	if c.UserID != "dev-uid-abcd" {
		t.Fatalf("token uid=%q want dev-uid-abcd", c.UserID)
	}
}

func TestLoginDevModeRequiresUserID(t *testing.T) {
	signer := auth.NewSigner([]byte("k"), time.Hour)
	h := LoginHandler(signer, WxConfig{})
	rec := postJSON(t, h, LoginRequest{Nickname: "Alice"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestLoginRejectsGet(t *testing.T) {
	signer := auth.NewSigner([]byte("k"), time.Hour)
	h := LoginHandler(signer, WxConfig{})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/login", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestLoginProdModeWXFlow(t *testing.T) {
	// Stand up a fake wx code2session endpoint.
	wxFake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("js_code") != "valid-code" {
			_, _ = w.Write([]byte(`{"errcode":40029,"errmsg":"invalid code"}`))
			return
		}
		_, _ = w.Write([]byte(`{"openid":"openid-from-wx","session_key":"key"}`))
	}))
	defer wxFake.Close()

	signer := auth.NewSigner([]byte("k"), time.Hour)
	h := LoginHandler(signer, WxConfig{
		AppID:     "wxFakeID",
		AppSecret: "fake-secret",
		Endpoint:  wxFake.URL,
	})

	t.Run("valid-code", func(t *testing.T) {
		rec := postJSON(t, h, LoginRequest{Code: "valid-code", Nickname: "Bob", Avatar: "🐶"})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var resp LoginResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.UserID != "wx:openid-from-wx" {
			t.Fatalf("uid=%q want wx:openid-from-wx", resp.UserID)
		}
	})

	t.Run("invalid-code", func(t *testing.T) {
		rec := postJSON(t, h, LoginRequest{Code: "bad-code"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d", rec.Code)
		}
	})
}
