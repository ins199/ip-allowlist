---
name: test
description: >
  Use when "接口测试", "测试接口", "test api".
  Local-only: dry-run 起服务，按测试清单逐条测接口，验证后标记 [x]。
  涉及 iptables 必须保持 IPAW_DRY_RUN=1，绝不上生产。
---

# test — 按测试清单逐条测接口

## 流程

### ① 加载测试清单

```bash
# 查看 plan 的测试清单
grep -A 20 "测试清单" plans/plan.md
```

从第一个未测 `[ ]` 开始，一个接一个测试。每次只测一条。

### ② dry-run 起服务

```bash
IPAW_DRY_RUN=1 go run . -config config.example.yaml -data /tmp/test.json -bind 127.0.0.1:10443
```

**IPAW_DRY_RUN=1 必须保持**——只打印 iptables 命令，不真正改防火墙。

### ③ 登录拿 token

```bash
curl -s -X POST http://127.0.0.1:10443/api/login \
  -d '{"username":"admin","password":"changeme"}' \
  -H 'Content-Type: application/json'
```

### ④ 测试单个接口

对当前正在测的接口：

```bash
TOKEN="<token>"
curl -s http://127.0.0.1:10443/api/<接口> -H "X-Auth-Token: $TOKEN"
```

**验证标准**：
- `code == 1` → ✅ 通过，标记 `[x]`
- `code != 1` → ❌ 失败，记录原因，暂停等我确认
- 涉及防火墙 → 检查 dry-run 打印的 iptables 规则顺序（先 ACCEPT 后 DROP）

### ⑤ 防锁死用例（必测）

- 删除当前来源 IP → 应拒绝
- 严格模式删到白名单空 → 应回滚
- 严格模式空白名单应用 → 应拒绝
- 开启严格模式 → 当前 IP 自动补入白名单

## 规则

- **测完立刻标 [x]** — code=1 算通过，不许攒到最后
- **只在本地 dry-run**，绝不上生产/预发布
- 每次只测一条，不并行
- 本地不通立刻修，不跳过
- 涉及写操作 → 先确认数据可回滚
- 全过才 push
