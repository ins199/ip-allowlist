---
name: refactor
description: ip-allowlist 重构 skill。涉及重构、拆分函数、重命名、调整目录结构、提取公共逻辑时加载。
  强制重构前后行为不变，禁止顺手改业务逻辑。
---

# ip-allowlist — 安全重构

## 核心规则

**重构 = 改结构不改行为。** 如果行为变了，那不是重构，是 feature/fix，走对应的 skill。

## 重构前

- [ ] `git log --oneline -10 -- <文件>` — 了解近期改动和脆弱点
- [ ] 确认重构范围——只动目标文件，不顺手改旁边
- [ ] `go build ./...` 确认当前基线通过

## 重构时

- [ ] 不改函数签名（除非调用方全部一起改）
- [ ] **不改已有 API path / HTTP method**（CLAUDE.md 红线）
- [ ] 不改已有 store 字段类型/名称
- [ ] 不改 config 字段名
- [ ] 保留原有错误处理分支（不吞错误、不改变错误信息）
- [ ] 提取公共逻辑时不改变原有行为（哪怕原写法不优雅）
- [ ] **`internal/iptables` 防锁死逻辑不动**——这是系统安全核心

## 重构后

- [ ] `git diff` 自查：只改了结构，没有行为变更
- [ ] `go build ./...` + `go vet ./...` + `gofmt -l .`（应为空）
- [ ] 涉及 api/iptables 改动：`IPAW_DRY_RUN=1` 起服务，验证核心接口行为不变
- [ ] 补充 commit message 说明：重构了什么、为什么重构、为什么不会影响行为
