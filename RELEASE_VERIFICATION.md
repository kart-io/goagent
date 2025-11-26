# Release 验证指南

**创建时间:** 2025-11-26
**Tag 版本:** v0.1.0

---

## 已完成的操作

1. ✅ 删除了旧的 v0.1.0 tag (由 auto-tag 创建的)
2. ✅ 创建了新的 v0.1.0 tag，包含详细的 release notes
3. ✅ 推送 tag 到远程仓库
4. ✅ 应该已触发 release workflow

---

## 检查 Release 状态

### 方法 1: 通过 GitHub Web UI

1. **检查 Workflow 运行状态:**
   ```
   https://github.com/kart-io/goagent/actions
   ```
   - 查找名为 "Release" 的 workflow
   - 应该看到一个正在运行或已完成的任务
   - 点击查看详细日志

2. **检查 Release 页面:**
   ```
   https://github.com/kart-io/goagent/releases
   ```
   - 应该看到 v0.1.0 release
   - 包含构建的二进制文件：
     - goagent-v0.1.0-linux-amd64.tar.gz
     - goagent-v0.1.0-linux-arm64.tar.gz
     - goagent-v0.1.0-darwin-amd64.tar.gz
     - goagent-v0.1.0-darwin-arm64.tar.gz
     - goagent-v0.1.0-windows-amd64.zip
     - checksums.txt

3. **检查 Tags 页面:**
   ```
   https://github.com/kart-io/goagent/tags
   ```
   - 应该看到 v0.1.0 tag
   - 显示 release notes

### 方法 2: 通过命令行 (如果已安装 gh CLI)

```bash
# 安装 gh CLI (如果还没安装)
# Ubuntu/Debian:
sudo apt install gh

# macOS:
brew install gh

# 登录
gh auth login

# 检查 workflow 状态
gh run list --workflow=release.yml

# 监控最新的 workflow 运行
gh run watch

# 查看 releases
gh release list

# 查看特定 release
gh release view v0.1.0
```

### 方法 3: 通过 Git 命令

```bash
# 确认 tag 已推送
git ls-remote --tags origin | grep v0.1.0

# 应该看到:
# <commit-hash>    refs/tags/v0.1.0

# 查看 tag 详情
git show v0.1.0
```

---

## 预期结果

### Release Workflow 应该执行以下步骤:

1. ✅ **Checkout code** - 检出代码
2. ✅ **Set up Go** - 设置 Go 1.23
3. ✅ **Run tests** - 运行所有测试（应该全部通过）
4. ✅ **Verify import layering** - 验证导入层级（应该通过）
5. ✅ **Build binaries** - 构建 5 个平台的二进制文件
6. ✅ **Generate checksums** - 生成 SHA256 校验和
7. ✅ **Extract release notes** - 提取 release notes
8. ✅ **Create GitHub Release** - 创建 GitHub Release
9. ✅ **Publish to pkg.go.dev** - 发布到 Go 包仓库

### 预期时间

- **总耗时:** 约 5-10 分钟
- **测试阶段:** 2-3 分钟
- **构建阶段:** 3-5 分钟
- **发布阶段:** 1-2 分钟

---

## 如果 Release 失败

### 常见问题和解决方案

#### 问题 1: 测试失败

**症状:** Workflow 在 "Run tests" 步骤失败

**解决方案:**
```bash
# 本地运行测试检查
make test

# 修复问题后，删除 tag 并重新创建
git tag -d v0.1.0
git push origin :refs/tags/v0.1.0

# 修复代码...
git commit -m "fix: resolve test failures"
git push origin master

# 重新创建 tag
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

#### 问题 2: 构建失败

**症状:** Workflow 在 "Build binaries" 步骤失败

**解决方案:**
```bash
# 本地测试多平台构建
GOOS=linux GOARCH=amd64 go build -v ./...
GOOS=darwin GOARCH=arm64 go build -v ./...
GOOS=windows GOARCH=amd64 go build -v ./...
```

#### 问题 3: 权限错误

**症状:** "Permission denied" 或 "403 Forbidden"

**解决方案:**
- 检查仓库设置 → Actions → General → Workflow permissions
- 确保选择了 "Read and write permissions"
- 保存设置后重新运行 workflow

#### 问题 4: Workflow 没有触发

**症状:** Actions 页面没有看到新的 workflow 运行

**解决方案:**
```bash
# 1. 确认 tag 格式正确 (必须是 v*.*.*)
git tag -l v0.1.0

