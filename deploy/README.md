# 部署清单

三条路径，按你的偏好选：

- **路线 A（推荐）· 一键安装脚本**：从 GitHub Release 下载预编译二进制，自动配 systemd + Caddy + TLS
- 路线 B · Docker Compose
- 路线 C · 手动 systemd

## 准备

- 一台公网机（1 vCPU / 1 GB RAM 起步够用），Debian 11/12 或 Ubuntu 22.04
- 一个域名解析到这台机器
- 微信小程序后台 → 服务器域名 → 加：
  - `request 合法域名`：`https://你的域名`
  - `socket 合法域名`：`wss://你的域名`

---

## 路线 A · 一键安装脚本（推荐）

### 1. 把二进制放到 GitHub

**A 方案：用 GitHub Releases**（推荐）

```bash
# 本地交叉编译两个架构（已经做好了）
cd server
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o texas-holdem-server-linux-amd64 ./cmd/server
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o texas-holdem-server-linux-arm64 ./cmd/server
```

然后在 GitHub 仓库页面 → Releases → "Draft a new release" → 上传上面这两个文件作为 release assets，发布。

**B 方案：直接放在 main 分支**

把两个文件 commit + push 到仓库根目录或 `server/` 下，安装脚本用 `--binary-url` 指定 raw 链接。

### 2. 在服务器上跑安装脚本

SSH 到服务器：

```bash
# 默认 GITHUB_REPO 是 jiangminghong/texas-holdem-mp，请改成你自己的
curl -fsSL https://raw.githubusercontent.com/jiangminghong/texas-holdem-mp/main/deploy/install.sh \
  | sudo GITHUB_REPO=你的用户名/你的仓库名 bash -s -- --domain www.zhoudegame.xyz
```

或者把 `install.sh` 上传后本地跑：

```bash
scp deploy/install.sh root@服务器IP:/tmp/
ssh root@服务器IP

# 用 latest release
sudo bash /tmp/install.sh \
  --domain www.zhoudegame.xyz \
  --repo 你的用户名/你的仓库名

# 或锁定某个版本
sudo bash /tmp/install.sh \
  --domain www.zhoudegame.xyz \
  --repo 你的用户名/你的仓库名 \
  --release-tag v0.1.0

# 或直接给二进制 URL（绕开 release 机制）
sudo bash /tmp/install.sh \
  --domain www.zhoudegame.xyz \
  --binary-url https://github.com/你/你/raw/main/server/texas-holdem-server-linux-amd64
```

脚本会：

1. 创建 `texas` 系统用户
2. 下载二进制到 `/usr/local/bin/texas-holdem-server`
3. 生成 `AUTH_SECRET` 写入 `/etc/default/texas-holdem`（重跑保留旧值）
4. 装 Caddy（如未装）+ 写 Caddyfile
5. 装 systemd unit + 启动 + 验证

### 3. 配置 wx 凭据（只在首次需要）

```bash
sudo nano /etc/default/texas-holdem
# 填入 WX_APPID 和 WX_APPSECRET
sudo systemctl restart texas-holdem
```

### 4. 升级（之后每次发新版本）

直接重跑同一条命令 —— 旧 `AUTH_SECRET` 和 `WX_*` 自动保留：

```bash
sudo bash /tmp/install.sh --domain www.zhoudegame.xyz --repo 你的用户名/你的仓库名
```

### 5. 卸载

```bash
sudo bash /tmp/uninstall.sh           # 保留数据
sudo bash /tmp/uninstall.sh --purge   # 连数据一起删
```

---

## 路线 B · Docker Compose

```bash
cd deploy
cp .env.example .env  # 填入 AUTH_SECRET / WX_APPID / WX_APPSECRET
# 编辑 Caddyfile，把 EXAMPLE_DOMAIN 换成真实域名
docker compose up -d
docker compose logs -f
```

升级：

```bash
git pull
docker compose build game
docker compose up -d
```

---

## 路线 C · 手动 systemd

如果你不信任脚本，按 `install.sh` 里的步骤手工跑也可以。具体可以打开 [install.sh](install.sh) 阅读 —— 它本身就是文档化的步骤清单。

---

## 上线检查

```bash
curl https://你的域名/health         # → ok
curl -X POST https://你的域名/login \
  -H 'Content-Type: application/json' \
  -d '{"code":"...wx code...","nickname":"Test"}'
# → {"token":"...","userId":"wx:...","expiresAt":...}
```

前端切到生产时：

- `miniprogram/utils/socket.ts` `DEFAULT_WS_URL = 'wss://你的域名/ws'`
- `miniprogram/utils/request.ts` `DEFAULT_HTTP_URL = 'https://你的域名'`
- `miniprogram/project.config.json` 把 `urlCheck` 改回 `true`，`miniprogram/project.private.config.json` 同样
- 重新预览/上传

## 常见坑

1. **80 端口被占**：`ss -tlnp | grep :80`，停掉 nginx/apache 之类的旧服务，否则 Caddy 签证书失败
2. **域名没备案**：开发期 `curl` 测能通，但正式发布的小程序请求会被运营商拦
3. **DNS 还没生效**：`dig 你的域名 +short` 应该返回服务器 IP；如果不返回先等 DNS 传播
4. **`AUTH_SECRET` 千万别丢**：丢了所有玩家 token 失效。脚本会持久化到 `/etc/default/texas-holdem`，注意备份这个文件。
