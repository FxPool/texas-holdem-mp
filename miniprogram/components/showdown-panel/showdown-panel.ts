import type { Card } from '../../types/game';

interface Contestant {
  userId: string;
  nickname: string;
  avatar: string;
  rankLabel: string;     // 中文牌型（葫芦/同花…）
  rankSlug: string;
  holeCards: Card[];
  amountWon: number;     // 0 if didn't win this pot
  isWinner: boolean;
}

const RANK_CN: Record<string, string> = {
  'high-card': '高牌',
  'one-pair': '一对',
  'two-pair': '两对',
  'three-of-a-kind': '三条',
  'straight': '顺子',
  'flush': '同花',
  'full-house': '葫芦',
  'four-of-a-kind': '四条',
  'straight-flush': '同花顺',
};

Component({
  options: { styleIsolation: 'isolated' },
  properties: {
    visible: { type: Boolean, value: false },
    contestants: { type: Array, value: [] as Contestant[] },
    community: { type: Array, value: [] as Card[] },
    uncontestedWinner: { type: String, value: '' }, // 单胜（无摊牌）时填昵称
    uncontestedAmount: { type: Number, value: 0 },
  },
  data: {
    rankCN: RANK_CN,
  },
  methods: {
    onDismiss() {
      this.triggerEvent('dismiss');
    },
  },
});
