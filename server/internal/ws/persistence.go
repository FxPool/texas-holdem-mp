package ws

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jiangminghong/texas-holdem-mp/server/internal/game"
)

// Snapshot is the on-disk JSON document. Captures only the things that should
// outlive a process restart: per-room player metadata (seats, chips) plus
// lifetime user stats. Active engine state is intentionally NOT persisted —
// any in-progress hand is abandoned on restart.
type Snapshot struct {
	Version int                       `json:"version"`
	SavedAt int64                     `json:"savedAt"`
	Rooms   []RoomSnapshot            `json:"rooms"`
	Stats   map[string]*UserStatsSnap `json:"stats"`
}

type RoomSnapshot struct {
	ID         string               `json:"id"`
	Config     RoomConfig           `json:"config"`
	DealerSeat int                  `json:"dealerSeat"`
	Players    []PlayerMetaSnapshot `json:"players"`
	StartedAt  int64                `json:"startedAt"` // unix ms
	EndsAt     int64                `json:"endsAt"`    // unix ms; 0 if no limit
	Ended      bool                 `json:"ended"`
}

type PlayerMetaSnapshot struct {
	UserID     string `json:"userId"`
	Nickname   string `json:"nickname"`
	Avatar     string `json:"avatar"`
	Seat       int    `json:"seat"`
	BuyIn      int    `json:"buyIn"`
	TotalBuyIn int    `json:"totalBuyIn"`
}

// UserStatsSnap is the persistent counterpart of in-memory user stats.
type UserStatsSnap struct {
	UserID        string `json:"userId"`
	HandsPlayed   int    `json:"handsPlayed"`
	HandsWon      int    `json:"handsWon"`
	BiggestPotWon int    `json:"biggestPotWon"`
	NetChips      int    `json:"netChips"`
	LastPlayedAt  int64  `json:"lastPlayedAt"` // unix sec
}

// SnapshotStore handles atomic write + load of Snapshot from a file. It also
// owns the in-memory user stats map.
type SnapshotStore struct {
	path string

	mu    sync.Mutex
	stats map[string]*UserStatsSnap
}

// NewSnapshotStore creates a store backed by `path`. If path is empty, the
// store no-ops on save (useful for in-memory tests).
func NewSnapshotStore(path string) *SnapshotStore {
	return &SnapshotStore{
		path:  path,
		stats: map[string]*UserStatsSnap{},
	}
}

// Load reads the snapshot file (if any) and returns its Rooms + populates the
// in-memory stats. Missing file → empty snapshot, no error.
func (s *SnapshotStore) Load() ([]RoomSnapshot, error) {
	if s.path == "" {
		return nil, nil
	}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc Snapshot
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	s.mu.Lock()
	for k, v := range doc.Stats {
		if v != nil {
			s.stats[k] = v
		}
	}
	s.mu.Unlock()
	return doc.Rooms, nil
}

