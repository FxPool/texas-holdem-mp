// WebSocket 客户端：连接、消息分发、心跳。
// 后端协议见 server/internal/ws/protocol.go

import type {
  ClientMessage,
  ClientMsgType,
  ServerMessage,
  ServerMsgType,
} from '../types/game';
import { WS_BASE } from './env';

type Listener = (msg: ServerMessage<unknown>) => void;

interface ConnectOptions {
  url: string;
  onOpen?: () => void;
  onClose?: (info: { code: number; reason: string }) => void;
  onError?: (err: unknown) => void;
  // When set, the socket will auto-reconnect with exponential backoff up to
  // maxAttempts. On a successful reconnect the onReconnect callback fires so
  // callers can re-issue any application-level join/setup messages.
  reconnect?: {
    maxAttempts?: number; // default 6
    baseDelayMs?: number; // default 800
    onReconnecting?: (attempt: number, delayMs: number) => void;
    onReconnect?: () => void;
    onGiveUp?: () => void;
  };
}

const TAG = '[socket]';

class GameSocket {
  private socketTask: WechatMiniprogram.SocketTask | null = null;
  private listeners = new Set<Listener>();
  private typeListeners = new Map<ServerMsgType, Set<Listener>>();
  private connected = false;
  private pingTimer: ReturnType<typeof setInterval> | null = null;
  private url = '';
  private connectStartedAt = 0;
  private reconnectAttempt = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private intentionallyClosed = false;
  private currentOpts: ConnectOptions | null = null;

  /** 建立连接。返回 onOpen 完成的 Promise（失败 reject 同一个 error）。 */
  connect(opts: ConnectOptions): Promise<void> {
    this.currentOpts = opts;
    this.intentionallyClosed = false;
    this.reconnectAttempt = 0;
    return this.dial(opts);
  }

  private dial(opts: ConnectOptions): Promise<void> {
    return new Promise((resolve, reject) => {
      this.url = opts.url;
      this.tearDownTask();
      this.connectStartedAt = Date.now();

      console.log(`${TAG} connecting to`, opts.url);
      try {
        this.socketTask = wx.connectSocket({
          url: opts.url,
          success: (r) => console.log(`${TAG} connectSocket.success`, r),
          fail: (err) => {
            console.error(`${TAG} connectSocket.fail`, JSON.stringify(err));
            opts.onError?.(err);
            reject(err);
          },
        });
      } catch (e) {
        console.error(`${TAG} connectSocket throw`, e);
        reject(e);
        return;
      }

      const task = this.socketTask;
      if (!task) {
        const err = new Error('connectSocket returned null');
        console.error(`${TAG} ${err.message}`);
        reject(err);
        return;
      }

      task.onOpen((res) => {
        const ms = Date.now() - this.connectStartedAt;
        console.log(`${TAG} OPEN (${ms}ms) header=`, JSON.stringify(res?.header || {}));
        this.connected = true;
        this.startPingLoop();
        opts.onOpen?.();
        resolve();
      });

      task.onError((err) => {
        const ms = Date.now() - this.connectStartedAt;
        console.error(`${TAG} ERROR (${ms}ms) errMsg=`, (err as { errMsg?: string })?.errMsg, 'raw=', JSON.stringify(err));
        opts.onError?.(err);
        if (!this.connected) reject(err);
      });

      task.onClose((res) => {
        const ms = Date.now() - this.connectStartedAt;
        console.warn(`${TAG} CLOSE (${ms}ms) code=${res?.code} reason=${res?.reason}`);
        const wasConnected = this.connected;
        this.connected = false;
        this.stopPingLoop();
        opts.onClose?.({ code: res?.code ?? 0, reason: res?.reason ?? '' });
        if (wasConnected && !this.intentionallyClosed && opts.reconnect) {
          this.scheduleReconnect();
        }
      });

      task.onMessage((res) => {
        if (typeof res.data !== 'string') {
          console.warn(`${TAG} non-string message`, typeof res.data);
          return;
        }
        let msg: ServerMessage<unknown>;
        try {
          msg = JSON.parse(res.data);
        } catch (e) {
          console.warn(`${TAG} bad json`, res.data, e);
          return;
        }
        if (msg.type !== 'pong') {
          console.log(`${TAG} ←`, msg.type, msg.data ?? '');
        }
        this.dispatch(msg);
      });
    });
  }

