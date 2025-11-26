#!/bin/bash
# Release 监控脚本
# 用于检查 GitHub Release 状态

echo "🔍 检查 GoAgent v0.1.0 Release 状态..."
echo "=========================================="
echo ""

# 检查 tag 是否存在
echo "1️⃣  检查 Tag 状态..."
if git ls-remote --tags origin | grep -q "v0.1.0"; then
    echo "   ✅ Tag v0.1.0 已推送到远程"
else
    echo "   ❌ Tag v0.1.0 未找到"
    exit 1
fi
echo ""

# 检查 tag 详情
echo "2️⃣  Tag 详细信息..."
git show v0.1.0 --no-patch --format="   标签: %d%n   提交: %H%n   作者: %an%n   日期: %ci" 2>/dev/null || echo "   ⚠️  无法获取 tag 详情"
echo ""

# 提供监控链接
echo "3️⃣  请在浏览器中检查以下页面:"
echo ""
echo "   📊 GitHub Actions (查看 workflow 运行状态):"
echo "      https://github.com/kart-io/goagent/actions"
echo ""
echo "   📦 GitHub Releases (完成后查看):"
echo "      https://github.com/kart-io/goagent/releases"
echo ""
echo "   🏷️  GitHub Tags:"
echo "      https://github.com/kart-io/goagent/tags"
echo ""

# 预期时间线
echo "4️⃣  预期时间线:"
echo ""
echo "   ⏱️  现在         - Workflow 已触发"
echo "   ⏱️  +2-3 分钟   - 测试完成"
echo "   ⏱️  +5-8 分钟   - 构建完成 (5个平台)"
echo "   ⏱️  +8-10 分钟  - Release 创建完成"
echo "   ⏱️  +15-20 分钟 - pkg.go.dev 索引完成"
echo ""

# Release 内容预览
echo "5️⃣  Release 应包含的文件:"
echo ""
echo "   📦 goagent-v0.1.0-linux-amd64.tar.gz"
echo "   📦 goagent-v0.1.0-linux-arm64.tar.gz"
echo "   📦 goagent-v0.1.0-darwin-amd64.tar.gz"
echo "   📦 goagent-v0.1.0-darwin-arm64.tar.gz"
echo "   📦 goagent-v0.1.0-windows-amd64.zip"
echo "   🔐 checksums.txt"
echo ""

# Workflow 步骤
echo "6️⃣  Workflow 执行步骤 (在 Actions 页面可以看到):"
echo ""
echo "   1. Checkout code"
echo "   2. Set up Go"
echo "   3. Run tests                  ← 如果这里失败，检查测试"
echo "   4. Verify import layering     ← 如果这里失败，运行 ./verify_imports.sh"
echo "   5. Build binaries             ← 如果这里失败，检查构建配置"
echo "   6. Generate checksums"
echo "   7. Extract release notes"
echo "   8. Create GitHub Release      ← 如果这里失败，检查权限"
echo "   9. Publish to pkg.go.dev"
echo ""

# 故障排查提示
echo "7️⃣  如果遇到问题:"
echo ""
echo "   ❌ Workflow 失败 → 查看 Actions 页面的错误日志"
echo "   ❌ 测试失败     → 运行 'make test' 本地检查"
echo "   ❌ 权限错误     → 检查仓库 Settings → Actions → General"
echo "   ❌ 构建失败     → 检查 Go 版本和依赖"
echo ""
echo "   📖 详细排查指南: RELEASE_VERIFICATION.md"
echo ""

echo "=========================================="
echo "✅ 监控脚本执行完成"
echo ""
echo "💡 提示: 在 5-10 分钟后访问 Releases 页面查看结果"
echo "        https://github.com/kart-io/goagent/releases"
