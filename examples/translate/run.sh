#!/bin/bash

# 智能翻译系统运行脚本

echo "========================================"
echo "智能翻译系统 (Multi-Agent)"
echo "========================================"
echo ""

# 检查 DeepSeek API Key
if [ -z "$DEEPSEEK_API_KEY" ]; then
    echo "❌ 错误: 未设置 DEEPSEEK_API_KEY 环境变量"
    echo ""
    echo "请先设置 DeepSeek API Key:"
    echo "  export DEEPSEEK_API_KEY='your-api-key'"
    echo ""
    echo "获取 API Key:"
    echo "  1. 访问 https://platform.deepseek.com/"
    echo "  2. 注册账号并获取 API Key"
    echo ""
    exit 1
fi

echo "✓ DeepSeek API Key 已配置"
echo ""

# 运行翻译系统
echo "启动翻译系统..."
echo ""

go run main.go

echo ""
echo "========================================"
echo "翻译系统运行完成"
echo "========================================"
