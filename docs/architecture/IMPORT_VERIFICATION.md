# Import Layering Verification Guide

This document provides tools, procedures, and scripts to verify and enforce import layering compliance in GoAgent.

## Quick Verification Commands

### Check for circular dependencies
```bash
go mod graph | awk '/goagent/{print}' | sort -u
```

### List all imports for a specific package
```bash
grep -r "^import" tools/ | grep github.com/kart-io/goagent
```

### Find violations of "tools should not import agents"
```bash
find tools -name "*.go" -exec grep -l "agents" {} \;
```

### Find violations of "production code importing examples"
```bash
find . -path "*/examples" -prune -o -name "*.go" -exec grep -l "examples" {} \;
```

### List all goagent imports
```bash
grep -r "github.com/kart-io/goagent/" . --include="*.go" | cut -d: -f2 | sort -u
```

### Auto-format imports
```bash
goimports -w ./
```

## Import Audit Checklist

When reviewing pull requests or adding new packages:

- [ ] New package has clear purpose within correct layer
- [ ] Package only imports from allowed layers
- [ ] No circular imports between packages
- [ ] No imports from Layer 4 (examples) in production code
- [ ] Test files properly named (`*_test.go`)
- [ ] Interfaces defined in Layer 1 if cross-layer
- [ ] Type aliases created for backward compatibility if needed
- [ ] Documentation updated (IMPORT_LAYERING.md)
- [ ] Examples added showing correct usage
- [ ] Verification script passes all checks

## Automated Verification Script

Create a file: `verify_imports.sh`

