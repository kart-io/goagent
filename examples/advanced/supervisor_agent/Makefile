# Makefile for SupervisorAgent Examples

.PHONY: help run-basic run-travel run-review run-all build clean check-env

# 默认目标
.DEFAULT_GOAL := help

# 帮助信息
help:
	@echo "SupervisorAgent 示例运行命令："
	@echo ""
	@echo "  make run-basic       - 运行基础示例（多 Agent 协作）"
	@echo "  make run-travel      - 运行旅行规划示例（层次聚合）"
	@echo "  make run-review      - 运行代码审查示例（协商聚合）"
	@echo "  make run-all         - 运行所有示例"
	@echo ""
	@echo "  make build           - 编译示例程序"
	@echo "  make clean           - 清理编译文件"
	@echo "  make check-env       - 检查环境变量"
	@echo ""
	@echo "环境变量："
	@echo "  DEEPSEEK_API_KEY     - DeepSeek API Key（推荐）"
	@echo "  OPENAI_API_KEY       - OpenAI API Key（可选）"
	@echo ""
	@echo "示例："
	@echo "  export DEEPSEEK_API_KEY=\"your-api-key\""
	@echo "  make run-basic"

# 检查环境变量
check-env:
	@if [ -z "$$DEEPSEEK_API_KEY" ] && [ -z "$$OPENAI_API_KEY" ]; then \
		echo "❌ 错误: 未设置 API Key"; \
		echo ""; \
		echo "请先设置环境变量："; \
		echo "  export DEEPSEEK_API_KEY=\"your-api-key\""; \
		echo "或"; \
		echo "  export OPENAI_API_KEY=\"your-api-key\""; \
		exit 1; \
	fi
	@echo "✅ 环境检查通过"

# 运行基础示例
run-basic: check-env
	@echo "🚀 运行基础示例..."
	go run main.go -scenario=basic

# 运行旅行规划示例
run-travel: check-env
	@echo "🚀 运行旅行规划示例..."
	go run main.go -scenario=travel

# 运行代码审查示例
run-review: check-env
	@echo "🚀 运行代码审查示例..."
	go run main.go -scenario=review

# 运行所有示例
run-all: check-env
	@echo "🚀 运行所有示例..."
	go run main.go -scenario=all

# 使用 OpenAI 运行基础示例
run-openai: check-env
	@echo "🚀 使用 OpenAI 运行基础示例..."
	go run main.go -scenario=basic -provider=openai

# 编译
build:
	@echo "🔨 编译 SupervisorAgent 示例..."
	go build -o supervisor_example main.go
	@echo "✅ 编译完成: ./supervisor_example"
	@echo ""
	@echo "运行方式："
	@echo "  ./supervisor_example -scenario=basic"
	@echo "  ./supervisor_example -scenario=travel"
	@echo "  ./supervisor_example -scenario=review"
	@echo "  ./supervisor_example -scenario=all"

# 清理
clean:
	@echo "🧹 清理编译文件..."
	rm -f supervisor_example
	@echo "✅ 清理完成"

# 显示可用的命令参数
params:
	@echo "可用的命令行参数："
	@echo ""
	@echo "  -scenario <场景>"
	@echo "    basic   - 基础示例（默认）"
	@echo "    travel  - 旅行规划"
	@echo "    review  - 代码审查"
	@echo "    all     - 所有示例"
	@echo ""
	@echo "  -provider <提供商>"
	@echo "    deepseek - DeepSeek（默认）"
	@echo "    openai   - OpenAI"
	@echo ""
	@echo "示例："
	@echo "  go run main.go -scenario=travel -provider=openai"
