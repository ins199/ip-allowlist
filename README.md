# ip-allowlist — 通用 IP 白名单管理系统

> 部署到任意 Linux 宿主机即用，通过 Web 页面管理 **多个端口** 的 IP 白名单（iptables）。
> 解决"防止外部 IP 拿到密钥连接服务器/数据库"，同时 IP 漂移时可以在网页上自助增删，不用上云控制台。

---

## 为什么需要它

- 服务器 SSH/数据库默认对公网开放，只靠密钥保护，**一旦密钥泄露 = 裸奔**。
- 固定 IP 白名单会因宽带断电重启 IP 漂移而**把自己锁死**，且每次都要上云控制台改安全组。
- 现有业务系统跑在 Docker 容器里，**没有权限改宿主机 iptables**，无法内嵌这个能力。

本系统作为**宿主机独立进程**（root 运行），天然有权限操作本机 iptables，部署到任何 ECS 即用，不依赖任何业务项目。

---

## 核心能力

| 能力 | 说明 |
|------|------|
| 多端口白名单 | 每条规则 = {端口, IP/CIDR, 备注}，可管 SSH(22)、Redis(6379)、webhook(9000) 等任意端口 |
| Web 管理页面 | 账号密码登录，查看/增删白名单，显示当前来源 IP |
| 账号密码 | bcrypt 哈希存储，支持修改密码 |
| 记住登录 | 登录勾选"记住我"保持 30 天，刷新不掉线 |
| 移动端适配 | 响应式布局，手机浏览器可用 |
| 严格模式 | 每端口可选"仅白名单可连"，非白名单 DROP；宽松模式只展示不阻断 |
| 防锁死 | 自动加入当前来源 IP、禁止删当前 IP、严格模式禁止删到空、先 ACCEPT 后 DROP |
| 持久化 | JSON 落盘 + systemd 开机自启 + 启动时自动恢复 iptables 规则 |
| 幂等同步 | 启动/手动触发 Reconcile，将 iptables 与配置对齐 |
| Dry-run | `IPAW_DRY_RUN=1` 只打印不执行，本地安全模拟 |

---

## 架构

```
┌─────────────────────────────────────────────┐
│  ip-allowlist (宿主机, systemd 自启, root)    │
│  ├─ Web 页面 (登录 + 白名单管理)              │
│  ├─ API (login/rules/rule/ip/strict/sync)    │
│  ├─ iptables 管理器 (规则重建, 防锁死)        │
│  └─ 持久化 (JSON 文件)                       │
└──────────────────────┬──────────────────────┘
                       │ 直接操作 iptables
                       ▼
             本机防火墙 (iptables)
```

### 规则结构

对每个受管端口建立一个独立链 `IPAW-<port>`：

```
# 链 IPAW-22（白名单 IP 放行，其他 RETURN 回 INPUT 走 fail2ban）
-A IPAW-22 -s 1.2.3.4/32 -j ACCEPT
-A IPAW-22 -s 5.6.7.8/32 -j ACCEPT
-A IPAW-22 -j RETURN

# INPUT 链
-I INPUT -p tcp --dport 22 -j IPAW-22      # 优先于 fail2ban
-A INPUT -p tcp --dport 22 -j DROP         # 严格模式才加，且白名单非空才加
```

---

## 快速部署

### 前置
- Linux 宿主机（Ubuntu/CentOS 均可），root 权限
- 已安装 `iptables`

### 步骤

```bash
# 1. 本地编译 Linux 二进制
cd ip-allowlist
GOOS=linux GOARCH=amd64 go build -o ip-allowlist .

# 2. 将整个目录传到服务器（含 deploy/）
scp -r ip-allowlist root@<服务器>:/tmp/

# 3. 服务器上安装（指定管理端口、密码、可选域名）
cd /tmp/ip-allowlist
sudo bash deploy/install.sh 10443 你的管理密码 你的域名.com

# 4. 打开管理页面
浏览器访问 http://<服务器IP>:10443
登录 admin / 你的管理密码
```

> ⚠️ 部署后**立即修改默认密码**（install.sh 默认 `changeme`）。
> ⚠️ 默认端口 `10443`（不常用，减少被扫描），可在 install.sh 参数或 config.yaml 修改。
> 强烈建议配置域名 + nginx 反代 + HTTPS（见下文"域名与 HTTPS"）。

### 开机自启 + 规则恢复

安装时 systemd 已启用开机自启。服务启动时 `main.go` 会自动执行一次 `Reconcile`，把白名单配置恢复为 iptables 规则。无需额外脚本。

---

## 使用

### 新增端口规则

