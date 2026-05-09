package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// dialClient connects to the test server and returns a websocket conn.
func dialClient(t *testing.T, server *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

func sendClient(t *testing.T, c *websocket.Conn, msgType ClientMsgType, payload any) {
	t.Helper()
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		raw = b
	}
	b, err := json.Marshal(ClientMessage{Type: msgType, Data: raw})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if err := c.WriteMessage(websocket.TextMessage, b); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readUntil reads server messages until predicate matches or deadline elapses.
// Returns the matching message.
func readUntil(t *testing.T, c *websocket.Conn, deadline time.Duration, match func(ServerMessage) bool) ServerMessage {
	t.Helper()
	end := time.Now().Add(deadline)
	for {
		left := time.Until(end)
		if left <= 0 {
			t.Fatalf("readUntil: timed out")
		}
		_ = c.SetReadDeadline(time.Now().Add(left))
		_, data, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var msg ServerMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if match(msg) {
			return msg
		}
	}
}

func decodeRoomState(t *testing.T, msg ServerMessage) RoomStateView {
	t.Helper()
	b, _ := json.Marshal(msg.Data)
	var view RoomStateView
	if err := json.Unmarshal(b, &view); err != nil {
		t.Fatalf("decode room state: %v", err)
	}
	return view
}

func TestTwoClientsPlayOneHand(t *testing.T) {
	hub := NewHub(RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	server := httptest.NewServer(HTTPHandler(hub))
	defer server.Close()

	a := dialClient(t, server)
	defer a.Close()
	b := dialClient(t, server)
	defer b.Close()

	// A joins
	sendClient(t, a, CMsgJoin, JoinPayload{
		RoomID: "room1", UserID: "alice", Nickname: "Alice", Avatar: "🐱", BuyIn: 1000,
	})
	readUntil(t, a, time.Second, func(m ServerMessage) bool { return m.Type == SMsgJoined })
	readUntil(t, a, time.Second, func(m ServerMessage) bool { return m.Type == SMsgRoomState })

	// B joins → triggers hand start
	sendClient(t, b, CMsgJoin, JoinPayload{
		RoomID: "room1", UserID: "bob", Nickname: "Bob", Avatar: "🐶", BuyIn: 1000,
	})
	readUntil(t, b, time.Second, func(m ServerMessage) bool { return m.Type == SMsgJoined })

	// Both should receive a room-state with stage=Preflop
	stateA := decodeRoomState(t, readUntil(t, a, 2*time.Second, func(m ServerMessage) bool {
		if m.Type != SMsgRoomState {
			return false
		}
		v := decodeRoomState(t, m)
		return v.Stage == "preflop"
	}))
	stateB := decodeRoomState(t, readUntil(t, b, 2*time.Second, func(m ServerMessage) bool {
		if m.Type != SMsgRoomState {
			return false
		}
		v := decodeRoomState(t, m)
		return v.Stage == "preflop"
	}))

	// Verify hole-card visibility: alice sees her own cards, NOT bob's
	if stateA.ViewerSeat < 0 {
		t.Errorf("alice viewerSeat=%d, want >=0", stateA.ViewerSeat)
	}
	for _, p := range stateA.Players {
		if p.UserID == "alice" {
			if len(p.HoleCards) != 2 {
				t.Errorf("alice should see her 2 hole cards, got %d", len(p.HoleCards))
			}
		} else {
			if len(p.HoleCards) != 0 {
				t.Errorf("alice should NOT see %s's hole cards, got %d", p.UserID, len(p.HoleCards))
			}
		}
	}
	for _, p := range stateB.Players {
		if p.UserID == "bob" {
			if len(p.HoleCards) != 2 {
				t.Errorf("bob should see his 2 hole cards, got %d", len(p.HoleCards))
			}
		} else if len(p.HoleCards) != 0 {
			t.Errorf("bob should NOT see %s's hole cards", p.UserID)
		}
	}

	// Heads-up: dealer/SB acts first preflop. Whoever has activeSeat acts.
	// Both A and B are seated; one of them is active.
	activeUserID := ""
	for _, p := range stateA.Players {
		if p.Seat == stateA.ActiveSeat {
			activeUserID = p.UserID
			break
		}
	}
	if activeUserID == "" {
		t.Fatalf("no active player found in state %+v", stateA)
	}

	// Active player folds → other wins uncontested
	activeConn := a
	otherConn := b
	if activeUserID == "bob" {
		activeConn, otherConn = b, a
	}
	sendClient(t, activeConn, CMsgAction, ActionPayload{
		RoomID: "room1", Type: "fold",
	})

	// One side gets HandComplete event
	readUntil(t, otherConn, 2*time.Second, func(m ServerMessage) bool {
		if m.Type != SMsgGameEvent {
			return false
		}
		b, _ := json.Marshal(m.Data)
		var ev GameEventPayload
		_ = json.Unmarshal(b, &ev)
		return ev.Type == "hand-complete"
	})
}

func TestPongOnPing(t *testing.T) {
	hub := NewHub(RoomConfig{})
	server := httptest.NewServer(HTTPHandler(hub))
	defer server.Close()

	c := dialClient(t, server)
	defer c.Close()

	sendClient(t, c, CMsgPing, nil)
	readUntil(t, c, time.Second, func(m ServerMessage) bool { return m.Type == SMsgPong })
}

func TestUnknownMessageReturnsError(t *testing.T) {
	hub := NewHub(RoomConfig{})
	server := httptest.NewServer(HTTPHandler(hub))
	defer server.Close()

	c := dialClient(t, server)
	defer c.Close()

	sendClient(t, c, ClientMsgType("gibberish"), nil)
	readUntil(t, c, time.Second, func(m ServerMessage) bool { return m.Type == SMsgError })
}

func TestActionWithoutJoinReturnsError(t *testing.T) {
	hub := NewHub(RoomConfig{})
	server := httptest.NewServer(HTTPHandler(hub))
	defer server.Close()

	c := dialClient(t, server)
	defer c.Close()

	sendClient(t, c, CMsgAction, ActionPayload{Type: "fold"})
	msg := readUntil(t, c, time.Second, func(m ServerMessage) bool { return m.Type == SMsgError })
	b, _ := json.Marshal(msg.Data)
	var ep ErrorPayload
	_ = json.Unmarshal(b, &ep)
	if ep.Code != "not-joined" {
		t.Errorf("expected not-joined code, got %s", ep.Code)
	}
}

func TestAutoRestartNextHand(t *testing.T) {
	hub := NewHub(RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	hub.SetAutoStartDelay(150 * time.Millisecond)
	server := httptest.NewServer(HTTPHandler(hub))
	defer server.Close()

	a := dialClient(t, server)
	defer a.Close()
	b := dialClient(t, server)
	defer b.Close()

	sendClient(t, a, CMsgJoin, JoinPayload{
		RoomID: "auto1", UserID: "alice", Nickname: "Alice", Avatar: "🐱", BuyIn: 1000,
	})
	sendClient(t, b, CMsgJoin, JoinPayload{
		RoomID: "auto1", UserID: "bob", Nickname: "Bob", Avatar: "🐶", BuyIn: 1000,
	})

	// Wait for first preflop
	stateA := decodeRoomState(t, readUntil(t, a, 2*time.Second, func(m ServerMessage) bool {
		if m.Type != SMsgRoomState {
			return false
		}
		v := decodeRoomState(t, m)
		return v.Stage == "preflop"
	}))

	// Find active seat → that user folds → hand ends uncontested
	activeUserID := ""
	for _, p := range stateA.Players {
		if p.Seat == stateA.ActiveSeat {
			activeUserID = p.UserID
			break
		}
	}
	if activeUserID == "" {
		t.Fatalf("no active player in state")
	}
	activeConn := a
	if activeUserID == "bob" {
		activeConn = b
	}
	sendClient(t, activeConn, CMsgAction, ActionPayload{RoomID: "auto1", Type: "fold"})

	// Wait for hand-complete event on either side
	readUntil(t, a, 2*time.Second, func(m ServerMessage) bool {
		if m.Type != SMsgGameEvent {
			return false
		}
		b, _ := json.Marshal(m.Data)
		var ev GameEventPayload
		_ = json.Unmarshal(b, &ev)
		return ev.Type == "hand-complete"
	})

	// Within autoStartDelay + buffer, a NEW preflop should arrive with stage=preflop
	// and freshly dealt hole cards. Stage transition is the marker.
	gotNewHand := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_ = a.SetReadDeadline(time.Now().Add(time.Until(deadline)))
		_, data, err := a.ReadMessage()
		if err != nil {
			break
		}
		var msg ServerMessage
		_ = json.Unmarshal(data, &msg)
		if msg.Type != SMsgRoomState {
			continue
		}
		v := decodeRoomState(t, msg)
		if v.Stage == "preflop" {
			// Verify this is a NEW hand by checking the pot was reset and hole cards fresh
			if v.Pot >= 50 && v.Pot <= 200 {
				gotNewHand = true
				break
			}
		}
	}
	if !gotNewHand {
		t.Errorf("expected auto-restart to deliver a fresh preflop room-state within 2s")
	}
}

func TestRebuyTriggersAutoStart(t *testing.T) {
	// Set up a 2-player room where alice has 0 chips (out of game) and bob has 2000.
	// Alice sends a rebuy → next hand should start automatically.
	hub := NewHub(RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	hub.SetAutoStartDelay(50 * time.Millisecond)
	server := httptest.NewServer(HTTPHandler(hub))
	defer server.Close()

	a := dialClient(t, server)
	defer a.Close()
	b := dialClient(t, server)
	defer b.Close()

	sendClient(t, a, CMsgJoin, JoinPayload{
		RoomID: "rb1", UserID: "alice", Nickname: "Alice", Avatar: "🐱", BuyIn: 0,
	})
	sendClient(t, b, CMsgJoin, JoinPayload{
		RoomID: "rb1", UserID: "bob", Nickname: "Bob", Avatar: "🐶", BuyIn: 2000,
	})

	// Wait for at least one waiting room-state to confirm the room is paused
	// (alice has 0 chips → can't auto-start)
	readUntil(t, a, 1*time.Second, func(m ServerMessage) bool {
		if m.Type != SMsgRoomState {
			return false
		}
		v := decodeRoomState(t, m)
		return v.Stage == "waiting"
	})

	// Alice sends rebuy → server should StartHand and broadcast preflop
	sendClient(t, a, CMsgRebuy, RebuyPayload{RoomID: "rb1", Amount: 1000})

	// Within autoStartDelay + buffer, a fresh preflop room-state should arrive
	readUntil(t, a, 2*time.Second, func(m ServerMessage) bool {
		if m.Type != SMsgRoomState {
			return false
		}
		v := decodeRoomState(t, m)
		return v.Stage == "preflop"
	})
}

func TestRebuyMidHandRejected(t *testing.T) {
	hub := NewHub(RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	hub.SetAutoStartDelay(time.Hour) // disable auto-start so hand stays open
	server := httptest.NewServer(HTTPHandler(hub))
	defer server.Close()

	a := dialClient(t, server)
	defer a.Close()
	b := dialClient(t, server)
	defer b.Close()

	sendClient(t, a, CMsgJoin, JoinPayload{
		RoomID: "rb2", UserID: "alice", Nickname: "Alice", Avatar: "🐱", BuyIn: 1000,
	})
	sendClient(t, b, CMsgJoin, JoinPayload{
		RoomID: "rb2", UserID: "bob", Nickname: "Bob", Avatar: "🐶", BuyIn: 1000,
	})

	// Wait for preflop on alice
	readUntil(t, a, 2*time.Second, func(m ServerMessage) bool {
		if m.Type != SMsgRoomState {
			return false
		}
		v := decodeRoomState(t, m)
		return v.Stage == "preflop"
	})

	// Try rebuy mid-hand → should get error
	sendClient(t, a, CMsgRebuy, RebuyPayload{RoomID: "rb2", Amount: 500})
	msg := readUntil(t, a, time.Second, func(m ServerMessage) bool { return m.Type == SMsgError })
	bb, _ := json.Marshal(msg.Data)
	var ep ErrorPayload
	_ = json.Unmarshal(bb, &ep)
	if ep.Code != "rebuy-failed" {
		t.Errorf("expected rebuy-failed code, got %s", ep.Code)
	}
}

func TestChatBroadcast(t *testing.T) {
	hub := NewHub(RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	hub.SetAutoStartDelay(time.Hour) // pin in waiting/preflop, irrelevant here
	server := httptest.NewServer(HTTPHandler(hub))
	defer server.Close()

	a := dialClient(t, server)
	defer a.Close()
	b := dialClient(t, server)
	defer b.Close()

	sendClient(t, a, CMsgJoin, JoinPayload{RoomID: "chat1", UserID: "alice", Nickname: "A", Avatar: "🐱", BuyIn: 1000})
	sendClient(t, b, CMsgJoin, JoinPayload{RoomID: "chat1", UserID: "bob", Nickname: "B", Avatar: "🐶", BuyIn: 1000})

	// Drain initial joined/room-state messages (using readUntil filter so we don't get stuck on stale deadlines)
	readUntil(t, b, time.Second, func(m ServerMessage) bool { return m.Type == SMsgJoined })

	// Alice sends a chat emoji
	sendClient(t, a, CMsgChat, ChatPayload{RoomID: "chat1", Emoji: "👍"})

	// Bob should receive a chat message
	msg := readUntil(t, b, 2*time.Second, func(m ServerMessage) bool { return m.Type == SMsgChat })
	bb, _ := json.Marshal(msg.Data)
	var payload struct {
		UserID string `json:"userId"`
		Emoji  string `json:"emoji"`
	}
	_ = json.Unmarshal(bb, &payload)
	if payload.UserID != "alice" || payload.Emoji != "👍" {
		t.Errorf("got payload=%+v", payload)
	}
}

func TestChatTooLongTruncated(t *testing.T) {
	hub := NewHub(RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	hub.SetAutoStartDelay(time.Hour)
	server := httptest.NewServer(HTTPHandler(hub))
	defer server.Close()

	a := dialClient(t, server)
	defer a.Close()
	b := dialClient(t, server)
	defer b.Close()

	sendClient(t, a, CMsgJoin, JoinPayload{RoomID: "chat2", UserID: "alice", Nickname: "A", Avatar: "🐱", BuyIn: 1000})
	sendClient(t, b, CMsgJoin, JoinPayload{RoomID: "chat2", UserID: "bob", Nickname: "B", Avatar: "🐶", BuyIn: 1000})
	readUntil(t, b, time.Second, func(m ServerMessage) bool { return m.Type == SMsgJoined })

	// Send a 20-rune message — should be truncated to 8
	sendClient(t, a, CMsgChat, ChatPayload{RoomID: "chat2", Emoji: "12345678901234567890"})

	msg := readUntil(t, b, 2*time.Second, func(m ServerMessage) bool { return m.Type == SMsgChat })
	bb, _ := json.Marshal(msg.Data)
	var payload struct {
		Emoji string `json:"emoji"`
	}
	_ = json.Unmarshal(bb, &payload)
	if len([]rune(payload.Emoji)) != 8 {
		t.Errorf("emoji should be truncated to 8 runes, got %d: %q", len([]rune(payload.Emoji)), payload.Emoji)
	}
}

func TestSoftLeaveReconnectPreservesChips(t *testing.T) {
	hub := NewHub(RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	hub.SetDisconnectGrace(500 * time.Millisecond)
	hub.SetAutoStartDelay(time.Hour) // pin to waiting/preflop, not relevant
	server := httptest.NewServer(HTTPHandler(hub))
	defer server.Close()

	a := dialClient(t, server)
	sendClient(t, a, CMsgJoin, JoinPayload{
		RoomID: "soft1", UserID: "alice", Nickname: "Alice", Avatar: "🐱", BuyIn: 1234,
	})
	readUntil(t, a, time.Second, func(m ServerMessage) bool { return m.Type == SMsgJoined })
	a.Close() // drop conn

	// Within the grace window the room should still hold alice's meta.
	time.Sleep(100 * time.Millisecond)
	room := hub.GetRoom("soft1")
	if room == nil {
		t.Fatalf("room missing after disconnect")
	}
	stillThere := false
	for _, m := range room.PlayerMetas() {
		if m.UserID == "alice" && m.BuyIn == 1234 {
			stillThere = true
		}
	}
	if !stillThere {
		t.Errorf("alice should still be seated within grace window")
	}

	// Reconnect with same uid; grace timer must be cancelled and chips preserved.
	a2 := dialClient(t, server)
	defer a2.Close()
	sendClient(t, a2, CMsgJoin, JoinPayload{
		RoomID: "soft1", UserID: "alice", Nickname: "Alice", Avatar: "🐱", BuyIn: 9999, // BuyIn here is ignored on rejoin
	})
	readUntil(t, a2, time.Second, func(m ServerMessage) bool { return m.Type == SMsgJoined })

	// Wait past the original grace window. The cancelled timer must NOT purge.
	time.Sleep(700 * time.Millisecond)
	stillThere = false
	for _, m := range room.PlayerMetas() {
		if m.UserID == "alice" {
			stillThere = true
		}
	}
	if !stillThere {
		t.Errorf("alice should still be seated after reconnect; cancel-timer regressed")
	}
}

func TestSoftLeavePurgeAfterGrace(t *testing.T) {
	hub := NewHub(RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	hub.SetDisconnectGrace(150 * time.Millisecond)
	hub.SetAutoStartDelay(time.Hour)
	server := httptest.NewServer(HTTPHandler(hub))
	defer server.Close()

	a := dialClient(t, server)
	sendClient(t, a, CMsgJoin, JoinPayload{
		RoomID: "soft2", UserID: "alice", Nickname: "Alice", Avatar: "🐱", BuyIn: 1000,
	})
	readUntil(t, a, time.Second, func(m ServerMessage) bool { return m.Type == SMsgJoined })
	a.Close()

	// Wait past grace window with no reconnect.
	time.Sleep(400 * time.Millisecond)
	room := hub.GetRoom("soft2")
	if room == nil {
		t.Fatalf("room missing")
	}
	for _, m := range room.PlayerMetas() {
		if m.UserID == "alice" {
			t.Errorf("alice should have been purged after grace window")
		}
	}
}

func TestBotPlaysAgainstHuman(t *testing.T) {
	hub := NewHub(RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	hub.SetAutoStartDelay(time.Hour)
	server := httptest.NewServer(HTTPHandler(hub))
	defer server.Close()

	// Pre-create a room and seat one bot.
	room, err := hub.CreateRoom("airoom", RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	if err != nil {
		t.Fatal(err)
	}
	bot := NewBotMeta()
	bot.BuyIn = 1000
	if _, err := room.AddBot(bot); err != nil {
		t.Fatal(err)
	}

	// Human joins; this should auto-start a hand. The bot should reply on its own.
	c := dialClient(t, server)
	defer c.Close()
	sendClient(t, c, CMsgJoin, JoinPayload{
		RoomID: "airoom", UserID: "alice", Nickname: "Alice", Avatar: "🐱", BuyIn: 1000,
	})

	deadline := time.Now().Add(4 * time.Second)
	gotPreflop := false
	gotBotAction := false
	for time.Now().Before(deadline) && !(gotPreflop && gotBotAction) {
		_ = c.SetReadDeadline(time.Now().Add(time.Until(deadline)))
		_, data, err := c.ReadMessage()
		if err != nil {
			break
		}
		var msg ServerMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case SMsgRoomState:
			v := decodeRoomState(t, msg)
			if v.Stage == "preflop" {
				gotPreflop = true
			}
		case SMsgGameEvent:
			b, _ := json.Marshal(msg.Data)
			var ev GameEventPayload
			_ = json.Unmarshal(b, &ev)
			if ev.Type == "action" {
				if seat, ok := ev.Data["seat"].(float64); ok && int(seat) == bot.Seat {
					gotBotAction = true
				}
			}
		}
	}
	if !gotPreflop {
		t.Errorf("did not receive preflop room-state")
	}
	if !gotBotAction {
		t.Errorf("bot did not produce an action within deadline")
	}
}

func TestHTTPHealthEndpoint(t *testing.T) {
	hub := NewHub(RoomConfig{})
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	})
	mux.Handle("/ws", HTTPHandler(hub))
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status=%d, want 200", resp.StatusCode)
	}
}
