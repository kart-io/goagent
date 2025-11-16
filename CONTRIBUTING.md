# Contributing to GoAgent

Thank you for your interest in contributing to GoAgent! This document provides guidelines and instructions for contributing to the project.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [How to Contribute](#how-to-contribute)
- [Coding Standards](#coding-standards)
- [Testing Guidelines](#testing-guidelines)
- [Documentation](#documentation)
- [Pull Request Process](#pull-request-process)
- [Issue Reporting](#issue-reporting)

## Code of Conduct

We are committed to providing a welcoming and inclusive environment for all contributors. By participating in this project, you agree to:

- Be respectful and considerate in your communication
- Accept constructive criticism gracefully
- Focus on what is best for the community
- Show empathy towards other community members

## Getting Started

### Prerequisites

- Go 1.25.0 or higher
- Git
- golangci-lint (for code quality checks)
- A GitHub account

### Fork and Clone

1. Fork the repository on GitHub
2. Clone your fork locally:
```bash
git clone https://github.com/YOUR_USERNAME/goagent.git
cd goagent
```

3. Add the upstream repository:
```bash
git remote add upstream https://github.com/kart-io/goagent.git
```

## Development Setup

### Install Dependencies

```bash
# Download Go modules
go mod download

# Install development tools
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install golang.org/x/tools/cmd/goimports@latest
```

### Verify Setup

```bash
# Run tests
go test ./...

# Run linter
golangci-lint run

# Verify import layering
./verify_imports.sh
```

## How to Contribute

### Types of Contributions

We welcome various types of contributions:

1. **Bug Fixes** - Fix issues reported in GitHub Issues
2. **New Features** - Implement new functionality
3. **Documentation** - Improve or add documentation
4. **Examples** - Add new usage examples
5. **Tests** - Increase test coverage
6. **Performance** - Optimize existing code

### Before You Start

1. **Check existing issues** - Look for related issues or discussions
2. **Create an issue** - For significant changes, create an issue to discuss first
3. **Claim the issue** - Comment that you're working on it to avoid duplication
4. **Create a branch** - Branch from `main` for your work

### Branch Naming

Use descriptive branch names:

```bash
# Feature branches
git checkout -b feature/add-anthropic-provider

# Bug fix branches
git checkout -b fix/memory-leak-in-cache

# Documentation branches
git checkout -b docs/update-quickstart-guide
```

## Coding Standards

### Go Style Guide

Follow the [official Go style guide](https://go.dev/doc/effective_go):

- Use `gofmt` for formatting (already done by `goimports`)
- Follow Go naming conventions (camelCase for private, PascalCase for public)
- Keep functions small and focused
- Use meaningful variable and function names
- Add comments for all exported functions, types, and constants

### Import Layering

GoAgent follows a strict 4-layer import architecture:

```
Layer 1: Foundation     - interfaces/, errors/, cache/, utils/
Layer 2: Business Logic - core/, llm/, memory/, store/
Layer 3: Implementation - agents/, tools/, middleware/, parsers/
Layer 4: Examples       - examples/, *_test.go
```

**Rules:**
- Layer 1 MUST NOT import from other GoAgent packages
- Layer 2 imports only from Layer 1
- Layer 3 imports from Layer 1 and 2
- Layer 4 can import from all layers

**Verify compliance:**
```bash
./verify_imports.sh
```

See [Import Layering Documentation](docs/architecture/IMPORT_LAYERING.md) for details.

### Code Organization

```go
// Package structure example
package mypackage

import (
    // Standard library first
    "context"
    "fmt"

    // External dependencies next
    "github.com/external/package"

    // Internal packages last
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/interfaces"
)

// Constants
const (
    DefaultTimeout = 30 * time.Second
)

// Types
type MyStruct struct {
    field1 string
    field2 int
}

// Functions
func NewMyStruct() *MyStruct {
    return &MyStruct{}
}

func (m *MyStruct) PublicMethod() error {
    return m.privateHelper()
}

func (m *MyStruct) privateHelper() error {
    // Implementation
    return nil
}
```

## Testing Guidelines

### Test Coverage Requirements

- **Minimum coverage**: 80% for all packages
- **New code**: Must have tests before PR is merged
- **Critical paths**: Should have >90% coverage

### Writing Tests

```go
package mypackage_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/kart-io/goagent/mypackage"
)

func TestMyFunction(t *testing.T) {
    // Arrange
    ctx := context.Background()
    input := "test"

    // Act
    result, err := mypackage.MyFunction(ctx, input)

    // Assert
    require.NoError(t, err)
    assert.Equal(t, "expected", result)
}

func TestMyFunction_ErrorCase(t *testing.T) {
    ctx := context.Background()

    result, err := mypackage.MyFunction(ctx, "")

    assert.Error(t, err)
    assert.Nil(t, result)
}
```

### Table-Driven Tests

For multiple test cases:

```go
func TestMyFunction_Cases(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"valid input", "test", "expected", false},
        {"empty input", "", "", true},
        {"special chars", "test!@#", "expected!@#", false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := mypackage.MyFunction(context.Background(), tt.input)

            if tt.wantErr {
                assert.Error(t, err)
                return
            }

            require.NoError(t, err)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

### Running Tests

```bash
# All tests
go test ./...

# Specific package
go test ./core

# With coverage
go test -cover ./...

# With coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

See [Testing Best Practices](docs/development/TESTING_BEST_PRACTICES.md) for more details.

## Documentation

### Code Documentation

All exported types, functions, and constants must have documentation comments:

```go
// Agent represents an autonomous entity that can reason and execute tasks.
// It coordinates between tools, memory, and LLM to accomplish goals.
type Agent interface {
    // Execute runs the agent with the given input and returns the result.
    // The context can be used to cancel the operation.
    Execute(ctx context.Context, input *AgentInput) (*AgentOutput, error)

    // Name returns the unique identifier for this agent.
    Name() string
}
```

### User Documentation

When adding features:

1. Update relevant markdown files in `docs/`
2. Add examples to `examples/`
3. Update [DOCUMENTATION_INDEX.md](DOCUMENTATION_INDEX.md)
4. Update [CHANGELOG.md](CHANGELOG.md)

### Example Code

Provide working examples for new features:

```go
// Example: Using the new feature
package main

import (
    "context"
    "log"

    "github.com/kart-io/goagent/builder"
)

func main() {
    agent, err := builder.NewAgentBuilder(llmClient).
        WithNewFeature(config).
        Build()

    if err != nil {
        log.Fatal(err)
    }

    result, err := agent.Execute(context.Background(), "task")
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("Result: %v", result)
}
```

## Pull Request Process

### Before Submitting

1. **Sync with upstream**:
```bash
git fetch upstream
git rebase upstream/main
```

2. **Run all checks**:
```bash
# Format code
goimports -w .

# Run linter
golangci-lint run

# Run tests
go test ./...

# Verify imports
./verify_imports.sh

# Check test coverage
go test -cover ./...
```

3. **Commit with clear messages**:
```bash
git commit -m "feat: add Anthropic Claude provider

- Implement ClaudeClient with streaming support
- Add configuration options for Claude models
- Include integration tests
- Update LLM_PROVIDERS.md documentation

Closes #123"
```

### Commit Message Format

Follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation changes
- `test:` - Adding or updating tests
- `refactor:` - Code refactoring
- `perf:` - Performance improvements
- `chore:` - Build process or auxiliary tool changes

### Creating the Pull Request

1. Push your branch:
```bash
git push origin feature/your-feature
```

2. Create PR on GitHub with:
   - Clear title describing the change
   - Description explaining what and why
   - Link to related issues
   - Screenshots/examples if applicable

3. Fill out the PR template:
```markdown
## Description
Brief description of changes

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Documentation update
- [ ] Performance improvement

## Testing
- [ ] Tests pass locally
- [ ] Added new tests
- [ ] Updated documentation
- [ ] Verified import layering

## Related Issues
Closes #123
```

### Review Process

1. Automated checks run (tests, linting, coverage)
2. Maintainers review code
3. Address feedback with new commits
4. Once approved, PR will be merged

## Issue Reporting

### Bug Reports

Include:

1. **Description** - Clear description of the bug
2. **Steps to Reproduce** - Minimal steps to reproduce
3. **Expected Behavior** - What should happen
4. **Actual Behavior** - What actually happens
5. **Environment** - Go version, OS, etc.
6. **Code Sample** - Minimal reproducible example

Example:
```markdown
**Description**
Memory leak when using Redis checkpointer

**Steps to Reproduce**
1. Create Redis checkpointer
2. Execute agent 1000 times
3. Monitor memory usage

**Expected**
Memory usage should be stable

**Actual**
Memory increases continuously

**Environment**
- Go 1.25.0
- GoAgent main branch
- Redis 7.0
- Ubuntu 22.04

**Code Sample**
<!-- paste minimal reproduction code -->
```

### Feature Requests

Include:

1. **Use Case** - What problem does it solve?
2. **Proposed Solution** - How might it work?
3. **Alternatives** - Other approaches considered
4. **Additional Context** - Examples, references

## Release Process

Maintainers follow this process for releases:

1. Update [CHANGELOG.md](CHANGELOG.md)
2. Update version numbers
3. Create release tag
4. Publish release notes
5. Update documentation

## Questions?

- **Documentation**: Check [DOCUMENTATION_INDEX.md](DOCUMENTATION_INDEX.md)
- **Discussions**: Use [GitHub Discussions](https://github.com/kart-io/goagent/discussions)
- **Issues**: Create an issue for bugs or features

## Recognition

Contributors are recognized in:
- GitHub contributor list
- Release notes for significant contributions
- Special acknowledgments for major features

Thank you for contributing to GoAgent!

---

**Last Updated:** 2025-11-15
**Maintained By:** GoAgent Team
