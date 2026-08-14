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
| 账号密码 | bcrypt 哈希存储 + JWT 无状态鉴权，支持修改密码 |
| 记住登录 | 登录勾选"记住我"保持 30 天（可配 `remember_days`），刷新不掉线 |
| 服务器概览 | 页面实时显示 CPU/内存/磁盘/负载/uptime/监听端口/最近登录等运维信息 |
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
│  ├─ Web 页面 (登录 + 白名单管理 + 服务器概览) │
│  ├─ API (login/rules/rule/ip/strict/sync/   │
│  │        me/change-password/server-info)    │
│  ├─ iptables 管理器 (规则重建, 防锁死)        │
│  ├─ sysinfo 采集 (CPU/内存/磁盘/负载/登录)    │
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

> **零依赖一键安装**：无需装 Go、无需 Docker、无需 git、无需任何编译工具链，只需 Linux 标配的 `iptables`。web 前端已 `go:embed` 内嵌进二进制，部署只有一个文件。安装脚本自动适配 CPU 架构（amd64/arm64），本机有 Go 时本地编译，无 Go 时自动从 GitHub Release 下载预编译二进制。

### 方式一：curl 一键安装（推荐，免克隆）

```bash
sudo bash -c "$(curl -fsSL https://raw.githubusercontent.com/ins199/ip-allowlist/master/deploy.sh)" 10443 MyPass123
```

一条命令完成：拉取安装脚本 → 自动编译或从 Release 下载二进制（按 CPU 架构自动选择）→ 安装 systemd → 启动。无需克隆代码、无需安装 Go/Docker/git。

### 方式二：克隆后一键部署

```bash
git clone https://github.com/ins199/ip-allowlist.git
cd ip-allowlist
sudo bash deploy.sh [管理端口] [管理密码] [可选域名]
```

**场景说明**（安装脚本自动选择二进制来源，前端已内嵌无需额外文件）：
1. 服务器有 Go → 本机编译，部署后即用
2. 服务器无 Go → 自动从 GitHub Release 下载对应架构（amd64/arm64）预编译二进制，全程不依赖 git
3. 下载失败（如服务器无法访问 GitHub）→ 可本机编译后手动上传：`GOOS=linux GOARCH=amd64 go build -o deploy/ip-allowlist .`，把文件放 `deploy/` 目录重跑 `deploy.sh`

> ⚠️ 部署后**立即修改默认密码**（默认 `changeme`）。
> ⚠️ 默认端口 `10443`（不常用，减少被扫描），可改参数或 config.yaml。
> 强烈建议配置域名 + nginx 反代 + HTTPS（见下文"域名与 HTTPS"）。

### 开机自启 + 规则恢复

安装时 systemd 已启用开机自启。服务启动时 `main.go` 会自动执行一次 `Reconcile`，把白名单配置恢复为 iptables 规则。无需额外脚本。

### 服务管理（systemd）

```bash
# 查看服务状态（是否运行）
systemctl status ip-allowlist

# 重启服务
systemctl restart ip-allowlist

# 停止服务（⚠️ 停止后 iptables 白名单规则不会被清除，已加的规则仍生效）
systemctl stop ip-allowlist

# 查看实时日志
journalctl -u ip-allowlist -f

# 查看最近日志
journalctl -u ip-allowlist -n 50

# 确认开机自启已开启
systemctl is-enabled ip-allowlist   # 应输出 enabled

# 修改配置文件后生效
vi /opt/ip-allowlist/config.yaml
systemctl restart ip-allowlist
```

**崩溃自愈**：服务单元配置了 `Restart=always`，进程崩溃后 systemd 会在 3 秒后自动拉起，规则自动恢复。开机也会自启。

### 发版（维护者）

打 tag 触发 GitHub Actions 自动交叉编译并发布预编译二进制（linux/amd64 + arm64），无需手动上传：

```bash
git tag v1.0.0
git push origin v1.0.0
```

Actions 编译完成后，`deploy.sh` 即自动从 Release 下载新版本二进制。部署时可用 `IPAW_VERSION` 固定版本：

```bash
sudo IPAW_VERSION=v1.0.0 bash -c "$(curl -fsSL https://raw.githubusercontent.com/ins199/ip-allowlist/master/deploy.sh)" 10443 MyPass123
```

> 注：`IPAW_VERSION` 默认 `latest`（始终拉最新 Release），生产环境建议固定到具体版本号。

### 升级

重新执行一键部署脚本即可覆盖安装到新版本。配置（`config.yaml`）与白名单数据（`allowlist.json`）保留在 `/opt/ip-allowlist/`，不会被清空。

---

## 使用

### 新增端口规则

