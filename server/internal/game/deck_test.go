package game

import (
	"testing"
)

func TestNewDeck52Unique(t *testing.T) {
	d := NewDeck()
	if d.Remaining() != 52 {
		t.Fatalf("new deck has %d cards, want 52", d.Remaining())
	}
	seen := map[Card]bool{}
	for _, c := range d.cards {
		if seen[c] {
			t.Errorf("duplicate card %v", c)
		}
		seen[c] = true
	}
	if len(seen) != 52 {
		t.Errorf("only %d unique cards", len(seen))
	}
}

func TestShufflePreservesCards(t *testing.T) {
	d := NewDeck()
	if err := d.Shuffle(); err != nil {
		t.Fatalf("shuffle err: %v", err)
	}
	if d.Remaining() != 52 {
		t.Errorf("shuffle changed deck size to %d", d.Remaining())
	}
	seen := map[Card]bool{}
	for _, c := range d.cards {
		if seen[c] {
			t.Errorf("duplicate card after shuffle: %v", c)
		}
		seen[c] = true
	}
}

func TestShuffleProducesDifferentOrders(t *testing.T) {
	a := NewDeck()
	b := NewDeck()
	if err := a.Shuffle(); err != nil {
		t.Fatal(err)
	}
	if err := b.Shuffle(); err != nil {
		t.Fatal(err)
	}
	// Probability that two crypto-random shuffles match exactly is ~1/52!.
	same := true
	for i := range a.cards {
		if a.cards[i] != b.cards[i] {
			same = false
			break
		}
	}
	if same {
		t.Errorf("two shuffles produced identical orders (astronomically unlikely; check entropy source)")
	}
}

func TestDeal(t *testing.T) {
	d := NewDeck()
	out, err := d.Deal(5)
	if err != nil {
		t.Fatalf("deal err: %v", err)
	}
	if len(out) != 5 {
		t.Errorf("dealt %d, want 5", len(out))
	}
	if d.Remaining() != 47 {
		t.Errorf("remaining %d, want 47", d.Remaining())
	}
	// Dealt cards should not appear in remaining
	dealt := map[Card]bool{}
	for _, c := range out {
		dealt[c] = true
	}
	for _, c := range d.cards {
		if dealt[c] {
			t.Errorf("dealt card %v still in deck", c)
		}
	}
}

func TestDealTooMany(t *testing.T) {
	d := NewDeck()
	_, err := d.Deal(53)
	if err == nil {
		t.Errorf("expected error dealing 53 from 52-card deck")
	}
}

func TestUnbiasedIntn(t *testing.T) {
	// Sample many times, check distribution roughly uniform across 0..n-1
	const n = 6
	const trials = 6000
	counts := make([]int, n)
	for i := 0; i < trials; i++ {
		v, err := unbiasedIntn(n)
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if v < 0 || v >= n {
			t.Fatalf("out of range: %d", v)
		}
		counts[v]++
	}
	// Expected ~1000 each; 3-sigma binomial std ≈ sqrt(1000*5/6) ≈ 28.9; allow 200 slack.
	for i, c := range counts {
		if c < 800 || c > 1200 {
			t.Errorf("bucket %d count %d outside [800,1200]; distribution looks biased", i, c)
		}
	}
}
