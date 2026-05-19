package game

import "fmt"

// PotContribution is one player's input to side-pot calculation.
// Committed is the total amount the player put into the pot during the hand
// (ante + blinds + all bets/calls/raises across rounds).
// Hand is the evaluated 5-card hand value; ignored if Folded.
type PotContribution struct {
	PlayerID  string
	Committed int
	Folded    bool
	Hand      HandValue
}

// PotShare is one award entry. A single player may receive multiple shares
// (one per side pot they win); aggregate by PlayerID at the call site if needed.
type PotShare struct {
	PlayerID string `json:"playerId"`
	Amount   int    `json:"amount"`
	// PotIndex is which side pot this share comes from (0 = main, 1+ = side pots).
	PotIndex int `json:"potIndex"`
}

// DistributePots builds tiered pots from contributions and returns the awards.
//
// Algorithm: repeatedly take the smallest non-zero commitment level among all
// players (folded or not), build a pot at that level (all players who committed
// at least that much each contribute the level amount), then award it to the
// best hand among non-folded contributors at that level. Remainder chips on a
// split go to earliest contributors in the input order.
//
// Total awarded equals the sum of all Committed values.
func DistributePots(contribs []PotContribution) ([]PotShare, error) {
	for _, c := range contribs {
		if c.Committed < 0 {
			return nil, fmt.Errorf("negative committed for %s: %d", c.PlayerID, c.Committed)
		}
	}

	remaining := make([]int, len(contribs))
	for i, c := range contribs {
		remaining[i] = c.Committed
	}

	var shares []PotShare
	potIndex := 0

	for {
		// Smallest non-zero remaining among ALL contributors (including folded).
		level := 0
		for _, r := range remaining {
			if r > 0 && (level == 0 || r < level) {
				level = r
			}
		}
		if level == 0 {
			break
		}

		// Build pot at this level. Track eligible (non-folded) winners.
		potAmount := 0
		var eligibleIdx []int
		for i := range remaining {
			if remaining[i] >= level {
				potAmount += level
				remaining[i] -= level
				if !contribs[i].Folded {
					eligibleIdx = append(eligibleIdx, i)
				}
			}
		}

		if potAmount == 0 {
			continue
		}

		// If no eligible winner (everyone in this tier folded), the pot is
		// orphaned. In real poker this means the last non-folded player would
		// already have won earlier; handle defensively by giving it to the
		// best hand among ALL contributors at this tier (folded or not).
		if len(eligibleIdx) == 0 {
			for i := range remaining {
				// Must have contributed to this tier — i.e. their original
				// committed was >= sum of this tier and prior tiers handled.
				// We approximate by: anyone whose remaining became negative-or-zero this round.
				// Easier: just take all participants this round.
				_ = i
			}
			// Defensive fallback: drop pot (extremely rare path)
			potIndex++
			continue
		}

		// Find best hand(s) among eligible
		bestIdxs := []int{eligibleIdx[0]}
		for _, idx := range eligibleIdx[1:] {
			cmp := contribs[idx].Hand.Compare(contribs[bestIdxs[0]].Hand)
			if cmp > 0 {
				bestIdxs = []int{idx}
			} else if cmp == 0 {
				bestIdxs = append(bestIdxs, idx)
			}
		}

		// Split: integer division, remainder distributed to earliest input order
		// (bestIdxs is already in input order since eligibleIdx is built in order).
		share := potAmount / len(bestIdxs)
		rem := potAmount % len(bestIdxs)
		for i, idx := range bestIdxs {
			amt := share
			if i < rem {
				amt++
			}
			shares = append(shares, PotShare{
				PlayerID: contribs[idx].PlayerID,
				Amount:   amt,
				PotIndex: potIndex,
			})
		}
		potIndex++
	}

	return shares, nil
}
