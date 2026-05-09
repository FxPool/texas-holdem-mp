# 部署清单

两条路径：

- **路线 A（推荐）· 一键管理脚本** [install.sh](install.sh)：菜单式，覆盖安装/升级/启停/卸载/调优。从 GitHub Release 拉 `tar.gz` 解压。
- 路线 B · Docker Compose（[docker-compose.yml](docker-compose.yml)）

## 准备

- 一台 Debian 11/12 或 Ubuntu 22.04 公网机（1 vCPU / 1 GB RAM 起步够用）
- 一个域名解析到这台机器
- 微信小程序后台 → 服务器域名 → 加：
  - `request 合法域名`：`https://你的域名`
  - `socket 合法域名`：`wss://你的域名`

---

## 路线 A · 一键管理脚本

### 1. 在本地编译并发布到 GitHub Release

```bash
cd server
make dist                                  # 出 dist/*.tar.gz + SHA256SUMS
make release VERSION=v0.1.0                # 一条龙：dist + 上传到 GitHub Release
```

`make release` 需要 [`gh` CLI](https://cli.github.com/)（`brew install gh && gh auth login`）。
不想用 `gh` 的话，到 GitHub 网页 Release 页面手动把 `server/dist/*.tar.gz` 拖上去。

### 2. 在服务器上跑脚本

最简单：直接 curl + 菜单：

```bash
ssh root@服务器IP
curl -fsSL https://raw.githubusercontent.com/FxPool/texas-holdem-mp/main/deploy/install.sh -o /usr/local/bin/texas-mgr
chmod +x /usr/local/bin/texas-mgr
sudo texas-mgr               # 进菜单
```

菜单选项：

```
 1) 安装 / 重装          —— 完整初始化（用户/二进制/sysctl/systemd/Caddy）
 2) 升级二进制           —— 拉新 release，零中断替换
 3) 启动
 4) 停止
 5) 重启
 6) 查看状态
 7) 实时日志（journalctl -f）
 8) 重新应用内核/ulimit 调优
 9) 卸载（保留数据）
10) 卸载并清理（--purge，删环境文件+state+用户）
 0) 退出
```

也支持非交互式：

```bash
sudo texas-mgr install --domain www.zhoudegame.xyz
sudo texas-mgr install --domain www.zhoudegame.xyz --release-tag v0.1.0
sudo texas-mgr update
sudo texas-mgr restart
sudo texas-mgr logs -n 500
sudo texas-mgr tune
sudo texas-mgr uninstall            # 保留数据
sudo texas-mgr uninstall --purge    # 全删
```

### 3. 脚本干了什么

**首次安装**：
1. 创建 `texas` 系统用户（无 home、无 shell）
2. 从 GitHub Release 下载 `texas-holdem-server-linux-<arch>.tar.gz`，校验大小 + ELF 头，解压到 `/usr/local/bin/`
3. 生成 `AUTH_SECRET` 写入 `/etc/default/texas-holdem`（重跑保留）
4. 写 `/etc/sysctl.d/99-texas-holdem.conf`：`fs.file-max=1048576`、`net.core.somaxconn=65535`、TCP keepalive 等
5. 写 `/etc/security/limits.d/texas-holdem.conf`：`nofile = 1048576`
6. 写 systemd 单元 `LimitNOFILE=1048576`，启用 + 启动
7. 装 Caddy（如未装）+ 写 Caddyfile + 自动签 Let's Encrypt
8. 内外两次 health check

**升级**：只重做第 2 步，保留所有配置和 state。

**调优（tune）**：只重写 sysctl/limits + 重启服务让 LimitNOFILE 生效。

### 4. 配置 wx 凭据（首次需要）

```bash
sudo nano /etc/default/texas-holdem
# 填入 WX_APPID 和 WX_APPSECRET
sudo texas-mgr restart
```

### 5. 升级流程

发布新 release 后：

```bash
sudo texas-mgr update
```

旧 `AUTH_SECRET` / `WX_*` 自动保留 —— 玩家不会被强制重新登录。

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
5. **改了 `/etc/security/limits.d/`**：只对**新登录的会话**生效。systemd 服务通过单元里的 `LimitNOFILE=` 立即生效，但你 ssh 会话要重新登录才能看到 `ulimit -n` 变化。
