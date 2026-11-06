# Go parameters
# 项目命令使用 go.mod 声明的开发工具链，避免启动器与陈旧 GOROOT 混用。
unexport GOROOT
export GOTOOLCHAIN ?= $(shell sed -n 's/^toolchain //p' go.mod)
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOPATH=$(shell go env GOPATH)
GOLINT=$(GOPATH)/bin/golangci-lint
GOVET=$(GOCMD) vet

# module name
GO_MOD_NAME := github.com/kart-io/goagent

# Library checks compile packages; run selects one example main package.
EXAMPLE ?= ./examples/execution-scope
COVERAGE_DIR=./coverage
COMPAT_ROOT ?= ..
COMPAT_CORE ?= $(CURDIR)
COMPAT_OUT ?= $(COVERAGE_DIR)/compatibility-$(shell date -u +%Y%m%dT%H%M%SZ)

# Colors for output
RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[1;33m
NC=\033[0m # No Color

.PHONY: all build build-all build-linux build-windows build-darwin run clean test test-frameworkbench test-contracts compatibility coverage deps help version

## help: Display this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@sed -n 's/^## /  /p' $(MAKEFILE_LIST)

## all: Build and test
all: test build

## build: Compile all library and example packages
build:
	$(GOBUILD) ./...

## build-all: Check compilation for Linux, Windows and macOS
build-all: build-linux build-windows build-darwin

## build-linux: Check compilation for Linux (amd64, arm64)
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GOBUILD) ./...
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GOBUILD) ./...

## build-windows: Check compilation for Windows (amd64)
build-windows:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GOBUILD) ./...

## build-darwin: Check compilation for macOS (amd64, arm64)
build-darwin:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 $(GOBUILD) ./...
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GOBUILD) ./...

## run: Run the offline host example (override with EXAMPLE=./examples/...)
run:
	$(GOCMD) run "$(EXAMPLE)"

## test: Run all tests
test:
	@echo "$(YELLOW)Running tests...$(NC)"
	GO_TEST_MODE=true $(GOTEST) -v -race -timeout 120s ./...

## test-short: Run short tests
test-short:
	@echo "$(YELLOW)Running short tests...$(NC)"
	GO_TEST_MODE=true $(GOTEST) -v -short ./...

## test-frameworkbench: 验证独立评测模块的依赖与执行契约（不运行计时基准）
test-frameworkbench:
	cd evaluation/frameworkbench && GOWORK=off GO_TEST_MODE=true $(GOTEST) -mod=readonly -race -count=1 -timeout 120s ./...

## test-contracts: 验证执行、模型流、恢复、审批、预算与工具公共契约
test-contracts:
	GO_TEST_MODE=true $(GOTEST) -race -count=1 -timeout 120s ./core/... ./llm/... ./builder/... ./workflow/... ./multiagent/... ./tools/... ./execution/...

## compatibility: 使用当前源码验证本地关联仓库（COMPAT_ROOT、COMPAT_CORE、COMPAT_OUT 可覆盖）
compatibility:
	$(GOCMD) run ./scripts/compatcheck -root "$(COMPAT_ROOT)" -core-dir "$(COMPAT_CORE)" -out "$(COMPAT_OUT)"

## test-integration: Run integration tests
test-integration:
	@echo "$(YELLOW)Running integration tests...$(NC)"
	$(GOTEST) -v -tags=integration ./...

## coverage: Generate test coverage report
coverage:
	@echo "$(YELLOW)Generating coverage report...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	GO_TEST_MODE=true $(GOTEST) -v -race -coverprofile=$(COVERAGE_DIR)/coverage.out -covermode=atomic ./...
	bash scripts/check_coverage.sh $(COVERAGE_DIR)/coverage.out
	$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "$(GREEN)Coverage report generated at $(COVERAGE_DIR)/coverage.html$(NC)"

## coverage-view: View coverage in browser
coverage-view: coverage
	@echo "$(GREEN)Opening coverage report...$(NC)"
	open $(COVERAGE_DIR)/coverage.html

## bench: Run benchmarks
bench:
	@echo "$(YELLOW)Running benchmarks...$(NC)"
	$(GOTEST) -bench=. -benchmem ./...

## lint: Run linter
lint:
	@echo "$(YELLOW)Running linter...$(NC)"
	@if [ ! -f "$(GOLINT)" ] || [ "$$($(GOLINT) version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 | cut -d. -f1)" != "2" ]; then \
		echo "$(RED)golangci-lint v2.x not found. Installing...$(NC)"; \
		rm -f $(GOLINT); \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v2.15.0; \
	fi
	@echo "$(GREEN)Using golangci-lint from: $(GOLINT)$(NC)"
	$(GOLINT) run ./...

