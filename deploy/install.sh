#!/usr/bin/env bash
# texas-holdem 服务管理脚本 —— 安装 / 升级 / 启停 / 卸载 一站式
#
# 交互菜单：
#   sudo bash install.sh
#
# 直接子命令：
#   sudo bash install.sh install [--domain X] [--release-tag vX.Y.Z]
#   sudo bash install.sh update                         # 拉新二进制 + 重启
#   sudo bash install.sh start | stop | restart | status
#   sudo bash install.sh logs [-n 200]
#   sudo bash install.sh tune                           # 重新写 sysctl/limits
#   sudo bash install.sh uninstall [--purge]
#
# 重新运行 install 子命令会保留 AUTH_SECRET / WX_APPID / WX_APPSECRET，
# 不让旧 token 失效。
#
# 仅在 Debian 11/12、Ubuntu 22.04 上测试。需要 root（或 sudo）。

set -euo pipefail

# ============================================================
# 配置
# ============================================================

GITHUB_REPO="${GITHUB_REPO:-FxPool/texas-holdem-mp}"
RELEASE_TAG="${RELEASE_TAG:-latest}"
TARBALL_URL="${TARBALL_URL:-}"
DOMAIN="${DOMAIN:-}"

INSTALL_DIR="/usr/local/bin"
BINARY_NAME="texas-holdem-server"
RUN_USER="texas"
ENV_FILE="/etc/default/texas-holdem"
SYSTEMD_UNIT="/etc/systemd/system/texas-holdem.service"
CADDYFILE="/etc/caddy/Caddyfile"
SYSCTL_FILE="/etc/sysctl.d/99-texas-holdem.conf"
LIMITS_FILE="/etc/security/limits.d/texas-holdem.conf"
STATE_DIR="/var/lib/texas-holdem"
LISTEN_ADDR="127.0.0.1:18080"

# ============================================================
# 输出
# ============================================================

if [[ -t 1 ]]; then
    GREEN=$'\033[0;32m'; YELLOW=$'\033[0;33m'; RED=$'\033[0;31m'
    BLUE=$'\033[0;34m'; BOLD=$'\033[1m'; RESET=$'\033[0m'
else
    GREEN=''; YELLOW=''; RED=''; BLUE=''; BOLD=''; RESET=''
fi

step()  { printf "${GREEN}==>${RESET} %s\n" "$*"; }
info()  { printf "${BLUE} i ${RESET} %s\n" "$*"; }
warn()  { printf "${YELLOW}!! ${RESET} %s\n" "$*" >&2; }
die()   { printf "${RED}xx ${RESET} %s\n" "$*" >&2; exit 1; }
hr()    { printf '%s\n' "----------------------------------------------------"; }

require_root() {
    if [[ "${EUID}" -ne 0 ]]; then
        die "请用 sudo 或 root 运行此脚本"
    fi
}

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || die "缺少 $1，请先安装"
}

require_systemd() {
    [[ -d /run/systemd/system ]] || die "需要 systemd（当前系统未运行 systemd）"
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *) die "不支持的架构: $(uname -m)（仅支持 amd64/arm64）" ;;
    esac
}

# ============================================================
# 各步骤函数
# ============================================================

ensure_user_and_dirs() {
    if id -u "${RUN_USER}" >/dev/null 2>&1; then
        info "用户 ${RUN_USER} 已存在"
    else
        step "创建运行用户 ${RUN_USER}"
        useradd --system --no-create-home --shell /usr/sbin/nologin "${RUN_USER}"
    fi
    mkdir -p "${STATE_DIR}"
    chown "${RUN_USER}:${RUN_USER}" "${STATE_DIR}"
}

resolve_tarball_url() {
    local arch="$1"
    if [[ -n "${TARBALL_URL}" ]]; then
        echo "${TARBALL_URL}"
        return
    fi
    if [[ "${RELEASE_TAG}" == "latest" ]]; then
        echo "https://github.com/${GITHUB_REPO}/releases/latest/download/${BINARY_NAME}-linux-${arch}.tar.gz"
    else
        echo "https://github.com/${GITHUB_REPO}/releases/download/${RELEASE_TAG}/${BINARY_NAME}-linux-${arch}.tar.gz"
    fi
}

