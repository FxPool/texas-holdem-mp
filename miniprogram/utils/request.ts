// HTTP 请求封装。生产环境改成 https + 小程序后台合法域名。
// HTTP 基址应与 socket.ts 中 DEFAULT_WS_URL 同主机/端口。
export const DEFAULT_HTTP_URL = 'https://www.zhoudegame.xyz';

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
