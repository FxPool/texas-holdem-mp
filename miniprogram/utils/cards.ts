import type { Card, Suit, Rank } from '../types/game';

export const SUITS: Suit[] = ['spade', 'heart', 'club', 'diamond'];

export const RANKS: Rank[] = ['2', '3', '4', '5', '6', '7', '8', '9', '10', 'J', 'Q', 'K', 'A'];

export const SUIT_SYMBOL: Record<Suit, string> = {
  spade: '♠',
  heart: '♥',
  club: '♣',
  diamond: '♦',
};

// 红色花色（红心 / 方片）
export const SUIT_COLOR: Record<Suit, 'red' | 'black'> = {
  spade: 'black',
  heart: 'red',
  club: 'black',
  diamond: 'red',
};

export function buildDeck(): Card[] {
  const deck: Card[] = [];
  for (const s of SUITS) {
    for (const r of RANKS) {
      deck.push({ suit: s, rank: r });
    }
  }
  return deck;
}

// 仅工具占位：本阶段不在前端真发牌，所有发牌由后端权威完成
export function shufflePlaceholder(deck: Card[]): Card[] {
  return deck.slice();
}
