package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/auth"
)

// setupAuthServer starts a hub + http test server with WithAuth wired in.
func setupAuthServer(t *testing.T, signer *auth.Signer, cfg RoomConfig) (*httptest.Server, *Hub) {
	t.Helper()
	hub := NewHub(cfg)
	hub.SetAutoStartDelay(50 * time.Millisecond)
	srv := httptest.NewServer(HTTPHandler(hub, WithAuth(signer)))
	t.Cleanup(func() { srv.Close() })
	return srv, hub
}

func dialWithToken(t *testing.T, srv *httptest.Server, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	if token != "" {
		wsURL += "?token=" + token
	}
	return websocket.DefaultDialer.Dial(wsURL, nil)
}

// TestAuthRequired verifies the upgrade is rejected without a token.
func TestAuthRequired(t *testing.T) {
	signer := auth.NewSigner([]byte("k"), time.Hour)
	srv, _ := setupAuthServer(t, signer, RoomConfig{MaxSeats: 6, MinPlayers: 2, SmallBlind: 5, BigBlind: 10})

	_, resp, err := dialWithToken(t, srv, "")
	if err == nil {
		t.Fatalf("expected dial without token to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %v", resp)
	}
}

// TestAuthRejectsBadToken verifies a bogus token fails.
func TestAuthRejectsBadToken(t *testing.T) {
	signer := auth.NewSigner([]byte("k"), time.Hour)
	srv, _ := setupAuthServer(t, signer, RoomConfig{MaxSeats: 6, MinPlayers: 2, SmallBlind: 5, BigBlind: 10})

	_, resp, err := dialWithToken(t, srv, "not-a-valid-token")
	if err == nil {
		t.Fatalf("expected dial with bad token to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %v", resp)
	}
}

// TestAuthClaimsOverrideJoinUserID verifies that when the upgrade is
// authenticated the userId from claims wins over the join payload, so
// clients cannot impersonate someone else by changing the payload.
func TestAuthClaimsOverrideJoinUserID(t *testing.T) {
	signer := auth.NewSigner([]byte("k"), time.Hour)
	tok, _, err := signer.Issue("real-user", "Alice", "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	srv, hub := setupAuthServer(t, signer, RoomConfig{MaxSeats: 6, MinPlayers: 2, SmallBlind: 5, BigBlind: 10})

	c, _, err := dialWithToken(t, srv, tok)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()

	// Client lies about the userId: server should still treat the user as
	// real-user (the claims subject).
	d, _ := json.Marshal(JoinPayload{RoomID: "r1", UserID: "impostor", Nickname: "Mallory", BuyIn: 1000})
	if err := c.WriteJSON(ClientMessage{Type: CMsgJoin, Data: d}); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var joined JoinedPayload
	for time.Now().Before(deadline) {
		c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		var msg ServerMessage
		if err := c.ReadJSON(&msg); err != nil {
			continue
		}
		if msg.Type == SMsgJoined {
			b, _ := json.Marshal(msg.Data)
			_ = json.Unmarshal(b, &joined)
			break
		}
	}
	if joined.UserID != "real-user" {
		t.Fatalf("expected joined userId to come from claims (real-user), got %q", joined.UserID)
	}

	// And the room should have the authenticated userId, not impostor.
	room := hub.GetRoom("r1")
	if room == nil {
		t.Fatalf("room r1 missing")
	}
	found := false
	for _, m := range room.PlayerMetas() {
		if m.UserID == "real-user" {
			found = true
		}
		if m.UserID == "impostor" {
			t.Fatalf("impostor should not be seated")
		}
	}
	if !found {
		t.Fatalf("real-user not seated in room")
	}
}
