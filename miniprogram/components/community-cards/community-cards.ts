import type { Card } from '../../types/game';

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
  },
  observers: {
    newIndices(arr: number[]) {
      const flags = [false, false, false, false, false];
      if (Array.isArray(arr)) {
        for (const i of arr) {
          if (i >= 0 && i < 5) flags[i] = true;
        }
      }
      this.setData({ flipFlags: flags });
    },
  },
});