  send<T>(type: ClientMsgType, data?: T): void {
    if (!this.socketTask || !this.connected) {
      console.warn(`${TAG} cannot send, not connected:`, type);
      return;
    }
    const envelope: ClientMessage<T> = { type, data };
    if (type !== 'ping') console.log(`${TAG} →`, type, data ?? '');
    this.socketTask.send({
      data: JSON.stringify(envelope),
      fail: (err) => console.warn(`${TAG} send fail`, err),
    });
  }

  onAny(fn: Listener): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  on<T = unknown>(type: ServerMsgType, fn: (msg: ServerMessage<T>) => void): () => void {
    let set = this.typeListeners.get(type);
    if (!set) {
      set = new Set();
      this.typeListeners.set(type, set);
    }
    const wrapped: Listener = (msg) => fn(msg as ServerMessage<T>);
    set.add(wrapped);
    return () => {
      set?.delete(wrapped);
    };
  }

  isConnected(): boolean {
    return this.connected;
  }

  close(): void {
    this.intentionallyClosed = true;
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    this.tearDownTask();
  }

  private tearDownTask(): void {
    this.stopPingLoop();
    this.connected = false;
    if (this.socketTask) {
      try {
        this.socketTask.close({});
      } catch {
        // ignore
      }
      this.socketTask = null;
    }
  }

  private scheduleReconnect(): void {
    const opts = this.currentOpts;
    if (!opts || !opts.reconnect) return;
    const max = opts.reconnect.maxAttempts ?? 6;
    const base = opts.reconnect.baseDelayMs ?? 800;
    if (this.reconnectAttempt >= max) {
      console.warn(`${TAG} reconnect: gave up after ${max} attempts`);
      opts.reconnect.onGiveUp?.();
      return;
    }
    this.reconnectAttempt += 1;
    const attempt = this.reconnectAttempt;
    // Exponential backoff with full jitter, capped at 15s.
    const cap = 15_000;
    const expo = Math.min(cap, base * Math.pow(2, attempt - 1));
    const delay = Math.floor(Math.random() * expo);
    console.log(`${TAG} reconnect attempt=${attempt} in ${delay}ms`);
    opts.reconnect.onReconnecting?.(attempt, delay);
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.dial(opts)
        .then(() => {
          console.log(`${TAG} reconnect succeeded on attempt ${attempt}`);
          this.reconnectAttempt = 0;
          opts.reconnect?.onReconnect?.();
        })
        .catch((err) => {
          console.warn(`${TAG} reconnect attempt ${attempt} failed`, err);
          this.scheduleReconnect();
        });
    }, delay);
  }

  private dispatch(msg: ServerMessage<unknown>): void {
    this.listeners.forEach((fn) => {
      try {
        fn(msg);
      } catch (e) {
        console.error(`${TAG} listener err`, e);
      }
    });
    const set = this.typeListeners.get(msg.type);
    if (set) {
      set.forEach((fn) => {
        try {
          fn(msg);
        } catch (e) {
          console.error(`${TAG} type listener err`, e);
        }
      });
    }
  }

  private startPingLoop(): void {
    this.stopPingLoop();
    this.pingTimer = setInterval(() => {
      if (this.connected) this.send('ping');
    }, 25_000);
  }

  private stopPingLoop(): void {
    if (this.pingTimer) {
      clearInterval(this.pingTimer);
      this.pingTimer = null;
    }
  }
}

export const gameSocket = new GameSocket();

// 生产/内网地址在 utils/env.ts 切换。
export const DEFAULT_WS_URL = WS_BASE;
