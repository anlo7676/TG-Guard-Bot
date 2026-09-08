#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."

configure_access() {
  [[ -f .env ]] || { echo '请先选择“安装 / 启动”。'; return 1; }
  local bind=$1 host='' next
  if [[ "$bind" == 0.0.0.0 ]]; then
    read -r -p '请输入服务器公网 IPv4 或域名（不含 http:// 和端口，留空取消）：' host
    [[ -n "$host" ]] || return 0
    [[ ${#host} -le 253 && "$host" =~ ^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$ ]] || { echo '地址格式不正确，请输入 IPv4 或域名。'; return 1; }
    echo '将开启 IP 直连。请在防火墙 / 安全组允许你的 IP 访问 8080；长期使用建议配置 HTTPS。'
  fi
  next=$(mktemp .env.menu.XXXXXX) || return 1
  chmod 600 "$next" || { rm -f -- "$next"; return 1; }
  if ! awk '!/^PANEL_BIND=/ && !/^PANEL_HOST=/' .env > "$next"; then rm -f -- "$next"; return 1; fi
  printf '\nPANEL_BIND=%s\nPANEL_HOST=%s\n' "$bind" "$host" >> "$next" || { rm -f -- "$next"; return 1; }
  mv -- "$next" .env || { rm -f -- "$next"; return 1; }
  # Apply only the app port mapping; database services and volumes are untouched.
  if ! (export PANEL_BIND="$bind"; docker compose --env-file .env up -d --no-build --no-deps --wait --wait-timeout 120 app); then
    echo '配置已保存，但应用未就绪。请查看运行日志；修复后选择“安装 / 启动”重试。'
    return 1
  fi
  bash scripts/deploy.sh --panel-only
}

while true; do
  printf '\n========== TG Guard 管理菜单 ==========\n'
  printf '1. 安装 / 启动（保留配置和数据）\n2. 开启 IP 访问 / 修改访问地址\n3. 切回仅本机访问\n4. 获取后台登录链接\n5. 更新到最新版本\n6. 查看运行状态\n7. 查看最近日志\n0. 退出\n'
  read -r -p '请选择 [0-7]：' choice || exit 0
  case "$choice" in
    1) bash scripts/deploy.sh || echo '启动未完成，请查看上方错误。';;
    2) configure_access 0.0.0.0 || echo '访问设置未完成。';;
    3) configure_access 127.0.0.1 || echo '访问设置未完成。';;
    4) bash scripts/deploy.sh --panel-only || echo '未取得登录链接，请先启动服务。';;
    5) bash install.sh --deploy || echo '更新未完成，配置和数据保留。';;
    6) docker compose --env-file .env ps || echo '无法读取状态，请检查 Docker。';;
    7) docker compose --env-file .env logs --tail 60 app || echo '无法读取日志。';;
    0) exit 0;;
    *) echo '请输入 0 到 7。';;
  esac
done
