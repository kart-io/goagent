#!/bin/bash

# 快速测试脚本（无需 DeepSeek API Key）

echo "========================================"
echo "翻译系统架构验证"
echo "========================================"
echo ""

echo "检查文件完整性..."
echo ""

files=("main.go" "README.md" "run.sh" "interactive/main.go")
all_good=true

for file in "${files[@]}"; do
    if [ -f "$file" ]; then
        echo "✓ $file 存在"
    else
        echo "✗ $file 缺失"
        all_good=false
    fi
done

echo ""

if [ "$all_good" = true ]; then
    echo "✓ 所有文件完整"
else
    echo "✗ 某些文件缺失"
    exit 1
fi

echo ""
echo "编译测试..."
echo ""

# 测试 main.go 编译
if go build -o /tmp/translate_test main.go 2>&1; then
    echo "✓ main.go 编译成功"
    rm -f /tmp/translate_test
else
    echo "✗ main.go 编译失败"
    exit 1
fi

# 测试 interactive/main.go 编译
if go build -o /tmp/interactive_test interactive/main.go 2>&1; then
    echo "✓ interactive/main.go 编译成功"
    rm -f /tmp/interactive_test
else
    echo "✗ interactive/main.go 编译失败"
    exit 1
fi

echo ""
echo "========================================"
echo "✨ 验证完成！"
echo "========================================"
echo ""
echo "系统架构："
echo "  🔍 语言检测 Agent - 识别输入语言"
echo "  🌐 翻译 Agent - 翻译成中文"
echo ""
echo "支持语言："
echo "  ✓ 英语、法语、日语、西班牙语"
echo "  ✓ 德语、俄语、中文、韩语等"
echo ""
echo "使用方法："
echo "  1. 设置环境变量:"
echo "     export DEEPSEEK_API_KEY='your-api-key'"
echo ""
echo "  2. 运行系统:"
echo "     ./run.sh"
echo ""
echo "  3. 交互式使用:"
echo "     cd interactive && go run main.go \"Hello, world!\""
echo ""
