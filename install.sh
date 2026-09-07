#!/usr/bin/env bash
set -euo pipefail

# Linux bootstrap. Docker repository setup follows docs.docker.com/engine/install/ubuntu/.
install_dependencies() {
  local missing=0 command
  for command in git curl openssl; do command -v "$command" >/dev/null || missing=1; done
  if (( missing )); then
    command -v apt-get >/dev/null || { echo '请先安装 git、curl、openssl。自动安装依赖支持 Ubuntu / Debian。' >&2; return 1; }
    apt-get update
    apt-get install -y ca-certificates curl git openssl
  fi
  if ! command -v docker >/dev/null || ! docker compose version >/dev/null 2>&1; then
    local ID='' VERSION_CODENAME='' UBUNTU_CODENAME=''
    . /etc/os-release
    case "$ID" in ubuntu|debian) ;; *) echo '自动安装 Docker 支持 Ubuntu / Debian；其他 Linux 请先安装 Docker 和 Compose v2。' >&2; return 1;; esac
    local codename=${UBUNTU_CODENAME:-$VERSION_CODENAME}
    [[ "$codename" =~ ^[a-z]+$ ]] || { echo '无法识别系统发行版本。' >&2; return 1; }
    # Do not remove or replace an existing third-party Docker installation.
    if command -v docker >/dev/null; then
      echo '已有 Docker 缺少 Compose v2，请为现有 Docker 安装 Compose 插件后重试。' >&2
      return 1
    fi
    apt-get update
    apt-get install -y ca-certificates curl git openssl
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL --retry 3 "https://download.docker.com/linux/$ID/gpg" -o /etc/apt/keyrings/tg-guard-docker.asc
    chmod 0644 /etc/apt/keyrings/tg-guard-docker.asc
    cat > /etc/apt/sources.list.d/tg-guard-docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/$ID
Suites: $codename
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/tg-guard-docker.asc
EOF
    apt-get update
    apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
  fi
  if ! docker info >/dev/null 2>&1; then
    command -v systemctl >/dev/null || { echo '请先启动 Docker 服务。' >&2; return 1; }
    systemctl enable --now docker
  fi
  docker info >/dev/null 2>&1 || { echo 'Docker 尚未就绪，请检查 Docker 服务后重试。' >&2; return 1; }
}

main() {
  [[ "$(uname -s)" == Linux ]] || { echo '此安装入口用于 Linux；Windows 请使用 scripts/deploy.ps1。' >&2; return 1; }
  [[ "$(id -u)" == 0 ]] || { echo '请使用 sudo bash 执行安装命令。' >&2; return 1; }
  local target=${TG_GUARD_INSTALL_DIR:-/opt/tg-guard}
  [[ "$target" == /* && "$target" != / && ! -L "$target" ]] || { echo '安装目录必须为非根目录的绝对路径，且不能为符号链接。' >&2; return 1; }
  install_dependencies
  if [[ -e "$target" ]]; then
    [[ -d "$target/.git" ]] || { echo "目录 $target 已存在且不是项目仓库；请使用空的新路径，原文件未修改。" >&2; return 1; }
    local remote branch
    remote=$(git -C "$target" remote get-url origin)
    case "$remote" in https://github.com/anlo7676/TG-Guard-Bot.git|git@github.com:anlo7676/TG-Guard-Bot.git) ;; *) echo '现有目录不是 TG Guard 官方项目仓库，停止更新。' >&2; return 1;; esac
    branch=$(git -C "$target" branch --show-current)
    [[ "$branch" == main && -z "$(git -C "$target" status --porcelain)" ]] || { echo '现有仓库不是干净的 main 分支，请先处理本地修改；配置和数据未清理。' >&2; return 1; }
    git -C "$target" fetch origin main
    git -C "$target" merge --ff-only origin/main
  else
    mkdir -p -- "$(dirname -- "$target")"
    git clone --branch main --single-branch https://github.com/anlo7676/TG-Guard-Bot.git "$target"
  fi
  echo "项目目录：$target；配置和数据将保留。"
  bash "$target/scripts/deploy.sh"
}

if [[ "${BASH_SOURCE[0]:-}" == "$0" || -z "${BASH_SOURCE[0]:-}" ]]; then main "$@"; fi