# 下载并解压二进制；服务在跑会先停后替换
download_and_install_binary() {
    local arch url tmpdir tar_path bin_path size magic
    arch="$(detect_arch)"
    url="$(resolve_tarball_url "${arch}")"
    step "下载 ${url}"

    tmpdir="$(mktemp -d)"
    trap 'rm -rf "${tmpdir}"' RETURN

    tar_path="${tmpdir}/binary.tar.gz"
    if ! curl -fsSL --retry 3 -o "${tar_path}" "${url}"; then
        die "下载失败。检查 GITHUB_REPO（${GITHUB_REPO}）和 release（${RELEASE_TAG}）是否已发布。"
    fi

    size="$(wc -c < "${tar_path}")"
    if [[ "${size}" -lt 100000 ]]; then
        warn "下载文件太小（${size} 字节），可能是 GitHub 错误页"
        head -c 200 "${tar_path}" >&2
        die "下载文件不像有效的 tar.gz"
    fi

    step "解压"
    tar -xzf "${tar_path}" -C "${tmpdir}"
    bin_path="${tmpdir}/${BINARY_NAME}-linux-${arch}"
    [[ -f "${bin_path}" ]] || die "tar 包内没找到 ${BINARY_NAME}-linux-${arch}"

    magic="$(head -c 4 "${bin_path}" | od -An -t x1 | tr -d ' \n')"
    [[ "${magic}" == "7f454c46" ]] || die "解压出的文件不是 ELF（前 4 字节 ${magic}）"

    if systemctl is-active --quiet texas-holdem 2>/dev/null; then
        step "停止运行中的服务以替换二进制"
        systemctl stop texas-holdem
    fi

    install -m 0755 -o root -g root "${bin_path}" "${INSTALL_DIR}/${BINARY_NAME}"
    step "二进制就位: ${INSTALL_DIR}/${BINARY_NAME} ($(du -h "${INSTALL_DIR}/${BINARY_NAME}" | cut -f1))"
}

write_env_file() {
    local existing_secret="" existing_appid="" existing_appsecret=""
    if [[ -f "${ENV_FILE}" ]]; then
        existing_secret="$(awk -F= '/^AUTH_SECRET=/{print substr($0, index($0, "=")+1)}' "${ENV_FILE}" || true)"
        existing_appid="$(awk -F= '/^WX_APPID=/{print substr($0, index($0, "=")+1)}' "${ENV_FILE}" || true)"
        existing_appsecret="$(awk -F= '/^WX_APPSECRET=/{print substr($0, index($0, "=")+1)}' "${ENV_FILE}" || true)"
    fi

    if [[ -z "${existing_secret}" ]]; then
        if command -v openssl >/dev/null 2>&1; then
            existing_secret="$(openssl rand -hex 32)"
        else
            existing_secret="$(head -c 32 /dev/urandom | xxd -p -c 64)"
        fi
        step "生成新的 AUTH_SECRET"
    else
        info "保留已存在的 AUTH_SECRET"
    fi

    cat > "${ENV_FILE}" <<EOF
# texas-holdem 服务环境变量
# 生成时间: $(date -Iseconds)
ADDR=${LISTEN_ADDR}
AUTH_SECRET=${existing_secret}
AUTH_TTL=168h
AUTH_REQUIRED=1
ALLOWED_ORIGINS=
WX_APPID=${existing_appid}
WX_APPSECRET=${existing_appsecret}
STATE_FILE=${STATE_DIR}/state.json
STATE_SAVE_INTERVAL=30s
EOF

    chmod 640 "${ENV_FILE}"
    chown root:"${RUN_USER}" "${ENV_FILE}"
    info "环境文件: ${ENV_FILE}"

    if [[ -z "${existing_appid}" ]]; then
        warn "WX_APPID/WX_APPSECRET 未配置，/login 走 DEV 模式（信任客户端 uid）"
        warn "正式上线请编辑 ${ENV_FILE} 并 systemctl restart texas-holdem"
    fi
}

# 内核 + ulimit 调优：高并发 WebSocket 必备
apply_sysctl_and_limits() {
    step "写入内核网络参数 ${SYSCTL_FILE}"
    cat > "${SYSCTL_FILE}" <<EOF
# texas-holdem: 高并发 WebSocket / 长连接调优
# 系统全局打开文件数上限（含 socket）
fs.file-max = 1048576
# accept 队列长度
net.core.somaxconn = 65535
# SYN 队列长度
net.ipv4.tcp_max_syn_backlog = 65535
# 设备 backlog
net.core.netdev_max_backlog = 16384
# 端口范围（出向连接用）
net.ipv4.ip_local_port_range = 1024 65535
# 复用 TIME_WAIT 端口
net.ipv4.tcp_tw_reuse = 1
# 缩短 FIN_WAIT_2
net.ipv4.tcp_fin_timeout = 15
# 长连接保活
net.ipv4.tcp_keepalive_time = 600
net.ipv4.tcp_keepalive_intvl = 30
net.ipv4.tcp_keepalive_probes = 6
# socket 缓冲
net.core.rmem_default = 262144
net.core.wmem_default = 262144
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
EOF

    sysctl -p "${SYSCTL_FILE}" >/dev/null
    info "已应用 sysctl 参数"

    step "写入 ulimit ${LIMITS_FILE}"
    cat > "${LIMITS_FILE}" <<EOF
# texas-holdem: 提升单进程最大打开文件数
*       soft    nofile  1048576
*       hard    nofile  1048576
root    soft    nofile  1048576
root    hard    nofile  1048576
${RUN_USER}  soft  nofile  1048576
${RUN_USER}  hard  nofile  1048576
EOF
    info "已写入 limits（注意：limits.conf 改动只对新登录会话生效；systemd 服务通过 LimitNOFILE 立即生效）"
}

