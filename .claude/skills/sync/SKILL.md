---
name: sync
description: >
  ip-allowlist 同步 skill。凡涉及完成任务、结束会话、提交代码前必须加载。
  强制更新 plan + go build 验证 + git push，防止漏记录漏推送。
---

# sync — 同步 plan + push

## 触发条件

以下任一情况必须加载此 skill：
- 用户说"完成""好了""搞定""sync push""提交并推送"
- 准备结束会话前
- 刚完成一个修复/功能/改动

## 强制流程（不得跳过）

### 1. 确认所有改动已提交

```bash
git status
git log --oneline -3
```

### 2. 更新 plan 文件（5 项检查）

对照 CLAUDE.md 的 5 项硬性检查：
1. 完成的 `[ ]` → `[x]`
2. 新迁移 SQL → 追到迁移脚本表（本项目 JSON 持久化无 SQL，跳过）
3. 新接口/功能 → 追到「测试清单」
4. 更新 `plans/plan.md` 模块状态
5. 当天修复/故障/运维 → 追到「变更日志」

> 漏掉等于撒谎。plan 是镜像，代码是真理。

### 3. 编译验证（编译不过禁止 push）

```bash
go build ./...
go vet ./...
gofmt -l .            # 应为空
bash -n deploy.sh     # 改过部署脚本时执行
```

任一步不通过 → **禁止 push**，修好再走 sync 流程。

### 4. Push

```bash
git push
```

### 5. 验证（按改动范围）

- 改 iptables 相关 → `IPAW_DRY_RUN=1 go run . -config config.example.yaml -data /tmp/test.json -bind 127.0.0.1:10443` 走防锁死用例
- 改前端/API → 本地起服务 curl 验证
- 改部署链路 → `bash -n deploy.sh` + 真机或 dry-run 走查

### 6. 最终确认

```bash
git status
# 预期: clean
```
