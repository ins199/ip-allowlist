# ip-allowlist 开发计划

> 通用 IP 白名单管理系统，部署到任意 Linux 宿主机即用，管理多端口 IP 白名单（iptables）。

## 状态

🔄 开发中

## 背景与目标

- 防止外部 IP 拿到 SSH 密钥/数据库凭据连接服务器。
- IP 漂移时能在 Web 页面自助增删白名单，不用上云控制台。
- 现有业务跑在 Docker 容器内，无法改宿主机 iptables → 需独立宿主机进程。
- 部署形态：单 Go 二进制 + systemd，任何 ECS 复制即用。

## 功能清单

- [x] 项目骨架：Go + gin 单二进制，internal/{api,auth,iptables,store} 分层
- [x] 多端口白名单：每条规则 {端口, IP/CIDR, 备注}，独立链 IPAW-<port>
- [x] 严格模式：仅白名单可连，非白名单 DROP；宽松模式只展示
- [x] 防锁死：自动加当前来源 IP、禁止删当前 IP、严格模式禁止删到空、先 ACCEPT 后 DROP
- [x] JSON 持久化 + systemd 开机自启 + 启动自动恢复规则
- [x] 幂等同步 Reconcile（启动/手动触发）
- [x] dry-run 模式（IPAW_DRY_RUN=1，本地安全模拟）
- [x] Web 页面：登录 + 白名单管理 + 当前来源 IP 展示
- [x] 账号密码登录（bcrypt 哈希）+ JWT 无状态鉴权（HttpOnly Cookie / X-Auth-Token / query）
- [x] 修改密码 + 记住登录（JWT 长会话，勾选 30 天，可配 remember_days）
- [x] 服务器概览：sysinfo 采集 CPU/内存/磁盘/负载/uptime/端口/最近登录，`/api/server-info`
- [x] 移动端适配（响应式布局）
- [x] deploy.sh 一键部署（curl 远程/本地）+ systemd 单元
- [x] README 完整文档
- [x] 登录记录：成功/失败登录时间+来源 IP，`/api/login-logs`，页面独立区块（保留最近 50 条）
- [x] 页面自升级：版本号显示+检查更新+一键升级（进度条/自动刷新/备份回滚），`/api/upgrade/check|upgrade|status`
- [x] 国内镜像源：阿里云 OSS 公共读桶 + CI 自动同步 + 默认 GitHub→镜像 fallback（IPAW_MIRROR）
- [x] 版本号自动生成：CI/deploy.sh 用 ldflags 从 git tag 注入，发版只打 tag
- [x] 安全：plans/plan.md 与 git 历史脱敏（filter-repo 重写）

## 测试清单

- [x] 本地编译 go build ./... 通过
- [x] dry-run 模式启动 + curl 登录测试
- [x] iptables 规则生成验证（dry-run 打印正确）
- [x] 防锁死异常用例（删当前 IP / 删到空 / 严格模式空白名单）
- [x] 真机部署到服务器验证（测试服务器 从 0 部署 + 自升级全链路）
- [x] 重启恢复验证
- [ ] 单元测试（iptables 规则生成、store 增删、auth 密码）

## 待办

- [x] **部署链路升级：零依赖一键安装**（2026-08-13 决策：不做 Docker，用 Release 预编译二进制；代码已完成，待真机验货）
  - [x] `.github/workflows/release.yml`：打 tag `v*` 触发，CGO_ENABLED=0 交叉编译 linux/amd64 + arm64 并上传 Release
  - [x] `deploy.sh`：无 Go 时按 `uname -m` 从 Release 下载预编译二进制（支持 `IPAW_VERSION` 指定版本）
  - [x] README 快速部署章节：明示无需装 Go/Docker/任何工具链，只需 Linux 标配 iptables；补"发版"说明
  - 边界：不改 Go 源码/API/配置字段；不引入 Docker