write_systemd_unit() {
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

# 资源上限（高并发 WS）
LimitNOFILE=1048576
LimitNPROC=65535

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
    info "systemd unit: ${SYSTEMD_UNIT}"
}

install_caddy_if_missing() {
    if command -v caddy >/dev/null 2>&1; then
        info "Caddy 已安装"
        return
    fi
    step "安装 Caddy"
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
}

write_caddyfile() {
    local domain="$1"
    local apex="${domain#www.}"
    local hosts="${domain}"
    if [[ "${apex}" != "${domain}" ]]; then
        hosts="${domain}, ${apex}"
    fi

    mkdir -p /var/log/caddy
    chown caddy:caddy /var/log/caddy 2>/dev/null || true

    cat > "${CADDYFILE}" <<EOF
${hosts} {
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
    info "Caddyfile: ${CADDYFILE}"
    caddy validate --config "${CADDYFILE}" >/dev/null
}

verify_health() {
    step "本地探测 http://${LISTEN_ADDR}/health"
    if curl -fsS "http://${LISTEN_ADDR}/health" 2>/dev/null | grep -q ok; then
        info "✅ 内部健康检查通过"
    else
        warn "内部探测失败，看 journalctl -u texas-holdem -n 50"
    fi

    if [[ -n "${DOMAIN}" ]]; then
        step "外部探测 https://${DOMAIN}/health"
        if curl -fsS --max-time 10 "https://${DOMAIN}/health" 2>/dev/null | grep -q ok; then
            info "✅ 外部健康检查通过"
        else
            warn "外部探测未通过，可能原因："
            warn "  1) DNS 还没生效（dig ${DOMAIN} +short）"
            warn "  2) Caddy 还在签证书（journalctl -u caddy -f）"
            warn "  3) 防火墙没放 80/443"
        fi
    fi
}

# ============================================================
# 子命令
# ============================================================

cmd_install() {
    require_root
    require_systemd
    require_cmd curl curl
    require_cmd tar tar

    if [[ -z "${DOMAIN}" ]]; then
        read -rp "请输入域名（如 www.zhoudegame.xyz）: " DOMAIN
    fi
    [[ -z "${DOMAIN}" ]] && die "域名不能为空"

    info "目标域名:  ${DOMAIN}"
    info "架构:      $(detect_arch)"
    info "下载源:    $(resolve_tarball_url "$(detect_arch)")"
    hr

    ensure_user_and_dirs
    download_and_install_binary
    write_env_file
    apply_sysctl_and_limits
    write_systemd_unit
    install_caddy_if_missing
    write_caddyfile "${DOMAIN}"

    step "启动 texas-holdem"
    systemctl enable --now texas-holdem
    sleep 1

    step "重启 Caddy"
    systemctl enable caddy
    systemctl restart caddy
    sleep 2

    hr
    verify_health

    cat <<EOF

${GREEN}===== 完成 =====${RESET}
  https://${DOMAIN}/health
  https://${DOMAIN}/rooms
  wss://${DOMAIN}/ws
EOF
}

cmd_update() {
    require_root
    require_systemd
    require_cmd curl curl
    require_cmd tar tar

    [[ -f "${ENV_FILE}" ]] || die "尚未安装。先运行 install。"
    info "升级到 release: ${RELEASE_TAG}"
    download_and_install_binary
    systemctl start texas-holdem
    sleep 1
    systemctl status texas-holdem --no-pager --lines=5 || true
    verify_health
}

cmd_start()   { require_root; systemctl start texas-holdem;   systemctl status texas-holdem --no-pager --lines=5; }
cmd_stop()    { require_root; systemctl stop texas-holdem;    systemctl status texas-holdem --no-pager --lines=5 || true; }
cmd_restart() { require_root; systemctl restart texas-holdem; systemctl status texas-holdem --no-pager --lines=5; verify_health; }
cmd_status()  {
    systemctl status texas-holdem --no-pager --lines=20 || true
    hr
    systemctl status caddy --no-pager --lines=5 || true
}

cmd_logs() {
    local n="${1:-200}"
    journalctl -u texas-holdem -n "${n}" --no-pager
}

cmd_tune() {
    require_root
    apply_sysctl_and_limits
    if systemctl is-active --quiet texas-holdem; then
        warn "重启 texas-holdem 让 LimitNOFILE 生效"
        systemctl restart texas-holdem
    fi
}

cmd_uninstall() {
    require_root
    local purge="${1:-}"
    warn "即将卸载 texas-holdem"
    if [[ "${purge}" != "--purge" && "${purge}" != "-y" ]]; then
        read -rp "确认卸载？(y/N): " ans
        [[ "${ans}" =~ ^[Yy]$ ]] || { info "已取消"; return; }
    fi

    systemctl disable --now texas-holdem 2>/dev/null || true
    rm -f "${SYSTEMD_UNIT}"
    rm -f "${INSTALL_DIR}/${BINARY_NAME}"
    rm -f "${SYSCTL_FILE}"
    rm -f "${LIMITS_FILE}"
    sysctl --system >/dev/null 2>&1 || true
    systemctl daemon-reload

    if [[ "${purge}" == "--purge" ]]; then
        rm -f "${ENV_FILE}"
        rm -rf "${STATE_DIR}"
        userdel "${RUN_USER}" 2>/dev/null || true
        warn "已 purge：环境文件 / 状态 / 用户全部删除"
    else
        info "保留 ${ENV_FILE} 与 ${STATE_DIR}（重新安装会复用 AUTH_SECRET 与玩家筹码）"
        info "如需彻底删除：sudo bash $0 uninstall --purge"
    fi

    info "Caddyfile 仍引用 texas-holdem，如不再需要请手工编辑 ${CADDYFILE}"
}

# ============================================================
# 菜单
# ============================================================

show_menu() {
    require_root
    while true; do
        clear
        cat <<EOF
${BOLD}===========================================${RESET}
${BOLD} texas-holdem 服务管理${RESET}
${BOLD}===========================================${RESET}
 仓库:     ${GITHUB_REPO}
 release:  ${RELEASE_TAG}
 服务状态: $(systemctl is-active texas-holdem 2>/dev/null || echo 'not-installed')
 Caddy:    $(systemctl is-active caddy 2>/dev/null || echo 'not-installed')

  1) 安装 / 重装
  2) 升级二进制（拉新 release）
  3) 启动
  4) 停止
  5) 重启
  6) 查看状态
  7) 实时日志（journalctl -f）
  8) 重新应用内核 / ulimit 调优
  9) 卸载（保留数据）
 10) 卸载并清理（--purge）
  0) 退出
