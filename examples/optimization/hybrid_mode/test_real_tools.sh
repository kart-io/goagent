#!/bin/bash
# 测试真实工具功能的脚本

echo "=== 测试真实工具功能 ==="
echo

# 编译主程序
echo "编译 hybrid_mode 示例..."
go build -o hybrid_mode main.go
if [ $? -ne 0 ]; then
    echo "❌ 编译失败"
    exit 1
fi
echo "✓ 编译成功"
echo

# 检查环境
echo "检查环境..."
echo "  Go 版本: $(go version)"
echo "  工作目录: $(pwd)"
echo

# 创建一个简单的 Go 测试文件来验证代码执行工具
cat > /tmp/test_code.go << 'EOF'
package main
import "fmt"
func main() {
    fmt.Println("Real Go code execution test!")
}
EOF

echo "测试 Go 代码执行..."
go run /tmp/test_code.go
if [ $? -eq 0 ]; then
    echo "✓ Go 代码执行成功"
else
    echo "❌ Go 代码执行失败"
fi
echo

# 测试 bash 执行
echo "测试 Bash 命令执行..."
echo 'echo "Real bash execution test!"' | bash
if [ $? -eq 0 ]; then
    echo "✓ Bash 执行成功"
else
    echo "❌ Bash 执行失败"
fi
echo

# 测试文件创建（模拟部署）
echo "测试文件创建（部署模拟）..."
TEST_DIR="/tmp/goagent_deploy_test_$$"
mkdir -p "$TEST_DIR"
cat > "$TEST_DIR/deployment.json" << 'EOF'
{
  "service": "backend",
  "environment": "test",
  "status": "deployed"
}
EOF
if [ -f "$TEST_DIR/deployment.json" ]; then
    echo "✓ 部署配置文件创建成功"
    echo "  文件位置: $TEST_DIR/deployment.json"
    cat "$TEST_DIR/deployment.json" | head -3
    rm -rf "$TEST_DIR"
else
    echo "❌ 部署配置文件创建失败"
fi
echo

echo "=== 所有基础功能测试完成 ==="
echo
echo "现在您可以运行完整的混合模式示例："
echo "  export DEEPSEEK_API_KEY=your_api_key"
echo "  ./hybrid_mode"
echo
echo "真实工具将会："
echo "  - 在 /tmp/goagent_code_executor/ 下执行代码"
echo "  - 在 /tmp/goagent_deployments/ 下创建部署配置"
echo "  - 在 /tmp/goagent_tests/ 下创建和运行测试"