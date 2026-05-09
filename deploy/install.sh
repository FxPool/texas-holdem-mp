#!/usr/bin/env bash
#
# Texas Hold'em Server — Interactive Installer
# https://github.com/FxPool/texas-holdem-mp
#

# When piped via `curl ... | sh`, the shell is sh/dash, not bash.
# Detect this and ask the user to use bash instead.
if [ -z "${BASH_VERSION:-}" ]; then
    echo ""
    echo "  [!] This script requires bash. Please run with:"
    echo ""
    echo "      curl -fsSL <url> | sudo bash"
    echo "    or:"
    echo "      sudo bash install.sh"
    echo ""
    exit 1
fi

set -euo pipefail

trap 'echo ""; echo "  [✗] Script failed at line $LINENO (exit code $?)"; echo "      Command: $BASH_COMMAND"; exit 1' ERR

REPO="${REPO:-FxPool/texas-holdem-mp}"
RELEASE_TAG="${RELEASE_TAG:-latest}"

INSTALL_DIR="/usr/local/bin"
SERVICE_DIR="/etc/systemd/system"
BIN_NAME="texas-holdem-server"
SERVICE_NAME="texas-holdem"
RUN_USER="texas"
DATA_DIR="/var/lib/texas-holdem"
ENV_FILE="/etc/default/texas-holdem"
SYSCTL_FILE="/etc/sysctl.d/99-texas-holdem.conf"
LIMITS_FILE="/etc/security/limits.d/texas-holdem.conf"
CADDYFILE="/etc/caddy/Caddyfile"
LISTEN_ADDR="127.0.0.1:18080"

VERSION="0.1.0"
BUILD="2026-05-09"

# ─── Colors ───────────────────────────────────────────────────────
if [ -t 1 ]; then
    RED=$'\033[0;31m'; GREEN=$'\033[0;32m'; YELLOW=$'\033[1;33m'; CYAN=$'\033[0;36m'
    BOLD=$'\033[1m'; DIM=$'\033[2m'; RESET=$'\033[0m'
else
    RED=''; GREEN=''; YELLOW=''; CYAN=''; BOLD=''; DIM=''; RESET=''
fi

print_banner() {
    echo ""
    printf "${CYAN}${BOLD}"
    echo "  ╔══════════════════════════════════════════╗"
    echo "  ║      Texas Hold'em Server Installer      ║"
    echo "  ║                v${VERSION}                    ║"
    echo "  ╚══════════════════════════════════════════╝"
    printf "${RESET}"
    echo ""
    printf "  ${DIM}build ${BUILD}${RESET}\n"
}

info()   { printf "  ${GREEN}[✓]${RESET} %s\n" "$1"; }
warn()   { printf "  ${YELLOW}[!]${RESET} %s\n" "$1"; }
error()  { printf "  ${RED}[✗]${RESET} %s\n" "$1" >&2; }
step()   { printf "\n  ${CYAN}${BOLD}▸ %s${RESET}\n" "$1"; }
prompt() { printf "  ${BOLD}%s${RESET}" "$1"; }
hr()     { printf "  ${DIM}────────────────────${RESET}\n"; }

# ─── Detect Architecture ─────────────────────────────────────────
detect_arch() {
    local os arch
    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    arch=$(uname -m)

    case "$arch" in
        x86_64|amd64)  arch="amd64" ;;
        aarch64|arm64) arch="arm64" ;;
        *)             error "Unsupported architecture: $arch (only amd64/arm64)"; exit 1 ;;
    esac

    if [ "$os" != "linux" ]; then
        error "Unsupported OS: $os (only linux)"
        exit 1
    fi

    DETECTED_OS="$os"
    DETECTED_ARCH="$arch"
    PLATFORM="${os}-${arch}"
}

# ─── Check Root ───────────────────────────────────────────────────
check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        error "This script must be run as root (use sudo)"
        exit 1
    fi
}

require_systemd() {
    if [ ! -d /run/systemd/system ]; then
        error "systemd is required (this script targets Debian/Ubuntu)"
        exit 1
    fi
}

