# 德州扑克 · 微信小程序

娱乐性质（非赌博）的德州扑克小程序。卡通风格，多人在线对战。

## 当前进度

✅ **阶段 1**：项目骨架 + 牌桌 UI（卡通风）
✅ **阶段 2**：Go 后端（HTTP + WebSocket Hub + 房间管理）
✅ **阶段 3**：游戏核心算法（牌堆、安全洗牌、牌型识别、Engine 状态机）
✅ **阶段 4a**：前端接入 WebSocket 实时联调
✅ **阶段 4b**：会话鉴权（HMAC token + `/login` + `?token=` 上行）
✅ **阶段 5**：动画 / 音效 / 表情（发牌 / 摊牌面板 / 筹码飞入 / 单卡精确翻牌 / 赢家拿池 / 表情聊天 / 可插拔音效）
✅ **阶段 6**：合规与安全收尾（部署清单 / 鉴权 / 限流 / 用户协议 / 隐私政策）

## 本地联调

终端 1（启动后端）：

```bash
cd server && go run ./cmd/server
# texas-holdem server listening on :18080
```

可选环境变量：

| 变量 | 含义 | 默认 |
|---|---|---|
| `ADDR` | 监听地址 | `:18080` |
| `AUTH_SECRET` | HMAC token 签名密钥（hex 64 字符或任意字符串） | 启动时生成临时（重启失效） |
| `AUTH_TTL` | token 有效期（如 `168h`、`3600`） | 7 天 |
| `AUTH_REQUIRED` | `1` = `/ws` 强制要求 token；其他 = 可选鉴权 | 可选鉴权 |
| `ALLOWED_ORIGINS` | 逗号分隔的允许 Origin（如 `https://a.com,https://b.com`）。空 = 允许所有 | 允许所有 |
| `WX_APPID` / `WX_APPSECRET` | 设置后 `/login` 进入生产模式（调 `code2session`） | DEV：信任客户端 uid |
| `STATE_FILE` | 启用持久化（房间/筹码/战绩存到 JSON 文件） | 不持久化 |
| `STATE_SAVE_INTERVAL` | 周期保存间隔 | 30s |

终端 2（开微信开发者工具）：

1. 打开 `miniprogram/` 项目
2. 详情 → 本地设置 → 勾选"不校验合法域名…"
3. 编译运行
4. 启动时自动 `wx.login` → 后端 `/login` → 拿 token，进牌桌时附带在 WS URL 上
5. 进入大厅 → 点房间卡片 → 牌桌页自动 join

> 模拟两人对战：开两个微信开发者工具实例（或两台真机）使用不同账号登录，进入同一房间号即可触发开局。

> 真机扫码联调：手机和电脑必须同 WiFi。`miniprogram/utils/socket.ts` 和 `request.ts` 中的主机地址需改为电脑在 LAN 中的 IP（`ifconfig | grep "inet "` 查询）。

## 目录结构

```
miniprogram/   微信小程序前端（原生 + TypeScript）
server/        Go 后端 + 单测 + Dockerfile
deploy/        Docker Compose / Caddy / systemd 部署清单
docs/          用户协议 / 隐私政策（占位模板）
```

## 测试与构建

```bash
cd server
go test ./...        # auth + api + game + ws 全套
go build ./...       # 编译检查
```

## 部署

完整流程见 [deploy/README.md](deploy/README.md)。两条路：

- **Docker Compose**（推荐）：Caddy 自动签发 Let's Encrypt 证书 + Go 服务器，一键 `docker compose up -d`
- **systemd**：直接跑二进制，前面挂 Nginx/Caddy 处理 TLS

```bash
bash <(curl -s -L https://raw.githubusercontent.com/FxPool/texas-holdem-mp/main/deploy/install.sh)
```

## 协议与隐私

模板见 [docs/USER_AGREEMENT.md](docs/USER_AGREEMENT.md) 和 [docs/PRIVACY.md](docs/PRIVACY.md)。**正式上线前请咨询律师并填实占位项**。

## 卡通风格配色

- 主背景：深紫 `#2B1B4D` → 靛蓝 `#1A2B5C` 渐变
- 桌面：薄荷绿 `#7FCBA4` → 深绿 `#3E8E5E` 径向渐变
- 桌沿：柔粉 `#F4B6A0`
- 强调（筹码、按钮）：金黄 `#FFD66B`
- 文字：浅米白 `#FFF6E0`

## 合规说明

- 虚拟筹码 ≠ 货币，**禁止任何形式的现金兑换**
- 仅作娱乐用途，避免赌博暗示
- 服务端权威：所有发牌、结算由服务端完成，客户端只展示
- 防作弊：服务端权威 + 玩家动作限流（5/s 突发，2/s 持续）+ 完整动作日志

## 后续可加的功能（路线图）

- ✅ **断线宽限期 60s**：掉线后保留座位/筹码，60 秒内重连无缝继续
- ✅ **WS 自动重连**：客户端指数退避 + 自动 re-join
- ✅ **AI 单人模式**：大厅创建时勾选「🤖 单人模式」，房间预置 3 个 bot
- ✅ **房间状态持久化**：`STATE_FILE=...` 启用 JSON 快照，30s 周期保存
- ✅ **战绩**：`/stats` 返回所有用户 lifetime 数据；`/stats?userId=X` 单查
- ✅ **限流**：WS 操作 5/2 token bucket（按 uid）；`/login` 10/1 token bucket（按 IP）
- ✅ **聊天敏感词过滤**：服务端 deny list 拦截赌博/支付相关词
- ✅ **首次启动协议同意**：用户协议+隐私政策同意框（写入 storage）
- ⬜ **好友列表与邀请**：仅有 `wx.shareAppMessage` 转发链接，未做真正的好友图
- ⬜ **真机 SFX 资源**：将 mp3 文件放入 `miniprogram/assets/sfx/`（代码无需改动）
