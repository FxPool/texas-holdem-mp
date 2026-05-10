import { store } from '../../utils/store';
import { request } from '../../utils/request';
import { isUrlAvatar } from '../../utils/wx-profile';

interface ServerRoomSummary {
  id: string;
  players: number;
  maxSeats: number;
  smallBlind: number;
  bigBlind: number;
  hasPassword?: boolean;
  durationMinutes?: number;
  endsAt?: number;
  ended?: boolean;
}

interface RoomItem {
  id: string;
  name: string;
  blinds: string;
  buyIn: number;
  seats: string;
  isLive: boolean;
  hasPassword: boolean;
  durationMinutes: number;
  endsAt: number;
  ended: boolean;
}

const DEFAULT_DURATION_MINUTES = 30;

const PRESETS: Array<{ name: string; smallBlind: number; bigBlind: number; buyIn: number; maxSeats: number }> = [
  { name: '萌新练手', smallBlind: 20, bigBlind: 50, buyIn: 500, maxSeats: 6 },
  { name: '欢乐桌', smallBlind: 50, bigBlind: 100, buyIn: 1000, maxSeats: 6 },
  { name: '高手互砍', smallBlind: 500, bigBlind: 1000, buyIn: 5000, maxSeats: 6 },
];

interface PresetView {
  name: string;
  smallBlind: number;
  bigBlind: number;
  buyIn: number;
  maxSeats: number;
}

interface PageData {
  rooms: RoomItem[];
  myNickname: string;
  myAvatar: string;
  myAvatarIsUrl: boolean;
  refreshing: boolean;

  // 创建房间弹窗
  createOpen: boolean;
  presets: PresetView[];
  createPresetIdx: number;
  createWithBots: boolean;
  createDurationStr: string;
  createPasswordStr: string;
  createSubmitting: boolean;

  // 加入有密码的房间
  joinPwOpen: boolean;
  joinPwRoomId: string;
  joinPwBuyIn: number;
  joinPwInput: string;
}

interface PageMethods {
  onJoinRoom(e: WechatMiniprogram.TouchEvent): void;
  onCreateRoom(): void;
  onCreateCancel(): void;
  onCreateConfirm(): Promise<void>;
  onPickPreset(e: WechatMiniprogram.TouchEvent): void;
  onToggleBots(): void;
  onCreateDurationInput(e: WechatMiniprogram.Input): void;
  onCreatePasswordInput(e: WechatMiniprogram.Input): void;
  onJoinPwCancel(): void;
  onJoinPwInput(e: WechatMiniprogram.Input): void;
  onJoinPwConfirm(): void;
  onChangeName(): void;
  refresh(): Promise<void>;
  onPullDownRefresh(): void;
  noop(): void;
}

function buildRoomItems(server: ServerRoomSummary[]): RoomItem[] {
  // Always show presets at the top so the lobby has content even when no live
  // rooms exist yet. If a live room matches a preset's blinds, show that one
  // instead so live player count surfaces. Skip live rooms that have a
  // password — those are private and shouldn't be auto-promoted to the
  // preset slot, since tapping should not route an unrelated user there.
  const liveByBlinds = new Map<string, ServerRoomSummary>();
  for (const s of server) {
    if (s.hasPassword) continue;
    if (s.ended) continue;
    liveByBlinds.set(`${s.smallBlind}/${s.bigBlind}`, s);
  }
  const items: RoomItem[] = PRESETS.map((p) => {
    const key = `${p.smallBlind}/${p.bigBlind}`;
    const live = liveByBlinds.get(key);
    if (live) {
      liveByBlinds.delete(key);
      return {
        id: live.id,
        name: p.name,
        blinds: key,
        buyIn: p.buyIn,
        seats: `${live.players}/${live.maxSeats}`,
        isLive: true,
        hasPassword: !!live.hasPassword,
        durationMinutes: live.durationMinutes || 0,
        endsAt: live.endsAt || 0,
        ended: !!live.ended,
      };
    }
    return {
      id: '',
      name: p.name,
      blinds: key,
      buyIn: p.buyIn,
      seats: `0/${p.maxSeats}`,
      isLive: false,
      hasPassword: false,
      durationMinutes: 0,
      endsAt: 0,
      ended: false,
    };
  });
  // Append any other live rooms (including private ones) so users with the
  // room id can see them in the list. Private rooms render with 🔒.
  for (const s of server) {
    if (s.ended) continue;
    if (liveByBlinds.has(`${s.smallBlind}/${s.bigBlind}`)) continue;
    // Already merged into a preset slot above.
    const matchedPreset = PRESETS.some(
      (p) => p.smallBlind === s.smallBlind && p.bigBlind === s.bigBlind && !s.hasPassword,
    );
    if (matchedPreset) continue;
    items.push({
      id: s.id,
      name: (s.hasPassword ? '🔒 ' : '') + '#' + s.id,
      blinds: `${s.smallBlind}/${s.bigBlind}`,
      buyIn: s.bigBlind * 10,
      seats: `${s.players}/${s.maxSeats}`,
      isLive: true,
      hasPassword: !!s.hasPassword,
      durationMinutes: s.durationMinutes || 0,
      endsAt: s.endsAt || 0,
      ended: !!s.ended,
    });
  }
  return items;
}