# ─── Download tarball + extract binary ───────────────────────────
resolve_tarball_url() {
    if [ -n "${TARBALL_URL:-}" ]; then
        echo "${TARBALL_URL}"
        return
    fi
    echo "https://github.com/FxPool/texas-holdem-mp/releases/download/v${VERSION}/texas-holdem-server-linux-amd64.tar.gz"
}

download_to() {
    local url="$1" dest="$2"
    if command -v curl >/dev/null 2>&1; then
        curl -fL --progress-bar -o "$dest" "$url" || { error "Download failed"; exit 1; }
    elif command -v wget >/dev/null 2>&1; then
        if wget --help 2>&1 | grep -q 'show-progress'; then
            wget -q --show-progress -O "$dest" "$url" || { error "Download failed"; exit 1; }
        else
            wget -O "$dest" "$url" || { error "Download failed"; exit 1; }
        fi
    else
        error "Neither curl nor wget found. Please install one."
        exit 1
    fi
}

fetch_and_install_binary() {
    local url tmpdir tar_path bin_path size magic
    url="$(resolve_tarball_url)"
    step "Downloading ${url}"

    tmpdir=$(mktemp -d)
    trap "rm -rf $tmpdir" EXIT

    tar_path="${tmpdir}/binary.tar.gz"
    download_to "$url" "$tar_path"

    size=$(wc -c < "$tar_path")
    if [ "$size" -lt 100000 ]; then
        warn "Downloaded file is suspiciously small (${size} bytes)"
        head -c 200 "$tar_path" >&2
        error "Not a valid tar.gz"
        exit 1
    fi

    step "Extracting"
    tar -xzf "$tar_path" -C "$tmpdir"
    bin_path="${tmpdir}/${BIN_NAME}-${PLATFORM}"
    if [ ! -f "$bin_path" ]; then
        error "Tarball did not contain ${BIN_NAME}-${PLATFORM}"
        exit 1
    fi

    magic=$(head -c 4 "$bin_path" | od -An -t x1 | tr -d ' \n')
    if [ "$magic" != "7f454c46" ]; then
        error "Extracted file is not an ELF executable (magic=${magic})"
        exit 1
    fi

    if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
        step "Stopping running service to swap binary"
        systemctl stop "$SERVICE_NAME"
    fi

    install -m 0755 -o root -g root "$bin_path" "${INSTALL_DIR}/${BIN_NAME}"
    info "Binary installed to ${INSTALL_DIR}/${BIN_NAME} ($(du -h ${INSTALL_DIR}/${BIN_NAME} | cut -f1))"
}

# ─── User & Directories ──────────────────────────────────────────
ensure_user_and_dirs() {
    if id -u "$RUN_USER" >/dev/null 2>&1; then
        info "User ${RUN_USER} exists"
    else
        step "Creating system user ${RUN_USER}"
        useradd --system --no-create-home --shell /usr/sbin/nologin "$RUN_USER"
    fi
    mkdir -p "$DATA_DIR"
    chown "${RUN_USER}:${RUN_USER}" "$DATA_DIR"
}

# ─── Environment File (preserve secrets across reinstalls) ───────
write_env_file() {
    local existing_secret="" existing_appid="" existing_appsecret=""
    if [ -f "$ENV_FILE" ]; then
        existing_secret=$(awk -F= '/^AUTH_SECRET=/{print substr($0, index($0, "=")+1)}' "$ENV_FILE" || true)
        existing_appid=$(awk -F= '/^WX_APPID=/{print substr($0, index($0, "=")+1)}' "$ENV_FILE" || true)
        existing_appsecret=$(awk -F= '/^WX_APPSECRET=/{print substr($0, index($0, "=")+1)}' "$ENV_FILE" || true)
    fi

    if [ -z "$existing_secret" ]; then
        if command -v openssl >/dev/null 2>&1; then
            existing_secret=$(openssl rand -hex 32)
        else
            existing_secret=$(head -c 32 /dev/urandom | xxd -p -c 64)
        fi
        info "Generated new AUTH_SECRET"
    else
        info "Preserved existing AUTH_SECRET"
    fi

    cat > "$ENV_FILE" <<EOF
# texas-holdem 服务环境变量
# 生成时间: $(date -Iseconds)
ADDR=${LISTEN_ADDR}
AUTH_SECRET=${existing_secret}
AUTH_TTL=168h
AUTH_REQUIRED=1
ALLOWED_ORIGINS=
WX_APPID=${existing_appid}
WX_APPSECRET=${existing_appsecret}
STATE_FILE=${DATA_DIR}/state.json
STATE_SAVE_INTERVAL=30s
EOF

    chmod 640 "$ENV_FILE"
    chown "root:${RUN_USER}" "$ENV_FILE"
    info "Environment file: ${ENV_FILE}"

    if [ -z "$existing_appid" ]; then
        warn "WX_APPID/WX_APPSECRET not set — /login will run in DEV mode"
        warn "Edit ${ENV_FILE} and run: $0 restart"
    fi
}

