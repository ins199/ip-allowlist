# 更新日志 (Changelog)

所有重要变更记录在此。版本格式遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## v1.0.0 (2026-08-14) — 正式首版

首个正式版本，完整功能如下。

### 核心能力
- 多端口 IP 白名单管理（iptables），单二进制部署到任意 Linux 宿主机即用
- 严格/宽松模式，防锁死（自动补当前 IP / 禁删当前 IP / 禁删到空 / 先 ACCEPT 后 DROP）
- JSON 持久化 + systemd 开机自启 + 启动自动恢复规则 + 幂等 Reconcile
- dry-run 模式（`IPAW_DRY_RUN=1`）本地安全模拟

### 管理与安全
- 账号密码登录（bcrypt 哈希）+ JWT 无状态鉴权（HttpOnly Cookie）
- 登录记录（成功/失败 + 来源 IP，页面独立区块）、服务器概览（sysinfo）
- 修改密码 + 记住登录、移动端适配

### 部署与自升级
- 零依赖一键安装：打 tag 触发 CI 交叉编译 linux/amd64 + arm64，发布 Release + 同步阿里云 OSS 镜像
- web 前端 `go:embed` 内嵌进二进制，单文件部署
- 页面自升级：版本显示 / 检查更新 / 一键升级（进度条 / 自动刷新 / 备份回滚 / SHA256 校验）
- 默认走 GitHub 官方源，下载失败自动 fallback 到 OSS 镜像（`IPAW_MIRROR`）
- 版本号自动生成（CI/deploy.sh 用 `-ldflags` 从 git tag 注入）

### 安全优化
- 随机 JWT secret + 拒绝默认值（防伪造 token）
- store 原子写 + 数据损坏自恢复
- 自升级 SHA256 校验和（防镜像投毒）+ 升级失败自动回滚
- clientIP 仅信任可信反代（防伪造 header 破坏防锁死）

> 开发历史：v1.0.0~v1.0.22 内部迭代版本已合并至本正式版。
