#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
for command in docker openssl curl; do
  command -v "$command" >/dev/null || { echo "缺少 $command；请先安装 Docker Engine 和 Compose v2，以及 openssl、curl。" >&2; exit 1; }
done
docker compose version >/dev/null
docker info >/dev/null 2>&1 || { echo 'Docker 未启动或当前用户无访问权限。' >&2; exit 1; }
mode=${1:-deploy}
[[ "$mode" == deploy || "$mode" == --panel-only ]] || { echo '不支持的参数。'; exit 1; }
if [[ "$mode" == --panel-only && ! -f .env ]]; then echo '请先安装并启动服务。'; exit 1; fi
if [[ ! -f .env ]]; then
  read -r -s -p '请输入 Bot Token（输入隐藏）：' bot_token
  printf '\n'
  [[ "$bot_token" =~ ^[0-9]+:[A-Za-z0-9_-]{30,}$ ]] || { echo 'Bot Token 格式不正确。' >&2; exit 1; }
  # Exclusive creation prevents accidental replacement if another installer runs concurrently.
  (umask 077; set -o noclobber; cat > .env <<EOF
BOT_TOKEN=$bot_token
BOT_MODE=polling
ADMIN_API_TOKEN=$(openssl rand -hex 32)
SETTINGS_ENCRYPTION_KEY=$(openssl rand -hex 32)
MYSQL_PASSWORD=$(openssl rand -hex 24)
MYSQL_ROOT_PASSWORD=$(openssl rand -hex 24)
REDIS_PASSWORD=$(openssl rand -hex 24)
REDIS_DB=0
EOF
  )
  unset bot_token
  echo '已生成 .env；请备份此文件，更新时会保留原有凭据。'
fi
# Read only required values as data, never execute a configuration file.
read_setting() {
  local line value=''
  while IFS= read -r line || [[ -n "$line" ]]; do
    line=${line%$'\r'}
    if [[ "$line" == "$1="* ]]; then value=${line#*=}; fi
  done < .env
  if [[ "$value" == \"*\" || "$value" == \'*\' ]]; then value=${value:1:${#value}-2}; fi
  printf '%s' "$value"
}
for key in BOT_TOKEN ADMIN_API_TOKEN MYSQL_PASSWORD MYSQL_ROOT_PASSWORD REDIS_PASSWORD; do
  value=$(read_setting "$key")
  [[ -n "$value" && "$value" != change-* ]] || { echo ".env 中 $key 未配置；已保留原文件，请补齐后重试。" >&2; exit 1; }
  if [[ "$key" == ADMIN_API_TOKEN && ${#value} -lt 32 ]]; then echo 'ADMIN_API_TOKEN 至少需要 32 个字符。' >&2; exit 1; fi
  # Keep Compose interpolation consistent with env_file, even if the caller exported old values.
  export "$key=$value"
done
unset value
if [[ "$mode" != --panel-only ]]; then
[[ ! -L .updates ]] || { echo '升级任务目录不能是符号链接，请检查 .updates。'; exit 1; }
if [[ ! -d .updates ]]; then mkdir .updates; fi
if [[ "$(id -u)" == 0 ]]; then chown 10001:10001 .updates; chmod 750 .updates; fi
echo '正在准备运行镜像并下载已编译程序，不会在服务器编译 Go。下方将显示完整步骤……'
export BUILDKIT_PROGRESS=plain
docker compose --env-file .env up -d --build --wait --wait-timeout 300
if [[ "${TG_WEB_UPDATE_JOB:-}" != 1 && "$(id -u)" == 0 ]] && command -v systemctl >/dev/null && [[ -d /run/systemd/system && -f scripts/setup-updater.sh ]]; then
  bash scripts/setup-updater.sh || echo '服务已启动；网页升级尚未启用，可稍后在管理菜单启用。'
fi
fi
# A short-lived ticket avoids displaying the permanent administrator credential.
if ! response=$(printf 'header = "Authorization: Bearer %s"\n' "$ADMIN_API_TOKEN" | curl --config - --fail --silent --show-error --max-time 15 -X POST -H 'Content-Type: application/json' -d '{}' http://127.0.0.1:8080/api/v1/panel-ticket); then
  if [[ "$mode" == --panel-only ]]; then echo '未取得登录链接，请检查服务状态或后台凭据。' >&2; exit 1; fi
  echo '服务已启动；暂时无法获取登录链接。稍后在管理菜单重新获取即可，服务无需回退。' >&2
  exit 0
fi
if [[ "$response" =~ \"ticket\":\"([A-Za-z0-9_-]+)\" ]]; then
    ticket=${BASH_REMATCH[1]}
  domain=$(read_setting PANEL_DOMAIN)
  if [[ -n "$domain" ]]; then
    echo "部署完成。后台地址：https://$domain/#ticket=$ticket"
  elif [[ "$(read_setting PANEL_BIND)" == 0.0.0.0 ]]; then
    host=$(read_setting PANEL_HOST)
    echo "部署完成。后台地址：http://${host:-服务器公网IP}:8080/#ticket=$ticket"
    echo '请在自己电脑的浏览器打开；未配置 PANEL_HOST 时，把“服务器公网IP”替换为实际 IP。'
    echo '无法连接时，请检查服务器防火墙和云安全组是否允许你的 IP 访问 TCP 8080。'
  else
    echo "部署完成。本机后台登录地址：http://127.0.0.1:8080/#ticket=$ticket"
    echo '当前仅允许服务器本机访问。需要 IP 直连时，在 .env 设置 PANEL_BIND=0.0.0.0 后重新运行本脚本。'
  fi
  echo '一次性登录链接有效期 1 分钟；过期后使用 .env 中的 ADMIN_API_TOKEN 登录。'
else
  if [[ "$mode" == --panel-only ]]; then echo '后台返回的登录票据无效，请检查服务日志。' >&2; exit 1; fi
  echo '服务已启动，但未能取得登录票据。可使用 .env 中的 ADMIN_API_TOKEN 登录后台。' >&2
  exit 0
fi

echo '在后台设置机器人管理员、批准群组；AI 接口按需填写。'
