package game

import (
	"encoding/json"
	"testing"
)

func TestCardMarshalJSON(t *testing.T) {
	cases := []struct {
		card Card
		want string
	}{
		{Card{Spade, Ace}, `{"suit":"spade","rank":"A"}`},
		{Card{Heart, Ten}, `{"suit":"heart","rank":"10"}`},
		{Card{Diamond, Two}, `{"suit":"diamond","rank":"2"}`},
		{Card{Club, King}, `{"suit":"club","rank":"K"}`},
		{Card{Heart, Jack}, `{"suit":"heart","rank":"J"}`},
	}
	for _, c := range cases {
		b, err := json.Marshal(c.card)
		if err != nil {
			t.Errorf("marshal %v: %v", c.card, err)
			continue
		}
		if got := string(b); got != c.want {
			t.Errorf("Card{%v,%v}: got %s, want %s", c.card.Suit, c.card.Rank, got, c.want)
		}
	}
}

func TestStageSlug(t *testing.T) {
	cases := []struct {
		stage Stage
		want  string
	}{
		{StagePreflop, "preflop"},
		{StageFlop, "flop"},
		{StageTurn, "turn"},
		{StageRiver, "river"},
		{StageShowdown, "showdown"},
		{StageHandComplete, "hand-complete"},
	}
	for _, c := range cases {
		if got := c.stage.Slug(); got != c.want {
			t.Errorf("Stage(%v).Slug() = %s, want %s", c.stage, got, c.want)
		}
	}
}

func TestPlayerStateSlug(t *testing.T) {
	cases := []struct {
		state PlayerState
		want  string
	}{
		{PlayerActive, "active"},
		{PlayerFolded, "folded"},
		{PlayerAllIn, "all-in"},
		{PlayerSitOut, "sit-out"},
	}
	for _, c := range cases {
		if got := c.state.Slug(); got != c.want {
			t.Errorf("PlayerState(%v).Slug() = %s, want %s", c.state, got, c.want)
		}
	}
}
