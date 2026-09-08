#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
[[ "$(uname -s)" == Linux && "$(id -u)" == 0 ]] || { echo '网页升级服务需要在 Linux 服务器以 root 或 sudo 启用。'; exit 1; }
command -v systemctl >/dev/null || { echo '当前系统没有 systemd，请继续使用服务器管理菜单更新。'; exit 1; }
[[ "$PWD" =~ ^/[A-Za-z0-9_/-]+$ && "$PWD" != / ]] || { echo '网页升级要求项目绝对路径仅含字母、数字、下划线、横线和斜杠。'; exit 1; }
[[ -d .git && -f .env && ! -L .updates ]] || { echo '请先完成 Git 安装并启动服务。'; exit 1; }
[[ ! -f .updates/active.json ]] || { echo '正在升级，请等待任务结束后再调整升级服务。'; exit 1; }
mkdir -p .updates
chown 10001:10001 .updates
chmod 750 .updates
install -m 0755 -d /usr/local/lib/tg-guard
next=$(mktemp /usr/local/lib/tg-guard/updater.XXXXXX)
trap 'rm -f -- "$next"' EXIT
docker compose --env-file .env cp app:/usr/local/bin/tgguard "$next"
chmod 755 "$next"
chown root:root "$next"
mv -- "$next" /usr/local/lib/tg-guard/updater
cat > /etc/systemd/system/tg-guard-updater.service <<EOF
[Unit]
Description=TG Guard stable release updater
After=network-online.target docker.service
Wants=network-online.target
[Service]
Type=simple
ExecStart=/usr/local/lib/tg-guard/updater -update-agent $PWD
WorkingDirectory=$PWD
Restart=on-failure
RestartSec=5
KillMode=control-group
TimeoutStopSec=30
UMask=0022
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable --now tg-guard-updater
systemctl restart tg-guard-updater
echo '网页升级服务已启用。请打开后台「系统升级」检查更新。'
