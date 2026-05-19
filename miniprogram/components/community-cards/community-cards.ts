import type { Card } from '../../types/game';

// Stagger between successive card flips during flop/turn/river reveals.
// Single-card stages (turn, river) just animate at delay 0.
const FLIP_STAGGER_MS = 220;

Component({
  options: { styleIsolation: 'isolated' },
  properties: {
    cards: { type: Array, value: [] as Card[] },
    revealedCount: { type: Number, value: 0 },
    // Indices (0..4) of cards that should play the flip-in animation right now.
    // Parent clears this after animation duration.
    newIndices: { type: Array, value: [] as number[] },
  },
  data: {
    slots: [0, 1, 2, 3, 4],
    // Per-slot flip flag, derived from newIndices.
    flipFlags: [false, false, false, false, false] as boolean[],
    // Per-slot animation-delay in ms, so cards in a flop reveal one by one.
    flipDelays: [0, 0, 0, 0, 0] as number[],
  },
  observers: {
    newIndices(arr: number[]) {
      const flags = [false, false, false, false, false];
      const delays = [0, 0, 0, 0, 0];
      if (Array.isArray(arr)) {
        const sorted = arr.filter((i) => i >= 0 && i < 5).sort((a, b) => a - b);
        sorted.forEach((i, order) => {
          flags[i] = true;
          delays[i] = order * FLIP_STAGGER_MS;
        });
      }
      this.setData({ flipFlags: flags, flipDelays: delays });
    },
  },
});