# 2. 确认 tag 已推送到远程
git ls-remote --tags origin | grep v0.1.0

# 3. 检查 workflow 文件是否在远程
git ls-remote origin | grep workflows

# 4. 手动触发 (如果 release.yml 支持 workflow_dispatch)
# 在 GitHub Actions 页面手动触发
```

---

## 验证 Release 完整性

### 1. 下载并测试二进制文件

```bash
# 下载 checksums
wget https://github.com/kart-io/goagent/releases/download/v0.1.0/checksums.txt

# 下载二进制
wget https://github.com/kart-io/goagent/releases/download/v0.1.0/goagent-v0.1.0-linux-amd64.tar.gz

# 验证校验和
sha256sum -c checksums.txt

# 解压并测试
tar -xzf goagent-v0.1.0-linux-amd64.tar.gz
./goagent-linux-amd64 --version
```

### 2. 验证 pkg.go.dev

```bash
# 等待几分钟后访问
curl https://pkg.go.dev/github.com/kart-io/goagent@v0.1.0

# 或在浏览器打开:
# https://pkg.go.dev/github.com/kart-io/goagent@v0.1.0
```

### 3. 测试 Go Module 安装

```bash
# 在新目录测试
mkdir /tmp/test-goagent
cd /tmp/test-goagent
go mod init test

# 安装特定版本
go get github.com/kart-io/goagent@v0.1.0

# 检查 go.mod
cat go.mod
```

---

## 监控命令

### 实时监控 (需要在另一个终端运行)

```bash
# 每 30 秒检查一次 workflow 状态
while true; do
  clear
  echo "=== Checking Release Status ==="
  echo ""

  # 检查 tag
  echo "Tag status:"
  git ls-remote --tags origin | grep v0.1.0
  echo ""

  # 尝试访问 release (需要 curl)
  echo "Release status:"
  curl -s https://api.github.com/repos/kart-io/goagent/releases/tags/v0.1.0 | \
    jq -r '.name, .published_at' 2>/dev/null || echo "Release not yet created"
  echo ""

  echo "Checking again in 30 seconds..."
  sleep 30
done
```

---

## Release Notes 内容

v0.1.0 release 包含以下内容:

### 🚀 Features (2)
- Automatic version tagging and release workflow
- Enhanced retry jitter fallback and SQL injection protection

### 📚 Documentation (3)
- Comprehensive next phase execution plan
- Code review checklist and test coverage report
- Complete documentation for Shell and Database tools

### 🧪 Tests (2)
- Comprehensive SQL injection protection tests (50+ test cases)
- LLM providers base functionality tests (60+ test cases)

### 📊 Statistics
- Total commits: 7
- New tests: 110+
- New assertions: 250+
- Coverage improvement: +3.5%
- Code added: 5,298 lines
- Documentation added: 3,346 lines

---

## 下一步

### 如果 Release 成功

1. ✅ 在 https://github.com/kart-io/goagent/releases 看到 v0.1.0
2. ✅ 下载并测试二进制文件
3. ✅ 等待 pkg.go.dev 索引（5-15 分钟）
4. ✅ 通知团队新版本已发布
5. ✅ 更新文档中的版本引用

### 如果 Release 失败

1. ❌ 查看 Actions 页面的错误日志
2. ❌ 根据错误类型应用上述解决方案
3. ❌ 修复问题
4. ❌ 删除并重新创建 tag
5. ❌ 或创建新的 patch 版本 (v0.1.1)

---

## 联系和支持

**遇到问题?**

1. 查看 GitHub Actions 日志
2. 查看本文档的故障排查部分
3. 查看 VERSION_MANAGEMENT.md
4. 创建 GitHub Issue

---

**文档生成时间:** 2025-11-26
**预计 Release 完成时间:** 约 5-10 分钟后
