package game

import (
	"testing"
)

// helper: build N players with given seats and equal starting chips
func makePlayers(seats []int, chips int) []*EnginePlayer {
	out := make([]*EnginePlayer, len(seats))
	for i, s := range seats {
		out[i] = &EnginePlayer{
			ID:    string(rune('A' + i)),
			Seat:  s,
			Chips: chips,
		}
	}
	return out
}

func mustStart(t *testing.T, e *Engine) {
	t.Helper()
	if _, err := e.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func TestNewEngineValidation(t *testing.T) {
	if _, err := NewEngine(makePlayers([]int{0}, 1000), 0, 50, 100); err == nil {
		t.Errorf("expected error for 1 player")
	}
	if _, err := NewEngine(makePlayers([]int{0, 1}, 1000), 0, 0, 100); err == nil {
		t.Errorf("expected error for sb=0")
	}
	if _, err := NewEngine(makePlayers([]int{0, 1}, 1000), 0, 100, 50); err == nil {
		t.Errorf("expected error for bb<sb")
	}
	if _, err := NewEngine(makePlayers([]int{0, 1}, 1000), 99, 50, 100); err == nil {
		t.Errorf("expected error for unknown dealer seat")
	}
}

func TestStartHeadsUpBlinds(t *testing.T) {
	players := makePlayers([]int{0, 1}, 1000)
	e, err := NewEngine(players, 0, 50, 100)
	if err != nil {
		t.Fatal(err)
	}
	mustStart(t, e)

	// Heads-up: dealer (seat 0) is SB, seat 1 is BB
	if players[0].BetThisRound != 50 {
		t.Errorf("seat 0 SB: bet=%d, want 50", players[0].BetThisRound)
	}
	if players[1].BetThisRound != 100 {
		t.Errorf("seat 1 BB: bet=%d, want 100", players[1].BetThisRound)
	}
	if e.Pot != 150 {
		t.Errorf("pot=%d, want 150", e.Pot)
	}
	// Heads-up: SB acts first preflop
	if e.ActiveSeat != 0 {
		t.Errorf("active=%d, want 0 (SB acts first heads-up)", e.ActiveSeat)
	}
	// Each player has 2 hole cards
	for _, p := range players {
		if len(p.HoleCards) != 2 {
			t.Errorf("seat %d hole=%d, want 2", p.Seat, len(p.HoleCards))
		}
	}
}

func TestStart6MaxBlinds(t *testing.T) {
	players := makePlayers([]int{0, 1, 2, 3, 4, 5}, 1000)
	e, err := NewEngine(players, 0, 50, 100)
	if err != nil {
		t.Fatal(err)
	}
	mustStart(t, e)
	// Dealer=0, SB=1, BB=2, UTG=3
	if players[1].BetThisRound != 50 {
		t.Errorf("SB seat 1 should post 50")
	}
	if players[2].BetThisRound != 100 {
		t.Errorf("BB seat 2 should post 100")
	}
	if e.ActiveSeat != 3 {
		t.Errorf("first-to-act=%d, want 3 (UTG)", e.ActiveSeat)
	}
}

func TestFoldAroundUncontestedWin(t *testing.T) {
	players := makePlayers([]int{0, 1, 2}, 1000)
	e, _ := NewEngine(players, 0, 50, 100)
	mustStart(t, e)
	// SB=1, BB=2, UTG=0. Everyone folds to BB.
	if _, err := e.Apply(0, Action{Type: ActionFold}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(1, Action{Type: ActionFold}); err != nil {
		t.Fatal(err)
	}
	// Hand should end uncontested with seat 2 winning the pot
	if e.Stage != StageHandComplete {
		// auto-end happens when countInHand drops to 1 and we call AdvanceStage
		// In our design, the engine sets ActiveSeat=-1 but doesn't auto-advance.
		// Caller needs to call AdvanceStage.
		if e.ActiveSeat != -1 {
			t.Errorf("expected betting to be closed, ActiveSeat=%d", e.ActiveSeat)
		}
		if _, err := e.AdvanceStage(); err != nil {
			t.Fatal(err)
		}
	}
	if e.Stage != StageHandComplete {
		t.Errorf("stage=%v, want HandComplete", e.Stage)
	}
	// Seat 2 should have starting chips - BB + entire pot (150)
	want := 1000 - 100 + 150
	if players[2].Chips != want {
		t.Errorf("BB chips=%d, want %d", players[2].Chips, want)
	}
}

func TestCheckedDownToShowdown(t *testing.T) {
	// 3 players limp/check to showdown — verify community cards dealt and pot distributed
	players := makePlayers([]int{0, 1, 2}, 1000)
	e, _ := NewEngine(players, 0, 50, 100)
	mustStart(t, e)
	// UTG=0 calls 100; SB=1 calls 50 more; BB=2 checks
	if _, err := e.Apply(0, Action{Type: ActionCall}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(1, Action{Type: ActionCall}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(2, Action{Type: ActionCheck}); err != nil {
		t.Fatal(err)
	}
	if e.ActiveSeat != -1 {
		t.Errorf("preflop should be closed, ActiveSeat=%d", e.ActiveSeat)
	}
	// Advance to flop
	if _, err := e.AdvanceStage(); err != nil {
		t.Fatal(err)
	}
	if e.Stage != StageFlop {
		t.Errorf("stage=%v, want Flop", e.Stage)
	}
	if len(e.Community) != 3 {
		t.Errorf("flop should be 3 cards, got %d", len(e.Community))
	}
	// Postflop first to act = SB (seat 1) — first Active clockwise from dealer 0
	if e.ActiveSeat != 1 {
		t.Errorf("flop first-to-act=%d, want 1", e.ActiveSeat)
	}
	// Check around
	for _, seat := range []int{1, 2, 0} {
		if _, err := e.Apply(seat, Action{Type: ActionCheck}); err != nil {
			t.Fatalf("check seat %d: %v", seat, err)
		}
	}
	// Turn
	if _, err := e.AdvanceStage(); err != nil {
		t.Fatal(err)
	}
	if e.Stage != StageTurn || len(e.Community) != 4 {
		t.Errorf("after turn: stage=%v community=%d", e.Stage, len(e.Community))
	}
	for _, seat := range []int{1, 2, 0} {
		if _, err := e.Apply(seat, Action{Type: ActionCheck}); err != nil {
			t.Fatal(err)
		}
	}
	// River
	if _, err := e.AdvanceStage(); err != nil {
		t.Fatal(err)
	}
	if e.Stage != StageRiver || len(e.Community) != 5 {
		t.Errorf("after river: stage=%v community=%d", e.Stage, len(e.Community))
	}
	for _, seat := range []int{1, 2, 0} {
		if _, err := e.Apply(seat, Action{Type: ActionCheck}); err != nil {
			t.Fatal(err)
		}
	}
	// Showdown
	if _, err := e.AdvanceStage(); err != nil {
		t.Fatal(err)
	}
	if e.Stage != StageHandComplete {
		t.Errorf("expected HandComplete, got %v", e.Stage)
	}
	totalChips := 0
	for _, p := range players {
		totalChips += p.Chips
	}
	if totalChips != 3000 {
		t.Errorf("total chips conserved: got %d, want 3000", totalChips)
	}
	if e.Pot != 0 {
		t.Errorf("pot should be empty, got %d", e.Pot)
	}
}

func TestRaiseReopensAction(t *testing.T) {
	players := makePlayers([]int{0, 1, 2}, 1000)
	e, _ := NewEngine(players, 0, 50, 100)
	mustStart(t, e)
	// UTG seat 0 raises to 300
	if _, err := e.Apply(0, Action{Type: ActionRaise, Amount: 300}); err != nil {
		t.Fatal(err)
	}
	if e.CurrentBet != 300 {
		t.Errorf("current bet=%d, want 300", e.CurrentBet)
	}
	if e.LastRaiseSize != 200 {
		t.Errorf("last raise size=%d, want 200", e.LastRaiseSize)
	}
	// SB seat 1's HasActed should be reset
	if players[1].HasActed {
		t.Errorf("SB HasActed should be false after UTG raise")
	}
	// SB calls 300 (puts in 250 more)
	if _, err := e.Apply(1, Action{Type: ActionCall}); err != nil {
		t.Fatal(err)
	}
	// BB re-raises to 800 (raise size 500 ≥ 200, so valid full raise)
	if _, err := e.Apply(2, Action{Type: ActionRaise, Amount: 800}); err != nil {
		t.Fatalf("BB raise: %v", err)
	}
	// UTG and SB should be reopened
	if players[0].HasActed {
		t.Errorf("UTG HasActed should be false after BB re-raise")
	}
	if players[1].HasActed {
		t.Errorf("SB HasActed should be false after BB re-raise")
	}
}

func TestMinRaiseRejectedUnlessAllIn(t *testing.T) {
	players := makePlayers([]int{0, 1, 2}, 1000)
	e, _ := NewEngine(players, 0, 50, 100)
	mustStart(t, e)
	// LastRaiseSize starts at BB=100 (preflop). UTG raise to 150 = +50 increment, below min 100.
	_, err := e.Apply(0, Action{Type: ActionRaise, Amount: 150})
	if err == nil {
		t.Errorf("expected min-raise rejection")
	}
}

func TestShortAllInRaiseAllowed(t *testing.T) {
	// UTG has only 150 chips, BB=100. UTG all-in for 150 = +50 raise (less than min 100),
	// allowed because it's all-in.
	players := []*EnginePlayer{
		{ID: "A", Seat: 0, Chips: 150},
		{ID: "B", Seat: 1, Chips: 1000},
		{ID: "C", Seat: 2, Chips: 1000},
	}
	e, _ := NewEngine(players, 0, 50, 100)
	mustStart(t, e)
	// UTG (seat 0) all-in 150
	if _, err := e.Apply(0, Action{Type: ActionAllIn}); err != nil {
		t.Fatalf("UTG all-in: %v", err)
	}
	if players[0].State != PlayerAllIn {
		t.Errorf("UTG state=%v, want AllIn", players[0].State)
	}
	if e.CurrentBet != 150 {
		t.Errorf("current bet=%d, want 150", e.CurrentBet)
	}
	// Short all-in does not reset LastRaiseSize (stays 100)
	if e.LastRaiseSize != 100 {
		t.Errorf("LastRaiseSize=%d, want 100 (short all-in shouldn't reset)", e.LastRaiseSize)
	}
}

func TestNotYourTurn(t *testing.T) {
	players := makePlayers([]int{0, 1, 2}, 1000)
	e, _ := NewEngine(players, 0, 50, 100)
	mustStart(t, e)
	// UTG=0 to act; seat 1 tries to act
	_, err := e.Apply(1, Action{Type: ActionFold})
	if err == nil {
		t.Errorf("expected not-your-turn error")
	}
}

func TestCheckRequiresMatchedBet(t *testing.T) {
	players := makePlayers([]int{0, 1, 2}, 1000)
	e, _ := NewEngine(players, 0, 50, 100)
	mustStart(t, e)
	// UTG owes 100 to call BB; check should fail
	_, err := e.Apply(0, Action{Type: ActionCheck})
	if err == nil {
		t.Errorf("expected check rejected (must call 100)")
	}
}

func TestAllInSidePotShowdown(t *testing.T) {
	// 3 players, A short-stack: A=200, B=1000, C=1000.
	// A all-in preflop, B and C call.
	// Postflop B raises, C calls. Goes to showdown.
	// Verify chip conservation and pot distribution.
	players := []*EnginePlayer{
		{ID: "A", Seat: 0, Chips: 200},
		{ID: "B", Seat: 1, Chips: 1000},
		{ID: "C", Seat: 2, Chips: 1000},
	}
	e, _ := NewEngine(players, 0, 50, 100)
	mustStart(t, e)
	// Preflop: SB=1 posted 50, BB=2 posted 100, UTG=A=0 to act
	// A all-in 200
	if _, err := e.Apply(0, Action{Type: ActionAllIn}); err != nil {
		t.Fatal(err)
	}
	// B calls 200 (puts in 150 more)
	if _, err := e.Apply(1, Action{Type: ActionCall}); err != nil {
		t.Fatal(err)
	}
	// C calls 200 (puts in 100 more)
	if _, err := e.Apply(2, Action{Type: ActionCall}); err != nil {
		t.Fatal(err)
	}
	// Preflop closed; advance through all streets (B,C still active)
	if _, err := e.AdvanceStage(); err != nil {
		t.Fatal(err)
	}
	// Flop: B,C check (A is all-in, no action)
	for _, seat := range []int{1, 2} {
		if _, err := e.Apply(seat, Action{Type: ActionCheck}); err != nil {
			t.Fatalf("check seat %d at flop: %v", seat, err)
		}
	}
	if _, err := e.AdvanceStage(); err != nil {
		t.Fatal(err)
	}
	// Turn check
	for _, seat := range []int{1, 2} {
		if _, err := e.Apply(seat, Action{Type: ActionCheck}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.AdvanceStage(); err != nil {
		t.Fatal(err)
	}
	// River check
	for _, seat := range []int{1, 2} {
		if _, err := e.Apply(seat, Action{Type: ActionCheck}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.AdvanceStage(); err != nil {
		t.Fatal(err)
	}
	if e.Stage != StageHandComplete {
		t.Errorf("stage=%v, want HandComplete", e.Stage)
	}
	total := players[0].Chips + players[1].Chips + players[2].Chips
	if total != 2200 {
		t.Errorf("chip conservation: total=%d, want 2200", total)
	}
	if e.Pot != 0 {
		t.Errorf("pot should be empty, got %d", e.Pot)
	}
}

func TestEveryoneAllInFastForward(t *testing.T) {
	// 2 players both all-in preflop. Engine should auto-deal flop/turn/river and showdown.
	players := []*EnginePlayer{
		{ID: "A", Seat: 0, Chips: 500},
		{ID: "B", Seat: 1, Chips: 500},
	}
	e, _ := NewEngine(players, 0, 50, 100)
	mustStart(t, e)
	// Heads-up: SB=A acts first
	if _, err := e.Apply(0, Action{Type: ActionAllIn}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(1, Action{Type: ActionCall}); err != nil {
		t.Fatal(err)
	}
	// Both all-in. ActiveSeat should be -1.
	if e.ActiveSeat != -1 {
		t.Errorf("ActiveSeat=%d, want -1", e.ActiveSeat)
	}
	// AdvanceStage should fast-forward to showdown
	if _, err := e.AdvanceStage(); err != nil {
		t.Fatal(err)
	}
	if e.Stage != StageHandComplete {
		t.Errorf("stage=%v, want HandComplete", e.Stage)
	}
	if len(e.Community) != 5 {
		t.Errorf("community=%d, want 5", len(e.Community))
	}
	total := players[0].Chips + players[1].Chips
	if total != 1000 {
		t.Errorf("chip conservation: total=%d, want 1000", total)
	}
}
