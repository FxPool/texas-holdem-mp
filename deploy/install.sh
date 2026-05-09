#!/usr/bin/env bash
# 一键安装/升级 texas-holdem 服务到 Debian/Ubuntu。
#
# 使用：
#   sudo bash install.sh                                  # 交互式
#   sudo bash install.sh --domain www.example.com         # 一行搞定
#   sudo bash install.sh --domain www.example.com --release-tag v0.1.0
#
# 重新运行此脚本会保留已有的 AUTH_SECRET（不让旧 token 失效）和已配置的
# WX_APPID/WX_APPSECRET，只更新二进制 + Caddyfile + systemd 单元。
#
# 需要 root（或 sudo）。仅在 Debian 11/12、Ubuntu 22.04 上测试过。

set -euo pipefail

# ============================================================
# 配置（按需修改）
# ============================================================

# GitHub owner/repo —— 改成你自己的仓库
GITHUB_REPO="${GITHUB_REPO:-jiangminghong/texas-holdem-mp}"

# 默认从 release 拿；不传 --release-tag 时读最新 release。
RELEASE_TAG="${RELEASE_TAG:-latest}"

# 直接指定二进制 URL（覆盖以上 release 模式）
BINARY_URL="${BINARY_URL:-}"

DOMAIN="${DOMAIN:-}"
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="texas-holdem-server"
RUN_USER="texas"
ENV_FILE="/etc/default/texas-holdem"
SYSTEMD_UNIT="/etc/systemd/system/texas-holdem.service"
CADDYFILE="/etc/caddy/Caddyfile"
STATE_DIR="/var/lib/texas-holdem"
LISTEN_ADDR="127.0.0.1:18080"

# ============================================================
# 工具
# ============================================================

GREEN=$'\033[0;32m'
YELLOW=$'\033[0;33m'
RED=$'\033[0;31m'
RESET=$'\033[0m'

step()  { printf "${GREEN}==>${RESET} %s\n" "$*"; }
warn()  { printf "${YELLOW}!! ${RESET} %s\n" "$*" >&2; }
die()   { printf "${RED}xx ${RESET} %s\n" "$*" >&2; exit 1; }

require_root() {
    if [[ "${EUID}" -ne 0 ]]; then
        die "请用 sudo 或 root 运行此脚本"
    fi
}

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || die "缺少 $1，请先 apt install -y $2"
}

# ============================================================
# 解析参数
# ============================================================

while [[ $# -gt 0 ]]; do
    case "$1" in
        --domain) DOMAIN="$2"; shift 2 ;;
        --release-tag) RELEASE_TAG="$2"; shift 2 ;;
        --binary-url) BINARY_URL="$2"; shift 2 ;;
        --repo) GITHUB_REPO="$2"; shift 2 ;;
        -h|--help)
            grep '^#' "$0" | sed 's/^# \{0,1\}//' | head -25
            exit 0
            ;;
        *) die "未知参数: $1" ;;
    esac
done

require_root
require_cmd curl curl

# ============================================================
# 1. 询问域名
# ============================================================

if [[ -z "${DOMAIN}" ]]; then
    read -rp "请输入域名（如 www.zhoudegame.xyz）: " DOMAIN
fi
[[ -z "${DOMAIN}" ]] && die "域名不能为空"

step "目标域名: ${DOMAIN}"

# ============================================================
# 2. 检测系统架构
# ============================================================

ARCH="$(uname -m)"
case "${ARCH}" in
    x86_64|amd64) ARCH_TAG="amd64" ;;
    aarch64|arm64) ARCH_TAG="arm64" ;;
    *) die "不支持的架构: ${ARCH}（仅支持 amd64/arm64）" ;;
esac
step "架构: ${ARCH_TAG}"

# ============================================================
# 3. 计算下载 URL
# ============================================================

if [[ -z "${BINARY_URL}" ]]; then
    if [[ "${RELEASE_TAG}" == "latest" ]]; then
        BINARY_URL="https://github.com/${GITHUB_REPO}/releases/latest/download/texas-holdem-server-linux-${ARCH_TAG}"
    else
        BINARY_URL="https://github.com/${GITHUB_REPO}/releases/download/${RELEASE_TAG}/texas-holdem-server-linux-${ARCH_TAG}"
    fi
