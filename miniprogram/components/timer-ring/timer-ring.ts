Component({
  options: { styleIsolation: 'isolated' },
  properties: {
    deadline: { type: Number, value: 0 }, // 截止时间戳 ms
    duration: { type: Number, value: 30 }, // 总时长秒
  },
  data: {
    progress: 1, // 1 -> 0
    secondsLeft: 30,
  },
  lifetimes: {
    attached() {
      this.startTick();
    },
    detached() {
      this.stopTick();
    },
  },
  observers: {
    'deadline': function () {
      this.startTick();
    },
  },
  methods: {
    startTick() {
      this.stopTick();
      const update = () => {
        const now = Date.now();
        const remainMs = Math.max(0, this.data.deadline - now);
        const totalMs = this.data.duration * 1000;
        const progress = totalMs > 0 ? Math.min(1, remainMs / totalMs) : 0;
        this.setData({
          progress,
          secondsLeft: Math.ceil(remainMs / 1000),
        });
        if (remainMs <= 0) this.stopTick();
      };
      update();
      // @ts-ignore
      this._timer = setInterval(update, 200);
    },
    stopTick() {
      // @ts-ignore
      if (this._timer) clearInterval(this._timer);
      // @ts-ignore
      this._timer = null;
    },
  },
});
