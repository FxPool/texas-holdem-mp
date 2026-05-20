// 切换环境：内网调试改 true，发布前改回 false。
// 同时切 HTTP 与 WS，避免漏改一个导致请求/连接走不同主机。
//
// 内网调试前置条件：
//   1) 服务端在 LAN_HOST 启动（默认监听 :18080，见 server/cmd/server/main.go）
//   2) 手机/电脑与服务器同一 WiFi 段
//   3) 微信开发者工具：详情 → 本地设置 → 勾选「不校验合法域名、web-view、
//      TLS 版本以及 HTTPS 证书」
const USE_LAN_DEV = false;

const LAN_HOST = '192.168.1.5:18080';
const PROD_HOST = 'www.zhoudegame.xyz';

export const HTTP_BASE = USE_LAN_DEV
  ? `http://${LAN_HOST}`
  : `https://${PROD_HOST}`;

export const WS_BASE = USE_LAN_DEV
  ? `ws://${LAN_HOST}/ws`
  : `wss://${PROD_HOST}/ws`;
