package game

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

type Deck struct {
	cards []Card
}

func NewDeck() *Deck {
	cards := make([]Card, 0, 52)
	for s := Spade; s <= Club; s++ {
		for r := Two; r <= Ace; r++ {
			cards = append(cards, Card{Suit: s, Rank: r})
		}
	}
	return &Deck{cards: cards}
}

// Shuffle uses crypto/rand for an unbiased Fisher-Yates shuffle.
func (d *Deck) Shuffle() error {
	n := len(d.cards)
	for i := n - 1; i > 0; i-- {
		j, err := unbiasedIntn(i + 1)
		if err != nil {
			return err
		}
		d.cards[i], d.cards[j] = d.cards[j], d.cards[i]
	}
	return nil
}

// Deal pops the top n cards. Returns error if not enough remaining.
func (d *Deck) Deal(n int) ([]Card, error) {
	if n < 0 {
		return nil, fmt.Errorf("deal n must be >= 0, got %d", n)
	}
	if n > len(d.cards) {
		return nil, fmt.Errorf("not enough cards: want %d, have %d", n, len(d.cards))
	}
	out := make([]Card, n)
	copy(out, d.cards[:n])
	d.cards = d.cards[n:]
	return out, nil
}

func (d *Deck) Remaining() int {
	return len(d.cards)
}

// unbiasedIntn returns a uniformly random integer in [0, n).
// crypto/rand.Int avoids modulo bias inherent in (rand.Read % n).
func unbiasedIntn(n int) (int, error) {
	if n <= 0 {
		return 0, fmt.Errorf("invalid n: %d", n)
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, err
	}
	return int(v.Int64()), nil
}