# ─── Kernel + ulimit Tuning ──────────────────────────────────────
apply_tuning() {
    step "Writing kernel parameters ${SYSCTL_FILE}"
    cat > "$SYSCTL_FILE" <<EOF
# texas-holdem: high-concurrency WebSocket / long-connection tuning
fs.file-max = 1048576
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
net.core.netdev_max_backlog = 16384
net.ipv4.ip_local_port_range = 1024 65535
net.ipv4.tcp_tw_reuse = 1
net.ipv4.tcp_fin_timeout = 15
net.ipv4.tcp_keepalive_time = 600
net.ipv4.tcp_keepalive_intvl = 30
net.ipv4.tcp_keepalive_probes = 6
net.core.rmem_default = 262144
net.core.wmem_default = 262144
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
EOF
    sysctl -p "$SYSCTL_FILE" >/dev/null 2>&1 || true
    info "sysctl applied"

    step "Writing ulimit ${LIMITS_FILE}"
    cat > "$LIMITS_FILE" <<EOF
# texas-holdem: raise per-process file descriptor limit
*       soft    nofile  1048576
*       hard    nofile  1048576
root    soft    nofile  1048576
root    hard    nofile  1048576
${RUN_USER}  soft  nofile  1048576
${RUN_USER}  hard  nofile  1048576
EOF
    info "limits.d written (new logins inherit; systemd LimitNOFILE applies immediately)"
}

# ─── systemd Unit ────────────────────────────────────────────────
write_systemd_unit() {
    cat > "${SERVICE_DIR}/${SERVICE_NAME}.service" <<EOF
[Unit]
Description=Texas Hold'em WS server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${RUN_USER}
Group=${RUN_USER}
EnvironmentFile=${ENV_FILE}
ExecStart=${INSTALL_DIR}/${BIN_NAME}
Restart=on-failure
RestartSec=2s

LimitNOFILE=1048576
LimitNPROC=65535

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
ReadWritePaths=${DATA_DIR}

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    info "systemd unit: ${SERVICE_DIR}/${SERVICE_NAME}.service"
}

# ─── Caddy ───────────────────────────────────────────────────────
install_caddy_if_missing() {
    if command -v caddy >/dev/null 2>&1; then
        info "Caddy already installed"
        return
    fi
    step "Installing Caddy"
    apt-get update -qq
    apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl gpg
    if [ ! -f /usr/share/keyrings/caddy-stable-archive-keyring.gpg ]; then
        curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
            | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    fi
    if [ ! -f /etc/apt/sources.list.d/caddy-stable.list ]; then
        curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
            > /etc/apt/sources.list.d/caddy-stable.list
    fi
    apt-get update -qq
    apt-get install -y caddy
}

write_caddyfile() {
    local domain="$1"
    local apex="${domain#www.}"
    local hosts="$domain"
    if [ "$apex" != "$domain" ]; then
        hosts="${domain}, ${apex}"
    fi

    mkdir -p /var/log/caddy
    chown caddy:caddy /var/log/caddy 2>/dev/null || true

    cat > "$CADDYFILE" <<EOF
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
    caddy validate --config "$CADDYFILE" >/dev/null
    info "Caddyfile: ${CADDYFILE}"
}

