package ws

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/game"
)

// DefaultAutoStartDelay is how long the hub waits after a hand completes
// before automatically starting the next one. Override in tests with
// Hub.SetAutoStartDelay.
const DefaultAutoStartDelay = 6 * time.Second

// DefaultDisconnectGrace is how long a disconnected player keeps their seat
// and chips before being purged. They're folded mid-hand immediately so the
// table doesn't stall, but rejoining within this window preserves their stack.
const DefaultDisconnectGrace = 60 * time.Second

// Hub is the central registry of rooms. It owns room creation/lookup and
// dispatches per-connection client messages into the appropriate room.
//
// Concurrency: hub-level mutations (room create/list) take h.mu; per-room
// state lives inside each Room with its own mutex.
type Hub struct {
	mu         sync.RWMutex
	rooms      map[string]*Room
	defaultCfg RoomConfig

	// Auto-restart machinery: one timer per room. Restarts the next hand
	// after autoStartDelay once the previous hand reaches HandComplete.
	timerMu        sync.Mutex
	handTimers     map[string]*time.Timer
	autoStartDelay time.Duration

	// Per-user rate limiter. Caps mutating client messages at ~5/s burst
	// 2/s sustained. Defends against runaway loops or hostile clients.
	limiter *rateLimiter

	// disconnectGrace is the soft-leave window before a dropped connection's
	// PlayerMeta is purged.
	disconnectGrace time.Duration

	// store, when set, persists room metadata + user stats to disk. nil =
	// in-memory only.
	store *SnapshotStore
}

func NewHub(defaultCfg RoomConfig) *Hub {
	if defaultCfg.SmallBlind == 0 {
		defaultCfg.SmallBlind = 50
	}
	if defaultCfg.BigBlind == 0 {
		defaultCfg.BigBlind = 100
	}
	return &Hub{
		rooms:           map[string]*Room{},
		defaultCfg:      defaultCfg,
		handTimers:      map[string]*time.Timer{},
		autoStartDelay:  DefaultAutoStartDelay,
		limiter:         newRateLimiter(5, 2),
		disconnectGrace: DefaultDisconnectGrace,
	}
}

// SetDisconnectGrace overrides the grace window. Mainly used in tests.
func (h *Hub) SetDisconnectGrace(d time.Duration) {
	h.disconnectGrace = d
}

// rateAllow gates mutating messages by user identity. Connections that
// haven't joined yet are keyed by the conn pointer to still get a quota.
func (h *Hub) rateAllow(c *Conn) bool {
	key := c.UserID
	if key == "" {
		key = "anon:" + fmt.Sprintf("%p", c)
	}
	return h.limiter.Allow(key)
}

// SetAutoStartDelay overrides the auto-restart wait. Mainly used in tests.
func (h *Hub) SetAutoStartDelay(d time.Duration) {
	h.timerMu.Lock()
	defer h.timerMu.Unlock()
	h.autoStartDelay = d
}

// GetOrCreateRoom returns an existing room or creates one with the default config.
func (h *Hub) GetOrCreateRoom(id string) *Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r, ok := h.rooms[id]; ok {
		return r
	}
	r := NewRoom(id, h.defaultCfg)
	h.rooms[id] = r
	return r
}

// CreateRoom builds a room with explicit config. Blanks fall through to the
// hub's default config. Returns the created room (caller can read .ID).
func (h *Hub) CreateRoom(id string, cfg RoomConfig) (*Room, error) {
	if cfg.SmallBlind == 0 {
		cfg.SmallBlind = h.defaultCfg.SmallBlind
	}
	if cfg.BigBlind == 0 {
		cfg.BigBlind = h.defaultCfg.BigBlind
	}
	if cfg.MaxSeats == 0 {
		cfg.MaxSeats = h.defaultCfg.MaxSeats
	}
	if cfg.MinPlayers == 0 {
		cfg.MinPlayers = h.defaultCfg.MinPlayers
	}
	if cfg.SmallBlind <= 0 || cfg.BigBlind <= 0 || cfg.BigBlind < cfg.SmallBlind {
		return nil, errors.New("invalid blinds")
	}
	if cfg.MaxSeats < 2 || cfg.MaxSeats > 9 {
		return nil, errors.New("maxSeats must be 2..9")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.rooms[id]; exists {
		return nil, errors.New("room already exists")
	}
	r := NewRoom(id, cfg)
	h.rooms[id] = r
	return r, nil
}

// GetRoom returns a room or nil.
func (h *Hub) GetRoom(id string) *Room {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.rooms[id]
}

// ListRooms returns a snapshot of room IDs and player counts.
type RoomSummary struct {
	ID         string `json:"id"`
	Players    int    `json:"players"`
	MaxSeats   int    `json:"maxSeats"`
	SmallBlind int    `json:"smallBlind"`
	BigBlind   int    `json:"bigBlind"`
}

func (h *Hub) ListRooms() []RoomSummary {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]RoomSummary, 0, len(h.rooms))
	for _, r := range h.rooms {
		out = append(out, RoomSummary{
			ID:         r.ID,
			Players:    len(r.PlayerMetas()),
			MaxSeats:   r.Config.MaxSeats,
			SmallBlind: r.Config.SmallBlind,
			BigBlind:   r.Config.BigBlind,
		})
	}
	return out
}

