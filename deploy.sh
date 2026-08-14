#!/bin/bash
# ip-allowlist 一键部署脚本（curl 远程执行 / 克隆后本地执行均支持）
#
# 用法: sudo bash deploy.sh [端口] [管理密码] [可选域名]
#   - 端口: 管理端口，默认 10443
#   - 密码: 管理密码，默认 changeme
#   - 域名: 可选，部署后用于提示 nginx 反代
#
# 远程 curl 安装:
#   sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/ins199/ip-allowlist/master/deploy.sh)" 10443 MyPass123
#
# 本地运行（已 clone 代码）:
#   sudo bash deploy.sh 10443 MyPass123
set -euo pipefail

WEB_PORT="${1:-10443}"
ADMIN_PASS="${2:-changeme}"
DOMAIN="${3:-}"
REPO="https://github.com/ins199/ip-allowlist.git"
WORK_DIR=""

# 检测 CPU 架构（决定编译/下载哪个平台的二进制）
detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    aarch64|arm64) echo "arm64" ;;
    *) echo "错误: 不支持的架构 $(uname -m)，仅支持 amd64/arm64" >&2; return 1 ;;
  esac
}

# 下载文件（curl/wget 兼容）
download_file() {
  local url="$1" out="$2"
  if command -v curl >/dev/null; then
    curl -fsSL --retry 3 -o "$out" "$url"
  elif command -v wget >/dev/null; then
    wget -qO "$out" "$url"
  else
    echo "错误: 缺少 curl/wget，无法下载预编译二进制" >&2
    return 1
  fi
}

cleanup() {
  if [ -n "$WORK_DIR" ] && [ -d "$WORK_DIR" ]; then
    rm -rf "$WORK_DIR"
  fi
}
trap cleanup EXIT

# 检查 root
if [ "$(id -u)" -ne 0 ]; then
  echo "请用 root 运行: sudo bash deploy.sh"
  exit 1
fi

# 检查 iptables
command -v iptables >/dev/null || { echo "错误: 缺少 iptables"; exit 1; }

# 定位项目源码目录（仅本机编译需要源码；web 已内嵌进二进制，无 Go 时走 Release 下载，无需源码）
SCRIPT_SRC="$(cd "$(dirname "$0")" && pwd 2>/dev/null || echo /dev/null)"
resolve_src() {
  # 场景A: 本地已 clone（deploy.sh 在项目根）
  if [ -f "$SCRIPT_SRC/main.go" ]; then
    SRC_DIR="$SCRIPT_SRC"
    echo "==> 使用本地源码: $SRC_DIR"
  # 场景B: curl 远程执行（当前目录不是项目），clone 源码
  else
    echo "==> 未找到本地源码，从 GitHub clone..."
    WORK_DIR="$(mktemp -d)"
    git clone --depth 1 "$REPO" "$WORK_DIR/ip-allowlist" 2>&1 | tail -2 || { echo "clone 失败（服务器可能无法访问 GitHub，请用方式二本地部署）"; exit 1; }
    SRC_DIR="$WORK_DIR/ip-allowlist"
    echo "==> 已 clone 到 $SRC_DIR"
  fi
}

# 编译或使用预编译二进制
INSTALL_DIR=/opt/ip-allowlist
BIN="$INSTALL_DIR/ip-allowlist"
mkdir -p "$INSTALL_DIR"

echo "==> 准备二进制"
ARCH="$(detect_arch)" || exit 1
if command -v go >/dev/null; then
  resolve_src
  echo "    使用本机 Go 编译 (linux/${ARCH})..."
  (cd "$SRC_DIR" && GOOS=linux GOARCH="$ARCH" go build -o "$BIN" .)
elif [ -f "$SCRIPT_SRC/deploy/ip-allowlist" ]; then
  echo "    使用仓库内预编译二进制..."
  cp "$SCRIPT_SRC/deploy/ip-allowlist" "$BIN"
else
  echo "    本机无 Go，从 GitHub Release 下载预编译二进制 (linux/${ARCH})..."
  RELEASE_VERSION="${IPAW_VERSION:-latest}"
  if [ "$RELEASE_VERSION" = "latest" ]; then
    RELEASE_URL="https://github.com/ins199/ip-allowlist/releases/latest/download/ip-allowlist-linux-${ARCH}"
  else
    RELEASE_URL="https://github.com/ins199/ip-allowlist/releases/download/${RELEASE_VERSION}/ip-allowlist-linux-${ARCH}"
  fi
  if download_file "$RELEASE_URL" "$BIN.download" && [ -s "$BIN.download" ]; then
    mv "$BIN.download" "$BIN"
    echo "    已从 Release 下载预编译二进制"
  else
    rm -f "$BIN.download"
    echo "错误: 服务器无 Go，且从 Release 下载失败: $RELEASE_URL"
    echo "可选方案: 1) 服务器装 Go 后重试  2) 本机 GOOS=linux GOARCH=${ARCH} go build -o ip-allowlist . 后放 deploy/ 目录重试"
    exit 1
  fi
fi
chmod +x "$BIN"

echo "==> 生成配置"
cat > "$INSTALL_DIR/config.yaml" <<EOF
server:
  addr: "0.0.0.0:${WEB_PORT}"
  data_file: "${INSTALL_DIR}/allowlist.json"
auth:
  username: "admin"
  password: "${ADMIN_PASS}"
  session_hours: 24
  remember_days: 30
EOF

echo "==> 安装 systemd 服务"
cat > /etc/systemd/system/ip-allowlist.service <<EOF
[Unit]
Description=IP Allowlist Manager
After=network.target

[Service]
Type=simple
User=root
Group=root
ExecStart=${INSTALL_DIR}/ip-allowlist -config ${INSTALL_DIR}/config.yaml -data ${INSTALL_DIR}/allowlist.json
Restart=always
RestartSec=3
Environment=IPAW_DRY_RUN=0

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable ip-allowlist
systemctl restart ip-allowlist

echo "==> 检查状态"
sleep 1
systemctl status ip-allowlist --no-pager | head -8 || true

echo ""
echo "===================================================="
echo " ✅ 部署完成"
echo "    管理页面:  http://<服务器IP>:${WEB_PORT}"
echo "    账号:      admin"
echo "    密码:      ${ADMIN_PASS}"
if [ -n "$DOMAIN" ]; then
  echo "    域名:      ${DOMAIN} (请确认已解析到本机公网 IP)"
fi
echo ""
echo " ⚠️ 安全提醒:"
echo "    1. 请立即修改管理密码"
echo "    2. 建议用 nginx 反代并启用 HTTPS（见 README）"
echo "    3. 首次登录后系统会自动把你的来源 IP 加入白名单"
echo "===================================================="