// Save writes the given rooms + current stats atomically (write-tmp + rename).
func (s *SnapshotStore) Save(rooms []RoomSnapshot) error {
	if s.path == "" {
		return nil
	}
	s.mu.Lock()
	doc := Snapshot{
		Version: 1,
		SavedAt: time.Now().Unix(),
		Rooms:   rooms,
		Stats:   make(map[string]*UserStatsSnap, len(s.stats)),
	}
	for k, v := range s.stats {
		cp := *v
		doc.Stats[k] = &cp
	}
	s.mu.Unlock()
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// RecordShowdown updates lifetime stats for one hand's resolution. shares is
// keyed by userId → amount won this hand. potTotal is the pot that was
// distributed. Players who participated but won nothing are credited a
// "hand played" but no win.
func (s *SnapshotStore) RecordShowdown(participants []string, shares map[string]int, potTotal int) {
	now := time.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, uid := range participants {
		st, ok := s.stats[uid]
		if !ok {
			st = &UserStatsSnap{UserID: uid}
			s.stats[uid] = st
		}
		st.HandsPlayed++
		st.LastPlayedAt = now
		if won := shares[uid]; won > 0 {
			st.HandsWon++
			st.NetChips += won
			if won > st.BiggestPotWon {
				st.BiggestPotWon = won
			}
		}
	}
	// Negative chip flow: anyone who put chips in the pot but didn't win.
	// We approximate at the per-hand level: net flow per player = won - committed.
	// Since we don't carry committed amounts here, the caller should pass the
	// `lost` map via RecordShowdownLosses if precise tracking matters. For now
	// we keep a coarse "biggest win" + "hands won" view.
	_ = potTotal
}

// RecordChipDelta accumulates the per-hand chip delta for a user. Positive =
// gained, negative = lost. Called by the hand-end path with the difference
// between (chips after) and (chips before) the hand.
func (s *SnapshotStore) RecordChipDelta(userID string, delta int) {
	if userID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.stats[userID]
	if !ok {
		st = &UserStatsSnap{UserID: userID}
		s.stats[userID] = st
	}
	st.NetChips += delta
}

// RecordHandPlayed bumps HandsPlayed (and HandsWon / BiggestPotWon when
// delta > 0) for one hand. delta is the chip change for this user this hand.
func (s *SnapshotStore) RecordHandPlayed(userID string, delta int) {
	if userID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.stats[userID]
	if !ok {
		st = &UserStatsSnap{UserID: userID}
		s.stats[userID] = st
	}
	st.HandsPlayed++
	st.LastPlayedAt = time.Now().Unix()
	if delta > 0 {
		st.HandsWon++
		if delta > st.BiggestPotWon {
			st.BiggestPotWon = delta
		}
	}
}

// GetStats returns a sorted slice for /stats endpoint. Sorted by NetChips desc
// so the leaderboard is the natural view.
func (s *SnapshotStore) GetStats() []UserStatsSnap {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]UserStatsSnap, 0, len(s.stats))
	for _, v := range s.stats {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NetChips > out[j].NetChips })
	return out
}

// GetUserStats returns one user's stats or nil.
func (s *SnapshotStore) GetUserStats(userID string) *UserStatsSnap {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.stats[userID]
	if !ok {
		return nil
	}
	cp := *st
	return &cp
}

// snapshotRoom serializes one Room into a RoomSnapshot. Caller must hold no
// locks (we acquire RLock internally).
func snapshotRoom(r *Room) RoomSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	metas := make([]PlayerMetaSnapshot, 0, len(r.players))
	for _, m := range r.players {
		total := m.TotalBuyIn
		if total == 0 {
			total = m.BuyIn
		}
		metas = append(metas, PlayerMetaSnapshot{
			UserID:     m.UserID,
			Nickname:   m.Nickname,
			Avatar:     m.Avatar,
			Seat:       m.Seat,
			BuyIn:      m.BuyIn,
			TotalBuyIn: total,
		})
	}
	sort.Slice(metas, func(i, j int) bool { return metas[i].Seat < metas[j].Seat })
	startedAt := int64(0)
	if !r.StartedAt.IsZero() {
		startedAt = r.StartedAt.UnixMilli()
	}
	endsAt := int64(0)
	if !r.EndsAt.IsZero() {
		endsAt = r.EndsAt.UnixMilli()
	}
	return RoomSnapshot{
		ID:         r.ID,
		Config:     r.Config,
		DealerSeat: r.dealerSeat,
		Players:    metas,
		StartedAt:  startedAt,
		EndsAt:     endsAt,
		Ended:      r.Ended,
	}
}

