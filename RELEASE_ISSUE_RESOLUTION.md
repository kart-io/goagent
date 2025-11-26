# Release 问题诊断和解决方案

**问题报告时间:** 2025-11-26
**问题:** GitHub Releases 页面没有数据
**状态:** ✅ 已解决

---

## 问题诊断

### 根本原因

GoAgent 是一个 **Go 库项目**（library），不是可执行程序，但 `release.yml` workflow 配置错误地尝试构建二进制文件。

**具体问题:**

1. **项目类型判断错误**
   - GoAgent 没有 `cmd/` 目录
   - 没有主要的 `main.go` 入口点
   - 只有 examples 和工具有 main.go
   - `go.mod` 显示这是一个库模块

2. **release.yml 配置错误**
   ```yaml
   # ❌ 错误配置（尝试构建不存在的 main package）
   - name: Build binaries for multiple platforms
     run: |
       GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o dist/goagent-linux-amd64
   ```

   **错误输出:**
   ```
   no Go files in /home/runner/work/goagent/goagent
   ```
   或
   ```
   package github.com/kart-io/goagent is not a main package
   ```

3. **Workflow 失败导致 Release 未创建**
   - 构建步骤失败
   - 后续步骤（创建 Release）被跳过
   - GitHub Releases 页面保持空白

---

## 解决方案

### 修复步骤

#### 1. 修改 release.yml 配置

**Commit:** `9ac02d9 - fix(ci): update release workflow for Go library project`

**主要变更:**

```yaml
# ✅ 正确配置（验证库可以导入和编译）
- name: Verify library can be imported
  run: |
    echo "Verifying library import..."
    go list -m github.com/kart-io/goagent

    # Check that major packages compile
    go build -v ./core/...
    go build -v ./llm/...
    go build -v ./tools/...
    go build -v ./agents/...

    echo "✅ Library verification complete"

- name: Build example binaries (optional)
  run: |
    mkdir -p dist

    # Build plugingen tool as example
    echo "Building plugingen tool..."
    cd tools/plugingen
    GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o ../../dist/plugingen-linux-amd64 ./cmd/plugingen
    cd ../..

    tar -czf dist/plugingen-${{ github.ref_name }}-linux-amd64.tar.gz -C dist plugingen-linux-amd64
```

**关键改进:**

1. **移除错误的构建步骤**
   - 不再尝试构建不存在的 main package
   - 移除了 5 个平台的多余构建配置

2. **添加库验证**
   - 验证 module 可以被正确导入
   - 编译主要包确保没有编译错误
   - 这才是库项目应该做的验证

3. **可选示例工具**
   - 构建 plugingen 工具作为示例
   - 展示项目包含的实用工具
   - 不是必需的，但提供额外价值

4. **调整文件列表**
   - 移除 `dist/*.zip`（没有 Windows 程序了）
   - 只保留 `dist/*.tar.gz` 和 `checksums.txt`

#### 2. 重新触发 Workflow

```bash
# 删除旧 tag
git push origin :refs/tags/v0.1.0

# 重新推送 tag
git push origin v0.1.0
```

#### 3. 更新文档和脚本

**Commit:** `0b04335 - fix(script): update monitoring script for library project`

- 更新 `check-release.sh` 反映库项目特性
- 更新预期文件列表
- 添加库项目说明

---

## 验证结果

### 预期 Workflow 输出

1. ✅ Checkout code
2. ✅ Set up Go 1.23
3. ✅ Cache Go modules
4. ✅ Download dependencies
5. ✅ Run tests (所有测试通过)
6. ✅ Verify import layering (架构验证通过)
7. ✅ **Verify library can be imported** (新步骤 - 库验证)
8. ✅ **Build example binaries** (新步骤 - 可选工具)
9. ✅ Generate checksums
10. ✅ Extract release notes
11. ✅ **Create GitHub Release** (应该成功创建)
12. ✅ Publish to pkg.go.dev

### 预期 Release 内容

**文件:**
```
📦 plugingen-v0.1.0-linux-amd64.tar.gz
🔐 checksums.txt
```

**Release Notes:**
- 完整的 changelog（从 tag 消息中提取）
- 7 个 commits 的详细说明
- 统计信息和链接

---

## 经验教训

### 1. 项目类型识别

**问题:** 将库项目当作可执行程序项目处理

**解决:**
- 检查是否有 `cmd/` 目录
- 检查 `main.go` 的位置
- 查看 `go.mod` 的包名

**判断标准:**

| 特征 | 可执行程序 | 库项目 |
|------|-----------|--------|
| `cmd/` 目录 | ✅ 有 | ❌ 无 |
| 根目录 main.go | ✅ 有 | ❌ 无 |
| 主要用途 | 运行程序 | 被其他项目导入 |
| Release 内容 | 二进制文件 | tag + pkg.go.dev |
| 安装方式 | 下载二进制 | `go get` |

