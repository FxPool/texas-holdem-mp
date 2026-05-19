// HTTP 请求封装。生产/内网地址在 ./env.ts 切换。
import { HTTP_BASE } from './env';

export const DEFAULT_HTTP_URL = HTTP_BASE;

interface RequestOptions<TBody = Record<string, unknown>> {
  url: string;
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE';
  data?: TBody;
  header?: Record<string, string>;
  baseURL?: string;
}

export function request<T = unknown, TBody = Record<string, unknown>>(
  opts: RequestOptions<TBody>,
): Promise<T> {
  const base = opts.baseURL ?? DEFAULT_HTTP_URL;
  return new Promise((resolve, reject) => {
    wx.request({
      url: base + opts.url,
      method: opts.method ?? 'GET',
      data: opts.data as Record<string, unknown> | undefined,
      header: { 'Content-Type': 'application/json', ...(opts.header ?? {}) },
      success: (res) => {
        if (res.statusCode >= 200 && res.statusCode < 300) {
          resolve(res.data as T);
        } else {
          reject(new Error(`HTTP ${res.statusCode}: ${JSON.stringify(res.data)}`));
        }
      },
      fail: (err) => reject(err),
    });
  });
}