fi
step "下载地址: ${BINARY_URL}"

# ============================================================
# 4. 创建运行用户
# ============================================================

if id -u "${RUN_USER}" >/dev/null 2>&1; then
    step "用户 ${RUN_USER} 已存在，跳过"
else
    step "创建运行用户 ${RUN_USER}"
    useradd --system --no-create-home --shell /usr/sbin/nologin "${RUN_USER}"
fi

mkdir -p "${STATE_DIR}"
chown "${RUN_USER}:${RUN_USER}" "${STATE_DIR}"

# ============================================================
# 5. 下载二进制
# ============================================================

TMP_BIN="$(mktemp)"
trap 'rm -f "${TMP_BIN}"' EXIT

step "下载二进制…"
if ! curl -fsSL -o "${TMP_BIN}" "${BINARY_URL}"; then
    die "下载失败。检查 GITHUB_REPO（当前: ${GITHUB_REPO}）和 release 是否已发布。"
fi

# 简单校验：必须是 ELF 文件（前 4 字节 7f 45 4c 46）
MAGIC="$(head -c 4 "${TMP_BIN}" | od -An -t x1 | tr -d ' \n')"
if [[ "${MAGIC}" != "7f454c46" ]]; then
    die "下载内容不是 ELF 可执行文件（开头字节 ${MAGIC}），可能是 404 HTML 页面。检查 URL: ${BINARY_URL}"
fi

# 停服（如已存在）以替换二进制
if systemctl is-active --quiet texas-holdem 2>/dev/null; then
    step "停止运行中的服务以替换二进制"
    systemctl stop texas-holdem
fi

install -m 0755 -o root -g root "${TMP_BIN}" "${INSTALL_DIR}/${BINARY_NAME}"
step "二进制就位: ${INSTALL_DIR}/${BINARY_NAME}"

# ============================================================
# 6. 写环境文件（保留旧 AUTH_SECRET / WX_APPID / WX_APPSECRET）
# ============================================================

EXISTING_SECRET=""
EXISTING_WX_APPID=""
EXISTING_WX_APPSECRET=""
if [[ -f "${ENV_FILE}" ]]; then
    EXISTING_SECRET="$(awk -F= '/^AUTH_SECRET=/{print substr($0, index($0, "=")+1)}' "${ENV_FILE}" || true)"
    EXISTING_WX_APPID="$(awk -F= '/^WX_APPID=/{print substr($0, index($0, "=")+1)}' "${ENV_FILE}" || true)"
    EXISTING_WX_APPSECRET="$(awk -F= '/^WX_APPSECRET=/{print substr($0, index($0, "=")+1)}' "${ENV_FILE}" || true)"
fi

if [[ -z "${EXISTING_SECRET}" ]]; then
    if command -v openssl >/dev/null 2>&1; then
        EXISTING_SECRET="$(openssl rand -hex 32)"
    else
        EXISTING_SECRET="$(head -c 32 /dev/urandom | xxd -p -c 64)"
    fi
    step "生成新的 AUTH_SECRET"
else
    step "保留已存在的 AUTH_SECRET"
fi

cat > "${ENV_FILE}" <<EOF
# texas-holdem 服务环境变量
# 生成时间: $(date -Iseconds)
ADDR=${LISTEN_ADDR}
AUTH_SECRET=${EXISTING_SECRET}
AUTH_TTL=168h
AUTH_REQUIRED=1
ALLOWED_ORIGINS=
WX_APPID=${EXISTING_WX_APPID}
WX_APPSECRET=${EXISTING_WX_APPSECRET}
STATE_FILE=${STATE_DIR}/state.json
STATE_SAVE_INTERVAL=30s
EOF

chmod 640 "${ENV_FILE}"
chown root:"${RUN_USER}" "${ENV_FILE}"
step "环境文件: ${ENV_FILE}"

if [[ -z "${EXISTING_WX_APPID}" ]]; then
    warn "WX_APPID/WX_APPSECRET 未配置，/login 将运行在 DEV 模式"
    warn "上线前请编辑 ${ENV_FILE} 填入真实凭据并 systemctl restart texas-holdem"