1. 顶部"新增端口规则"输入端口（如 22）+ 用途（SSH）+ 是否严格模式 → 创建。
2. 在规则卡片下方输入 IP/CIDR + 备注 → 添加。
3. 首次添加时系统**自动把当前来源 IP 加入白名单**（防锁死）。
4. 严格模式开关：启用后非白名单 IP 无法连接该端口。

### 严格模式安全设计

- **自动加当前 IP**：启用严格模式时，若你的来源 IP 不在白名单，自动补入并持久化（重启不丢）。
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

所有接口需登录后携带 JWT：浏览器自动走 HttpOnly Cookie，API 调用可用 `X-Auth-Token` header 或 `?token=` 查询参数。

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/login` | 登录，body `{username, password, remember}`，返回 `{token}` |
| GET  | `/api/rules` | 当前所有端口规则 + 当前来源 IP + DROP 状态 |
| POST | `/api/rule` | 新增/更新端口规则，body `{port, comment, strict}` |
| DELETE | `/api/rule/:port` | 删除端口规则 |
| POST | `/api/rule/:port/ip` | 添加白名单 IP，body `{ip, remark}` |
| DELETE | `/api/rule/:port/ip/:ip` | 删除白名单 IP |
| POST | `/api/rule/:port/strict` | 切换严格模式，body `{strict}` |
| GET  | `/api/me` | 当前登录信息（用户名 + 来源 IP） |
| POST | `/api/change-password` | 修改密码，body `{old_password, new_password}` |
| POST | `/api/logout` | 退出登录（清除 Cookie） |
| GET  | `/api/sync` | 手动同步 iptables 与配置 |
| GET  | `/api/server-info` | 服务器运维信息（CPU/内存/磁盘/负载/端口/最近登录） |

---

## 配置文件

`/opt/ip-allowlist/config.yaml`：

```yaml
server:
  addr: "0.0.0.0:10443"                  # 监听地址
  data_file: "/opt/ip-allowlist/allowlist.json"  # 白名单数据
auth:
  username: "admin"                      # 管理账号
  password: "你的密码"                   # 管理密码
  secret: "随机长字符串"                  # JWT 签名密钥（务必改为随机长字符串）
  session_hours: 24                      # 会话时长（小时）
  remember_days: 30                      # "记住我"会话时长（天）
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
IPAW_DRY_RUN=1 go run . -config config.example.yaml -data /tmp/test-allowlist.json -bind 127.0.0.1:10443

# 登录测试
curl -X POST http://127.0.0.1:10443/api/login -d '{"username":"admin","password":"changeme"}' -H 'Content-Type: application/json'
```

---

## 目录结构

```
ip-allowlist/
├── main.go                 # 入口
├── embed.go                # go:embed 内嵌 web/ 前端进二进制（部署只需一个文件）
├── config.go               # 配置加载
├── config.example.yaml     # 配置示例
├── internal/
│   ├── api/                # HTTP API + Web
│   ├── auth/               # 账号密码 + JWT 鉴权
│   ├── iptables/           # 规则生成/应用/防锁死
│   ├── sysinfo/            # 服务器运维信息采集
│   └── store/              # JSON 持久化
├── web/                    # 前端页面（编译时内嵌，部署无需此目录）
├── deploy.sh               # 一键部署入口（curl 远程/本地都支持）
├── deploy/                 # systemd 单元
├── .github/workflows/      # 发版 CI（打 tag 自动编译并发布预编译二进制）
└── README.md
```

---

## 安全注意事项

1. **管理密码**：部署后立即修改，勿用默认值。
2. **Web 端口**：建议 nginx 反代 + HTTPS，不要裸 HTTP 公网开放。
3. **iptables 权限**：服务以 root 运行，请确保部署环境可信。
4. **防锁死**：首次操作请先加当前来源 IP，再开严格模式。
5. **备份**：`allowlist.json` 建议定期备份（含全部白名单配置）。

---

## 技术架构

### 分层设计

```
┌────────────────────────────────────────────────────┐
│  main.go  (入口：加载配置 → 初始化 → 启动)          │
│   ├── config.go     YAML 配置加载                  │
│   ├── internal/api       HTTP 层 (gin)            │
│   │    ├── 路由注册                                │
│   │    ├── 鉴权中间件 (JWT)                       │
│   │    └── handler (rules/ip/strict/... )         │
│   ├── internal/auth      鉴权                     │
│   │    ├── bcrypt 密码哈希                         │
│   │    └── JWT 签发/校验 (无状态)                  │
│   ├── internal/iptables  防火墙核心               │
│   │    ├── 规则生成 (每端口独立链)                │
│   │    ├── 规则应用 (重建, 防锁死)                │
│   │    └── dry-run 模式                           │
│   ├── internal/sysinfo   运维信息采集             │
│   │    └── 读 /proc + ss/df/last，零外部依赖      │
│   └── internal/store     持久化                   │
│        ├── JSON 文件读写                          │
│        └── 并发安全 (读写锁)                      │
└────────────────────────────────────────────────────┘
```

**各层职责单一，通过明确定义的接口通信**：
- `api` 层只做 HTTP 入参/出参转换，调用 `store` 读写、`iptables` 应用、`auth` 鉴权
- `iptables` 层不关心 HTTP，只负责"给定规则 → 生成/应用 iptables 命令"
- `store` 层不关心防火墙，只负责"配置持久化"
- 这种隔离让 `iptables` 核心可独立复用（未来中台 Agent 直接内嵌）

### 数据流

```
用户点击"添加 IP"
  → api.handleAddIP
  → store.AddIP (更新 allowlist.json)
  → iptables.ApplyPortRule (重建 IPAW 链)
  → 立即生效
