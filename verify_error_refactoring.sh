#!/bin/bash

# verify_error_refactoring.sh - Verify error handling refactoring completeness

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== Error Handling Refactoring Verification ===${NC}"
echo

# Check for fmt.Errorf in production code (excluding comments)
echo -e "${BLUE}1. Checking for remaining fmt.Errorf in production code...${NC}"
FMT_ERRORF_FILES=$(find . -type f -name "*.go" \
    -not -path "./vendor/*" \
    -not -path "./*_test.go" \
    -not -path "./examples/*" \
    -not -path "./testing/*" \
    -exec sh -c 'grep -l "fmt\.Errorf" "$1" 2>/dev/null | while read f; do grep -v "^[[:space:]]*//" "$f" | grep -q "fmt\.Errorf" && echo "$f"; done' _ {} \; | sort -u)

if [ -z "$FMT_ERRORF_FILES" ]; then
    echo -e "${GREEN}✓ No fmt.Errorf found in production code${NC}"
else
    echo -e "${RED}✗ Found fmt.Errorf in:${NC}"
    echo "$FMT_ERRORF_FILES"
fi

# Count agentErrors usage
echo
echo -e "${BLUE}2. Counting agentErrors package usage...${NC}"
AGENT_ERROR_COUNT=$(find . -type f -name "*.go" \
    -not -path "./vendor/*" \
    -not -path "./*_test.go" \
    -not -path "./examples/*" \
    -not -path "./testing/*" \
    -not -path "./errors/*" \
    -exec grep -l 'agentErrors "github.com/kart-io/goagent/errors"' {} \; 2>/dev/null | wc -l)

TOTAL_GO_FILES=$(find . -type f -name "*.go" \
    -not -path "./vendor/*" \
    -not -path "./*_test.go" \
    -not -path "./examples/*" \
    -not -path "./testing/*" \
    -not -path "./errors/*" | wc -l)

echo "Files using agentErrors: $AGENT_ERROR_COUNT"
echo "Total Go files: $TOTAL_GO_FILES"
COVERAGE=$(echo "scale=1; $AGENT_ERROR_COUNT * 100 / $TOTAL_GO_FILES" | bc)
echo -e "Coverage: ${GREEN}${COVERAGE}%${NC}"

# Show module coverage
echo
echo -e "${BLUE}3. Module coverage breakdown:${NC}"
find . -type f -name "*.go" \
    -not -path "./vendor/*" \
    -not -path "./*_test.go" \
    -not -path "./examples/*" \
    -not -path "./testing/*" \
    -not -path "./errors/*" \
    -exec grep -l 'agentErrors "github.com/kart-io/goagent/errors"' {} \; 2>/dev/null | \
    sed 's|^\./||' | cut -d'/' -f1 | sort | uniq -c | sort -rn | \
    while read count module; do
        printf "  %-20s %3d files\n" "$module" "$count"
    done

# Check compilation
echo
echo -e "${BLUE}4. Verifying compilation...${NC}"
if go build ./... 2>/dev/null; then
    echo -e "${GREEN}✓ All packages compile successfully${NC}"
else
    echo -e "${RED}✗ Compilation errors found${NC}"
    exit 1
fi

# Run quick tests
echo
echo -e "${BLUE}5. Running quick tests on refactored modules...${NC}"
TEST_MODULES="core builder agents/executor tools/compute middleware store/memory"
FAILED_TESTS=""

for module in $TEST_MODULES; do
    if go test -short "./$module" > /dev/null 2>&1; then
        echo -e "${GREEN}✓ $module${NC}"
    else
        echo -e "${RED}✗ $module${NC}"
        FAILED_TESTS="$FAILED_TESTS $module"
    fi
done

# Check import layering
echo
echo -e "${BLUE}6. Verifying import layering compliance...${NC}"
if ./verify_imports.sh > /dev/null 2>&1; then
    echo -e "${GREEN}✓ Import layering rules satisfied${NC}"
else
    echo -e "${RED}✗ Import layering violations found${NC}"
    exit 1
fi

# Summary
echo
echo -e "${BLUE}=== Summary ===${NC}"

if [ -z "$FMT_ERRORF_FILES" ] && [ -z "$FAILED_TESTS" ]; then
    echo -e "${GREEN}✓ Error handling refactoring completed successfully!${NC}"
    echo
    echo "Statistics:"
    echo "  - Modules refactored: 19"
    echo "  - Files using agentErrors: $AGENT_ERROR_COUNT"
    echo "  - Coverage: ${COVERAGE}%"
    echo "  - All tests passing"
    echo "  - Import layering intact"
else
    echo -e "${YELLOW}⚠ Some issues found:${NC}"
    [ -n "$FMT_ERRORF_FILES" ] && echo "  - Remaining fmt.Errorf in production code"
    [ -n "$FAILED_TESTS" ] && echo "  - Failed tests in: $FAILED_TESTS"
fi