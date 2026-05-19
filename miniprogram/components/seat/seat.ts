import type { Player } from '../../types/game';

Component({
  options: {
    styleIsolation: 'isolated',
  },
  properties: {
    player: { type: Object, value: null as Player | null },
    isActive: { type: Boolean, value: false },     // 当前行动中
    deadline: { type: Number, value: 0 },           // 行动截止时间戳
    showHoleCards: { type: Boolean, value: false }, // 是否显示手牌正面（仅自己 true）
    hasHand: { type: Boolean, value: false },       // 本手是否在进行中（控制对手手牌背面是否显示）
  },
});
