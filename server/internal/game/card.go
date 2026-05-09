package game

import (
	"encoding/json"
	"fmt"
)

type Suit uint8

const (
	Spade Suit = iota
	Heart
	Diamond
	Club
)

func (s Suit) String() string {
	switch s {
	case Spade:
		return "♠"
	case Heart:
		return "♥"
	case Diamond:
		return "♦"
	case Club:
		return "♣"
	}
	return "?"
}

type Rank uint8

const (
	Two   Rank = 2
	Three Rank = 3
	Four  Rank = 4
	Five  Rank = 5
	Six   Rank = 6
	Seven Rank = 7
	Eight Rank = 8
	Nine  Rank = 9
	Ten   Rank = 10
	Jack  Rank = 11
	Queen Rank = 12
	King  Rank = 13
	Ace   Rank = 14
)

func (r Rank) String() string {
	switch r {
	case Ten:
		return "T"
	case Jack:
		return "J"
	case Queen:
		return "Q"
	case King:
		return "K"
	case Ace:
		return "A"
	default:
		if r >= Two && r <= Nine {
			return fmt.Sprintf("%d", r)
		}
	}
	return "?"
}

type Card struct {
	Suit Suit
	Rank Rank
}

func (c Card) String() string {
	return c.Rank.String() + c.Suit.String()
}

// SuitSlug returns the JSON-friendly suit name used over the wire.
func (s Suit) SuitSlug() string {
	switch s {
	case Spade:
		return "spade"
	case Heart:
		return "heart"
	case Diamond:
		return "diamond"
	case Club:
		return "club"
	}
	return ""
}

// RankSlug returns the JSON-friendly rank label. Unlike String(), Ten is "10"
// (not "T") to match what the front-end renders.
func (r Rank) RankSlug() string {
	switch r {
	case Jack:
		return "J"
	case Queen:
		return "Q"
	case King:
		return "K"
	case Ace:
		return "A"
	}
	if r >= Two && r <= Ten {
		return fmt.Sprintf("%d", int(r))
	}
	return ""
}

// MarshalJSON serializes a Card as {"suit":"spade","rank":"A"}.
func (c Card) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Suit string `json:"suit"`
		Rank string `json:"rank"`
	}{
		Suit: c.Suit.SuitSlug(),
		Rank: c.Rank.RankSlug(),
	})
}

// UnmarshalJSON parses a Card from {"suit":"spade","rank":"A"}.
func (c *Card) UnmarshalJSON(data []byte) error {
	var raw struct {
		Suit string `json:"suit"`
		Rank string `json:"rank"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	switch raw.Suit {
	case "spade":
		c.Suit = Spade
	case "heart":
		c.Suit = Heart
	case "diamond":
		c.Suit = Diamond
	case "club":
		c.Suit = Club
	default:
		return fmt.Errorf("unknown suit %q", raw.Suit)
	}
	switch raw.Rank {
	case "J":
		c.Rank = Jack
	case "Q":
		c.Rank = Queen
	case "K":
		c.Rank = King
	case "A":
		c.Rank = Ace
	default:
		var n int
		if _, err := fmt.Sscanf(raw.Rank, "%d", &n); err != nil || n < 2 || n > 10 {
			return fmt.Errorf("unknown rank %q", raw.Rank)
		}
		c.Rank = Rank(n)
	}
	return nil
}
