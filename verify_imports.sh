#!/bin/bash
#
# verify_imports.sh - Verify import layering compliance for pkg/agent
#
# This script checks that all packages follow the import layering rules
# defined in ARCHITECTURE.md.
#
# Usage: ./verify_imports.sh [--strict] [--verbose]
#
# Options:
#   --strict    Treat warnings as errors
#   --verbose   Show detailed output
#

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
AGENT_PKG="$SCRIPT_DIR"
STRICT=0
VERBOSE=0
VIOLATIONS=0
WARNINGS=0

# Colors for output
RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --strict) STRICT=1; shift ;;
        --verbose) VERBOSE=1; shift ;;
        *) echo "Unknown option: $1"; exit 1 ;;
    esac
done

print_header() {
    echo -e "${BLUE}=== $1 ===${NC}\n"
}

print_error() {
    echo -e "${RED}ERROR: $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}WARNING: $1${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

check_layer1_imports() {
    print_header "Layer 1: Foundation Packages"
    echo "Checking that interfaces/, errors/, cache/, utils/ don't import other pkg/agent packages..."
    echo ""

    local found=0
    for pkg in interfaces errors cache utils; do
        if [ -d "$AGENT_PKG/$pkg" ]; then
            # Find non-test files that import other pkg/agent packages
            if find "$AGENT_PKG/$pkg" -maxdepth 2 -name "*.go" ! -name "*_test.go" -exec grep -l "pkg/agent/[^[:space:]]*\(interfaces\|errors\|cache\|utils\)" {} \; 2>/dev/null | grep -v "^$"; then
                print_error "Found $pkg importing other Layer 1 packages"
                found=$((found + 1))
            fi
        fi
    done

    if [ $found -eq 0 ]; then
        print_success "Layer 1 packages have no cross-dependencies"
    else
        VIOLATIONS=$((VIOLATIONS + found))
    fi
    echo ""
}

check_layer3_no_examples() {
    print_header "Layer 3: No Examples Imports"
    echo "Checking that Layer 3 packages don't import examples/ (except in test files)..."
    echo ""

    # Exclude examples/ and test files
    local violations=$(find "$AGENT_PKG" \
        -path "*/examples" -prune -o \
        -name "*_test.go" -prune -o \
        -name "*.go" -type f -print | \
        xargs grep -l "pkg/agent/examples" 2>/dev/null | \
        grep -v "examples/" | \
        wc -l)

    if [ "$violations" -gt 0 ]; then
        print_error "Found $violations files importing examples in non-test code:"
        find "$AGENT_PKG" \
            -path "*/examples" -prune -o \
            -name "*_test.go" -prune -o \
            -name "*.go" -type f -print | \
            xargs grep -l "pkg/agent/examples" 2>/dev/null | \
            grep -v "examples/" | \
            sed 's/^/  /'
        VIOLATIONS=$((VIOLATIONS + violations))
    else
        print_success "No Layer 3 production code imports examples"
    fi
    echo ""
}

check_tools_no_agents() {
    print_header "Layer 3: Tools Don't Import Agents"
    echo "Checking that tools/ doesn't import agents/..."
    echo ""

    if [ -d "$AGENT_PKG/tools" ]; then
        local violations=$(find "$AGENT_PKG/tools" -maxdepth 2 -name "*.go" ! -name "*_test.go" -exec grep -l "pkg/agent/agents" {} \; 2>/dev/null | wc -l)

        if [ "$violations" -gt 0 ]; then
            print_error "Found $violations tool files importing agents:"
            find "$AGENT_PKG/tools" -maxdepth 2 -name "*.go" ! -name "*_test.go" -exec grep -l "pkg/agent/agents" {} \; 2>/dev/null | sed 's/^/  /'
            VIOLATIONS=$((VIOLATIONS + violations))
        else
            print_success "Tools package has no imports from agents"
        fi
    fi
    echo ""
}

check_parsers_isolation() {
    print_header "Layer 3: Parsers Isolation"
    echo "Checking that parsers/ doesn't import agents/ or middleware/..."
    echo ""

    if [ -d "$AGENT_PKG/parsers" ]; then
        local violations=$(find "$AGENT_PKG/parsers" -maxdepth 2 -name "*.go" ! -name "*_test.go" -exec grep -l "pkg/agent/\(agents\|middleware\)" {} \; 2>/dev/null | wc -l)

        if [ "$violations" -gt 0 ]; then
            print_error "Found $violations parser files with disallowed imports:"
            find "$AGENT_PKG/parsers" -maxdepth 2 -name "*.go" ! -name "*_test.go" -exec grep -l "pkg/agent/\(agents\|middleware\)" {} \; 2>/dev/null | sed 's/^/  /'
            VIOLATIONS=$((VIOLATIONS + violations))
        else
            print_success "Parsers package is properly isolated"
        fi
    fi
    echo ""
}

check_core_no_layer3() {
    print_header "Layer 2: Core Doesn't Import Layer 3"
    echo "Checking that core/ doesn't import agents/, tools/, middleware/, or parsers/..."
    echo ""

    if [ -d "$AGENT_PKG/core" ]; then
        local violations=$(find "$AGENT_PKG/core" -maxdepth 3 -name "*.go" ! -name "*_test.go" -exec grep -l "pkg/agent/\(agents\|middleware\|tools\|parsers\)" {} \; 2>/dev/null | wc -l)

        if [ "$violations" -gt 0 ]; then
            print_error "Found $violations core files importing Layer 3:"
            find "$AGENT_PKG/core" -maxdepth 3 -name "*.go" ! -name "*_test.go" -exec grep -l "pkg/agent/\(agents\|middleware\|tools\|parsers\)" {} \; 2>/dev/null | sed 's/^/  /'
            VIOLATIONS=$((VIOLATIONS + violations))
        else
            print_success "Core package doesn't import Layer 3"
        fi
    fi
    echo ""
}

check_builder_layer3_isolation() {
    print_header "Layer 2: Builder Isolation"
    echo "Checking that builder/ doesn't import agents/ or middleware/..."
    echo ""

    if [ -d "$AGENT_PKG/builder" ]; then
        local violations=$(find "$AGENT_PKG/builder" -maxdepth 2 -name "*.go" ! -name "*_test.go" -exec grep -l "pkg/agent/\(agents\|middleware\)" {} \; 2>/dev/null | wc -l)

        if [ "$violations" -gt 0 ]; then
            print_error "Found $violations builder files with disallowed Layer 3 imports:"
            find "$AGENT_PKG/builder" -maxdepth 2 -name "*.go" ! -name "*_test.go" -exec grep -l "pkg/agent/\(agents\|middleware\)" {} \; 2>/dev/null | sed 's/^/  /'
            VIOLATIONS=$((VIOLATIONS + violations))
        else
            print_success "Builder package is properly layered"
        fi
    fi
    echo ""
}

check_layer1_isolation() {
    print_header "Layer 1: Interfaces Isolation"
    echo "Checking that interfaces/ doesn't import any pkg/agent packages..."
    echo ""

    if [ -d "$AGENT_PKG/interfaces" ]; then
        local violations=$(find "$AGENT_PKG/interfaces" -maxdepth 2 -name "*.go" ! -name "*_test.go" -exec grep -l "pkg/agent" {} \; 2>/dev/null | wc -l)

        if [ "$violations" -gt 0 ]; then
            print_error "Found $violations interfaces files with pkg/agent imports:"
            find "$AGENT_PKG/interfaces" -maxdepth 2 -name "*.go" ! -name "*_test.go" -exec grep -l "pkg/agent" {} \; 2>/dev/null | sed 's/^/  /'
            VIOLATIONS=$((VIOLATIONS + violations))
        else
            print_success "Interfaces package has no pkg/agent dependencies"
        fi
    fi
    echo ""
}

check_layer1_errors() {
    print_header "Layer 1: Errors Package Isolation"
    echo "Checking that errors/ doesn't import any pkg/agent packages..."
    echo ""

    if [ -d "$AGENT_PKG/errors" ]; then
        local violations=$(find "$AGENT_PKG/errors" -maxdepth 2 -name "*.go" ! -name "*_test.go" -exec grep -l "pkg/agent" {} \; 2>/dev/null | wc -l)

        if [ "$violations" -gt 0 ]; then
            print_error "Found $violations errors files with pkg/agent imports:"
            find "$AGENT_PKG/errors" -maxdepth 2 -name "*.go" ! -name "*_test.go" -exec grep -l "pkg/agent" {} \; 2>/dev/null | sed 's/^/  /'
            VIOLATIONS=$((VIOLATIONS + violations))
        else
            print_success "Errors package has no pkg/agent dependencies"
        fi
    fi
    echo ""
}

check_circular_in_layer2() {
    print_header "Layer 2: Circular Dependency Check"
    echo "Checking for circular imports within Layer 2 packages..."
    echo ""

    # Check for common circular patterns
    local found=0

    if [ -d "$AGENT_PKG/core" ] && [ -d "$AGENT_PKG/builder" ]; then
        if grep -q "pkg/agent/builder" "$AGENT_PKG/core"/*.go 2>/dev/null && \
           grep -q "pkg/agent/core" "$AGENT_PKG/builder"/*.go 2>/dev/null; then
            print_warning "Possible circular dependency: core <-> builder"
            found=$((found + 1))
        fi
    fi

    if [ $found -gt 0 ]; then
        if [ $STRICT -eq 1 ]; then
            VIOLATIONS=$((VIOLATIONS + found))
        else
            WARNINGS=$((WARNINGS + found))
        fi
    else
        print_success "No obvious circular dependencies in Layer 2"
    fi
    echo ""
}

print_summary() {
    echo -e "${BLUE}=== Summary ===${NC}\n"

    if [ $VIOLATIONS -eq 0 ]; then
        echo -e "${GREEN}All import layering rules are satisfied!${NC}"
        if [ $WARNINGS -gt 0 ]; then
            echo -e "${YELLOW}(with $WARNINGS warnings)${NC}"
        fi
        return 0
    else
        echo -e "${RED}Found $VIOLATIONS rule violations${NC}"
        if [ $WARNINGS -gt 0 ]; then
            echo -e "${YELLOW}(and $WARNINGS warnings)${NC}"
        fi
        return 1
    fi
}

main() {
    print_header "Import Layering Verification for pkg/agent"

    # Run all checks
    check_layer1_isolation
    check_layer1_errors
    check_layer1_imports
    check_core_no_layer3
    check_builder_layer3_isolation
    check_tools_no_agents
    check_parsers_isolation
    check_layer3_no_examples
    check_circular_in_layer2

    print_summary
}

main
exit $?