# ─── Health Verification ─────────────────────────────────────────
verify_health() {
    step "Health check"
    if curl -fsS "http://${LISTEN_ADDR}/health" 2>/dev/null | grep -q ok; then
        info "Internal probe ${LISTEN_ADDR}/health: ok"
    else
        warn "Internal probe failed; check: journalctl -u ${SERVICE_NAME} -n 50"
    fi

    if [ -n "${DOMAIN:-}" ]; then
        if curl -fsS --max-time 10 "https://${DOMAIN}/health" 2>/dev/null | grep -q ok; then
            info "External probe https://${DOMAIN}/health: ok"
        else
            warn "External probe failed. Possible reasons:"
            warn "  - DNS not yet resolved (dig ${DOMAIN} +short)"
            warn "  - Caddy still issuing certificate (journalctl -u caddy -f)"
            warn "  - Firewall blocking 80/443"
        fi
    fi
}

# ─── Install ─────────────────────────────────────────────────────
install_server() {
    require_systemd

    if [ -z "${DOMAIN:-}" ]; then
        if [ -t 0 ]; then
            echo ""
            local DEFAULT_DOMAIN="www.example.com"
            printf "  ${DIM}Domain must already point to this server's IP${RESET}\n"
            prompt "Domain [${DEFAULT_DOMAIN}]: "
            read -r DOMAIN < /dev/tty
            DOMAIN="${DOMAIN:-$DEFAULT_DOMAIN}"
        else
            error "Domain is required in non-interactive mode"
            error "Usage: curl ... | sudo DOMAIN=your.domain.com sh"
            error "   or: sudo sh install.sh install --domain your.domain.com"
            exit 1
        fi
    fi
    info "Domain:    ${DOMAIN}"
    info "Platform:  ${PLATFORM}"
    info "Source:    $(resolve_tarball_url)"
    echo ""

    ensure_user_and_dirs
    fetch_and_install_binary
    write_env_file
    apply_tuning
    write_systemd_unit
    install_caddy_if_missing
    write_caddyfile "$DOMAIN"

    step "Enabling and starting ${SERVICE_NAME}"
    systemctl enable --now "$SERVICE_NAME"
    sleep 1

    step "Restarting Caddy"
    systemctl enable caddy
    systemctl restart caddy
    sleep 2

    echo ""
    verify_health

    echo ""
    info "Installation complete"
    echo ""
    printf "  ${DIM}Endpoints:${RESET}\n"
    echo "    https://${DOMAIN}/health"
    echo "    https://${DOMAIN}/rooms"
    echo "    wss://${DOMAIN}/ws"
    echo ""
    printf "  ${DIM}Manage with:${RESET}\n"
    echo "    $0 status"
    echo "    $0 logs"
    echo "    $0 restart"
}

# ─── Update ──────────────────────────────────────────────────────
update_server() {
    if [ ! -f "${INSTALL_DIR}/${BIN_NAME}" ]; then
        warn "Server is not installed. Run install first."
        return
    fi
    if [ ! -f "$ENV_FILE" ]; then
        warn "Environment file ${ENV_FILE} missing. Run install first."
        return
    fi

    fetch_and_install_binary
    systemctl start "$SERVICE_NAME"
    sleep 1
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        info "Server restarted"
    else
        warn "Server failed to start after update. Recent logs:"
        echo ""
        journalctl -u "$SERVICE_NAME" -n 20 --no-pager 2>/dev/null || true
    fi
    verify_health
}

# ─── Uninstall ───────────────────────────────────────────────────
uninstall_server() {
    step "Uninstalling Texas Hold'em Server"

    if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
        systemctl stop "$SERVICE_NAME"
        info "Stopped ${SERVICE_NAME}"
    fi
    if [ -f "${SERVICE_DIR}/${SERVICE_NAME}.service" ]; then
        systemctl disable "$SERVICE_NAME" 2>/dev/null || true
        rm -f "${SERVICE_DIR}/${SERVICE_NAME}.service"
        info "Removed systemd unit"
    fi
    systemctl daemon-reload

    if [ -f "${INSTALL_DIR}/${BIN_NAME}" ]; then
        rm -f "${INSTALL_DIR}/${BIN_NAME}"
        info "Removed binary"
    fi

    rm -f "$SYSCTL_FILE" "$LIMITS_FILE"
    sysctl --system >/dev/null 2>&1 || true
    info "Removed sysctl + limits config"

    echo ""
    if [ -t 0 ]; then
        prompt "Also remove environment file (${ENV_FILE}) and state (${DATA_DIR})? [y/N]: "
        read -r RM_DATA < /dev/tty
    else
        RM_DATA="n"
        info "Non-interactive: preserving environment and state files"
    fi
    case "$RM_DATA" in
        [yY]*)
            rm -f "$ENV_FILE"
            rm -rf "$DATA_DIR"
            userdel "$RUN_USER" 2>/dev/null || true
            warn "Purged environment + state + user"
            ;;
        *)
            info "Preserved ${ENV_FILE} and ${DATA_DIR} (will be reused on next install)"
            ;;
    esac

    info "Caddyfile (${CADDYFILE}) was NOT removed — edit manually if no longer needed"
    info "Uninstall complete"
}

