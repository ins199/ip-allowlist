# ip-allowlist — Claude Code 协作规范

> 本文件为 Claude Code 及所有 AI 协作工具使用。任务开始前完整阅读。

## 项目概览

- **项目名**：ip-allowlist
- **技术栈**：Go + gin + 直接调系统 iptables
- **定位**：通用 IP 白名单管理系统，单二进制部署到任意 Linux 宿主机即用
- **核心约束**：改的是**宿主机** iptables，任何误操作可能锁死 SSH

## 目录结构

```
ip-allowlist/
├── main.go              # 入口（加载配置→初始化→启动）
├── config.go            # YAML 配置加载
├── internal/
│   ├── api/             # HTTP API + Web 页面服务
│   ├── auth/            # 账号密码(bcrypt) + session
│   ├── iptables/        # iptables 规则生成/应用/防锁死
│   └── store/           # JSON 持久化
├── web/                 # 前端页面（内嵌单页）
├── deploy/              # install.sh + systemd 单元
└── plans/plan.md        # 开发计划
```

## 安全铁律（最高优先级）

1. **严禁执行会锁死 SSH 的操作**：禁止删当前来源 IP、禁止严格模式删到空、必须先 ACCEPT 后 DROP。
2. **dry-run 优先**：本地测试必须 `IPAW_DRY_RUN=1`，只打印 iptables 命令不执行。生产操作需用户明确确认。
3. **改 iptables 的代码路径**必须经过防锁死校验：`ApplyPortRule` 中严格模式空白名单拒绝 + 自动补当前 IP。
4. **不擅自改动部署环境**：生产服务器操作需用户授权。

## 开发规范

- 分层清晰：api 调 store/iptables/auth，不跨层。
- 所有新增 handler 注册在 `internal/api/server.go` 的路由组中。
- iptables 规则逻辑在 `internal/iptables/iptables.go`，防锁死逻辑集中在此。
- store 用带锁的 `Store`，JSON 持久化。
- 敏感配置（密码）不提交，放 `config.yaml`（gitignore）。

## 常用命令

```bash
go build ./...                    # 编译
IPAW_DRY_RUN=1 go run . -config config.example.yaml -data /tmp/test.json -bind 127.0.0.1:10443  # dry-run 本地运行
curl -X POST http://127.0.0.1:10443/api/login -d '{"username":"admin","password":"changeme"}' -H 'Content-Type: application/json'
```

## 完成标准

1. `go build ./...` 通过
2. dry-run 模式验证 iptables 规则输出正确
3. 防锁死异常用例走查（删当前 IP / 删到空 / 严格模式空白名单）
4. 更新 `plans/plan.md` 清单
