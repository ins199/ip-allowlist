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
  addr: "0.0.0.0:10443"                  # 监听地址
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
IPAW_DRY_RUN=1 go run . -config config.example.yaml -data /tmp/test-allowlist.json -bind 127.0.0.1:10443

# 登录测试
curl -X POST http://127.0.0.1:10443/api/login -d '{"username":"admin","password":"changeme"}' -H 'Content-Type: application/json'
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

---

## 技术架构

### 分层设计

```
┌────────────────────────────────────────────────────┐
│  main.go  (入口：加载配置 → 初始化 → 启动)          │
│   ├── config.go     YAML 配置加载                  │
│   ├── internal/api       HTTP 层 (gin)            │
│   │    ├── 路由注册                                │
│   │    ├── 鉴权中间件 (session token)             │
│   │    └── handler (rules/ip/strict/... )         │
│   ├── internal/auth      鉴权                     │
│   │    ├── bcrypt 密码哈希                         │
│   │    └── 内存 session 管理                       │
│   ├── internal/iptables  防火墙核心               │
│   │    ├── 规则生成 (每端口独立链)                │
│   │    ├── 规则应用 (重建, 防锁死)                │
│   │    └── dry-run 模式                           │
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
| 语言 | Go | 单二进制、跨平台编译、并发安全、部署零依赖 |
| Web 框架 | gin | 轻量、性能好、社区成熟 |
| 前端 | 原生 HTML/JS | 单文件内嵌，无构建步骤，移动端适配 |
| 密码哈希 | bcrypt | 抗暴力破解，业界标准 |
| 持久化 | JSON 文件 | 简单可靠，无外部依赖，适合配置型数据 |
| 防火墙 | iptables | Linux 标配，规则细粒度，fail2ban 兼容 |

---

## 实现原理

### iptables 规则如何工作

对每个受管端口，系统维护一条独立链 `IPAW-<port>`。Linux 内核按链顺序匹配规则，**第一条匹配即生效**：

```
INPUT 链（按顺序匹配）
  1. ACCEPT  tcp dpt:8443          # 原有规则
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