# ─── Service Control ─────────────────────────────────────────────
svc_check_installed() {
    if [ ! -f "${SERVICE_DIR}/${SERVICE_NAME}.service" ]; then
        error "Service is not installed. Run: $0 install"
        exit 1
    fi
}

svc_start()   { svc_check_installed; systemctl start   "$SERVICE_NAME" && info "${SERVICE_NAME} started"   || error "Failed to start"; }
svc_stop()    {                       systemctl stop    "$SERVICE_NAME" && info "${SERVICE_NAME} stopped"   || error "Failed to stop"; }
svc_restart() { svc_check_installed; systemctl restart "$SERVICE_NAME" && info "${SERVICE_NAME} restarted" || error "Failed to restart"; verify_health; }
svc_logs()    { journalctl -u "$SERVICE_NAME" -f --no-pager -n 50; }

apply_tuning_only() {
    apply_tuning
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        warn "Restarting ${SERVICE_NAME} to apply LimitNOFILE"
        systemctl restart "$SERVICE_NAME"
    fi
}

# ─── Status ──────────────────────────────────────────────────────
show_status() {
    step "Server Status"
    echo ""

    if [ -f "${INSTALL_DIR}/${BIN_NAME}" ]; then
        printf "  Binary:       ${GREEN}installed${RESET}  (${INSTALL_DIR}/${BIN_NAME}, $(du -h ${INSTALL_DIR}/${BIN_NAME} | cut -f1))\n"
    else
        printf "  Binary:       ${DIM}not installed${RESET}\n"
    fi

    if systemctl is-active --quiet "$SERVICE_NAME" 2>/dev/null; then
        printf "  ${SERVICE_NAME}: ${GREEN}running${RESET}\n"
    elif [ -f "${SERVICE_DIR}/${SERVICE_NAME}.service" ]; then
        printf "  ${SERVICE_NAME}: ${YELLOW}stopped${RESET}\n"
    else
        printf "  ${SERVICE_NAME}: ${DIM}not configured${RESET}\n"
    fi

    if systemctl is-active --quiet caddy 2>/dev/null; then
        printf "  Caddy:        ${GREEN}running${RESET}\n"
    elif command -v caddy >/dev/null 2>&1; then
        printf "  Caddy:        ${YELLOW}stopped${RESET}\n"
    else
        printf "  Caddy:        ${DIM}not installed${RESET}\n"
    fi

    echo ""
    printf "  Platform:     ${BOLD}${PLATFORM}${RESET}\n"
    if [ -f "$ENV_FILE" ]; then
        local domain_hint
        domain_hint=$(grep '^# Domain' "$ENV_FILE" 2>/dev/null | cut -d: -f2- | head -1)
        printf "  Env file:     %s\n" "$ENV_FILE"
    fi
    if [ -f "$CADDYFILE" ]; then
        local hosts
        hosts=$(head -1 "$CADDYFILE" | sed 's/ {.*//')
        printf "  Domains:      ${BOLD}%s${RESET}\n" "$hosts"
    fi
    if [ -f "${DATA_DIR}/state.json" ]; then
        local sz
        sz=$(du -h "${DATA_DIR}/state.json" | cut -f1)
        printf "  State file:   %s (%s)\n" "${DATA_DIR}/state.json" "$sz"
    fi

    echo ""
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        if curl -fsS "http://${LISTEN_ADDR}/health" >/dev/null 2>&1; then
            info "Internal /health: ok"
        else
            warn "Internal /health: failing"
        fi
    fi
}

