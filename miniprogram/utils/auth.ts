// Authenticates the current user against /login on the backend and stores the
// resulting session token. Falls back gracefully when wx.login is unavailable
// (e.g. tools test environment) by sending the locally-generated uid in DEV
// mode payload, which the backend's DEV-mode /login accepts as-is.

import { store } from './store';
import { request } from './request';

interface LoginResponse {
  token: string;
  userId: string;
  nickname: string;
  avatar: string;
  expiresAt: number;
}

interface LoginPayload {
  code?: string;
  userId?: string;
  nickname?: string;
  avatar?: string;
}

function wxLoginCode(): Promise<string> {
  return new Promise((resolve) => {
    if (!wx.login) {
      resolve('');
      return;
    }
    wx.login({
      success: (res) => resolve(res.code || ''),
      fail: () => resolve(''),
    });
  });
}

/**
 * Acquire a fresh session token. Idempotent across calls — returns the cached
 * token if it isn't close to expiring. Throws on network or backend error.
 */
export async function ensureLoggedIn(force = false): Promise<string> {
  const user = store.getUser();
  if (!user) throw new Error('store not initialized');
  if (!force && !store.tokenStale()) {
    return user.token;
  }

  const code = await wxLoginCode();
  const payload: LoginPayload = {
    code,
    userId: user.uid,
    nickname: user.nickname,
    avatar: user.avatar,
  };
  const resp = await request<LoginResponse, LoginPayload>({
    url: '/login',
    method: 'POST',
    data: payload,
  });
  if (!resp || !resp.token) {
    throw new Error('login response missing token');
  }
  // The backend may have authoritative identity (wx mode prefixes uid with `wx:`)
  // — adopt it if it differs.
  if (resp.userId && resp.userId !== user.uid) {
    store.updateUser({ uid: resp.userId });
  }
  store.setToken(resp.token, resp.expiresAt);
  return resp.token;
}