```

### 技术选型

| 组件 | 选择 | 理由 |
|------|------|------|
| 语言 | Go | 单二进制、跨平台编译、并发安全、部署零依赖；CI 交叉编译出 linux/amd64 + arm64 预编译二进制 |
| Web 框架 | gin | 轻量、性能好、社区成熟 |
| 前端 | 原生 HTML/JS | 单文件内嵌，无构建步骤，移动端适配 |
| 密码哈希 | bcrypt | 抗暴力破解，业界标准 |
| 鉴权 | JWT (golang-jwt) | 无状态，服务重启不掉登录，多实例可共享同一密钥 |
| 持久化 | JSON 文件 | 简单可靠，无外部依赖，适合配置型数据 |
| 防火墙 | iptables | Linux 标配，规则细粒度，fail2ban 兼容 |

---

## 实现原理

### iptables 规则如何工作

对每个受管端口，系统维护一条独立链 `IPAW-<port>`。Linux 内核按链顺序匹配规则，**第一条匹配即生效**：

```
INPUT 链（按顺序匹配）
  1. ACCEPT  tcp dpt:<其他服务端口>   # 服务器原有规则
  2. IPAW-22                       # 本系统插入，优先于 fail2ban
  3. f2b-sshd                      # fail2ban 兜底
  ...
```

进入 `IPAW-22` 链后：
```
IPAW-22
  1. -s 203.0.113.10/32 -j ACCEPT   # 白名单 → 放行
  2. -s 127.0.0.1/32 -j ACCEPT      # 当前来源 → 放行(自动补)
  3. -j RETURN                      # 其他 → 回 INPUT 继续匹配
```

- **宽松模式**：链里只有 ACCEPT + RETURN，非白名单回 INPUT 走 fail2ban，只记录不阻断。
- **严格模式**：INPUT 链末尾追加 `-j DROP`，非白名单直接丢弃，fail2ban 之前就拦下。

### 防锁死机制（核心安全设计）

这是整个系统最重要的设计，防止误操作把 SSH 永久锁死：

| 机制 | 实现 | 何时触发 |
|------|------|---------|
| 自动补当前 IP | `ApplyPortRule` 应用前，若当前来源 IP 不在白名单则自动加入 | 每次应用规则时 |
| 禁止删当前 IP | `handleDelIP` 校验，严格模式下拒绝 | 删除时 |
| 禁止删到空 | 严格模式下删到白名单为空则回滚 | 删除时 |
| 先 ACCEPT 后 DROP | 规则顺序保证白名单放行优先 | 每次重建链 |
| 严格模式空白名单拒绝 | `ApplyPortRule` 拒绝应用，防止裸 DROP | 应用时 |

**为什么顺序重要**：iptables 按顺序匹配，若 DROP 在 ACCEPT 之前，白名单 IP 也会被丢。系统始终保证 ACCEPT 在前。

### 幂等与重启恢复

- 应用规则采用**重建**而非逐条增删：删旧链 → 建新链 → 全量重写。保证可重复执行、顺序正确。
- 服务启动时调用 `Reconcile`，从 `allowlist.json` 重建全部规则。
- systemd `Restart=always`，进程崩溃自动拉起，规则自动恢复。
- 即使 `iptables` 重启（内核规则丢失），服务启动也会自动重建。

### dry-run 模式

`IPAW_DRY_RUN=1` 时，所有 iptables 操作只打印不执行：

```
[dry-run] iptables -N IPAW-22
[dry-run] iptables -A IPAW-22 -s 203.0.113.10/32 -j ACCEPT
```

用于本地开发、规则验证、CI 测试，**不会真正改动防火墙**，避免误操作。

---

## 开源

本项目采用 [MIT License](LICENSE)。

**欢迎贡献**：提 issue、PR、或 star 支持。核心待办见 [plans/plan.md](plans/plan.md)。

**演进方向**：当前单机版适用于几十台服务器；远期可演进为"中台管理端 + 每服务器轻量 Agent"，`internal/iptables` 核心逻辑可直接复用。
