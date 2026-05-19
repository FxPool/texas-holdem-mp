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
	endTimers      map[string]*time.Timer
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
		endTimers:       map[string]*time.Timer{},
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
	if cfg.DurationMinutes < 0 || cfg.DurationMinutes > 24*60 {
		return nil, errors.New("durationMinutes must be 0..1440")
	}
	if len(cfg.Password) > 32 {
		return nil, errors.New("password too long")
	}
	h.mu.Lock()
	if _, exists := h.rooms[id]; exists {
		h.mu.Unlock()
		return nil, errors.New("room already exists")
	}
	r := NewRoom(id, cfg)
	h.rooms[id] = r
	h.mu.Unlock()
	if cfg.DurationMinutes > 0 {
		h.scheduleGameEnd(r)
	}
	return r, nil
}

// scheduleGameEnd arms the duration timer for room r. Idempotent — replaces
// any prior end timer for the same room.
func (h *Hub) scheduleGameEnd(room *Room) {
	if room.EndsAt.IsZero() {
		return
	}
	delay := time.Until(room.EndsAt)
	if delay <= 0 {
		// Already past the deadline: trigger immediately.
		go h.triggerGameEnd(room)
		return
	}
	roomID := room.ID
	h.timerMu.Lock()
	if old := h.endTimers[roomID]; old != nil {
		old.Stop()
	}
	h.endTimers[roomID] = time.AfterFunc(delay, func() {
		h.timerMu.Lock()
		delete(h.endTimers, roomID)
		h.timerMu.Unlock()
		h.triggerGameEnd(room)
	})
	h.timerMu.Unlock()
}

// triggerGameEnd marks the room as end-pending. If no hand is in progress it
// settles immediately; otherwise settlement waits for HandComplete.
func (h *Hub) triggerGameEnd(room *Room) {
	if !room.MarkEndPending() {
		return
	}
	log.Printf("[hub] room %s duration timer fired, end-pending", room.ID)
	// Cancel any auto-restart so we don't deal a fresh hand after the deadline.
	h.cancelNextHand(room.ID)

	// If a hand is currently in progress, defer settlement until it
	// completes (advanceUntilBlocked picks it up). Otherwise settle now.
	room.mu.RLock()
	midHand := room.engine != nil && room.engine.Stage != game.StageHandComplete && room.engine.Stage != game.StageWaiting
	room.mu.RUnlock()
	// Notify clients that the timer is up so they can render a "结算中" hint.
	h.broadcastRoomState(room)
	if !midHand {
		h.finalizeGameEnd(room)
	}
}

// finalizeGameEnd computes settlement, broadcasts game-ended, and freezes
// the room. Idempotent.
func (h *Hub) finalizeGameEnd(room *Room) {
	if !room.FinalizeEnd() {
		return
	}
	log.Printf("[hub] room %s settling final scores", room.ID)
	entries := room.BuildSettlement()
	views := make([]PlayerSettlementView, 0, len(entries))
	for i, e := range entries {
		views = append(views, PlayerSettlementView{
			UserID:     e.UserID,
			Nickname:   e.Nickname,
			Avatar:     e.Avatar,
			Seat:       e.Seat,
			IsBot:      e.IsBot,
			Chips:      e.Chips,
			TotalBuyIn: e.TotalBuyIn,
			Net:        e.Net,
			Rank:       i + 1,
		})
	}
	payload := GameEndedPayload{
		RoomID:  room.ID,
		EndedAt: time.Now().UnixMilli(),
		Players: views,
	}
	for _, conn := range room.Conns() {
		conn.SendMessage(ServerMessage{Type: SMsgGameEnded, Data: payload})
	}
	h.broadcastRoomState(room)
}

// GetRoom returns a room or nil.
func (h *Hub) GetRoom(id string) *Room {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.rooms[id]
}

// ListRooms returns a snapshot of room IDs and player counts.
type RoomSummary struct {
	ID              string `json:"id"`
	Players         int    `json:"players"`
	MaxSeats        int    `json:"maxSeats"`
	SmallBlind      int    `json:"smallBlind"`
	BigBlind        int    `json:"bigBlind"`
	HasPassword     bool   `json:"hasPassword"`
	DurationMinutes int    `json:"durationMinutes"`
	EndsAt          int64  `json:"endsAt"` // unix ms; 0 = no limit
	Ended           bool   `json:"ended"`
}