// HandleClientMessage routes a parsed message from a connection.
// Returns an error which the caller can choose to propagate to the client.
func (h *Hub) HandleClientMessage(c *Conn, msg ClientMessage) {
	switch msg.Type {
	case CMsgPing:
		c.SendMessage(ServerMessage{Type: SMsgPong})

	case CMsgJoin:
		var p JoinPayload
		if err := json.Unmarshal(msg.Data, &p); err != nil {
			c.SendError("bad-payload", err.Error())
			return
		}
		// When the connection was authenticated at upgrade time, trust the
		// claims for identity. The client cannot impersonate other users by
		// supplying a different userId in the join payload.
		if c.Authenticated != nil {
			p.UserID = c.Authenticated.UserID
			if p.Nickname == "" {
				p.Nickname = c.Authenticated.Nickname
			}
			if p.Avatar == "" {
				p.Avatar = c.Authenticated.Avatar
			}
		}
		if p.RoomID == "" || p.UserID == "" {
			c.SendError("bad-payload", "roomId and userId required")
			return
		}
		room := h.GetOrCreateRoom(p.RoomID)
		seat, err := room.Join(PlayerMeta{
			UserID:   p.UserID,
			Nickname: p.Nickname,
			Avatar:   p.Avatar,
			BuyIn:    p.BuyIn,
		}, c)
		if err != nil {
			c.SendError("join-failed", err.Error())
			return
		}
		c.UserID = p.UserID
		c.RoomID = p.RoomID

		c.SendMessage(ServerMessage{Type: SMsgJoined, Data: JoinedPayload{
			RoomID: p.RoomID, Seat: seat, UserID: p.UserID,
		}})

		// Notify others in room
		h.broadcastExcept(room, c.UserID, ServerMessage{
			Type: SMsgPlayerJoined,
			Data: map[string]any{
				"userId": p.UserID, "nickname": p.Nickname, "avatar": p.Avatar, "seat": seat,
			},
		})

		// Push initial room state to the joiner
		h.sendRoomState(room, p.UserID)

		// Auto-start a hand if minimum reached and no hand in progress
		h.maybeStartHand(room)
		h.checkBotTurn(room)

	case CMsgLeave:
		if c.RoomID == "" || c.UserID == "" {
			c.SendError("not-joined", "")
			return
		}
		room := h.GetRoom(c.RoomID)
		if room == nil {
			return
		}
		uid, rid := c.UserID, c.RoomID
		room.Leave(uid)
		h.broadcastExcept(room, uid, ServerMessage{
			Type: SMsgPlayerLeft,
			Data: map[string]any{"userId": uid, "roomId": rid},
		})

	case CMsgRebuy:
		if !h.rateAllow(c) {
			c.SendError("rate-limited", "slow down")
			return
		}
		var p RebuyPayload
		if err := json.Unmarshal(msg.Data, &p); err != nil {
			c.SendError("bad-payload", err.Error())
			return
		}
		if c.RoomID == "" || c.UserID == "" {
			c.SendError("not-joined", "")
			return
		}
		room := h.GetRoom(c.RoomID)
		if room == nil {
			c.SendError("no-room", "")
			return
		}
		if err := room.Rebuy(c.UserID, p.Amount); err != nil {
			c.SendError("rebuy-failed", err.Error())
			return
		}
		log.Printf("[hub] rebuy user=%s room=%s amount=%d", c.UserID, c.RoomID, p.Amount)
		// Cancel any pending auto-restart so we re-evaluate fresh
		h.cancelNextHand(room.ID)
		// Reset engine to clear any HandComplete state, then attempt to start
		room.ResetEngine()
		h.broadcastRoomState(room)
		h.maybeStartHand(room)
		h.checkBotTurn(room)

	case CMsgAction:
		if !h.rateAllow(c) {
			c.SendError("rate-limited", "slow down")
			return
		}
		var p ActionPayload
		if err := json.Unmarshal(msg.Data, &p); err != nil {
			c.SendError("bad-payload", err.Error())
			return
		}
		if c.RoomID == "" || c.UserID == "" {
			c.SendError("not-joined", "")
			return
		}
		room := h.GetRoom(c.RoomID)
		if room == nil {
			c.SendError("no-room", "")
			return
		}
		action, err := parseAction(p)
		if err != nil {
			c.SendError("bad-action", err.Error())
			return
		}
		events, err := room.ApplyAction(c.UserID, action)
		if err != nil {
			c.SendError("action-failed", err.Error())
			return
		}
		h.broadcastEvents(room, events)
		// Auto-advance stage / showdown when betting round closes
		h.advanceUntilBlocked(room)
		h.checkBotTurn(room)

	case CMsgChat:
		if !h.rateAllow(c) {
			c.SendError("rate-limited", "slow down")
			return
		}
		var p ChatPayload
		if err := json.Unmarshal(msg.Data, &p); err != nil {
			c.SendError("bad-payload", err.Error())
			return
		}
		if c.RoomID == "" || c.UserID == "" {
			c.SendError("not-joined", "")
			return
		}
		emoji := truncateRunes(p.Emoji, 8)
		if emoji == "" {
			c.SendError("bad-payload", "empty emoji")
			return
		}
		if !chatAllowed(emoji) {
			c.SendError("chat-blocked", "包含不允许的内容")
			return
		}
		room := h.GetRoom(c.RoomID)
		if room == nil {
			c.SendError("no-room", "")
			return
		}
		// Find the sender's seat for client-side rendering
		seat := -1
		for _, m := range room.PlayerMetas() {
			if m.UserID == c.UserID {
				seat = m.Seat
				break
			}
		}
		broadcast := ServerMessage{Type: SMsgChat, Data: map[string]any{
			"userId": c.UserID,
			"seat":   seat,
			"emoji":  emoji,
			"ts":     time.Now().UnixMilli(),
		}}
		for _, conn := range room.Conns() {
			conn.SendMessage(broadcast)
		}

	default:
		c.SendError("unknown-type", string(msg.Type))
	}
}

