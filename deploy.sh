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

# 定位项目根目录
# 场景A: 本地已 clone（deploy.sh 在项目根）
SCRIPT_SRC="$(cd "$(dirname "$0")" && pwd 2>/dev/null || echo /dev/null)"
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

# 编译或使用预编译二进制
INSTALL_DIR=/opt/ip-allowlist
BIN="$INSTALL_DIR/ip-allowlist"
mkdir -p "$INSTALL_DIR/web"

echo "==> 准备二进制"
if command -v go >/dev/null; then
  echo "    使用 Go 编译 (linux/amd64)..."
  (cd "$SRC_DIR" && GOOS=linux GOARCH=amd64 go build -o "$BIN" .)
elif [ -f "$SRC_DIR/deploy/ip-allowlist" ]; then
  echo "    使用预编译二进制..."
  cp "$SRC_DIR/deploy/ip-allowlist" "$BIN"
else
  echo "错误: 服务器无 Go，且无预编译二进制"
  echo "请在本机: GOOS=linux GOARCH=amd64 go build -o ip-allowlist . 然后放到 deploy/ 目录重试"
  exit 1
fi
chmod +x "$BIN"
cp -r "$SRC_DIR/web/"* "$INSTALL_DIR/web/" 2>/dev/null || true

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