fi

# ============================================================
# 7. systemd 单元
# ============================================================

cat > "${SYSTEMD_UNIT}" <<EOF
[Unit]
Description=Texas Holdem WS server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${RUN_USER}
Group=${RUN_USER}
EnvironmentFile=${ENV_FILE}
ExecStart=${INSTALL_DIR}/${BINARY_NAME}
Restart=on-failure
RestartSec=2s

# 硬化
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictAddressFamilies=AF_INET AF_INET6
RestrictNamespaces=true
LockPersonality=true
MemoryDenyWriteExecute=true
ReadWritePaths=${STATE_DIR}

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
step "systemd 单元: ${SYSTEMD_UNIT}"

# ============================================================
# 8. 安装并配置 Caddy
# ============================================================

if ! command -v caddy >/dev/null 2>&1; then
    step "安装 Caddy（首次）"
    apt-get update -qq
    apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl gpg
    if [[ ! -f /usr/share/keyrings/caddy-stable-archive-keyring.gpg ]]; then
        curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
          | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    fi
    if [[ ! -f /etc/apt/sources.list.d/caddy-stable.list ]]; then
        curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
          > /etc/apt/sources.list.d/caddy-stable.list
    fi
    apt-get update -qq
    apt-get install -y caddy
else
    step "Caddy 已安装，跳过 apt"
fi

# 写 Caddyfile
APEX_DOMAIN="${DOMAIN#www.}"
HOSTS="${DOMAIN}"
if [[ "${APEX_DOMAIN}" != "${DOMAIN}" ]]; then
    HOSTS="${DOMAIN}, ${APEX_DOMAIN}"
fi

mkdir -p /var/log/caddy
chown caddy:caddy /var/log/caddy 2>/dev/null || true

cat > "${CADDYFILE}" <<EOF
${HOSTS} {
    encode gzip

    @ws {
        path /ws
        header Connection *Upgrade*
        header Upgrade websocket
    }
    handle @ws {
        reverse_proxy ${LISTEN_ADDR}
    }

    handle {
        reverse_proxy ${LISTEN_ADDR}
    }

    log {
        output file /var/log/caddy/access.log
        format console
    }
}
EOF

step "Caddyfile: ${CADDYFILE}"
caddy validate --config "${CADDYFILE}" >/dev/null

# ============================================================
# 9. 启动
# ============================================================

step "启动 texas-holdem"
systemctl enable --now texas-holdem
sleep 1
systemctl status texas-holdem --no-pager --lines=5 || true

step "重启 Caddy"
systemctl enable caddy
systemctl restart caddy
sleep 2

# ============================================================
# 10. 验证
# ============================================================

step "本地探测 ${LISTEN_ADDR}/health"
if curl -fsS "http://${LISTEN_ADDR}/health" | grep -q ok; then
    step "✅ 内部健康检查通过"
else
    warn "内部探测失败，检查 journalctl -u texas-holdem -n 50"
fi

step "外部探测 https://${DOMAIN}/health（需要域名 DNS 已解析、80/443 开放、Caddy 签证书完成）"
if curl -fsS --max-time 10 "https://${DOMAIN}/health" 2>/dev/null | grep -q ok; then
    step "✅ 外部健康检查通过"
else
    warn "外部探测未通过。可能原因："
    warn "  1) DNS 还没生效（dig ${DOMAIN} +short）"
    warn "  2) Caddy 还在签证书中（journalctl -u caddy -n 50 -f）"
    warn "  3) 防火墙没放 80/443"
fi

cat <<EOF

${GREEN}===== 完成 =====${RESET}

服务地址:
  https://${DOMAIN}/health     -> 健康检查
  https://${DOMAIN}/rooms      -> 房间列表
  https://${DOMAIN}/login      -> 登录（POST）
  wss://${DOMAIN}/ws           -> WebSocket

常用命令:
  systemctl status texas-holdem
  journalctl -u texas-holdem -f
  systemctl restart texas-holdem
  systemctl restart caddy
  cat ${ENV_FILE}

升级（直接重跑此脚本）:
  sudo bash install.sh --domain ${DOMAIN}

EOF
