package ws

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/game"
)

type RoomConfig struct {
	SmallBlind int
	BigBlind   int
	MaxSeats   int // default 6
	MinPlayers int // default 2; hand can start once this many seated
}

type PlayerMeta struct {
	UserID   string
	Nickname string
	Avatar   string
	BuyIn    int
	Seat     int  // assigned at Join, persistent across hands while in room
	IsBot    bool // true for AI-controlled players (uid prefix `bot:`)
}

// Room owns one Engine plus connections. All mutations through r.mu.
type Room struct {
	ID     string
	Config RoomConfig

	mu      sync.RWMutex
	engine  *game.Engine
	players map[string]*PlayerMeta // userID → meta
	conns   map[string]*Conn       // userID → conn

	dealerSeat int // -1 means "first hand: assign at StartHand"
	sbSeat     int // recomputed each StartHand
	bbSeat     int

	// Disconnect grace timers. When a Conn drops, we keep PlayerMeta around
	// for `disconnectGrace` so the user can reconnect without losing their
	// seat or chips. The map is keyed by userID.
	disconnectTimers map[string]*time.Timer
}

func NewRoom(id string, cfg RoomConfig) *Room {
	if cfg.MaxSeats == 0 {
		cfg.MaxSeats = 6
	}
	if cfg.MinPlayers == 0 {
		cfg.MinPlayers = 2
	}
	return &Room{
		ID:               id,
		Config:           cfg,
		players:          map[string]*PlayerMeta{},
		conns:            map[string]*Conn{},
		disconnectTimers: map[string]*time.Timer{},
		dealerSeat:       -1,
		sbSeat:           -1,
		bbSeat:           -1,
	}
}

// Join adds (or rejoins) a player, returning the assigned seat.
// Reconnects within the disconnect-grace window cancel the pending purge.
func (r *Room) Join(meta PlayerMeta, conn *Conn) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Cancel any pending soft-leave purge for this user (they're back).
	if t := r.disconnectTimers[meta.UserID]; t != nil {
		t.Stop()
		delete(r.disconnectTimers, meta.UserID)
	}

	// Reconnect
	if existing, ok := r.players[meta.UserID]; ok {
		if old := r.conns[meta.UserID]; old != nil && old != conn {
			old.Close()
		}
		r.conns[meta.UserID] = conn
		return existing.Seat, nil
	}

	if len(r.players) >= r.Config.MaxSeats {
		return -1, errors.New("room is full")
	}

	seat := r.findFreeSeatLocked()
	if seat < 0 {
		return -1, errors.New("no free seat")
	}
	meta.Seat = seat
	r.players[meta.UserID] = &meta
	r.conns[meta.UserID] = conn
	return seat, nil
}

// AddBot seats an AI-controlled player. Returns the seat or an error.
func (r *Room) AddBot(meta PlayerMeta) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.players) >= r.Config.MaxSeats {
		return -1, errors.New("room is full")
	}
	seat := r.findFreeSeatLocked()
	if seat < 0 {
		return -1, errors.New("no free seat")
	}
	meta.Seat = seat
	meta.IsBot = true
	if meta.BuyIn <= 0 {
		meta.BuyIn = 1000
	}
	r.players[meta.UserID] = &meta
	return seat, nil
}

// IsBotSeat reports whether the given seat is currently occupied by a bot.
func (r *Room) IsBotSeat(seat int) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, m := range r.players {
		if m.Seat == seat {
			return m.IsBot
		}
	}
	return false
}

// BotUserIDForSeat returns the bot's userID at this seat, or "".
func (r *Room) BotUserIDForSeat(seat int) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, m := range r.players {
		if m.Seat == seat && m.IsBot {
			return m.UserID
		}
	}
	return ""
}

func (r *Room) findFreeSeatLocked() int {
	used := map[int]bool{}
	for _, m := range r.players {
		used[m.Seat] = true
	}
	for s := 0; s < r.Config.MaxSeats; s++ {
		if !used[s] {
			return s
		}
	}
	return -1
}

// Leave removes a player. Folds them mid-hand if applicable.
func (r *Room) Leave(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removePlayerLocked(userID)
}

