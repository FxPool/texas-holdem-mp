#!/usr/bin/env bash
# 卸载 texas-holdem。可选保留持久化数据 + AUTH_SECRET。
# Caddy 不动（可能你还在用它跑别的站点）。
#
#   sudo bash uninstall.sh                # 保留数据
#   sudo bash uninstall.sh --purge        # 连数据一起删

set -euo pipefail
PURGE=false
[[ "${1:-}" == "--purge" ]] && PURGE=true

if [[ "${EUID}" -ne 0 ]]; then
    echo "请用 sudo 运行" >&2; exit 1
fi

systemctl disable --now texas-holdem 2>/dev/null || true
rm -f /etc/systemd/system/texas-holdem.service
rm -f /usr/local/bin/texas-holdem-server
systemctl daemon-reload

if [[ "${PURGE}" == "true" ]]; then
    rm -f /etc/default/texas-holdem
    rm -rf /var/lib/texas-holdem
    userdel texas 2>/dev/null || true
    echo "已 purge：环境文件、状态、用户都删除了"
else
    echo "保留 /etc/default/texas-holdem 和 /var/lib/texas-holdem。如果不需要，加 --purge 重跑。"
fi

# 把 Caddyfile 的 texas 站点段去掉？太脆弱，让用户自己改。
echo "提示: /etc/caddy/Caddyfile 仍然引用 texas-holdem，如不再需要请手工编辑。"