EOF
        echo
        read -rp "选择: " ch
        echo
        case "${ch}" in
            1) cmd_install; pause ;;
            2) cmd_update; pause ;;
            3) cmd_start; pause ;;
            4) cmd_stop; pause ;;
            5) cmd_restart; pause ;;
            6) cmd_status; pause ;;
            7) journalctl -u texas-holdem -f ;;
            8) cmd_tune; pause ;;
            9) cmd_uninstall; pause ;;
            10) cmd_uninstall --purge; pause ;;
            0) exit 0 ;;
            *) warn "未知选项"; sleep 1 ;;
        esac
    done
}

pause() {
    echo
    read -rp "按回车返回菜单..." _
}

# ============================================================
# 入口
# ============================================================

usage() {
    grep '^#' "$0" | sed 's/^# \{0,1\}//' | head -25
}

main() {
    if [[ $# -eq 0 ]]; then
        show_menu
        return
    fi

    local cmd="$1"; shift || true
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --domain) DOMAIN="$2"; shift 2 ;;
            --release-tag) RELEASE_TAG="$2"; shift 2 ;;
            --tarball-url) TARBALL_URL="$2"; shift 2 ;;
            --repo) GITHUB_REPO="$2"; shift 2 ;;
            --purge) PURGE_FLAG="--purge"; shift ;;
            -n) LOG_LINES="$2"; shift 2 ;;
            -h|--help) usage; exit 0 ;;
            *) die "未知参数: $1" ;;
        esac
    done

    case "${cmd}" in
        install)   cmd_install ;;
        update)    cmd_update ;;
        start)     cmd_start ;;
        stop)      cmd_stop ;;
        restart)   cmd_restart ;;
        status)    cmd_status ;;
        logs)      cmd_logs "${LOG_LINES:-200}" ;;
        tune)      cmd_tune ;;
        uninstall) cmd_uninstall "${PURGE_FLAG:-}" ;;
        menu)      show_menu ;;
        -h|--help) usage ;;
        *)         die "未知子命令: ${cmd}" ;;
    esac
}

main "$@"
