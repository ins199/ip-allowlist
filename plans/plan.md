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

## 测试清单

- [ ] 本地编译 go build ./... 通过
- [ ] dry-run 模式启动 + curl 登录测试
- [ ] iptables 规则生成验证（dry-run 打印正确）
- [ ] 防锁死异常用例（删当前 IP / 删到空）
- [ ] 真机部署到服务器验证规则生效
- [ ] 重启恢复验证

## 待办

- [ ] **部署链路升级：零依赖一键安装**（2026-08-13 决策：不做 Docker，用 Release 预编译二进制）
  - 新建 `.github/workflows/release.yml`：打 tag `v*` 触发，CGO_ENABLED=0 交叉编译 linux/amd64 + arm64 并上传 Release
  - 改 `deploy.sh`：无 Go 时按 `uname -m` 从 Release 下载预编译二进制（支持 `IPAW_VERSION` 指定版本）
  - 更新 README 快速部署章节：明示无需装 Go/Docker/任何工具链，只需 Linux 标配 iptables；补"发版"说明
  - 边界：不改 Go 源码/API/配置字段；不引入 Docker
- [ ] 单元测试（iptables 规则生成、store 增删、auth 密码）
- [ ] 生产部署验证
- [ ] 迁移到 GitHub
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
