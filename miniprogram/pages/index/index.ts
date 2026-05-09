import { ensureConsent } from '../../utils/consent';

const SHOW_DEV_QUICK_PLAY = false;

Page({
  data: {
    showDevQuickPlay: SHOW_DEV_QUICK_PLAY,
  },
  onShow() {
    // Re-check on every onShow rather than only onLoad so that if the user
    // declined and re-opened the app the prompt comes back.
    ensureConsent();
  },
  async onEnterLobby() {
    const ok = await ensureConsent();
    if (!ok) return;
    wx.navigateTo({ url: '/pages/lobby/lobby' });
  },
  async onQuickPlay() {
    const ok = await ensureConsent();
    if (!ok) return;
    wx.navigateTo({ url: '/pages/table/table' });
  },
});