## lint-fix: Run linter with auto-fix
lint-fix:
	@echo "$(YELLOW)Running linter with auto-fix...$(NC)"
	@if [ ! -f "$(GOLINT)" ] || [ "$$($(GOLINT) version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 | cut -d. -f1)" != "2" ]; then \
		echo "$(RED)golangci-lint v2.x not found. Installing...$(NC)"; \
		rm -f $(GOLINT); \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v2.15.0; \
	fi
	$(GOLINT) run --fix ./...

## lint-basic: Run basic linting (skip compile errors)
lint-basic:
	@echo "$(YELLOW)Running basic linter checks...$(NC)"
	@if [ ! -f "$(GOLINT)" ] || [ "$$($(GOLINT) version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 | cut -d. -f1)" != "2" ]; then \
		echo "$(RED)golangci-lint v2.x not found. Installing...$(NC)"; \
		rm -f $(GOLINT); \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v2.15.0; \
	fi
	@echo "$(GREEN)Checking code formatting and basic issues...$(NC)"
	-$(GOLINT) run --disable-all --enable=gofmt --enable=goimports --enable=misspell --enable=whitespace --enable=unconvert --enable=predeclared --enable=nakedret --enable=prealloc --enable=nilerr ./... 2>/dev/null || echo "$(YELLOW)Some linting issues found (non-critical)$(NC)"

## lint-clean: Clean and reinstall golangci-lint
lint-clean:
	@echo "$(YELLOW)Cleaning golangci-lint...$(NC)"
	rm -f $(GOLINT)
	@echo "$(GREEN)golangci-lint removed. Run 'make lint' to reinstall.$(NC)"

## lint-version: Show golangci-lint version
lint-version:
	@if [ -f "$(GOLINT)" ]; then \
		$(GOLINT) version; \
	else \
		echo "$(RED)golangci-lint not installed$(NC)"; \
	fi

## fmt: Format code
fmt:
	@echo "$(YELLOW)Formatting code...$(NC)"
	@gofumpt -version || go install mvdan.cc/gofumpt@latest
	gofumpt -w .
	@gci -v || go install github.com/daixiang0/gci@latest
	gci write -s standard -s default -s 'Prefix($(GO_MOD_NAME))' --skip-generated .

## vet: Run go vet
vet:
	@echo "$(YELLOW)Running go vet...$(NC)"
	$(GOVET) ./...

## check: Run fmt, vet, and lint
check: fmt vet lint-basic

## deps: Download dependencies
deps:
	@echo "$(GREEN)Downloading dependencies...$(NC)"
	$(GOGET) -v -d ./...

## deps-update: Update dependencies
deps-update:
	@echo "$(GREEN)Updating dependencies...$(NC)"
	$(GOGET) -u ./...

## mod-tidy: Tidy and verify module dependencies
mod-tidy:
	@echo "$(GREEN)Tidying module dependencies...$(NC)"
	$(GOMOD) tidy
	$(GOMOD) verify

## mod-download: Download module dependencies
mod-download:
	@echo "$(GREEN)Downloading module dependencies...$(NC)"
	$(GOMOD) download

## clean: Clean build files
clean:
	@echo "$(RED)Cleaning...$(NC)"
	$(GOCLEAN)
	rm -rf $(COVERAGE_DIR)

## proto: Generate protobuf files
proto:
	@echo "$(YELLOW)Generating protobuf files...$(NC)"
	@if [ -d "proto" ]; then \
		protoc --go_out=. --go_opt=paths=source_relative \
			--go-grpc_out=. --go-grpc_opt=paths=source_relative \
			proto/*.proto; \
	else \
		echo "$(YELLOW)No proto directory found$(NC)"; \
	fi

## mock: Generate mocks
mock:
	@echo "$(YELLOW)Generating mocks...$(NC)"
	@if command -v mockgen &> /dev/null; then \
		go generate ./...; \
	else \
		echo "$(RED)mockgen not found. Install with: go install github.com/golang/mock/mockgen@latest$(NC)"; \
	fi

## security: Run security checks
security:
	@echo "$(YELLOW)Running security checks...$(NC)"
	@if ! command -v gosec &> /dev/null; then \
		echo "$(RED)gosec not found. Installing...$(NC)"; \
		go install github.com/securego/gosec/v2/cmd/gosec@latest; \
	fi
	gosec ./...

## version: Display the library revision
version:
	@git describe --tags --always --dirty

.DEFAULT_GOAL := help
