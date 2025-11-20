# Go parameters
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

# Binary name
BINARY_NAME=goagent
BINARY_UNIX=$(BINARY_NAME)_unix
BINARY_WINDOWS=$(BINARY_NAME).exe

# Build variables
VERSION?=dev
COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildTime=$(BUILD_TIME)"

# Directories
CMD_DIR=./cmd
PKG_DIR=./pkg
INTERNAL_DIR=./internal
DIST_DIR=./dist
COVERAGE_DIR=./coverage

# Colors for output
RED=\033[0;31m
GREEN=\033[0;32m
YELLOW=\033[1;33m
NC=\033[0m # No Color

.PHONY: all build clean test coverage deps help

## help: Display this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Available targets:"
	@grep -E '^##' $(MAKEFILE_LIST) | sed -e 's/## /  /' | column -t -s ':'

## all: Build and test
all: test build

## build: Build the binary for current OS
build:
	@echo "$(GREEN)Building $(BINARY_NAME)...$(NC)"
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) -v ./...

## build-all: Build for multiple platforms
build-all: build-linux build-windows build-darwin

## build-linux: Build for Linux
build-linux:
	@echo "$(GREEN)Building for Linux...$(NC)"
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 -v ./...
	GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 -v ./...

## build-windows: Build for Windows
build-windows:
	@echo "$(GREEN)Building for Windows...$(NC)"
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_WINDOWS) -v ./...

## build-darwin: Build for macOS
build-darwin:
	@echo "$(GREEN·)Building for macOS...$(NC)"
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 -v ./...
	GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 -v ./...

## run: Run the application
run: build
	@echo "$(GREEN)Running $(BINARY_NAME)...$(NC)"
	./$(BINARY_NAME)

## test: Run all tests
test:
	@echo "$(YELLOW)Running tests...$(NC)"
	GO_TEST_MODE=true $(GOTEST) -v -race -timeout 30s ./...

## test-short: Run short tests
test-short:
	@echo "$(YELLOW)Running short tests...$(NC)"
	GO_TEST_MODE=true $(GOTEST) -v -short ./...

## test-integration: Run integration tests
test-integration:
	@echo "$(YELLOW)Running integration tests...$(NC)"
	$(GOTEST) -v -tags=integration ./...

## coverage: Generate test coverage report
coverage:
	@echo "$(YELLOW)Generating coverage report...$(NC)"
	@mkdir -p $(COVERAGE_DIR)
	GO_TEST_MODE=true $(GOTEST) -v -race -coverprofile=$(COVERAGE_DIR)/coverage.out -covermode=atomic ./...
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
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v2.6.2; \
	fi
	@echo "$(GREEN)Using golangci-lint from: $(GOLINT)$(NC)"
	$(GOLINT) run ./...

## lint-fix: Run linter with auto-fix
lint-fix:
	@echo "$(YELLOW)Running linter with auto-fix...$(NC)"
	@if [ ! -f "$(GOLINT)" ] || [ "$$($(GOLINT) version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 | cut -d. -f1)" != "2" ]; then \
		echo "$(RED)golangci-lint v2.x not found. Installing...$(NC)"; \
		rm -f $(GOLINT); \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v2.6.2; \
	fi
	$(GOLINT) run --fix ./...

## lint-basic: Run basic linting (skip compile errors)
lint-basic:
	@echo "$(YELLOW)Running basic linter checks...$(NC)"
	@if [ ! -f "$(GOLINT)" ] || [ "$$($(GOLINT) version 2>&1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1 | cut -d. -f1)" != "2" ]; then \
		echo "$(RED)golangci-lint v2.x not found. Installing...$(NC)"; \
		rm -f $(GOLINT); \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v2.6.2; \
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
	$(GOFMT) -s -w .
	$(GOCMD) fmt ./...

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
	rm -f $(BINARY_NAME)
	rm -f $(BINARY_UNIX)
	rm -f $(BINARY_WINDOWS)
	rm -rf $(DIST_DIR)
	rm -rf $(COVERAGE_DIR)

## install: Install the binary
install: build
	@echo "$(GREEN)Installing $(BINARY_NAME)...$(NC)"
	$(GOCMD) install $(LDFLAGS) ./...

## uninstall: Uninstall the binary
uninstall:
	@echo "$(RED)Uninstalling $(BINARY_NAME)...$(NC)"
	rm -f $(GOPATH)/bin/$(BINARY_NAME)

## docker-build: Build Docker image
docker-build:
	@echo "$(GREEN)Building Docker image...$(NC)"
	docker build -t $(BINARY_NAME):$(VERSION) .

## docker-run: Run Docker container
docker-run:
	@echo "$(GREEN)Running Docker container...$(NC)"
	docker run -it --rm $(BINARY_NAME):$(VERSION)

## docker-push: Push Docker image to registry
docker-push:
	@echo "$(GREEN)Pushing Docker image...$(NC)"
	docker push $(BINARY_NAME):$(VERSION)

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

## init: Initialize project structure
init:
	@echo "$(GREEN)Initializing project structure...$(NC)"
	@mkdir -p cmd/$(BINARY_NAME)
	@mkdir -p $(PKG_DIR)
	@mkdir -p $(INTERNAL_DIR)
	@mkdir -p test
	@echo "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, goagent!\")\n}" > cmd/$(BINARY_NAME)/main.go
	@echo "$(GREEN)Project structure created!$(NC)"

## version: Display version information
version:
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT)"
	@echo "Build Time: $(BUILD_TIME)"

.DEFAULT_GOAL := help