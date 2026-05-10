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
      console.warn('[auth] wx.login not available');
      resolve('');
      return;
    }
    console.log('[auth] wx.login calling...');
    let resolved = false;
    const timer = setTimeout(() => {
      if (!resolved) {
        resolved = true;
        console.warn('[auth] wx.login timed out after 5s, proceeding without code');
        resolve('');
      }
    }, 5000);
    wx.login({
      success: (res) => {
        console.log('[auth] wx.login success, code:', res.code ? 'obtained' : 'empty');
        if (!resolved) { resolved = true; clearTimeout(timer); resolve(res.code || ''); }
      },
      fail: (err) => {
        console.warn('[auth] wx.login fail:', JSON.stringify(err));
        if (!resolved) { resolved = true; clearTimeout(timer); resolve(''); }
      },
    });
  });
}

/**
 * Acquire a fresh session token. Idempotent across calls — returns the cached
 * token if it isn't close to expiring. Throws on network or backend error.
 */
export async function ensureLoggedIn(force = false): Promise<string> {
  console.log('[auth] ensureLoggedIn start');
  const user = store.getUser();
  if (!user) throw new Error('store not initialized');
  if (!force && !store.tokenStale()) {
    console.log('[auth] token still valid, skipping login');
    return user.token;
  }

  const code = await wxLoginCode();
  console.log('[auth] got code, calling /login...');
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
  console.log('[auth] /login response:', JSON.stringify(resp));
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