```bash
#!/bin/bash
set -e

# Root directory of goagent
ROOT_DIR="."
VIOLATIONS=0
WARNINGS=0

# Colors for output
RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

echo "=== Import Layering Verification ==="
echo ""

# Rule 1: Layer 1 packages should not import other goagent packages
echo "[1/8] Checking Layer 1 (interfaces, errors, cache, utils) isolation..."
for pkg in interfaces errors cache utils; do
    if [ -d "$ROOT_DIR/$pkg" ]; then
        violations=$(find "$ROOT_DIR/$pkg" -name "*.go" ! -name "*_test.go" -exec grep -l "github.com/kart-io/goagent/" {} \; 2>/dev/null | wc -l)
        if [ "$violations" -gt 0 ]; then
            echo -e "${YELLOW}  WARNING: $pkg has $violations files importing GoAgent packages (check if comments)${NC}"
            WARNINGS=$((WARNINGS + 1))
        fi
    fi
done

# Rule 2: Layer 3 should not import Layer 4 (examples)
echo "[2/8] Checking Layer 3 doesn't import examples..."
violations=$(find "$ROOT_DIR" -path "*/examples" -prune -o -name "*_test.go" -prune -o -name "*.go" -exec grep -l "examples" {} \; 2>/dev/null | grep -v examples | wc -l)
if [ "$violations" -gt 0 ]; then
    echo -e "${RED}  ERROR: Found $violations files importing examples in non-test code${NC}"
    find "$ROOT_DIR" -path "*/examples" -prune -o -name "*_test.go" -prune -o -name "*.go" -exec grep -l "examples" {} \; 2>/dev/null | grep -v examples
    VIOLATIONS=$((VIOLATIONS + 1))
fi

# Rule 3: tools should not import agents
echo "[3/8] Checking tools don't import agents..."
if [ -d "$ROOT_DIR/tools" ]; then
    violations=$(find "$ROOT_DIR/tools" -name "*.go" ! -name "*_test.go" -exec grep -l "agents" {} \; 2>/dev/null | wc -l)
    if [ "$violations" -gt 0 ]; then
        echo -e "${RED}  ERROR: Found $violations files in tools importing agents${NC}"
        find "$ROOT_DIR/tools" -name "*.go" ! -name "*_test.go" -exec grep -l "agents" {} \; 2>/dev/null
        VIOLATIONS=$((VIOLATIONS + 1))
    fi
fi

# Rule 4: parsers should not import tools or agents
echo "[4/8] Checking parsers don't import tools/agents..."
if [ -d "$ROOT_DIR/parsers" ]; then
    violations=$(find "$ROOT_DIR/parsers" -name "*.go" ! -name "*_test.go" -exec grep -l "github.com/kart-io/goagent/\(tools\|agents\)" {} \; 2>/dev/null | wc -l)
    if [ "$violations" -gt 0 ]; then
        echo -e "${RED}  ERROR: Found $violations files in parsers importing tools/agents${NC}"
        find "$ROOT_DIR/parsers" -name "*.go" ! -name "*_test.go" -exec grep -l "github.com/kart-io/goagent/\(tools\|agents\)" {} \; 2>/dev/null
        VIOLATIONS=$((VIOLATIONS + 1))
    fi
fi

# Rule 5: core should not import Layer 3
echo "[5/8] Checking core doesn't import Layer 3..."
if [ -d "$ROOT_DIR/core" ]; then
    violations=$(find "$ROOT_DIR/core" -name "*.go" ! -name "*_test.go" -exec grep -l "github.com/kart-io/goagent/\(agents\|tools\|middleware\|parsers\)" {} \; 2>/dev/null | wc -l)
    if [ "$violations" -gt 0 ]; then
        echo -e "${RED}  ERROR: Found $violations files in core importing Layer 3${NC}"
        find "$ROOT_DIR/core" -name "*.go" ! -name "*_test.go" -exec grep -l "github.com/kart-io/goagent/\(agents\|tools\|middleware\|parsers\)" {} \; 2>/dev/null
        VIOLATIONS=$((VIOLATIONS + 1))
    fi
fi

# Rule 6: builder should not import agents
echo "[6/8] Checking builder doesn't import agents..."
if [ -d "$AGENT_PKG/builder" ]; then
    violations=$(find "$AGENT_PKG/builder" -name "*.go" ! -name "*_test.go" -exec grep -l "agents" {} \; 2>/dev/null | wc -l)
    if [ "$violations" -gt 0 ]; then
        echo -e "${RED}  ERROR: Found $violations files in builder importing agents${NC}"
        find "$AGENT_PKG/builder" -name "*.go" ! -name "*_test.go" -exec grep -l "agents" {} \; 2>/dev/null
        VIOLATIONS=$((VIOLATIONS + 1))
    fi
fi

# Rule 7: No circular dependencies
echo "[7/8] Checking for circular dependencies..."
circular=$(go mod graph 2>/dev/null | grep -E "goagent.*->.*goagent.*->.*goagent" | head -5)
if [ -n "$circular" ]; then
    echo -e "${YELLOW}  WARNING: Possible circular dependencies detected${NC}"
    echo "$circular"
    WARNINGS=$((WARNINGS + 1))
fi

# Rule 8: middleware should not import agents
echo "[8/8] Checking middleware doesn't import agents..."
if [ -d "$AGENT_PKG/middleware" ]; then
    violations=$(find "$AGENT_PKG/middleware" -name "*.go" ! -name "*_test.go" -exec grep -l "agents" {} \; 2>/dev/null | wc -l)
    if [ "$violations" -gt 0 ]; then
        echo -e "${RED}  ERROR: Found $violations files in middleware importing agents${NC}"
        find "$AGENT_PKG/middleware" -name "*.go" ! -name "*_test.go" -exec grep -l "agents" {} \; 2>/dev/null
        VIOLATIONS=$((VIOLATIONS + 1))
    fi
fi

echo ""
if [ "$VIOLATIONS" -eq 0 ] && [ "$WARNINGS" -eq 0 ]; then
    echo -e "${GREEN}✓ SUCCESS: All import layering rules verified!${NC}"
    exit 0
elif [ "$VIOLATIONS" -eq 0 ]; then
    echo -e "${YELLOW}⚠ WARNINGS: $WARNINGS warnings found (review above)${NC}"
    exit 0
else
    echo -e "${RED}✗ FAILURE: $VIOLATIONS rule violations, $WARNINGS warnings${NC}"
    exit 1
fi
```

