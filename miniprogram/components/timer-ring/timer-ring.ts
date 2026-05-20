// Action-deadline ring around the active seat. The progress shrinks from
// full → empty over `duration` seconds.
//
// The timer-ring is mounted via `wx:if="{{isActive}}"` on the seat, so a fresh
// CSS animation kicks off every time the active player changes — no manual
// restart needed. Previously this component ran a 200ms setInterval that
// rewrote the inline conic-gradient on every tick (~5×/sec setData + bridge
// round-trip). Now setData runs once per turn (when `deadline` becomes known)
// and the GPU advances the keyframe between ticks.
Component({
  options: { styleIsolation: 'isolated' },
  properties: {
    deadline: { type: Number, value: 0 }, // 截止时间戳 ms
    duration: { type: Number, value: 30 }, // 总时长秒
  },
  data: {
    totalMs: 0,
    // Negative value passed to CSS as animation-delay so the keyframe starts
    // mid-way through (matching how much of the timer has already elapsed).
    elapsedMs: 0,
  },
  observers: {
    'deadline,duration': function (deadline: number, duration: number) {
      if (!deadline || !duration) {
        if (this.data.totalMs !== 0) this.setData({ totalMs: 0, elapsedMs: 0 });
        return;
      }
      const totalMs = duration * 1000;
      const remainMs = Math.max(0, deadline - Date.now());
      const elapsedMs = Math.max(0, totalMs - remainMs);
      if (this.data.totalMs !== totalMs || this.data.elapsedMs !== elapsedMs) {
        this.setData({ totalMs, elapsedMs });
      }
    },
  },
});