function buildNavigateUrl(roomId: string, buyIn: number, password: string): string {
  const params: Record<string, string> = { roomId, buyIn: String(buyIn) };
  if (password) params.password = password;
  const qs = Object.keys(params)
    .map((k) => `${k}=${encodeURIComponent(params[k])}`)
    .join('&');
  return `/pages/table/table?${qs}`;
}

Page<PageData, PageMethods>({
  data: {
    rooms: buildRoomItems([]),
    myNickname: '',
    myAvatar: '',
    myAvatarIsUrl: false,
    refreshing: false,

    createOpen: false,
    presets: PRESETS.map((p) => ({ ...p })),
    createPresetIdx: 1, // 默认欢乐桌
    createWithBots: false,
    createDurationStr: String(DEFAULT_DURATION_MINUTES),
    createPasswordStr: '',
    createSubmitting: false,

    joinPwOpen: false,
    joinPwRoomId: '',
    joinPwBuyIn: 0,
    joinPwInput: '',
  },

  onShow() {
    const u = store.getUser();
    if (u) {
      this.setData({
        myNickname: u.nickname,
        myAvatar: u.avatar,
        myAvatarIsUrl: isUrlAvatar(u.avatar),
      });
    }
    this.refresh();
  },

  onPullDownRefresh() {
    this.refresh().finally(() => wx.stopPullDownRefresh());
  },

  async refresh() {
    if (this.data.refreshing) return;
    this.setData({ refreshing: true });
    try {
      const list = await request<ServerRoomSummary[]>({ url: '/rooms', method: 'GET' });
      this.setData({ rooms: buildRoomItems(list || []) });
    } catch (e) {
      console.warn('[lobby] /rooms fetch failed', e);
    } finally {
      this.setData({ refreshing: false });
    }
  },

  async onJoinRoom(e) {
    const idx = Number(e.currentTarget.dataset.index || 0);
    const item = this.data.rooms[idx];
    if (!item) return;
    if (!item.id) {
      // Preset that has no live room yet — create a fresh one with these blinds.
      const preset = PRESETS.find((p) => `${p.smallBlind}/${p.bigBlind}` === item.blinds);
      if (!preset) return;
      try {
        const resp = await request<{ id: string }>({
          url: '/rooms',
          method: 'POST',
          data: {
            smallBlind: preset.smallBlind,
            bigBlind: preset.bigBlind,
            maxSeats: preset.maxSeats,
            durationMinutes: DEFAULT_DURATION_MINUTES,
          },
        });
        wx.navigateTo({ url: buildNavigateUrl(resp.id, item.buyIn, '') });
      } catch (err) {
        wx.showToast({ title: '创建房间失败', icon: 'none' });
        console.warn('[lobby] create failed', err);
      }
      return;
    }
    if (item.hasPassword) {
      this.setData({
        joinPwOpen: true,
        joinPwRoomId: item.id,
        joinPwBuyIn: item.buyIn,
        joinPwInput: '',
      });
      return;
    }
    wx.navigateTo({ url: buildNavigateUrl(item.id, item.buyIn, '') });
  },

  onCreateRoom() {
    this.setData({
      createOpen: true,
      createPresetIdx: this.data.createPresetIdx,
      createWithBots: false,
      createDurationStr: String(DEFAULT_DURATION_MINUTES),
      createPasswordStr: '',
      createSubmitting: false,
    });
  },

  onCreateCancel() {
    if (this.data.createSubmitting) return;
    this.setData({ createOpen: false });
  },

  onPickPreset(e) {
    const idx = Number(e.currentTarget.dataset.idx || 0);
    if (idx < 0 || idx >= PRESETS.length) return;
    this.setData({ createPresetIdx: idx });
  },

  onToggleBots() {
    this.setData({ createWithBots: !this.data.createWithBots });
  },

  onCreateDurationInput(e) {
    const v = String((e.detail as { value?: string })?.value ?? '').replace(/[^0-9]/g, '');
    this.setData({ createDurationStr: v });
  },

  onCreatePasswordInput(e) {
    const raw = String((e.detail as { value?: string })?.value ?? '');
    this.setData({ createPasswordStr: raw.slice(0, 32) });
  },

  async onCreateConfirm() {
    if (this.data.createSubmitting) return;
    const preset = PRESETS[this.data.createPresetIdx];
    if (!preset) return;
    const raw = this.data.createDurationStr.trim();
    let duration = DEFAULT_DURATION_MINUTES;
    if (raw !== '') {
      const n = Number(raw);
      if (!Number.isFinite(n) || n < 0 || n > 1440) {
        wx.showToast({ title: '时长需在 0-1440 之间', icon: 'none' });
        return;
      }
      duration = Math.floor(n);
    }
    const password = this.data.createPasswordStr.trim();
    const withBots = this.data.createWithBots;
    this.setData({ createSubmitting: true });
    try {
      const resp = await request<{ id: string }>({
        url: '/rooms',
        method: 'POST',
        data: {
          smallBlind: preset.smallBlind,
          bigBlind: preset.bigBlind,
          maxSeats: preset.maxSeats,
          durationMinutes: duration,
          password: password || undefined,
          bots: withBots ? 3 : 0,
          botBuyIn: withBots ? preset.buyIn : undefined,
        },
      });
      this.setData({ createOpen: false, createSubmitting: false });
      wx.navigateTo({ url: buildNavigateUrl(resp.id, preset.buyIn, password) });
    } catch (err) {
      console.warn('[lobby] create failed', err);
      wx.showToast({ title: '创建失败', icon: 'none' });
      this.setData({ createSubmitting: false });
    }
  },

  onJoinPwCancel() {
    this.setData({ joinPwOpen: false, joinPwInput: '' });
  },

  onJoinPwInput(e) {
    const raw = String((e.detail as { value?: string })?.value ?? '');
    this.setData({ joinPwInput: raw.slice(0, 32) });
  },

  onJoinPwConfirm() {
    const pw = this.data.joinPwInput.trim();
    if (!pw) {
      wx.showToast({ title: '请输入密码', icon: 'none' });
      return;
    }
    const roomId = this.data.joinPwRoomId;
    const buyIn = this.data.joinPwBuyIn;
    this.setData({ joinPwOpen: false, joinPwInput: '' });
    wx.navigateTo({ url: buildNavigateUrl(roomId, buyIn, pw) });
  },

  noop() {},

  onChangeName() {
    const cur = store.getUser();
    if (!cur) return;
    wx.showModal({
      title: '修改昵称',
      editable: true,
      placeholderText: cur.nickname,
      success: (res) => {
        if (!res.confirm || !res.content) return;
        const trimmed = String(res.content).trim().slice(0, 12);
        if (!trimmed) return;
        store.updateUser({ nickname: trimmed });
        this.setData({ myNickname: trimmed });
      },
    });
  },
});

