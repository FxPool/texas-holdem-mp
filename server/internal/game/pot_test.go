package game

import "testing"

// helper: HandValue with given rank + first tiebreaker
func hv(rank HandRank, top Rank) HandValue {
	return HandValue{Rank: rank, Tiebreakers: [5]Rank{top}}
}

// aggregate shares per player for assertions
func sumByPlayer(shares []PotShare) map[string]int {
	m := map[string]int{}
	for _, s := range shares {
		m[s.PlayerID] += s.Amount
	}
	return m
}

func TestSinglePotSingleWinner(t *testing.T) {
	contribs := []PotContribution{
		{PlayerID: "A", Committed: 100, Hand: hv(Flush, Ace)},
		{PlayerID: "B", Committed: 100, Hand: hv(Straight, King)},
		{PlayerID: "C", Committed: 100, Hand: hv(OnePair, Queen)},
	}
	shares, err := DistributePots(contribs)
	if err != nil {
		t.Fatal(err)
	}
	got := sumByPlayer(shares)
	if got["A"] != 300 || got["B"] != 0 || got["C"] != 0 {
		t.Errorf("want A=300 B=0 C=0, got %v", got)
	}
}

func TestSplitPotEvenly(t *testing.T) {
	// Two equal hands split 200 evenly = 100 each
	contribs := []PotContribution{
		{PlayerID: "A", Committed: 100, Hand: hv(Flush, Ace)},
		{PlayerID: "B", Committed: 100, Hand: hv(Flush, Ace)},
	}
	shares, err := DistributePots(contribs)
	if err != nil {
		t.Fatal(err)
	}
	got := sumByPlayer(shares)
	if got["A"] != 100 || got["B"] != 100 {
		t.Errorf("want 100/100, got %v", got)
	}
}

func TestSplitPotRemainder(t *testing.T) {
	// 3-way tie on a 100 pot → 34/33/33; remainder goes to first input order
	contribs := []PotContribution{
		{PlayerID: "A", Committed: 33, Hand: hv(Flush, Ace)},
		{PlayerID: "B", Committed: 33, Hand: hv(Flush, Ace)},
		{PlayerID: "C", Committed: 34, Hand: hv(Flush, Ace)},
	}
	shares, err := DistributePots(contribs)
	if err != nil {
		t.Fatal(err)
	}
	got := sumByPlayer(shares)
	total := got["A"] + got["B"] + got["C"]
	if total != 100 {
		t.Errorf("total should be 100, got %d (breakdown %v)", total, got)
	}
}

func TestFoldedPlayerContributesNotEligible(t *testing.T) {
	// B folded but had committed 50; pot includes B's 50
	contribs := []PotContribution{
		{PlayerID: "A", Committed: 100, Hand: hv(OnePair, Two)},
		{PlayerID: "B", Committed: 50, Folded: true},
		{PlayerID: "C", Committed: 100, Hand: hv(HighCard, Three)},
	}
	shares, err := DistributePots(contribs)
	if err != nil {
		t.Fatal(err)
	}
	got := sumByPlayer(shares)
	// Total committed = 250. A wins.
	if got["A"] != 250 {
		t.Errorf("A should win all 250, got %v", got)
	}
	if got["B"] != 0 {
		t.Errorf("B folded should get 0, got %d", got["B"])
	}
}

func TestSidePotAllIn(t *testing.T) {
	// A all-in 50, B and C each committed 200.
	// Main pot: 50*3 = 150 (A,B,C eligible)
	// Side pot: 150*2 = 300 (only B,C eligible)
	// A has best hand → wins main pot 150. B has 2nd best → wins side 300.
	contribs := []PotContribution{
		{PlayerID: "A", Committed: 50, Hand: hv(Flush, Ace)},      // best
		{PlayerID: "B", Committed: 200, Hand: hv(Straight, King)}, // 2nd
		{PlayerID: "C", Committed: 200, Hand: hv(OnePair, Two)},   // worst
	}
	shares, err := DistributePots(contribs)
	if err != nil {
		t.Fatal(err)
	}
	got := sumByPlayer(shares)
	if got["A"] != 150 {
		t.Errorf("A should win main pot 150, got %d", got["A"])
	}
	if got["B"] != 300 {
		t.Errorf("B should win side pot 300, got %d", got["B"])
	}
	if got["C"] != 0 {
		t.Errorf("C should win 0, got %d", got["C"])
	}
}

func TestMultipleSidePots(t *testing.T) {
	// Three all-ins at different levels:
	// A all-in 50 (best hand)
	// B all-in 100 (2nd)
	// C committed 200 (3rd)
	// Pot 1 (level 50): 50*3 = 150, eligible A,B,C → A wins 150
	// Pot 2 (level 50→100, delta 50): 50*2 = 100, eligible B,C → B wins 100
	// Pot 3 (level 100→200, delta 100): 100, eligible C → C wins 100
	contribs := []PotContribution{
		{PlayerID: "A", Committed: 50, Hand: hv(Flush, Ace)},
		{PlayerID: "B", Committed: 100, Hand: hv(Straight, King)},
		{PlayerID: "C", Committed: 200, Hand: hv(OnePair, Two)},
	}
	shares, err := DistributePots(contribs)
	if err != nil {
		t.Fatal(err)
	}
	got := sumByPlayer(shares)
	if got["A"] != 150 {
		t.Errorf("A=%d, want 150", got["A"])
	}
	if got["B"] != 100 {
		t.Errorf("B=%d, want 100", got["B"])
	}
	if got["C"] != 100 {
		t.Errorf("C=%d, want 100", got["C"])
	}
	total := got["A"] + got["B"] + got["C"]
	if total != 350 {
		t.Errorf("total=%d, want 350", total)
	}
}

func TestZeroCommittedIgnored(t *testing.T) {
	contribs := []PotContribution{
		{PlayerID: "A", Committed: 0},
		{PlayerID: "B", Committed: 100, Hand: hv(OnePair, Two)},
		{PlayerID: "C", Committed: 100, Hand: hv(HighCard, Three)},
	}
	shares, err := DistributePots(contribs)
	if err != nil {
		t.Fatal(err)
	}
	got := sumByPlayer(shares)
	if got["B"] != 200 {
		t.Errorf("B should win 200, got %d", got["B"])
	}
}

func TestNegativeCommittedReturnsError(t *testing.T) {
	contribs := []PotContribution{
		{PlayerID: "A", Committed: -1, Hand: hv(Flush, Ace)},
	}
	_, err := DistributePots(contribs)
	if err == nil {
		t.Errorf("expected error for negative Committed")
	}
}