// truncateRunes returns at most n runes of s. Used to cap emoji/text payloads
// before broadcasting to other clients.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// HandleDisconnect cleans up state for a connection that has dropped. It
// soft-leaves: the player is folded if mid-hand and their conn is closed, but
// their seat + chips persist for `disconnectGrace`. If they don't reconnect
// in time the timer fires and the player is fully purged.
func (h *Hub) HandleDisconnect(c *Conn) {
	if c.UserID != "" {
		h.limiter.Forget(c.UserID)
	}
	if c.RoomID == "" || c.UserID == "" {
		return
	}
	room := h.GetRoom(c.RoomID)
	if room == nil {
		return
	}
	uid := c.UserID
	// Tell others the player went offline (UI can grey them out).
	h.broadcastExcept(room, uid, ServerMessage{
		Type: SMsgPlayerLeft,
		Data: map[string]any{"userId": uid, "disconnected": true, "soft": true},
	})
	room.SoftLeave(uid, h.disconnectGrace, func() {
		// Fired only after the grace window without a reconnect. Notify
		// remaining players that the seat is now actually free.
		h.broadcastExcept(room, uid, ServerMessage{
			Type: SMsgPlayerLeft,
			Data: map[string]any{"userId": uid, "purged": true},
		})
	})
}

// --- helpers ---

func (h *Hub) maybeStartHand(room *Room) {
	eng := func() *game.Engine {
		room.mu.RLock()
		defer room.mu.RUnlock()
		return room.engine
	}()
	if eng != nil && eng.Stage != game.StageHandComplete {
		return
	}
	events, err := room.StartHand()
	if err != nil {
		// Not enough chip-positive players or other reason; benign.
		return
	}
	h.broadcastEvents(room, events)
	h.broadcastRoomState(room)
}