func (r *Room) removePlayerLocked(userID string) {
	if t := r.disconnectTimers[userID]; t != nil {
		t.Stop()
		delete(r.disconnectTimers, userID)
	}
	if conn := r.conns[userID]; conn != nil {
		conn.Close()
	}
	delete(r.conns, userID)
	delete(r.players, userID)
	if r.engine != nil {
		for _, p := range r.engine.Players {
			if p.ID == userID && (p.State == game.PlayerActive || p.State == game.PlayerAllIn) {
				p.State = game.PlayerFolded
			}
		}
	}
}

// SoftLeave handles a dropped connection. It closes the conn and folds the
// player in any active hand (so the table doesn't hang waiting on them), but
// keeps their PlayerMeta around for `grace`. If the same userID rejoins
// before the timer fires, the purge is cancelled and they slide right back.
// onPurge fires from the timer goroutine *after* the player is removed.
func (r *Room) SoftLeave(userID string, grace time.Duration, onPurge func()) {
	r.mu.Lock()
	if conn := r.conns[userID]; conn != nil {
		conn.Close()
	}
	delete(r.conns, userID)
	if r.engine != nil {
		for _, p := range r.engine.Players {
			if p.ID == userID && (p.State == game.PlayerActive || p.State == game.PlayerAllIn) {
				p.State = game.PlayerFolded
			}
		}
	}
	if t := r.disconnectTimers[userID]; t != nil {
		t.Stop()
	}
	if grace <= 0 {
		// Purge immediately (used by tests)
		delete(r.players, userID)
		r.mu.Unlock()
		if onPurge != nil {
			onPurge()
		}
		return
	}
	// Schedule purge.
	r.disconnectTimers[userID] = time.AfterFunc(grace, func() {
		r.mu.Lock()
		// If user re-joined in the meantime the timer would have been Stop+deleted,
		// so simply not finding it means we should bail.
		if _, ok := r.disconnectTimers[userID]; !ok {
			r.mu.Unlock()
			return
		}
		delete(r.disconnectTimers, userID)
		delete(r.players, userID)
		r.mu.Unlock()
		if onPurge != nil {
			onPurge()
		}
	})
	r.mu.Unlock()
}

// HasPendingPurge reports whether a soft-leave timer is currently scheduled
// for the given user. Mainly for tests.
func (r *Room) HasPendingPurge(userID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.disconnectTimers[userID]
	return ok
}

// StartHand creates a new Engine seeded from current players and runs Start().
// Persistent chip stacks: if a previous hand existed, surviving chip counts
// carry forward via PlayerMeta.BuyIn (updated by SyncBuyInsFromEngine after each hand).
//
// Players with 0 chips remain in the room but are seated as sit-out by Engine.Start.
// We need at least MinPlayers chip-positive players to actually run a hand.
func (r *Room) StartHand() ([]game.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	eligible := 0
	for _, m := range r.players {
		if m.BuyIn > 0 {
			eligible++
		}
	}
	if eligible < r.Config.MinPlayers {
		return nil, fmt.Errorf("need %d players with chips, have %d", r.Config.MinPlayers, eligible)
	}

	metas := make([]*PlayerMeta, 0, len(r.players))
	for _, m := range r.players {
		metas = append(metas, m)
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Seat < metas[j].Seat })

	enginePlayers := make([]*game.EnginePlayer, 0, len(metas))
	for _, m := range metas {
		enginePlayers = append(enginePlayers, &game.EnginePlayer{
			ID:    m.UserID,
			Seat:  m.Seat,
			Chips: m.BuyIn,
		})
	}

	// Move dealer button: first hand → lowest seat; later → next seat clockwise
	if r.dealerSeat < 0 {
		r.dealerSeat = enginePlayers[0].Seat
	} else {
		// find current dealer index, advance
		curIdx := -1
		for i, p := range enginePlayers {
			if p.Seat == r.dealerSeat {
				curIdx = i
				break
			}
		}
		if curIdx < 0 {
			r.dealerSeat = enginePlayers[0].Seat
		} else {
			r.dealerSeat = enginePlayers[(curIdx+1)%len(enginePlayers)].Seat
		}
	}

	eng, err := game.NewEngine(enginePlayers, r.dealerSeat, r.Config.SmallBlind, r.Config.BigBlind)
	if err != nil {
		return nil, err
	}
	r.engine = eng
	events, err := eng.Start()
	if err != nil {
		return events, err
	}
	r.recomputeBlindSeatsLocked()
	return events, nil
}

