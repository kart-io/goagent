# Makefile for Multi-Agent Patterns Examples

.PHONY: help run run-aggregator run-router run-loop clean build

# 默认目标
.DEFAULT_GOAL := help

# LLM 提供者（可以通过环境变量覆盖）
PROVIDER ?= deepseek

help: ## 显示帮助信息
	@echo "多智能体协作模式示例"
	@echo ""
	@echo "可用命令:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "环境变量:"
	@echo "  PROVIDER - LLM提供者 (deepseek 或 openai，默认: deepseek)"
	@echo ""
	@echo "示例:"
	@echo "  make run                    # 运行所有模式"
	@echo "  make run-aggregator         # 运行聚合模式"
	@echo "  PROVIDER=openai make run    # 使用OpenAI运行所有模式"

run: ## 运行所有模式
	@echo "运行所有多智能体协作模式示例..."
	@go run . -pattern=all -provider=$(PROVIDER)

run-aggregator: ## 运行聚合模式（社交媒体情感分析）
	@echo "运行聚合模式..."
	@go run . -pattern=aggregator -provider=$(PROVIDER)

run-router: ## 运行路由模式（智能客服工单分配）
	@echo "运行路由模式..."
	@go run . -pattern=router -provider=$(PROVIDER)

run-loop: ## 运行循环模式（代码迭代优化）
	@echo "运行循环模式..."
	@go run . -pattern=loop -provider=$(PROVIDER)

build: ## 构建可执行文件
	@echo "构建示例程序..."
	@go build -o multiagent-patterns .
	@echo "✅ 构建完成: ./multiagent-patterns"

clean: ## 清理构建产物
	@echo "清理构建产物..."
	@rm -f multiagent-patterns
	@echo "✅ 清理完成"

deps: ## 下载依赖
	@echo "下载依赖..."
	@go mod download
	@echo "✅ 依赖下载完成"

test: ## 运行测试（当前无测试文件）
	@echo "运行测试..."
	@go test -v ./...

fmt: ## 格式化代码
	@echo "格式化代码..."
	@go fmt ./...
	@echo "✅ 格式化完成"

lint: ## 运行代码检查
	@echo "运行代码检查..."
	@golangci-lint run ./...

check-env: ## 检查环境变量配置
	@echo "检查LLM API密钥配置..."
	@if [ "$(PROVIDER)" = "deepseek" ]; then \
		if [ -z "$$DEEPSEEK_API_KEY" ]; then \
			echo "❌ DEEPSEEK_API_KEY 未设置"; \
			echo "请运行: export DEEPSEEK_API_KEY=\"your-api-key\""; \
			exit 1; \
		else \
			echo "✅ DEEPSEEK_API_KEY 已设置"; \
		fi \
	elif [ "$(PROVIDER)" = "openai" ]; then \
		if [ -z "$$OPENAI_API_KEY" ]; then \
			echo "❌ OPENAI_API_KEY 未设置"; \
			echo "请运行: export OPENAI_API_KEY=\"your-api-key\""; \
			exit 1; \
		else \
			echo "✅ OPENAI_API_KEY 已设置"; \
		fi \
	fi

info: ## 显示项目信息
	@echo "================================================"
	@echo "  多智能体协作模式示例"
	@echo "================================================"
	@echo ""
	@echo "包含的模式:"
	@echo "  1. Aggregator - 聚合模式（社交媒体情感分析）"
	@echo "  2. Router     - 路由模式（客服工单分配）"
	@echo "  3. Loop       - 循环模式（代码迭代优化）"
	@echo ""
	@echo "当前配置:"
	@echo "  LLM提供者: $(PROVIDER)"
	@echo ""
	@echo "更多信息请查看 README.md"
	@echo "================================================"