- [x] **国内服务器 curl 一键安装不可用**（测试服务器 实测 raw.githubusercontent.com 超时被墙）→ 已解决：OSS 镜像源 + GitHub→镜像 fallback（见变更日志）
- [x] 生产部署验证（测试服务器 真机从 0 部署 + 自升级全链路）
- [x] 迁移到 GitHub（含 Actions 发版 CI）
- [ ] **中台形态演进**（远期）：几十台内单机够用；后续可拆"中台管理端 + 每服务器轻量 Agent"，iptables 核心逻辑直接复用

## 架构演进（重要决策）

> 决策日期 2026-08-13：用户确认几十台以内，先单机版跑通再演进中台。

**当前形态**：单机版（管理端 + 执行端一体），每台服务器装一套完整服务。适用于几十台以内。

### 中台演进路线图（远期）

**阶段一 · 单机版（当前，已完成）**
- 管理端 + 执行端一体，每台服务器装一套完整服务
- 一套配置只作用于本机 iptables

**阶段二 · 中台 + Agent（下一步）**
- 中台：唯一 Web 管理端，集中管理所有服务器；一套白名单可下发/推送到多台
- Agent：每台服务器轻量执行端，只负责改 iptables + 上报状态（心跳/规则生效结果），无 Web
- 复用：`internal/iptables`（规则生成/防锁死）零改动

**阶段三 · 中台增强（可选）**
- 多租户/账号体系、操作审计、规则灰度发布、Agent 离线告警

### 拆分要动的地方（阶段二）

| 模块 | 单机版现状 | 中台版改造 |
|------|-----------|-----------|
| api | 管理 API + 页面一体 | 拆「管理 API（中台）」与「Agent 上报/接收 API」 |
| store | 本地 JSON 文件 | 中台下发 + 本地缓存兜底（断网仍生效，重启不丢） |
| auth | 单机 JWT（本机 secret） | 中台统一鉴权，Agent 用服务凭证 |
| 新增 | — | Agent 心跳、规则同步、状态上报协议 |

### 关键约束（演进不破坏现有）

- `internal/iptables` 核心防锁死逻辑不变，Agent 直接内嵌复用
- 单机版仍可独立运行（Agent 可退化为单机模式），不强制依赖中台
- 返回字段只增不删，已有 API path/method 不变，中台能力追加在末尾

## 变更日志

