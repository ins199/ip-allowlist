#!/bin/bash
# ip-allowlist 一键部署脚本（需 root 运行）
# 用法: sudo bash install.sh [端口] [管理密码] [域名]
#   - 端口: Web 管理页面监听端口，默认 10443（不常用端口）
#   - 密码: 管理密码，默认 changeme（强烈建议修改）
#   - 域名: 可选，部署后解析到本机公网 IP，配合 nginx 反代 HTTPS
set -euo pipefail

INSTALL_DIR=/opt/ip-allowlist
WEB_PORT="${1:-10443}"
ADMIN_PASS="${2:-changeme}"
DOMAIN="${3:-}"
BIN="$INSTALL_DIR/ip-allowlist"

echo "==> 检查 root 权限"
[ "$(id -u)" -eq 0 ] || { echo "请用 root 运行: sudo bash install.sh"; exit 1; }

echo "==> 检查 iptables"
command -v iptables >/dev/null || { echo "缺少 iptables，请先安装"; exit 1; }

echo "==> 创建目录 $INSTALL_DIR"
mkdir -p "$INSTALL_DIR/web" "$INSTALL_DIR"

echo "==> 复制二进制和前端"
# 部署前请先本地编译: GOOS=linux GOARCH=amd64 go build -o ip-allowlist .
# 然后把二进制 + web/ 目录放到本脚本同级
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -f "$SCRIPT_DIR/ip-allowlist" ]; then
  cp "$SCRIPT_DIR/ip-allowlist" "$BIN"
else
  echo "未找到 $SCRIPT_DIR/ip-allowlist，请先: GOOS=linux GOARCH=amd64 go build -o ip-allowlist ."
  exit 1
fi
chmod +x "$BIN"
cp -r "$SCRIPT_DIR/../web/"* "$INSTALL_DIR/web/" 2>/dev/null || true

echo "==> 生成配置文件"
cat > "$INSTALL_DIR/config.yaml" <<EOF
server:
  addr: "0.0.0.0:${WEB_PORT}"
  data_file: "${INSTALL_DIR}/allowlist.json"
auth:
  username: "admin"
  password: "${ADMIN_PASS}"
  session_hours: 24
EOF

echo "==> 安装 systemd 服务"
cp "$SCRIPT_DIR/ip-allowlist.service" /etc/systemd/system/ip-allowlist.service
sed -i "s|/opt/ip-allowlist|${INSTALL_DIR}|g" /etc/systemd/system/ip-allowlist.service
systemctl daemon-reload

echo "==> 启动服务"
systemctl enable ip-allowlist
systemctl restart ip-allowlist

echo "==> 检查状态"
sleep 1
systemctl status ip-allowlist --no-pager | head -10 || true

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
