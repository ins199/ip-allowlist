---
name: feature
description: >
  Use when 新增功能、新接口、新页面、新配置. "新功能开发".
  写代码前先读现有分层模式（api/store/iptables/auth），禁止凭记忆从零写。
---

# feature — 新功能开发

## 硬性规则

- **写代码前先读现有模式** — 参考 `internal/api/server.go` 现有 handler、`internal/store/store.go` 现有方法、`web/index.html` 现有页面写法，禁止凭记忆
- **分层不跨层** — api 调 store/iptables/auth，iptables 不碰 HTTP，store 不碰防火墙
- **涉及 iptables 的改动必须过防锁死校验** — 参考 `internal/iptables/iptables.go` 的防锁死逻辑，dry-run 验证
- **参考 CLAUDE.md** — 安全铁律、开发规范、完成标准

## 流程

### 1. 读现有模式

```bash
# 每次写新代码前，先看对应的现有实现：
cat internal/api/server.go        # handler 写法 + 路由注册
cat internal/store/store.go       # 数据结构 + 方法模式
cat internal/iptables/iptables.go # 规则/防锁死（涉及防火墙时）
cat web/index.html                # 前端写法（涉及页面时）
```

### 2. 分层写代码

**api 层**（`internal/api/server.go`）：
- handler 结构跟随现有写法（ResForXxx 响应结构、错误用 `gin.H{"code":0,"msg":...}`）
- **新路由追加到 authGroup 末尾**，禁止插入中间
- 返回字段**只增不删**，字段类型不变
- 涉及白名单/防火墙的 handler 必须带防锁死（禁止删当前 IP、禁止删到空、自动补当前 IP）

**store 层**（`internal/store/store.go`）：
- 方法带锁（`s.mu.Lock()`/`RLock()`），返回副本
- 持久化走 `saveLocked()`，字段加 `json` 标签

**iptables 层**（`internal/iptables/iptables.go`）：
- 规则生成集中在此，防锁死逻辑不动
- 严格模式空白名单拒绝、先 ACCEPT 后 DROP

**前端**（`web/index.html`）：
- 单文件内嵌，样式跟随现有卡片/badge 风格
- 用户输入用 `esc()` 转义（防 XSS）

### 3. 写完后对照 CLAUDE.md 完成标准自检

- [ ] 有无动无关文件
- [ ] 防锁死用例走查（删当前 IP / 删到空 / 严格模式空白名单）
- [ ] 正常流程 + 边界 + 错误分支日志
- [ ] 更新 `plans/plan.md` 测试清单

### 4. 验证

```bash
go build ./...
# 涉及 iptables：dry-run 验证规则输出
IPAW_DRY_RUN=1 go run . -config config.example.yaml -data /tmp/test.json -bind 127.0.0.1:10443
```
