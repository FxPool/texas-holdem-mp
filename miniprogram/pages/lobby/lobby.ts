import { store } from '../../utils/store';
import { request } from '../../utils/request';

interface ServerRoomSummary {
  id: string;
  players: number;
  maxSeats: number;
  smallBlind: number;
  bigBlind: number;
}

interface RoomItem {
  id: string;
  name: string;
  blinds: string;
  buyIn: number;
  seats: string;
  isLive: boolean;
}

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
  // instead so live player count surfaces.
  const liveByBlinds = new Map<string, ServerRoomSummary>();
  for (const s of server) {
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
      };
    }
    return {
      id: '',
      name: p.name,
      blinds: key,
      buyIn: p.buyIn,
      seats: `0/${p.maxSeats}`,
      isLive: false,
    };
  });
  // Append any other live rooms that didn't match a preset
  for (const s of liveByBlinds.values()) {
    items.push({
      id: s.id,
      name: '#' + s.id,
      blinds: `${s.smallBlind}/${s.bigBlind}`,
      buyIn: s.bigBlind * 10,
      seats: `${s.players}/${s.maxSeats}`,
      isLive: true,
    });
  }
  return items;
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
    if (!id) {
      // Preset that has no live room yet — create a fresh one with these blinds.
      const preset = PRESETS.find((p) => `${p.smallBlind}/${p.bigBlind}` === item.blinds);
      if (!preset) return;
      try {
        const resp = await request<{ id: string }>({
          url: '/rooms',
          method: 'POST',
          data: { smallBlind: preset.smallBlind, bigBlind: preset.bigBlind, maxSeats: preset.maxSeats },
        });
        id = resp.id;
      } catch (err) {
        wx.showToast({ title: '创建房间失败', icon: 'none' });
        console.warn('[lobby] create failed', err);
        return;
      }
    }
    wx.navigateTo({ url: `/pages/table/table?roomId=${id}&buyIn=${item.buyIn}` });
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
        try {
          const resp = await request<{ id: string }>({
            url: '/rooms',
            method: 'POST',
            data: { smallBlind: preset.smallBlind, bigBlind: preset.bigBlind, maxSeats: preset.maxSeats },
          });
          wx.navigateTo({ url: `/pages/table/table?roomId=${resp.id}&buyIn=${preset.buyIn}` });
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