func (h *Hub) ListRooms() []RoomSummary {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]RoomSummary, 0, len(h.rooms))
	for _, r := range h.rooms {
		endsAt := int64(0)
		if !r.EndsAt.IsZero() {
			endsAt = r.EndsAt.UnixMilli()
		}
		out = append(out, RoomSummary{
			ID:              r.ID,
			Players:         len(r.PlayerMetas()),
			MaxSeats:        r.Config.MaxSeats,
			SmallBlind:      r.Config.SmallBlind,
			BigBlind:        r.Config.BigBlind,
			HasPassword:     r.HasPassword(),
			DurationMinutes: r.Config.DurationMinutes,
			EndsAt:          endsAt,
			Ended:           r.IsEnded(),
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
		// Look up first so we can enforce a password on existing rooms.
		// Auto-create only when the room has no password requirement.
		room := h.GetRoom(p.RoomID)
		if room == nil {
			room = h.GetOrCreateRoom(p.RoomID)
		} else {
			// Reconnects (player already seated) bypass password — they
			// already passed the check on their first join.
			isReconnect := false
			for _, m := range room.PlayerMetas() {
				if m.UserID == p.UserID {
					isReconnect = true
					break
				}
			}
			if !isReconnect {
				if err := room.CheckPassword(p.Password); err != nil {
					c.SendError("password-required", "房间密码错误")
					return
				}
			}
		}
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
		h.MaybeDeleteEmptyRoom(rid)

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
	// If the user already explicitly left (CMsgLeave ran first and removed
	// them), don't schedule a soft-leave grace timer — they're gone for good.
	// Otherwise a stale disconnect timer would block MaybeDeleteEmptyRoom for
	// the full grace window when multiple players exit at once.
	if !room.HasPlayer(uid) {
		h.MaybeDeleteEmptyRoom(c.RoomID)
		return
	}
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
		h.MaybeDeleteEmptyRoom(c.RoomID)
	})
}

// --- helpers ---

func (h *Hub) maybeStartHand(room *Room) {
	if room.IsEnded() {
		return
	}
	room.mu.RLock()
	eng := room.engine
	endPending := room.EndPending
	room.mu.RUnlock()
	if endPending {
		return
	}
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
			room.mu.RLock()
			endPending := room.EndPending && !room.Ended
			room.mu.RUnlock()
			if endPending {
				h.finalizeGameEnd(room)
				return
			}
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

// MaybeDeleteEmptyRoom removes the room from the hub if no humans remain
// (bots-only rooms count as empty — bots can't sustain a table without a
// human). Pending soft-leave grace timers keep the room alive so a
// reconnecting player isn't stranded. Returns true when the room is deleted.
//
// Locking: takes h.mu then r.mu (consistent with Join's ordering). Hold the
// hub lock across the check-and-delete so a concurrent Join can't seat a
// player into a room we're about to drop.
func (h *Hub) MaybeDeleteEmptyRoom(roomID string) bool {
	h.mu.Lock()
	room, ok := h.rooms[roomID]
	if !ok {
		h.mu.Unlock()
		return false
	}
	room.mu.RLock()
	for _, m := range room.players {
		if !m.IsBot {
			room.mu.RUnlock()
			h.mu.Unlock()
			return false
		}
	}
	// If any soft-leave timer is pending, a human may still rejoin within
	// the grace window — keep the room.
	if len(room.disconnectTimers) > 0 {
		room.mu.RUnlock()
		h.mu.Unlock()
		return false
	}
	room.mu.RUnlock()
	delete(h.rooms, roomID)
	h.mu.Unlock()

	// Cancel auto-restart and duration timers.
	h.cancelNextHand(roomID)
	h.timerMu.Lock()
	if t := h.endTimers[roomID]; t != nil {
		t.Stop()
		delete(h.endTimers, roomID)
	}
	h.timerMu.Unlock()

	// Drop bot metas so any lingering references die quickly. Bots have no
	// Conn, so nothing else to close.
	room.mu.Lock()
	room.players = map[string]*PlayerMeta{}
	room.engine = nil
	room.mu.Unlock()

	log.Printf("[hub] room %s deleted (no humans remaining)", roomID)
	return true
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
