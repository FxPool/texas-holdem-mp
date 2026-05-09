package game

import "testing"

// helper to build cards from compact "Ah Kd 5s ..." notation
func cs(spec ...string) []Card {
	out := make([]Card, len(spec))
	for i, s := range spec {
		out[i] = parseCard(s)
	}
	return out
}

func parseCard(s string) Card {
	if len(s) < 2 || len(s) > 3 {
		panic("bad card: " + s)
	}
	rankStr := s[:len(s)-1]
	suitStr := s[len(s)-1:]
	var r Rank
	switch rankStr {
	case "2":
		r = Two
	case "3":
		r = Three
	case "4":
		r = Four
	case "5":
		r = Five
	case "6":
		r = Six
	case "7":
		r = Seven
	case "8":
		r = Eight
	case "9":
		r = Nine
	case "T":
		r = Ten
	case "J":
		r = Jack
	case "Q":
		r = Queen
	case "K":
		r = King
	case "A":
		r = Ace
	default:
		panic("bad rank: " + rankStr)
	}
	var su Suit
	switch suitStr {
	case "s":
		su = Spade
	case "h":
		su = Heart
	case "d":
		su = Diamond
	case "c":
		su = Club
	default:
		panic("bad suit: " + suitStr)
	}
	return Card{Suit: su, Rank: r}
}

func TestEvaluate5_AllRanks(t *testing.T) {
	cases := []struct {
		name string
		in   []Card
		want HandRank
	}{
		{"high-card", cs("Ah", "Kc", "9d", "5s", "2c"), HighCard},
		{"one-pair", cs("Ah", "Ac", "9d", "5s", "2c"), OnePair},
		{"two-pair", cs("Ah", "Ac", "9d", "9s", "2c"), TwoPair},
		{"three-of-a-kind", cs("Ah", "Ac", "Ad", "9s", "2c"), ThreeOfAKind},
		{"straight-broadway", cs("Ah", "Kc", "Qd", "Js", "Tc"), Straight},
		{"straight-mid", cs("9h", "8c", "7d", "6s", "5c"), Straight},
		{"wheel-straight", cs("Ah", "5c", "4d", "3s", "2c"), Straight},
		{"flush", cs("Ah", "Kh", "9h", "5h", "2h"), Flush},
		{"full-house", cs("Ah", "Ac", "Ad", "9s", "9c"), FullHouse},
		{"four-of-a-kind", cs("Ah", "Ac", "Ad", "As", "9c"), FourOfAKind},
		{"straight-flush", cs("9s", "8s", "7s", "6s", "5s"), StraightFlush},
		{"royal-flush", cs("As", "Ks", "Qs", "Js", "Ts"), StraightFlush},
		{"steel-wheel", cs("As", "5s", "4s", "3s", "2s"), StraightFlush},
	}
	for _, c := range cases {
		got := evaluate5(c.in)
		if got.Rank != c.want {
			t.Errorf("%s: got %v, want %v (full=%v)", c.name, got.Rank, c.want, got)
		}
	}
}

func TestWheelTiebreaker(t *testing.T) {
	wheel := evaluate5(cs("Ah", "5c", "4d", "3s", "2c"))
	if wheel.Tiebreakers[0] != Five {
		t.Errorf("wheel high should be 5, got %v", wheel.Tiebreakers[0])
	}
	// 6-high straight beats wheel
	six := evaluate5(cs("6h", "5c", "4d", "3s", "2c"))
	if six.Compare(wheel) <= 0 {
		t.Errorf("6-high straight should beat wheel; got cmp=%d", six.Compare(wheel))
	}
}

func TestStraightFlushBeatsFour(t *testing.T) {
	sf := evaluate5(cs("9s", "8s", "7s", "6s", "5s"))
	four := evaluate5(cs("Ah", "Ac", "Ad", "As", "Kc"))
	if sf.Compare(four) <= 0 {
		t.Errorf("straight flush should beat four of a kind")
	}
}

func TestFlushKickers(t *testing.T) {
	a := evaluate5(cs("Ah", "Kh", "9h", "5h", "2h"))
	b := evaluate5(cs("Ah", "Qh", "9h", "5h", "2h"))
	if a.Compare(b) != 1 {
		t.Errorf("Ace-King flush should beat Ace-Queen flush")
	}
}

func TestFullHouseTrips(t *testing.T) {
	// Aces full of 2s vs Kings full of Aces — Aces full wins.
	a := evaluate5(cs("Ah", "As", "Ad", "2s", "2c"))
	b := evaluate5(cs("Kh", "Ks", "Kd", "Ah", "Ac"))
	if a.Compare(b) != 1 {
		t.Errorf("Aces full of 2s should beat Kings full of Aces")
	}
}

func TestTwoPairKicker(t *testing.T) {
	a := evaluate5(cs("Ah", "Ac", "9d", "9s", "Kc")) // AA99 K
	b := evaluate5(cs("Ah", "Ac", "9d", "9s", "Qc")) // AA99 Q
	if a.Compare(b) != 1 {
		t.Errorf("AA99K should beat AA99Q")
	}
}

func TestExactTie(t *testing.T) {
	a := evaluate5(cs("Ah", "Kc", "Qd", "Js", "Tc"))
	b := evaluate5(cs("As", "Kd", "Qh", "Jc", "Td"))
	if a.Compare(b) != 0 {
		t.Errorf("two broadway straights of mixed suits should tie; got %d", a.Compare(b))
	}
}

func TestBest5From7_Hold(t *testing.T) {
	// Hole: A♠ A♥; Board: A♣ K♠ K♥ 2♦ 3♦ → Aces full of Kings
	cards := cs("As", "Ah", "Ac", "Ks", "Kh", "2d", "3d")
	got := EvaluateBest5(cards)
	if got.Rank != FullHouse {
		t.Errorf("expected FullHouse, got %v", got.Rank)
	}
	if got.Tiebreakers[0] != Ace || got.Tiebreakers[1] != King {
		t.Errorf("expected Aces full of Kings, got %v", got.Tiebreakers)
	}
}

func TestBest5From7_PicksFlushOverStraight(t *testing.T) {
	// 7 cards where there's both a straight and a flush available.
	// Board: 9♥ 8♥ 7♥ 2♣ 3♦; Hole: 6♥ 5♣
	// Possible: 9♥8♥7♥6♥? — no, 6 is club. So flush is just 9876? Need 5 hearts.
	// Let's set: cards = 9♥ 8♥ 7♥ 6♥ 5♥ 4♣ 3♣ — straight flush 9-high.
	cards := cs("9h", "8h", "7h", "6h", "5h", "4c", "3c")
	got := EvaluateBest5(cards)
	if got.Rank != StraightFlush {
		t.Errorf("expected StraightFlush, got %v", got.Rank)
	}
	if got.Tiebreakers[0] != Nine {
		t.Errorf("expected 9-high straight flush, got %v", got.Tiebreakers[0])
	}
}

func TestBest5From7_PrefersHigherKickers(t *testing.T) {
	// Pair of Aces with various kickers. Best 5 should pick top 3 kickers.
	cards := cs("As", "Ah", "Kc", "Qd", "Js", "9c", "2d")
	got := EvaluateBest5(cards)
	if got.Rank != OnePair {
		t.Errorf("expected OnePair, got %v", got.Rank)
	}
	if got.Tiebreakers[0] != Ace || got.Tiebreakers[1] != King ||
		got.Tiebreakers[2] != Queen || got.Tiebreakers[3] != Jack {
		t.Errorf("expected pair Aces with KQJ kickers, got %v", got.Tiebreakers)
	}
}
