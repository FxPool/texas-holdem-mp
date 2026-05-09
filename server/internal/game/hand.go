package game

import (
	"fmt"
	"sort"
)

type HandRank int

const (
	HighCard HandRank = iota
	OnePair
	TwoPair
	ThreeOfAKind
	Straight
	Flush
	FullHouse
	FourOfAKind
	StraightFlush
)

func (r HandRank) String() string {
	switch r {
	case HighCard:
		return "HighCard"
	case OnePair:
		return "OnePair"
	case TwoPair:
		return "TwoPair"
	case ThreeOfAKind:
		return "ThreeOfAKind"
	case Straight:
		return "Straight"
	case Flush:
		return "Flush"
	case FullHouse:
		return "FullHouse"
	case FourOfAKind:
		return "FourOfAKind"
	case StraightFlush:
		return "StraightFlush"
	}
	return "?"
}

// Slug returns the wire-friendly hand-rank name (kebab-case).
func (r HandRank) Slug() string {
	switch r {
	case HighCard:
		return "high-card"
	case OnePair:
		return "one-pair"
	case TwoPair:
		return "two-pair"
	case ThreeOfAKind:
		return "three-of-a-kind"
	case Straight:
		return "straight"
	case Flush:
		return "flush"
	case FullHouse:
		return "full-house"
	case FourOfAKind:
		return "four-of-a-kind"
	case StraightFlush:
		return "straight-flush"
	}
	return "?"
}

// HandValue is comparable: higher Rank wins; on tie, lex-compare Tiebreakers.
// Tiebreakers are stored most-significant-first; unused slots are 0 and compare equal.
type HandValue struct {
	Rank        HandRank
	Tiebreakers [5]Rank
}

func (a HandValue) String() string {
	return fmt.Sprintf("%s%v", a.Rank, a.Tiebreakers)
}

// Compare returns 1 if a beats b, -1 if a loses to b, 0 on exact tie.
func (a HandValue) Compare(b HandValue) int {
	if a.Rank != b.Rank {
		if a.Rank > b.Rank {
			return 1
		}
		return -1
	}
	for i := 0; i < 5; i++ {
		if a.Tiebreakers[i] != b.Tiebreakers[i] {
			if a.Tiebreakers[i] > b.Tiebreakers[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

// EvaluateBest5 picks the best 5-card hand from 5..7 input cards.
// Texas Hold'em: pass 7 (2 hole + 5 community).
func EvaluateBest5(cards []Card) HandValue {
	if len(cards) < 5 {
		panic(fmt.Sprintf("EvaluateBest5 needs >= 5 cards, got %d", len(cards)))
	}
	if len(cards) == 5 {
		return evaluate5(cards)
	}
	n := len(cards)
	var best HandValue
	first := true
	pick := make([]Card, 5)
	var rec func(start, depth int)
	rec = func(start, depth int) {
		if depth == 5 {
			v := evaluate5(pick)
			if first || v.Compare(best) > 0 {
				best = v
				first = false
			}
			return
		}
		for i := start; i <= n-(5-depth); i++ {
			pick[depth] = cards[i]
			rec(i+1, depth+1)
		}
	}
	rec(0, 0)
	return best
}

// evaluate5 evaluates exactly 5 cards.
func evaluate5(cards []Card) HandValue {
	if len(cards) != 5 {
		panic(fmt.Sprintf("evaluate5 needs exactly 5 cards, got %d", len(cards)))
	}

	ranks := []Rank{cards[0].Rank, cards[1].Rank, cards[2].Rank, cards[3].Rank, cards[4].Rank}
	sort.Slice(ranks, func(i, j int) bool { return ranks[i] > ranks[j] })

	suit0 := cards[0].Suit
	isFlush := true
	for i := 1; i < 5; i++ {
		if cards[i].Suit != suit0 {
			isFlush = false
			break
		}
	}

	isStraight, straightHigh := checkStraight(ranks)

	// Group ranks by count, sorted by (count desc, rank desc).
	type group struct {
		rank  Rank
		count int
	}
	counts := map[Rank]int{}
	for _, r := range ranks {
		counts[r]++
	}
	groups := make([]group, 0, len(counts))
	for r, c := range counts {
		groups = append(groups, group{r, c})
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].count != groups[j].count {
			return groups[i].count > groups[j].count
		}
		return groups[i].rank > groups[j].rank
	})

	switch {
	case isFlush && isStraight:
		return HandValue{Rank: StraightFlush, Tiebreakers: [5]Rank{straightHigh}}
	case groups[0].count == 4:
		return HandValue{Rank: FourOfAKind, Tiebreakers: [5]Rank{groups[0].rank, groups[1].rank}}
	case groups[0].count == 3 && groups[1].count == 2:
		return HandValue{Rank: FullHouse, Tiebreakers: [5]Rank{groups[0].rank, groups[1].rank}}
	case isFlush:
		return HandValue{Rank: Flush, Tiebreakers: [5]Rank{ranks[0], ranks[1], ranks[2], ranks[3], ranks[4]}}
	case isStraight:
		return HandValue{Rank: Straight, Tiebreakers: [5]Rank{straightHigh}}
	case groups[0].count == 3:
		return HandValue{Rank: ThreeOfAKind, Tiebreakers: [5]Rank{groups[0].rank, groups[1].rank, groups[2].rank}}
	case groups[0].count == 2 && groups[1].count == 2:
		// Two pair: bigger pair, smaller pair, kicker
		return HandValue{Rank: TwoPair, Tiebreakers: [5]Rank{groups[0].rank, groups[1].rank, groups[2].rank}}
	case groups[0].count == 2:
		// One pair: pair rank + 3 kickers descending
		return HandValue{Rank: OnePair, Tiebreakers: [5]Rank{groups[0].rank, groups[1].rank, groups[2].rank, groups[3].rank}}
	default:
		return HandValue{Rank: HighCard, Tiebreakers: [5]Rank{ranks[0], ranks[1], ranks[2], ranks[3], ranks[4]}}
	}
}

// checkStraight reports whether the (descending-sorted) ranks form a straight,
// returning the high-card rank. Handles the wheel (A-5-4-3-2 → Five-high).
func checkStraight(ranks []Rank) (bool, Rank) {
	if len(ranks) != 5 {
		return false, 0
	}
	standard := true
	for i := 1; i < 5; i++ {
		if ranks[i-1] != ranks[i]+1 {
			standard = false
			break
		}
	}
	if standard {
		return true, ranks[0]
	}
	// Wheel: A,5,4,3,2 sorted desc = [A,5,4,3,2]
	if ranks[0] == Ace && ranks[1] == Five && ranks[2] == Four && ranks[3] == Three && ranks[4] == Two {
		return true, Five
	}
	return false, 0
}
