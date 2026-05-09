package game

import "testing"

func TestCardString(t *testing.T) {
	cases := []struct {
		card Card
		want string
	}{
		{Card{Spade, Ace}, "A♠"},
		{Card{Heart, King}, "K♥"},
		{Card{Diamond, Ten}, "T♦"},
		{Card{Club, Two}, "2♣"},
		{Card{Spade, Nine}, "9♠"},
	}
	for _, c := range cases {
		if got := c.card.String(); got != c.want {
			t.Errorf("Card{%v,%v}.String() = %q, want %q", c.card.Suit, c.card.Rank, got, c.want)
		}
	}
}