func (r *Room) recomputeBlindSeatsLocked() {
	r.sbSeat, r.bbSeat = -1, -1
	if r.engine == nil {
		return
	}
	inHand := 0
	for _, p := range r.engine.Players {
		if p.State != game.PlayerSitOut {
			inHand++
		}
	}
	if inHand < 2 {
		return
	}
	dealerIdx := -1
	for i, p := range r.engine.Players {
		if p.Seat == r.engine.DealerSeat {
			dealerIdx = i
			break
		}
	}
	if dealerIdx < 0 {
		return
	}
	if inHand == 2 {
		r.sbSeat = r.engine.DealerSeat
		r.bbSeat = nextNonSitOutSeat(r.engine.Players, dealerIdx)
		return
	}
	sbIdx := nextNonSitOutIdx(r.engine.Players, dealerIdx)
	r.sbSeat = r.engine.Players[sbIdx].Seat
	r.bbSeat = nextNonSitOutSeat(r.engine.Players, sbIdx)
}

// MaxRebuyAmount caps a single rebuy request to keep abuse manageable.
// Production deployments can wire this to per-room config.
const MaxRebuyAmount = 100_000

// Rebuy adds chips to a player's stack. Only allowed when no hand is in
// progress (engine nil OR HandComplete) — preventing mid-hand bankroll injection.
func (r *Room) Rebuy(userID string, amount int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if amount <= 0 {
		return errors.New("rebuy amount must be positive")
	}
	if amount > MaxRebuyAmount {
		amount = MaxRebuyAmount
	}
	m, ok := r.players[userID]
	if !ok {
		return errors.New("not in room")
	}
	if r.engine != nil {
		stage := r.engine.Stage
		midHand := stage != game.StageWaiting && stage != game.StageHandComplete
		if midHand {
			return fmt.Errorf("cannot rebuy mid-hand (stage=%s)", stage)
		}
	}
	m.BuyIn += amount
	return nil
}

// ResetEngine drops the current Engine so the view falls back to the
// "Waiting" snapshot. Called when auto-restart cannot start a new hand
// (e.g. only one player has chips).
func (r *Room) ResetEngine() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.engine = nil
	r.sbSeat = -1
	r.bbSeat = -1
}

// SyncBuyInsFromEngine copies engine chip counts back to PlayerMeta.BuyIn.
// Called at end of hand so the next hand starts with current stacks.
func (r *Room) SyncBuyInsFromEngine() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.engine == nil {
		return
	}
	for _, p := range r.engine.Players {
		if m := r.players[p.ID]; m != nil {
			m.BuyIn = p.Chips
		}
	}
}

func (r *Room) ApplyAction(userID string, action game.Action) ([]game.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.engine == nil {
		return nil, errors.New("no hand in progress")
	}
	seat := -1
	for _, p := range r.engine.Players {
		if p.ID == userID {
			seat = p.Seat
			break
		}
	}
	if seat < 0 {
		return nil, errors.New("not seated")
	}
	return r.engine.Apply(seat, action)
}

func (r *Room) AdvanceStage() ([]game.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.engine == nil {
		return nil, errors.New("no hand in progress")
	}
	return r.engine.AdvanceStage()
}

// blindSeats returns the SB and BB seats for the current hand, or (-1,-1).
// Caller must hold r.mu (read or write).
func (r *Room) blindSeats() (int, int) {
	return r.sbSeat, r.bbSeat
}

func (r *Room) Conns() map[string]*Conn {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]*Conn, len(r.conns))
	for k, v := range r.conns {
		out[k] = v
	}
	return out
}

func (r *Room) ConnFor(userID string) *Conn {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.conns[userID]
}

func (r *Room) PlayerMetas() []*PlayerMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*PlayerMeta, 0, len(r.players))
	for _, m := range r.players {
		out = append(out, m)
	}
	return out
}

func nextNonSitOutIdx(players []*game.EnginePlayer, fromIdx int) int {
	n := len(players)
	for i := 1; i <= n; i++ {
		j := (fromIdx + i) % n
		if players[j].State != game.PlayerSitOut {
			return j
		}
	}
	return -1
}

func nextNonSitOutSeat(players []*game.EnginePlayer, fromIdx int) int {
	idx := nextNonSitOutIdx(players, fromIdx)
	if idx < 0 {
		return -1
	}
	return players[idx].Seat
}
