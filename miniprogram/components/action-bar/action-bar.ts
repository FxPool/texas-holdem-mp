Component({
  options: { styleIsolation: 'isolated' },
  properties: {
    canCheck: { type: Boolean, value: false },     // 当前能 check（无需跟注）
    callAmount: { type: Number, value: 0 },        // 跟注需追加的筹码
    minRaise: { type: Number, value: 0 },          // 最小加注总额（含跟注部分）
    maxRaise: { type: Number, value: 0 },          // 最大加注总额（=自己筹码）
    myChips: { type: Number, value: 0 },
    disabled: { type: Boolean, value: false },     // 非自己回合时禁用
  },
  data: {
    raiseAmount: 0,
    showSlider: false,
  },
  observers: {
    'minRaise': function (n: number) {
      if (this.data.raiseAmount < n) this.setData({ raiseAmount: n });
    },
  },
  methods: {
    onFold() {
      if (this.data.disabled) return;
      this.triggerEvent('action', { type: 'fold' });
    },
    onCheckOrCall() {
      if (this.data.disabled) return;
      const t = this.data.callAmount > 0 ? 'call' : 'check';
      this.triggerEvent('action', { type: t, amount: this.data.callAmount });
    },
    onRaiseToggle() {
      if (this.data.disabled) return;
      this.setData({ showSlider: !this.data.showSlider });
    },
    onRaiseConfirm() {
      if (this.data.disabled) return;
      const amt = this.data.raiseAmount;
      this.triggerEvent('action', {
        type: amt >= this.data.maxRaise ? 'all-in' : 'raise',
        amount: amt,
      });
      this.setData({ showSlider: false });
    },
    onSliderChange(e: WechatMiniprogram.SliderChange) {
      this.setData({ raiseAmount: e.detail.value });
    },
    onQuickBet(e: WechatMiniprogram.TouchEvent) {
      const ratio = Number(e.currentTarget.dataset.ratio);
      const target = Math.min(this.data.maxRaise, Math.max(this.data.minRaise, Math.round(this.data.maxRaise * ratio)));
      this.setData({ raiseAmount: target });
    },
  },
});
