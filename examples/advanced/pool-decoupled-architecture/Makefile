.PHONY: all run build clean help

# 默认目标
all: help

# 运行示例
run:
	@echo "Running pool decoupled architecture example..."
	@go run main.go

# 编译示例
build:
	@echo "Building pool-decoupled-architecture example..."
	@go build -o bin/pool-decoupled-architecture main.go
	@echo "Binary created at: bin/pool-decoupled-architecture"

# 运行已编译的二进制文件
run-binary: build
	@echo "Running compiled binary..."
	@./bin/pool-decoupled-architecture

# 清理编译产物
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf bin/
	@echo "Clean complete."

# 显示帮助信息
help:
	@echo "GoAgent Pool Decoupled Architecture Example"
	@echo ""
	@echo "Available targets:"
	@echo "  make run         - Run example directly"
	@echo "  make build       - Build the example binary"
	@echo "  make run-binary  - Build and run the binary"
	@echo "  make clean       - Clean build artifacts"
	@echo "  make help        - Show this help message"
	@echo ""
	@echo "Architecture Features:"
	@echo "  • Interface abstraction (PoolManager)"
	@echo "  • Dependency injection"
	@echo "  • Strategy pattern (Adaptive/Scenario/Metrics/Priority)"
	@echo "  • Agent pattern integration"
	@echo "  • Isolated testing support"
