import type { WireRoomState } from '../types/game';

interface SystemInfo {
  windowWidth: number;
  windowHeight: number;
  pixelRatio: number;
  safeAreaTop: number;
  safeAreaBottom: number;
}

export interface UserIdentity {
  uid: string;
  nickname: string;
  avatar: string;
  // Session token from /login. Renewed before WebSocket connect.
  // Empty string = not yet authenticated (server may still accept in optional-auth mode).
  token: string;
  tokenExpiresAt: number; // unix seconds; 0 if no token
}

const UID_STORAGE_KEY = 'tx_uid';
const PROFILE_STORAGE_KEY = 'tx_profile';
const TOKEN_STORAGE_KEY = 'tx_token';

const FRIENDLY_AVATARS = ['😎', '🦁', '🐱', '🐶', '🦊', '🐻', '🐰', '🐼', '🐯', '🐵'];

function genUid(): string {
  // 16 hex chars, time-prefixed
  const t = Date.now().toString(16);
  let r = '';
  for (let i = 0; i < 8; i++) r += Math.floor(Math.random() * 16).toString(16);
  return t + r;
}

function pick<T>(arr: T[]): T {
  return arr[Math.floor(Math.random() * arr.length)];
}

class Store {
  private systemInfo: SystemInfo | null = null;
  private user: UserIdentity | null = null;
  private roomState: WireRoomState | null = null;
  private listeners = new Set<(s: WireRoomState | null) => void>();

  init(): void {
    let uid = wx.getStorageSync(UID_STORAGE_KEY) as string;
    if (!uid) {
      uid = genUid();
      wx.setStorageSync(UID_STORAGE_KEY, uid);
    }
    let profile = wx.getStorageSync(PROFILE_STORAGE_KEY) as Partial<UserIdentity> | null;
    if (!profile || !profile.nickname) {
      profile = {
        nickname: '玩家' + uid.slice(-4),
        avatar: pick(FRIENDLY_AVATARS),
      };
      wx.setStorageSync(PROFILE_STORAGE_KEY, profile);
    }
    const tokenInfo = (wx.getStorageSync(TOKEN_STORAGE_KEY) as { token: string; expiresAt: number } | null) || null;
    this.user = {
      uid,
      nickname: profile.nickname || '玩家',
      avatar: profile.avatar || '😎',
      token: tokenInfo?.token || '',
      tokenExpiresAt: tokenInfo?.expiresAt || 0,
    };
  }

  setToken(token: string, expiresAt: number): void {
    if (!this.user) return;
    this.user = { ...this.user, token, tokenExpiresAt: expiresAt };
    wx.setStorageSync(TOKEN_STORAGE_KEY, { token, expiresAt });
  }

  /** Returns true when no token exists or it expires within `bufferSec` seconds. */
  tokenStale(bufferSec = 60 * 60): boolean {
    if (!this.user || !this.user.token) return true;
    const now = Math.floor(Date.now() / 1000);
    return this.user.tokenExpiresAt - now < bufferSec;
  }

  setSystemInfo(info: SystemInfo): void {
    this.systemInfo = info;
  }

  getSystemInfo(): SystemInfo | null {
    return this.systemInfo;
  }

  getUser(): UserIdentity | null {
    return this.user;
  }

  updateUser(patch: Partial<UserIdentity>): void {
    if (!this.user) return;
    this.user = { ...this.user, ...patch };
    wx.setStorageSync(PROFILE_STORAGE_KEY, {
      nickname: this.user.nickname,
      avatar: this.user.avatar,
    });
    if (patch.uid !== undefined) {
      wx.setStorageSync(UID_STORAGE_KEY, this.user.uid);
    }
  }

  setRoomState(s: WireRoomState | null): void {
    this.roomState = s;
    this.listeners.forEach((fn) => {
      try {
        fn(s);
      } catch (e) {
        console.error('[store] listener err', e);
      }
    });
  }

  getRoomState(): WireRoomState | null {
    return this.roomState;
  }

  subscribeRoomState(fn: (s: WireRoomState | null) => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }
}

export const store = new Store();
