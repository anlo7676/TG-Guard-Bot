#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
umask 077
[[ -f .env && ! -L backups ]] || { echo '缺少 .env 或备份目录是符号链接，停止备份。' >&2; exit 1; }
mode=${1:-run}
if [[ "$mode" == --install ]]; then
  [[ "$(uname -s)" == Linux && "$(id -u)" == 0 && "$PWD" =~ ^/[A-Za-z0-9_/-]+$ && "$PWD" != / ]] || { echo '自动备份需要 Linux root/sudo，项目路径仅支持字母数字、横线、下划线和斜杠。' >&2; exit 1; }
  command -v systemctl >/dev/null
  cat > /etc/systemd/system/tg-guard-backup.service <<EOF
[Unit]
Description=TG Guard database and configuration backup
After=docker.service
[Service]
Type=oneshot
WorkingDirectory=$PWD
ExecStart=/bin/bash $PWD/scripts/backup.sh
UMask=0077
TimeoutStartSec=20min
EOF
  cat > /etc/systemd/system/tg-guard-backup.timer <<EOF
[Unit]
Description=Daily TG Guard backup
[Timer]
OnCalendar=*-*-* 03:30:00 UTC
Persistent=true
RandomizedDelaySec=300
[Install]
WantedBy=timers.target
EOF
  systemctl daemon-reload
  systemctl enable --now tg-guard-backup.timer
  echo '自动备份已启用：每天 UTC 03:30（北京时间 11:30）左右运行，保留 30 天；请另存异机副本。'
  exit 0
fi
[[ "$mode" == run ]] || { echo '用法：backup.sh [--install]' >&2; exit 1; }
for cmd in docker flock tar gzip sha256sum timeout git; do command -v "$cmd" >/dev/null; done
exec 9> .git/tg-guard-upgrade.lock
flock -n 9 || { echo '正在更新，请更新完成后再备份。' >&2; exit 1; }
mkdir -p backups
chmod 700 backups
exec 8> backups/.backup.lock
flock -n 8 || { echo '已有备份任务正在进行。' >&2; exit 1; }
work=$(mktemp -d "$PWD/backups/.pending.XXXXXX")
archive="$work/archive.tar.gz"
trap 'rm -rf -- "$work"' EXIT
cp -- .env "$work/config.env"
git rev-parse HEAD > "$work/revision.txt"
# Read credentials inside the containers; never pass passwords in process arguments.
timeout 600 docker compose --env-file .env exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_PASSWORD" exec mysqldump -utgguard --single-transaction --quick --no-tablespaces --set-gtid-purged=OFF tgguard' </dev/null | gzip > "$work/mysql.sql.gz"
timeout 300 docker compose --env-file .env exec -T redis sh -c 'f=$(mktemp /tmp/tg-guard-rdb.XXXXXX); trap '\''rm -f "$f"'\'' EXIT; REDISCLI_AUTH="$REDIS_PASSWORD" redis-cli --rdb "$f" >/dev/null && redis-check-rdb "$f" >/dev/null && cat "$f"' </dev/null > "$work/redis.rdb"
[[ -s "$work/redis.rdb" && -s "$work/mysql.sql.gz" ]]
gzip -t "$work/mysql.sql.gz"
(cd "$work"; sha256sum config.env revision.txt mysql.sql.gz redis.rdb > SHA256SUMS)
tar -czf "$archive" -C "$work" config.env revision.txt mysql.sql.gz redis.rdb SHA256SUMS
tar -tzf "$archive" >/dev/null
name="tg-guard-$(date -u +%Y%m%dT%H%M%SZ)-$$.tar.gz"
mv -- "$archive" "$PWD/backups/$name"
# Only expire completed archives created by this script, after a successful backup.
find "$PWD/backups" -maxdepth 1 -type f -name 'tg-guard-*.tar.gz' -mtime +30 -delete
echo "备份完成：$PWD/backups/$name（含敏感配置，请妥善保管并另存异机副本）"