1. 顶部"新增端口规则"输入端口（如 22）+ 用途（SSH）+ 是否严格模式 → 创建。
2. 在规则卡片下方输入 IP/CIDR + 备注 → 添加。
3. 首次添加时系统**自动把当前来源 IP 加入白名单**（防锁死）。
4. 严格模式开关：启用后非白名单 IP 无法连接该端口。

### 严格模式安全设计

- **自动加当前 IP**：启用严格模式时，若你的来源 IP 不在白名单，自动补入。
- **禁止删当前 IP**：严格模式下删除当前来源 IP 会被拒绝。
- **禁止删到空**：严格模式下删到白名单为空会回滚，提示保留至少一条。
- **先 ACCEPT 后 DROP**：规则顺序保证白名单放行优先于兜底拒绝。

### 页面按钮

| 按钮 | 作用 |
|------|------|
| 添加 | 向该端口白名单加入 IP |
| ✕ | 删除某 IP |
| 同步规则 | 将 iptables 与配置对齐（手动 Reconcile） |
| 删除规则 | 删除整条端口规则 |
| 严格模式 | 切换严格/宽松 |

---

## API 参考

所有接口需登录后携带 `X-Auth-Token` header（登录返回的 token）。

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/login` | 登录，body `{username, password, remember}`，返回 `{token}` |
| GET  | `/api/rules` | 当前所有端口规则 + 当前来源 IP + DROP 状态 |
| POST | `/api/rule` | 新增/更新端口规则，body `{port, comment, strict}` |
| DELETE | `/api/rule/:port` | 删除端口规则 |
| POST | `/api/rule/:port/ip` | 添加白名单 IP，body `{ip, remark}` |
| DELETE | `/api/rule/:port/ip/:ip` | 删除白名单 IP |
| POST | `/api/rule/:port/strict` | 切换严格模式，body `{strict}` |
| POST | `/api/change-password` | 修改密码，body `{old_password, new_password}` |
| POST | `/api/logout` | 退出登录 |
| GET  | `/api/sync` | 手动同步 iptables 与配置 |

---

## 配置文件

`/opt/ip-allowlist/config.yaml`：

```yaml
server:
  addr: "0.0.0.0:8443"                  # 监听地址
  data_file: "/opt/ip-allowlist/allowlist.json"  # 白名单数据
auth:
  username: "admin"                      # 管理账号
  password: "你的密码"                   # 管理密码
  session_hours: 24                      # 会话时长
```

白名单数据文件 `allowlist.json`（示例）：

```json
{
  "rules": [
    {
      "port": 22,
      "comment": "SSH",
      "strict": true,
      "allow_list": [
        { "ip": "203.0.113.10", "remark": "本机宽带" }
      ]
    }
  ]
}
```

---

## 域名与 HTTPS

管理页面建议绑定域名并用 nginx 反代 + HTTPS（公网不暴露裸 HTTP）：

```nginx
# /etc/nginx/conf.d/ip-allow.conf
server {
    listen 443 ssl;
    server_name allow.example.com;          # 你的域名，解析到本机公网 IP
    ssl_certificate     /etc/ssl/fullchain.pem;
    ssl_certificate_key /etc/ssl/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:10443;  # 本机监听端口（不常用端口）
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

配置后访问 `https://allow.example.com` 即可。域名解析到本机公网 IP 即可，本服务监听 10443（nginx 反向代理转发）。

---

## 本地开发与测试

```bash
# 编译
go build ./...

# 运行（dry-run 模式，只打印 iptables 命令，不真正改动防火墙）
IPAW_DRY_RUN=1 go run . -config config.example.yaml -data /tmp/test-allowlist.json -bind 127.0.0.1:8443

# 登录测试
curl -X POST http://127.0.0.1:8443/api/login -d '{"username":"admin","password":"changeme"}' -H 'Content-Type: application/json'
```

---

## 目录结构

```
ip-allowlist/
├── main.go                 # 入口
├── config.go               # 配置加载
├── config.example.yaml     # 配置示例
├── internal/
│   ├── api/                # HTTP API + Web
│   ├── auth/               # 账号密码 + session
│   ├── iptables/           # 规则生成/应用/防锁死
│   └── store/              # JSON 持久化
├── web/                    # 前端页面
├── deploy/                 # install.sh + systemd 单元
└── README.md
```

---

## 安全注意事项

1. **管理密码**：部署后立即修改，勿用默认值。
2. **Web 端口**：建议 nginx 反代 + HTTPS，不要裸 HTTP 公网开放。
3. **iptables 权限**：服务以 root 运行，请确保部署环境可信。
4. **防锁死**：首次操作请先加当前来源 IP，再开严格模式。
5. **备份**：`allowlist.json` 建议定期备份（含全部白名单配置）。
