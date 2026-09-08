#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."

configure_access() {
  [[ -f .env ]] || { echo '请先选择“安装 / 启动”。'; return 1; }
  local bind=$1 host='' next backup
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
  backup=$(mktemp .env.menu-backup.XXXXXX) || { rm -f -- "$next"; return 1; }
  cp -p -- .env "$backup" || { rm -f -- "$next" "$backup"; return 1; }
  mv -- "$next" .env || { rm -f -- "$next" "$backup"; return 1; }
  # Apply only the app port mapping; database services and volumes are untouched.
  if ! (export PANEL_BIND="$bind"; docker compose --env-file .env up -d --no-build --no-deps --wait --wait-timeout 120 app); then
    mv -- "$backup" .env || { echo '原配置恢复失败，请保留备份并人工恢复。'; return 1; }
    if ! (unset PANEL_BIND; docker compose --env-file .env up -d --no-build --no-deps --wait --wait-timeout 120 app); then
      echo '原配置已恢复，但运行状态无法确认。请检查 Docker，不能据此认为公网入口已关闭。'
    else
      echo '设置未生效，已恢复原配置和服务。'
    fi
    return 1
  fi
  rm -f -- "$backup"
  bash scripts/deploy.sh --panel-only
}

while true; do
  printf '\n========== TG Guard 管理菜单 ==========\n'
  printf '1. 安装 / 启动（保留配置和数据）\n2. 开启 IP 访问 / 修改访问地址\n3. 切回仅本机访问\n4. 获取后台登录链接\n5. 更新到最新版本\n6. 查看运行状态\n7. 查看最近日志\n8. 启用网页升级\n0. 退出\n'
  read -r -p '请选择 [0-8]：' choice || exit 0
  case "$choice" in
    1) bash scripts/deploy.sh || echo '启动未完成，请查看上方错误。';;
    2) configure_access 0.0.0.0 || echo '访问设置未完成。';;
    3) configure_access 127.0.0.1 || echo '访问设置未完成。';;
    4) bash scripts/deploy.sh --panel-only || echo '未取得登录链接，请先启动服务。';;
    5) TG_GUARD_INSTALL_DIR="$PWD" bash install.sh --deploy || echo '更新未完成，配置和数据保留。';;
    6) docker compose --env-file .env ps || echo '无法读取状态，请检查 Docker。';;
    7) docker compose --env-file .env logs --tail 60 app || echo '无法读取日志。';;
    8) bash scripts/setup-updater.sh || echo '网页升级未启用，请检查上方提示。';;
    0) exit 0;;
    *) echo '请输入 0 到 8。';;
  esac
done