| 日期 | 内容 |
|------|------|
| 2026-08-13 | 初始化项目，完成骨架/核心/API/前端/部署/README |
| 2026-08-13 | 新增改密码 + 记住登录 + 移动端适配 |
| 2026-08-13 | 鉴权改 JWT 无状态 + 新增服务器概览（sysinfo），README/plan 同步 |
| 2026-08-13 | 部署链路升级：Release 预编译二进制（CI 交叉编译 amd64/arm64）+ deploy.sh 自动下载 + README 零依赖说明 |
| 2026-08-14 | fix: "当前来源"被固化进持久化 remark 导致历史 IP 误导——自动加入备注改中性 `auto`，"当前"唯一性由前端实时 current_ip 判断，旧数据前端归一化显示 |
| 2026-08-14 | 部署链路走读修复：Release latest URL 写错(Critical)、无 Go 场景不再依赖 git clone、web 前端 go:embed 内嵌进二进制（部署只需一个文件，前后端版本永不漂移） |
| 2026-08-14 | 真机验证（测试服务器 测试服务器）：无 Go 服务器 Release 下载分支成功、go:embed 单文件页面正常、allowlist 保留、config 恢复原密码/secret。发现：raw.githubusercontent.com 国内被墙，curl 一键安装对国内服务器不可用 |
| 2026-08-14 | fix: Secure cookie 导致裸 HTTP 部署登录后 401 循环、前端跳回登录页（测试服务器 真机触发）——secure 改为按 HTTPS/X-Forwarded-Proto 动态判断 |
| 2026-08-14 | feat: 登录记录功能——成功/失败登录记录时间+来源IP，`/api/login-logs` 接口，页面单独区块展示最近 50 条 |
| 2026-08-14 | feat: 自升级——页面显示版本号+检查更新（GitHub 对比）+一键升级（下载→校验→备份→原子替换→重启回滚），`/api/upgrade/check` + `/api/upgrade` |
| 2026-08-14 | 从0真机部署验证（测试服务器 全新安装）：预编译/Release 分支、go:embed 单文件、首次启动宽松规则、登录记录、版本检查全部正常。修复 deploy.sh 缺 mkdir（从0部署目录不存在导致下载失败） |
| 2026-08-14 | 安全脱敏：plans/plan.md 移除测试服务器 IP，git 历史重写（filter-repo）+ force push 彻底清除 |
| 2026-08-14 | feat: 自升级加 IPAW_MIRROR 镜像前缀 + 精确进度条——升级改异步状态机，`/upgrade/status` 轮询下载百分比，前端进度条展示 |
| 2026-08-14 | fix: 自升级跨设备替换失败（/tmp 独立挂载点，os.Rename EXDEV）——下载临时文件改到二进制同目录，保证同文件系统原子替换（真机演示暴露） |
| 2026-08-14 | 自升级全链路真机验证完成（测试服务器）：v1.0.12→v1.0.13 页面升级，进度条 100% + 自动刷新正常。发版节奏反思：正常应攒批发版，勿一功能一版（今天演示产生 v1.0.0~v1.0.13 属异常） |
| 2026-08-14 | 自升级镜像源：gitee 不可行（强制登录，匿名下载 403，自升级无法带认证）→ 改用阿里云 OSS 公共读桶 + CI 自动同步上传，IPAW_MIRROR 指向 OSS。配 RAM 子用户最小权限 + GitHub Secrets（OSS_BUCKET/OSS_ENDPOINT/OSS_AK_ID/OSS_AK_SECRET）|
| 2026-08-14 | 自升级下载源策略：**默认 GitHub，失败自动 fallback IPAW_MIRROR 镜像**（每源超时 30s→10s）。真机验证：GitHub 超时→OSS 接管升级 v1.0.15→v1.0.16；OSS 下载 0.28s（阿里云互访）|
| 2026-08-14 | 10s fallback 真机验证完成（v1.0.18）：日志确认 GitHub 恰好 10s 超时→自动切 OSS→升级成功，优化生效 |
| 2026-08-14 | 版本号自动生成（v1.0.19）：main.go 默认 `dev`，CI/deploy.sh 用 `-ldflags -X main.Version=<tag>` 注入，发版不再手改版本号。验证：OSS 二进制 -version 正确显示 v1.0.19 |
| 2026-08-14 | 从 内部项目 适配 6 个核心开发流程 skill 到本仓库（sync/feature/bugfix/hotfix/refactor/test）：保留方法论骨架，替换为 ip-allowlist 技术栈（gin 分层/防锁死/dry-run/systemctl），已脱敏内部信息 |
| 2026-08-14 | 生产服务器（正式环境，内部项目 服务器 [内部域名]/[服务器IP]）部署升级：v1.0.0→v1.0.20，严格模式规则/白名单/config 全保留。发现 SSH 断连是域名解析到旧 IP，用稳定 IP 解决 |
| 2026-08-14 | fix（v1.0.20）：cookie 自动登录时初始加载不显示版本号/登录记录——初始加载补 loadLoginLogs+loadUpgradeInfo（用户刷新 生产服务器 页面触发） |
| 2026-08-14 | 安全优化（v1.0.21）：① deploy.sh 生成随机 JWT secret + main.go 拒绝默认 secret（防伪造 token）② store 原子写（tmp+rename）+ 损坏自 .bak 恢复（防崩溃损坏）③ 自升级 SHA256 校验和（release.yml 发 SHA256SUMS，升级拉同源校验，防镜像投毒）|
