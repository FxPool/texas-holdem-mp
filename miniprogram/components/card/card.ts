import { SUIT_SYMBOL, SUIT_COLOR } from '../../utils/cards';
import type { Suit } from '../../types/game';

Component({
  options: {
    styleIsolation: 'isolated',
  },
  properties: {
    suit: { type: String, value: '' },     // 'spade' | 'heart' | 'club' | 'diamond' | ''
    rank: { type: String, value: '' },     // '2'..'10' | 'J' | 'Q' | 'K' | 'A' | ''
    revealed: { type: Boolean, value: true },
    size: { type: String, value: 'md' },   // 'sm' | 'md' | 'lg'
    placeholder: { type: Boolean, value: false }, // 空位（虚线轮廓）
  },
  data: {
    symbol: '',
    color: 'black' as 'red' | 'black',
  },
  observers: {
    'suit': function (suit: string) {
      if (!suit) return;
      this.setData({
        symbol: SUIT_SYMBOL[suit as Suit] ?? '',
        color: SUIT_COLOR[suit as Suit] ?? 'black',
      });
    },
  },
});
