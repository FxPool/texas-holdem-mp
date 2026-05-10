import { ensureConsent } from '../../utils/consent';
import { store } from '../../utils/store';
import { ensureLoggedIn } from '../../utils/auth';
import { persistAvatar, isUrlAvatar } from '../../utils/wx-profile';

const SHOW_DEV_QUICK_PLAY = false;

interface PageData {
  showDevQuickPlay: boolean;
  profileOpen: boolean;
  profileAvatar: string;          // emoji 或 临时/本地图片路径
  profileAvatarIsUrl: boolean;
  profileNickname: string;
  profileSubmitting: boolean;
}

interface PageMethods {
  onShow(): void;
  onEnterLobby(): Promise<void>;
  onQuickPlay(): Promise<void>;
  onProfileSkip(): void;
  onProfileConfirm(): Promise<void>;
  onChooseAvatar(e: WechatMiniprogram.CustomEvent<{ avatarUrl: string }>): void;
  onNicknameInput(e: WechatMiniprogram.Input): void;
  noop(): void;
}

Page<PageData, PageMethods>({
  data: {
    showDevQuickPlay: SHOW_DEV_QUICK_PLAY,
    profileOpen: false,
    profileAvatar: '',
    profileAvatarIsUrl: false,
    profileNickname: '',
    profileSubmitting: false,
  },

  onShow() {
    // Re-check on every onShow rather than only onLoad so that if the user
    // declined and re-opened the app the prompt comes back.
    ensureConsent();
  },

  async onEnterLobby() {
    console.log('[index] onEnterLobby tapped');
    try {
      const ok = await ensureConsent();
      console.log('[index] ensureConsent ->', ok);
      if (!ok) return;
      const asked = store.hasAskedProfile();
      console.log('[index] hasAskedProfile ->', asked);
      if (!asked) {
        const u = store.getUser();
        const av = u?.avatar || '';
        this.setData({
          profileOpen: true,
          profileAvatar: av,
          profileAvatarIsUrl: isUrlAvatar(av),
          profileNickname: u?.nickname || '',
          profileSubmitting: false,
        });
        return;
      }
      wx.navigateTo({
        url: '/pages/lobby/lobby',
        fail: (err) => console.warn('[index] navigateTo lobby failed', err),
      });
    } catch (err) {
      console.error('[index] onEnterLobby threw', err);
      wx.showToast({ title: '进入大厅失败，请重试', icon: 'none' });
    }
  },

  async onQuickPlay() {
    const ok = await ensureConsent();
    if (!ok) return;
    wx.navigateTo({ url: '/pages/table/table' });
  },

  onChooseAvatar(e) {
    const url = e?.detail?.avatarUrl || '';
    if (!url) return;
    this.setData({ profileAvatar: url, profileAvatarIsUrl: isUrlAvatar(url) });
  },

  onNicknameInput(e) {
    const raw = String((e.detail as { value?: string })?.value ?? '');
    this.setData({ profileNickname: raw.slice(0, 12) });
  },

  onProfileSkip() {
    if (this.data.profileSubmitting) return;
    store.markProfileAsked();
    this.setData({ profileOpen: false });
    wx.navigateTo({ url: '/pages/lobby/lobby' });
  },

  async onProfileConfirm() {
    if (this.data.profileSubmitting) return;
    const u = store.getUser();
    if (!u) {
      wx.navigateTo({ url: '/pages/lobby/lobby' });
      return;
    }
    this.setData({ profileSubmitting: true });

    const trimmedNick = (this.data.profileNickname || '').trim().slice(0, 12);
    let avatar = this.data.profileAvatar || u.avatar;
    // chooseAvatar 返回的是 wxfile:// 或 http://tmp 的临时路径，落盘成持久路径
    if (avatar && avatar !== u.avatar && (avatar.indexOf('/') >= 0 || avatar.indexOf(':') >= 0)) {
      avatar = await persistAvatar(avatar);
    }

    const patch: { nickname?: string; avatar?: string } = {};
    if (trimmedNick && trimmedNick !== u.nickname) patch.nickname = trimmedNick;
    if (avatar && avatar !== u.avatar) patch.avatar = avatar;
    if (patch.nickname || patch.avatar) {
      store.updateUser(patch);
      // 让服务端拿到最新昵称/头像（下一次 ensureLoggedIn 会带上）
      ensureLoggedIn(true).catch((err) => {
        console.warn('[index] re-login after profile update failed', err);
      });
    }
    store.markProfileAsked();
    this.setData({ profileOpen: false, profileSubmitting: false });
    wx.navigateTo({ url: '/pages/lobby/lobby' });
  },

  noop() {},
});