// loadRoomFromSnapshot recreates a Room from disk state. Players are seated
// without active connections; they reattach when a CMsgJoin arrives.
func loadRoomFromSnapshot(s RoomSnapshot) *Room {
	r := NewRoom(s.ID, s.Config)
	r.dealerSeat = s.DealerSeat
	if s.StartedAt > 0 {
		r.StartedAt = time.UnixMilli(s.StartedAt)
	}
	if s.EndsAt > 0 {
		r.EndsAt = time.UnixMilli(s.EndsAt)
	} else {
		r.EndsAt = time.Time{}
	}
	r.Ended = s.Ended
	for _, p := range s.Players {
		total := p.TotalBuyIn
		if total == 0 {
			total = p.BuyIn
		}
		// We deep-copy to avoid sharing the snapshot pointer.
		meta := PlayerMeta{
			UserID:     p.UserID,
			Nickname:   p.Nickname,
			Avatar:     p.Avatar,
			Seat:       p.Seat,
			BuyIn:      p.BuyIn,
			TotalBuyIn: total,
		}
		r.players[p.UserID] = &meta
	}
	return r
}

// snapshotAllRooms collects snapshots of every room currently registered.
func (h *Hub) snapshotAllRooms() []RoomSnapshot {
	h.mu.RLock()
	rooms := make([]*Room, 0, len(h.rooms))
	for _, r := range h.rooms {
		rooms = append(rooms, r)
	}
	h.mu.RUnlock()
	out := make([]RoomSnapshot, 0, len(rooms))
	for _, r := range rooms {
		out = append(out, snapshotRoom(r))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AttachStore wires the hub to a snapshot store. After loading, any persisted
// rooms are re-registered and stats are restored. The hub will save
// periodically (every saveInterval) and the caller can also invoke SaveNow().
func (h *Hub) AttachStore(store *SnapshotStore, saveInterval time.Duration) error {
	h.mu.Lock()
	h.store = store
	h.mu.Unlock()
	rooms, err := store.Load()
	if err != nil {
		return err
	}
	h.mu.Lock()
	loaded := make([]*Room, 0, len(rooms))
	for _, rs := range rooms {
		if _, exists := h.rooms[rs.ID]; exists {
			continue
		}
		room := loadRoomFromSnapshot(rs)
		h.rooms[rs.ID] = room
		loaded = append(loaded, room)
	}
	h.mu.Unlock()
	for _, room := range loaded {
		if room.Ended || room.EndsAt.IsZero() {
			continue
		}
		h.scheduleGameEnd(room)
	}
	if saveInterval > 0 {
		go h.persistenceLoop(saveInterval)
	}
	return nil
}

// SaveNow forces an immediate snapshot to disk. Safe to call concurrently.
func (h *Hub) SaveNow() error {
	store := h.store
	if store == nil {
		return nil
	}
	return store.Save(h.snapshotAllRooms())
}

func (h *Hub) persistenceLoop(interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for range t.C {
		if err := h.SaveNow(); err != nil {
			log.Printf("[hub] snapshot save err: %v", err)
		}
	}
}

// recordHandStats updates lifetime stats based on a freshly-completed hand.
// Must be called BEFORE SyncBuyInsFromEngine so PlayerMeta.BuyIn still
// reflects the pre-hand stack. delta = engine.Chips - meta.BuyIn; positive
// values are credited as wins for the biggest-pot stat. Bots (uid prefix
// `bot:`) are skipped to keep leaderboards focused on humans.
func (h *Hub) recordHandStats(room *Room, _ *game.Engine) {
	if h.store == nil {
		return
	}
	room.mu.RLock()
	defer room.mu.RUnlock()
	if room.engine == nil {
		return
	}
	for _, p := range room.engine.Players {
		meta := room.players[p.ID]
		if meta == nil {
			continue
		}
		if strings.HasPrefix(p.ID, "bot:") {
			continue
		}
		delta := p.Chips - meta.BuyIn
		if p.Committed == 0 && delta == 0 {
			continue // didn't participate
		}
		h.store.RecordChipDelta(p.ID, delta)
		h.store.RecordHandPlayed(p.ID, delta)
	}
}
