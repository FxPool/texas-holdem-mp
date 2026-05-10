import { store } from '../../utils/store';
import { request } from '../../utils/request';

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

interface PageData {
  rooms: RoomItem[];
  myNickname: string;
  myAvatar: string;
  refreshing: boolean;
}

interface PageMethods {
  onJoinRoom(e: WechatMiniprogram.TouchEvent): void;
  onCreateRoom(): void;
  onChangeName(): void;
  refresh(): Promise<void>;
  onPullDownRefresh(): void;
  createAIRoom(): Promise<void>;
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

// Prompts user for a password via wx.showModal. Resolves with the entered
// string (may be empty) or null if user cancelled.
function promptPassword(title: string, placeholder: string): Promise<string | null> {
  return new Promise((resolve) => {
    wx.showModal({
      title,
      editable: true,
      placeholderText: placeholder,
      confirmText: '确认',
      cancelText: '取消',
      success: (res) => {
        if (!res.confirm) {
          resolve(null);
          return;
        }
        resolve(String(res.content || '').trim());
      },
      fail: () => resolve(null),
    });
  });
}

// Prompts for an integer game-duration (minutes). Returns 0 = unlimited,
// or null on cancel.
function promptDuration(): Promise<number | null> {
  return new Promise((resolve) => {
    wx.showModal({
      title: '游戏时长（分钟）',
      content: '到时自动结算所有玩家，留空使用 30 分钟，输入 0 表示不限时',
      editable: true,
      placeholderText: String(DEFAULT_DURATION_MINUTES),
      confirmText: '确认',
      cancelText: '取消',
      success: (res) => {
        if (!res.confirm) {
          resolve(null);
          return;
        }
        const raw = String(res.content || '').trim();
        if (raw === '') {
          resolve(DEFAULT_DURATION_MINUTES);
          return;
        }
        const n = Number(raw);
        if (!Number.isFinite(n) || n < 0 || n > 1440) {
          wx.showToast({ title: '请输入 0-1440 之间的数字', icon: 'none' });
          resolve(null);
          return;
        }
        resolve(Math.floor(n));
      },
      fail: () => resolve(null),
    });
  });
}

Page<PageData, PageMethods>({
  data: {
    rooms: buildRoomItems([]),
    myNickname: '',
    myAvatar: '',
    refreshing: false,
  },

  onShow() {
    const u = store.getUser();
    if (u) {
      this.setData({ myNickname: u.nickname, myAvatar: u.avatar });
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
    let id = item.id;
    let password = '';
    if (!id) {
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
        id = resp.id;
      } catch (err) {
        wx.showToast({ title: '创建房间失败', icon: 'none' });
        console.warn('[lobby] create failed', err);
        return;
      }
    } else if (item.hasPassword) {
      const entered = await promptPassword('请输入房间密码', '请输入房间密码');
      if (entered === null) return;
      password = entered;
    }
    const params: Record<string, string> = { roomId: id, buyIn: String(item.buyIn) };
    if (password) params.password = password;
    const qs = Object.keys(params)
      .map((k) => `${k}=${encodeURIComponent(params[k])}`)
      .join('&');
    wx.navigateTo({ url: `/pages/table/table?${qs}` });
  },

  onCreateRoom() {
    const itemList = [
      ...PRESETS.map((p) => `${p.name} · 盲注 ${p.smallBlind}/${p.bigBlind} · 带入 ${p.buyIn}`),
      '🤖 单人模式（带 AI 对手）',
    ];
    wx.showActionSheet({
      itemList,
      success: async (res) => {
        const aiIndex = PRESETS.length;
        if (res.tapIndex === aiIndex) {
          await this.createAIRoom();
          return;
        }
        const preset = PRESETS[res.tapIndex];
        if (!preset) return;

        const duration = await promptDuration();
        if (duration === null) return;
        const password = await promptPassword(
          '设置房间密码（可选）',
          '留空则为公开房间',
        );
        if (password === null) return;
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
            },
          });
          const params: Record<string, string> = {
            roomId: resp.id,
            buyIn: String(preset.buyIn),
          };
          if (password) params.password = password;
          const qs = Object.keys(params)
            .map((k) => `${k}=${encodeURIComponent(params[k])}`)
            .join('&');
          wx.navigateTo({ url: `/pages/table/table?${qs}` });
        } catch (err) {
          wx.showToast({ title: '创建失败', icon: 'none' });
          console.warn('[lobby] create failed', err);
        }
      },
    });
  },

  async createAIRoom() {
    const preset = PRESETS[1]; // default 50/100 buy-in 1000
    try {
      const resp = await request<{ id: string }>({
        url: '/rooms',
        method: 'POST',
        data: {
          smallBlind: preset.smallBlind,
          bigBlind: preset.bigBlind,
          maxSeats: preset.maxSeats,
          bots: 3,
          botBuyIn: preset.buyIn,
          durationMinutes: DEFAULT_DURATION_MINUTES,
        },
      });
      wx.navigateTo({ url: `/pages/table/table?roomId=${resp.id}&buyIn=${preset.buyIn}` });
    } catch (err) {
      wx.showToast({ title: '创建失败', icon: 'none' });
      console.warn('[lobby] AI create failed', err);
    }
  },

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
