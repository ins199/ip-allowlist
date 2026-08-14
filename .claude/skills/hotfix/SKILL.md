---
name: hotfix
description: ip-allowlist 热修复 skill。涉及"热修复""hotfix""紧急修复""生产bug"时加载。
  开发分支修好→cherry-pick到hotfix分支→merge到master→tag发版。
  不直接在master上改，不污染开发分支，不影响工作区。
---

# hotfix — 热修复

## 设计理念

直接在 master 改 → 工作区被毁、没验证就推了。
整分支 cherry-pick → 夹带未验证代码。
**正确：拉一个独立 hotfix 分支，cherry-pick 上去，merge 到 master。**

- hotfix 分支隔离，冲突不影响 master
- merge commit 干净，历史可追溯
- 操作完切回开发分支，工作区毫发无伤

## 执行清单

### 1. 确认当前状态

```bash
git branch --show-current   # 开发分支
git status                   # clean（修复已提交并 push）
git log --oneline -5         # 确认要上生产的 commit hash
```

### 2. 确认 hotfix commit

列出来让用户确认。**只挑修复相关的那几个 commit，不夹带其他。**

### 3. 创建 hotfix 分支 + cherry-pick

```bash
git checkout master
git pull
git checkout -b hotfix-<简述>       # 如 hotfix-login-401
git cherry-pick <commit1> <commit2>  # 只挑修复 commit
```

有冲突 → 在 hotfix 分支上解决，不影响 master。不顺利 → `git checkout <开发分支>` 回去，`git branch -D hotfix-xxx` 删掉重来。

### 4. 编译验证

```bash
go build ./...
go vet ./...
```

**编译不过绝对不能继续。**

### 5. Merge 到 master + tag 发版

```bash
git checkout master
git merge hotfix-xxx
git push origin master
git tag -a v<下个版本> -m "hotfix: <描述>"
git push origin v<下个版本>
```

tag 触发 CI 自动编译并发布 Release + 同步 OSS 镜像。

### 6. 切回开发分支

```bash
git checkout <原来的开发分支>    # 恢复工作区，这一步不能忘
```

### 7. 验证发布

```bash
# 等 CI 完成
gh run watch <run-id>

# 确认 Release 资产 + OSS 镜像
gh release view v<版本> --json assets
curl -sI https://<your-bucket>.oss-cn-<region>.aliyuncs.com/ip-allowlist-linux-amd64

# 服务器自升级或手动替换后验证
systemctl status ip-allowlist
/opt/ip-allowlist/ip-allowlist -version
```

**验证清单：**
- [ ] CI success，Release 资产存在
- [ ] OSS 镜像已同步
- [ ] 服务器版本正确、服务 active

### 8. 记录到 plan

`plans/plan.md` 变更日志追加：

```markdown
- [x] **hotfix: <简述>** — cherry-pick <commit> → hotfix 分支 → merge master → tag v<版本>
```

### 9. 清理（可选）

```bash
git branch -d hotfix-xxx      # 本地删掉，历史已在 master 的 merge commit 中
```

## 历史事故复盘

| 事故 | 根因 | 教训 |
|------|------|------|
| 编译失败上线 | 合并冲突括号丢失，没 go build 就 push | **编译不过绝不能 push** |
| 工作区被切分支破坏 | 热修复切到 master，状态全丢 | **hotfix 分支隔离，不破坏开发分支工作区** |
| 发布未生效 | tag push 后没验证 CI/部署 | **tag push 后必须验证 Release/OSS/服务器状态** |
