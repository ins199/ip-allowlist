# 更新日志 (Changelog)

所有重要变更记录在此。版本格式遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## v1.0.6 (2026-08-14)

- feat: 自升级精确进度条——升级改异步状态机，`/api/upgrade/status` 轮询下载百分比，前端进度条实时展示
- feat: 自升级支持 `IPAW_MIRROR` 环境变量镜像前缀（国内服务器访问 GitHub release 资产受限时，可配置国内可达镜像）
- fix: `deploy.sh` 补 `mkdir -p INSTALL_DIR`（从 0 部署时目录不存在导致二进制下载失败）

## v1.0.5 (2026-08-14)

- chore: 版本号更新（自升级演示目标）

## v1.0.4 (2026-08-14)

- feat: 自升级支持 `IPAW_MIRROR` 环境变量镜像前缀

## v1.0.3 (2026-08-14)

- chore: 版本号更新（自升级演示目标）

## v1.0.2 (2026-08-14)

- feat: 页面自升级——版本号显示 + 检查更新（对比 GitHub 最新 release，语义化版本比较）+ 一键升级（下载→校验→备份当前→原子替换→重启，失败回滚）
- 新增接口：`GET /api/upgrade/check`、`POST /api/upgrade`

## v1.0.1 (2026-08-14)

- feat: 登录记录功能——成功/失败登录记录时间 + 来源 IP，持久化保留最近 50 条；页面独立区块展示
- 新增接口：`GET /api/login-logs`
- fix: Secure cookie 导致裸 HTTP 部署登录后 401 循环、前端跳回登录页——secure 改为按 HTTPS/X-Forwarded-Proto 动态判断

## v1.0.0 (2026-08-14)

- feat: 零依赖一键安装——打 tag 触发 CI 交叉编译 linux/amd64 + arm64 发布 Release，无 Go 服务器自动按架构下载，无需装 Go/Docker/git
- feat: web 前端 `go:embed` 内嵌进二进制，部署只需一个文件，前后端版本永不漂移
- fix: 当前来源标注——自动加入白名单的备注改中性 `auto`，"当前"唯一性由前端实时 `current_ip` 判断，旧数据归一化显示