# ─── Main Menu ────────────────────────────────────────────────────
main_menu() {
    print_banner
    detect_arch
    info "Detected platform: ${BOLD}${PLATFORM}${RESET}"
    info "Repository:        ${REPO}"
    info "Release tag:       ${RELEASE_TAG}"
    echo ""

    printf "  ${BOLD}Select an option:${RESET}\n"
    echo ""
    printf "    ${CYAN}1)${RESET}  Install Server\n"
    printf "    ${CYAN}2)${RESET}  Update Server\n"
    printf "    ${CYAN}3)${RESET}  Uninstall Server\n"
    hr
    printf "    ${CYAN}4)${RESET}  Start Server\n"
    printf "    ${CYAN}5)${RESET}  Stop Server\n"
    printf "    ${CYAN}6)${RESET}  Restart Server\n"
    printf "    ${CYAN}7)${RESET}  View Logs\n"
    hr
    printf "    ${CYAN}8)${RESET}  Apply Tuning (sysctl + ulimit)\n"
    printf "    ${CYAN}9)${RESET}  Show Status\n"
    printf "    ${CYAN}0)${RESET}  Exit\n"
    echo ""
    prompt "Enter choice [0-9]: "
    read -r choice < /dev/tty

    case "$choice" in
        1) check_root; install_server ;;
        2) check_root; update_server ;;
        3) check_root; uninstall_server ;;
        4) check_root; svc_start ;;
        5) check_root; svc_stop ;;
        6) check_root; svc_restart ;;
        7) svc_logs ;;
        8) check_root; apply_tuning_only ;;
        9) show_status ;;
        0) echo "  Bye."; exit 0 ;;
        *) error "Invalid choice"; exit 1 ;;
    esac

    echo ""
    info "Done!"
    echo ""
}

# ─── Argument Parsing ─────────────────────────────────────────────
parse_extra_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --domain)       DOMAIN="$2"; shift 2 ;;
            --release-tag)  RELEASE_TAG="$2"; shift 2 ;;
            --tarball-url)  TARBALL_URL="$2"; shift 2 ;;
            --repo)         REPO="$2"; shift 2 ;;
            *)              shift ;;
        esac
    done
}

cmd="${1:-}"
shift 2>/dev/null || true
parse_extra_args "$@"

case "$cmd" in
    install)    check_root; detect_arch; install_server ;;
    update)     check_root; detect_arch; update_server ;;
    uninstall)  check_root; detect_arch; uninstall_server ;;
    start)      check_root; detect_arch; svc_start ;;
    stop)       check_root; detect_arch; svc_stop ;;
    restart)    check_root; detect_arch; svc_restart ;;
    logs)       detect_arch; svc_logs ;;
    status)     detect_arch; show_status ;;
    tune)       check_root; detect_arch; apply_tuning_only ;;
    "")
        if [ -t 0 ]; then
            main_menu
        else
            detect_arch
            print_banner
            check_root
            info "Detected platform: ${BOLD}${PLATFORM}${RESET}"
            info "Non-interactive mode — running install"
            echo ""
            install_server
        fi
        ;;
    -h|--help)
        cat <<EOF
Texas Hold'em Server installer (v${VERSION})

Usage:
  sudo $0                                  # interactive menu
  sudo $0 install [--domain X] [--release-tag vX.Y.Z]
  sudo $0 update
  sudo $0 uninstall
  sudo $0 start | stop | restart | logs | status | tune

Remote install:
  curl -fsSL <url>/install.sh | sudo DOMAIN=your.domain.com bash

Environment:
  DOMAIN=your.domain.com    domain name (required for non-interactive install)
  REPO=owner/name           override default repo (${REPO})
  RELEASE_TAG=vX.Y.Z        pin a release (default: latest)
  TARBALL_URL=https://...   bypass release URL composition entirely
EOF
        ;;
    *)
        error "Unknown command: $cmd"
        echo "Run with no arguments for menu, or: $0 --help"
        exit 1
        ;;
esac