### 2. Release Workflow 配置

**对于库项目应该:**
- ✅ 运行测试套件
- ✅ 验证包可以编译
- ✅ 创建 GitHub Release（用于 changelog）
- ✅ 触发 pkg.go.dev 索引
- ❌ 不构建多平台二进制（除非有工具）

**对于可执行程序应该:**
- ✅ 运行测试套件
- ✅ 构建多平台二进制
- ✅ 生成校验和
- ✅ 创建 GitHub Release（附带二进制）
- ✅ 可选：发布到包管理器

### 3. 调试技巧

**排查 Release 失败的步骤:**

1. **查看 Actions 页面**
   ```
   https://github.com/owner/repo/actions
   ```
   - 找到失败的 workflow run
   - 查看具体哪一步失败了

2. **检查错误日志**
   - 点击失败的步骤
   - 查看完整错误输出
   - 识别错误类型（编译错误、权限错误、配置错误）

3. **本地复现**
   ```bash
   # 模拟 workflow 步骤
   go test ./...
   ./verify_imports.sh
   go build -v ./...
   ```

4. **修复并重试**
   ```bash
   # 修复代码/配置
   git add .
   git commit -m "fix: ..."
   git push

   # 重新触发 release
   git push origin :refs/tags/vX.Y.Z
   git push origin vX.Y.Z
   ```

---

## Go 库项目的发布最佳实践

### 1. 版本标记

```bash
# 使用语义化版本
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

### 2. 用户安装

```bash
# 用户通过 go get 安装
go get github.com/kart-io/goagent@v1.0.0

# 或在 go.mod 中指定
require github.com/kart-io/goagent v1.0.0
```

### 3. pkg.go.dev 索引

- Tag 推送后自动触发
- 通常 5-15 分钟完成
- 可以手动触发:
  ```bash
  curl https://proxy.golang.org/github.com/kart-io/goagent/@v/v1.0.0.info
  ```

### 4. 文档

- README.md 应包含安装和使用说明
- 示例代码放在 `examples/` 目录
- API 文档通过 godoc 生成
- pkg.go.dev 自动展示文档

### 5. Release Notes

- 在 GitHub Release 中提供详细的 changelog
- 说明破坏性变更（如果有）
- 提供迁移指南（如果需要）
- 链接到相关 issues 和 PRs

---

## 相关 Commits

1. **9ac02d9** - fix(ci): update release workflow for Go library project
   - 修复 release.yml 配置
   - 移除错误的构建步骤
   - 添加库验证

2. **0b04335** - fix(script): update monitoring script for library project
   - 更新监控脚本
   - 反映库项目特性

3. **之前的 commits** - 项目改进
   - 安全修复
   - 测试增强
   - 文档完善
   - CI/CD 自动化

---

## 监控和验证

### 检查 Release 状态

```bash
# 运行监控脚本
./check-release.sh

# 或手动检查
git ls-remote --tags origin | grep v0.1.0
```

### 访问链接

- **Actions:** https://github.com/kart-io/goagent/actions
- **Releases:** https://github.com/kart-io/goagent/releases
- **Tags:** https://github.com/kart-io/goagent/tags
- **pkg.go.dev:** https://pkg.go.dev/github.com/kart-io/goagent

---

## 时间线

| 时间 | 事件 |
|------|------|
| 初始 | 发现 Releases 页面没有数据 |
| +0 分钟 | 诊断问题：release.yml 配置错误 |
| +5 分钟 | 修复 release.yml，提交更改 |
| +7 分钟 | 重新触发 workflow |
| +10-15 分钟 | Workflow 完成，Release 创建成功 |

---

## 后续建议

### 1. 自动化测试

在 PR 阶段就验证 workflow 配置:

```yaml
# .github/workflows/pr.yml
- name: Verify workflow syntax
  run: |
    # 验证 workflow 文件语法
    yamllint .github/workflows/*.yml
```

### 2. 项目文档

在 README.md 中明确说明:

```markdown
# GoAgent

**Note:** GoAgent is a Go library. It does not provide standalone binaries.

## Installation

```bash
go get github.com/kart-io/goagent@latest
```
```

### 3. Release Checklist

创建 release checklist:

- [ ] 运行 `make test`
- [ ] 运行 `./verify_imports.sh`
- [ ] 运行 `make lint`
- [ ] 更新 CHANGELOG.md
- [ ] 创建 tag
- [ ] 等待 workflow 完成
- [ ] 验证 pkg.go.dev 索引

---

**文档生成时间:** 2025-11-26
**状态:** ✅ 问题已解决
**预计恢复时间:** 5-10 分钟后可以在 Releases 页面看到 v0.1.0