### Usage

```bash
# Make executable
chmod +x verify_imports.sh

# Run basic check
./verify_imports.sh

# Run in CI/CD
./verify_imports.sh || exit 1
```

## CI/CD Integration

### GitHub Actions

Add to `.github/workflows/lint.yml`:

```yaml
- name: Verify import layering
  run: |
    chmod +x verify_imports.sh
    ./verify_imports.sh
```

### Makefile

```makefile
.PHONY: check-import-layering
check-import-layering:
	@./verify_imports.sh

.PHONY: pre-commit
pre-commit: fmt lint test check-import-layering
```

## Monitoring and Metrics

Track these metrics over time:

1. **Number of import rule violations** (should be 0)
2. **Average dependency depth** (layer count imports traverse)
3. **Cyclomatic import complexity** (should be low)
4. **Package coupling** (how many packages import each package)

### Example: Count imports per package

```bash
for pkg in core llm store memory builder agents tools middleware; do
    if [ -d "github.com/kart-io/goagent/$pkg" ]; then
        count=$(grep -r "import.*github.com/kart-io/goagent/$pkg" github.com/kart-io/goagent/ --include="*.go" ! -path "*/examples/*" ! -path "*_test.go" | wc -l)
        echo "$pkg: $count imports"
    fi
done
```

## Detailed Dependency Visualization

