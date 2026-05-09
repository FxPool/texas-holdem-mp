package ws

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewSnapshotStore(path)

	hub := NewHub(RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	if err := hub.AttachStore(store, 0); err != nil {
		t.Fatal(err)
	}

	// Manually create a room with a couple of seated players.
	r, err := hub.CreateRoom("snap1", RoomConfig{SmallBlind: 100, BigBlind: 200, MaxSeats: 6, MinPlayers: 2})
	if err != nil {
		t.Fatal(err)
	}
	r.players["alice"] = &PlayerMeta{UserID: "alice", Nickname: "Alice", Avatar: "🐱", Seat: 0, BuyIn: 1500}
	r.players["bob"] = &PlayerMeta{UserID: "bob", Nickname: "Bob", Avatar: "🐶", Seat: 1, BuyIn: 800}

	// Lifetime stats too.
	store.RecordChipDelta("alice", 200)
	store.RecordHandPlayed("alice", 200)
	store.RecordChipDelta("bob", -200)
	store.RecordHandPlayed("bob", -200)

	if err := hub.SaveNow(); err != nil {
		t.Fatalf("save: %v", err)
	}

	// New hub → load from disk.
	hub2 := NewHub(RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	store2 := NewSnapshotStore(path)
	if err := hub2.AttachStore(store2, 0); err != nil {
		t.Fatalf("attach: %v", err)
	}
	got := hub2.GetRoom("snap1")
	if got == nil {
		t.Fatalf("room 'snap1' should be restored")
	}
	if got.Config.BigBlind != 200 {
		t.Errorf("config not restored: bb=%d", got.Config.BigBlind)
	}
	metas := got.PlayerMetas()
	if len(metas) != 2 {
		t.Errorf("expected 2 seated, got %d", len(metas))
	}
	for _, m := range metas {
		if m.UserID == "alice" && m.BuyIn != 1500 {
			t.Errorf("alice buy-in not restored: %d", m.BuyIn)
		}
	}
	aliceStats := store2.GetUserStats("alice")
	if aliceStats == nil || aliceStats.HandsPlayed != 1 || aliceStats.NetChips != 200 {
		t.Errorf("alice stats not restored: %+v", aliceStats)
	}
}

func TestSnapshotEmptyPathNoOp(t *testing.T) {
	store := NewSnapshotStore("")
	if err := store.Save(nil); err != nil {
		t.Errorf("save with empty path should be no-op, got err=%v", err)
	}
	rooms, err := store.Load()
	if err != nil {
		t.Errorf("load with empty path should be no-op, got err=%v", err)
	}
	if rooms != nil {
		t.Errorf("expected nil rooms, got %d", len(rooms))
	}
}

func TestRecordHandPlayedTracksWins(t *testing.T) {
	store := NewSnapshotStore("")
	store.RecordHandPlayed("alice", 500)
	store.RecordHandPlayed("alice", -300)
	store.RecordHandPlayed("alice", 700) // bigger pot
	st := store.GetUserStats("alice")
	if st == nil {
		t.Fatal("stats should exist")
	}
	if st.HandsPlayed != 3 {
		t.Errorf("hands played = %d, want 3", st.HandsPlayed)
	}
	if st.HandsWon != 2 {
		t.Errorf("hands won = %d, want 2", st.HandsWon)
	}
	if st.BiggestPotWon != 700 {
		t.Errorf("biggest pot = %d, want 700", st.BiggestPotWon)
	}
}

func TestSnapshotPeriodicSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewSnapshotStore(path)
	hub := NewHub(RoomConfig{SmallBlind: 50, BigBlind: 100, MaxSeats: 6, MinPlayers: 2})
	if err := hub.AttachStore(store, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	hub.GetOrCreateRoom("auto1")
	time.Sleep(250 * time.Millisecond)
	// File should exist now from the periodic loop.
	if rooms, err := NewSnapshotStore(path).Load(); err != nil {
		t.Fatal(err)
	} else if len(rooms) == 0 {
		t.Errorf("periodic save should have written rooms")
	}
}