// advanceUntilBlocked drives the engine forward through stages while the
// betting round is closed (ActiveSeat == -1) and the hand isn't complete.
// When HandComplete is reached, it persists chips and schedules the next hand.
func (h *Hub) advanceUntilBlocked(room *Room) {
	for {
		room.mu.RLock()
		eng := room.engine
		room.mu.RUnlock()
		if eng == nil {
			return
		}
		if eng.ActiveSeat != -1 {
			h.broadcastRoomState(room)
			return
		}
		if eng.Stage == game.StageHandComplete {
			h.recordHandStats(room, eng)
			room.SyncBuyInsFromEngine()
			h.broadcastRoomState(room)
			h.scheduleNextHand(room)
			return
		}
		events, err := room.AdvanceStage()
		if err != nil {
			log.Printf("[hub] AdvanceStage err in room %s: %v", room.ID, err)
			return
		}
		h.broadcastEvents(room, events)
	}
}

// scheduleNextHand fires StartHand after autoStartDelay. If the auto-start
// fails (e.g. < MinPlayers with chips left), the engine is reset so the room
// shows a Waiting snapshot until conditions change.
func (h *Hub) scheduleNextHand(room *Room) {
	h.timerMu.Lock()
	delay := h.autoStartDelay
	if old := h.handTimers[room.ID]; old != nil {
		old.Stop()
	}
	h.timerMu.Unlock()

	roomID := room.ID
	timer := time.AfterFunc(delay, func() {
		h.timerMu.Lock()
		delete(h.handTimers, roomID)
		h.timerMu.Unlock()

		events, err := room.StartHand()
		if err != nil {
			log.Printf("[hub] auto-restart hand for %s skipped: %v", roomID, err)
			room.ResetEngine()
			h.broadcastRoomState(room)
			return
		}
		h.broadcastEvents(room, events)
		h.advanceUntilBlocked(room)
		h.checkBotTurn(room)
	})

	h.timerMu.Lock()
	h.handTimers[roomID] = timer
	h.timerMu.Unlock()
}

// cancelNextHand stops a pending auto-restart timer for the room, if any.
// Called when the room becomes empty or for cleanup.
func (h *Hub) cancelNextHand(roomID string) {
	h.timerMu.Lock()
	defer h.timerMu.Unlock()
	if t := h.handTimers[roomID]; t != nil {
		t.Stop()
		delete(h.handTimers, roomID)
	}
}

func (h *Hub) broadcastEvents(room *Room, events []game.Event) {
	for _, ev := range events {
		msg := ServerMessage{Type: SMsgGameEvent, Data: GameEventPayload{
			Type: string(ev.Type), Data: ev.Data,
		}}
		for _, conn := range room.Conns() {
			conn.SendMessage(msg)
		}
	}
}

func (h *Hub) broadcastRoomState(room *Room) {
	for uid, conn := range room.Conns() {
		conn.SendMessage(ServerMessage{
			Type: SMsgRoomState,
			Data: BuildRoomStateView(room, uid),
		})
	}
}

func (h *Hub) sendRoomState(room *Room, userID string) {
	conn := room.ConnFor(userID)
	if conn == nil {
		return
	}
	conn.SendMessage(ServerMessage{
		Type: SMsgRoomState,
		Data: BuildRoomStateView(room, userID),
	})
}

func (h *Hub) broadcastExcept(room *Room, exceptUserID string, msg ServerMessage) {
	for uid, conn := range room.Conns() {
		if uid == exceptUserID {
			continue
		}
		conn.SendMessage(msg)
	}
}

func parseAction(p ActionPayload) (game.Action, error) {
	switch p.Type {
	case "fold":
		return game.Action{Type: game.ActionFold}, nil
	case "check":
		return game.Action{Type: game.ActionCheck}, nil
	case "call":
		return game.Action{Type: game.ActionCall}, nil
	case "raise":
		return game.Action{Type: game.ActionRaise, Amount: p.Amount}, nil
	case "all-in":
		return game.Action{Type: game.ActionAllIn}, nil
	}
	return game.Action{}, errors.New("unknown action type: " + p.Type)
}