```
┌────────────────────────────────────────────────────────────────────┐
│                    LAYER 1: FOUNDATIONS                            │
│ ┌──────────┐  ┌────────┐  ┌───────┐  ┌────────┐                    │
│ │interfaces│  │ errors │  │ cache │  │ utils  │                    │
│ └────┬─────┘  └───┬────┘  └───┬───┘  └───┬────┘                    │
│      └────────────┴───────────┴─────────┘                           │
│      (All import ONLY stdlib + external)                            │
└────────────────────────┬────────────────────────────────────────────┘
                         │ (One-way dependency: Layer 1 ← All)
┌────────────────────────▼────────────────────────────────────────────┐
│                  LAYER 2: BUSINESS LOGIC                            │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │              core/ (Foundation of execution)                │   │
│  ├─────────────────────────────────────────────────────────────┤   │
│  │ ├─ agent.go: BaseAgent implementation                       │   │
│  │ ├─ execution/: Execution engine                             │   │
│  │ ├─ state/: State management                                │   │
│  │ ├─ checkpoint/: Checkpointing logic                         │   │
│  │ ├─ middleware/: Middleware framework                        │   │
│  │ └─ callback/: Callback system                               │   │
│  │                                                             │   │
│  │ Imports: interfaces/, errors/, cache/                       │   │
│  │ Exports: Core types, BaseAgent, execution infrastructure    │   │
│  └────────────────────────────────────────────────────────────┘   │
│                                                                      │
│  ┌──────────────┐  ┌────────────┐  ┌──────────┐                   │
│  │  llm/        │  │  store/    │  │ memory/  │                   │
│  ├──────────────┤  ├────────────┤  ├──────────┤                   │
│  │ • client.go  │  │ • store.go │  │ • manager│                   │
│  │ • providers/ │  │ • memory/  │  │ • types  │                   │
│  └──────────────┘  │ • redis/   │  └──────────┘                   │
│                    │ • postgres/│                                   │
│                    └────────────┘                                   │
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │         builder/ (Fluent API for agent construction)          │  │
│  ├──────────────────────────────────────────────────────────────┤  │
│  │ Imports: core/, llm/, store/, memory/, tools/                │  │
│  │ Exports: AgentBuilder, fluent configuration API              │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  Other: retrieval/, observability/, performance/, planning/,       │
│         prompt/, reflection/                                        │
│                                                                      │
└──────────────────────────┬─────────────────────────────────────────┘
                           │ (One-way dependency: Layer 3 → Layer 2)
┌──────────────────────────▼─────────────────────────────────────────┐
│                   LAYER 3: IMPLEMENTATION                           │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │           agents/ (Agent implementations)                    │  │
│  ├──────────────────────────────────────────────────────────────┤  │
│  │ executor/   → Tool execution agent                           │  │
│  │ react/      → ReAct reasoning agent                          │  │
│  │ specialized/→ Domain-specific agents                         │  │
│  │                                                              │  │
│  │ Imports: core/, interfaces/, tools/, memory/, llm/           │  │
│  │ May import: parsers/ (same layer)                            │  │
│  │ Exports: Specific agent implementations                      │  │
│  └──────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  ┌──────────────────┐  ┌─────────────────┐  ┌─────────────────┐   │
│  │ tools/           │  │ middleware/     │  │ parsers/        │   │
│  ├──────────────────┤  ├─────────────────┤  ├─────────────────┤   │
│  │ shell/           │  │ observability.go│  │ output_parser.go│   │
│  │ http/            │  │ tool_selector.go│  │ parser_react.go │   │
│  │ search/          │  │ cache_mw.go     │  └─────────────────┘   │
│  │ practical/       │  └─────────────────┘                         │
│  │ registry.go      │                                              │
│  └──────────────────┘                                              │
│                                                                      │
│  Other: stream/, multiagent/, distributed/, mcp/, document/,       │
│         toolkits/                                                  │
│                                                                      │
└──────────────────────────┬─────────────────────────────────────────┘
                           │ (One-way dependency: Layer 4 → Layer 3)
┌──────────────────────────▼─────────────────────────────────────────┐
│                  LAYER 4: EXAMPLES & TESTS                          │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  examples/basic/     → Basic usage patterns                         │
│  examples/advanced/  → Advanced patterns                            │
│  examples/integration/→ Integration examples                        │
│                                                                      │
│  *_test.go files     → Unit and integration tests                  │
│                                                                      │
│  Can import: ALL layers (for teaching/testing)                      │
│  Cannot export: Nothing imports from examples                       │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

## Troubleshooting Common Violations

### Violation: Layer 1 importing from GoAgent

**Symptom:**
```
interfaces/memory.go:15: import "github.com/kart-io/goagent/core"
```

**Solution:**
1. If it's a comment reference - OK (document only)
2. If it's actual import - move type to interfaces/
3. If it's a complex type - use interface in Layer 1

### Violation: tools importing agents

**Symptom:**
```
tools/agent_tool.go:10: import "github.com/kart-io/goagent/agents"
```

**Solution:**
1. Move common functionality to Layer 2 (core/ or new package)
2. Use interfaces/ to define contract
3. Pass agent as parameter instead of importing

### Violation: Production code importing examples

**Symptom:**
```
agents/executor.go:5: import "github.com/kart-io/goagent/examples"
```

**Solution:**
1. Move reusable code from examples/ to appropriate layer
2. Create helper package in Layer 2 if needed
3. Examples should only be for demonstration

### Violation: Circular dependency

**Symptom:**
```
core/agent.go imports builder/
builder/builder.go imports core/
```

**Solution:**
1. Extract interface to Layer 1 (interfaces/)
2. Use dependency injection
3. Restructure code to remove cycle

## Pre-commit Hook

Create `.git/hooks/pre-commit`:

```bash
#!/bin/bash

echo "Running import layering verification..."
if [ -f "verify_imports.sh" ]; then
    ./verify_imports.sh
    if [ $? -ne 0 ]; then
        echo "Import layering violations detected. Commit aborted."
        exit 1
    fi
fi
```

Make it executable:
```bash
chmod +x .git/hooks/pre-commit
```

## See Also

- [Import Layering Architecture](./IMPORT_LAYERING.md) - Main architecture specification
- [README.md](../../README.md) - Package overview
- [MIGRATION_GUIDE.md](../guides/MIGRATION_GUIDE.md) - Migration procedures
- [examples/](../../examples/) - Usage examples

---

**Version:** 1.0
**Last Updated:** 2025-11-15
**Status:** Production Ready
