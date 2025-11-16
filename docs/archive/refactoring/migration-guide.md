# Migration Guide: pkg/agent Refactoring

## Overview

This guide helps you migrate from the old package structure to the new refactored structure introduced in v0.10.0.

## Quick Reference Table

| Old Import                                         | New Import                                            | Version  | Status                     |
| -------------------------------------------------- | ----------------------------------------------------- | -------- | -------------------------- |
| `github.com/kart-io/goagent/core.Agent`            | `github.com/kart-io/goagent/interfaces.Agent`         | v0.10.0+ | Deprecated (remove v1.0.0) |
| `github.com/kart-io/goagent/core.Runnable`         | `github.com/kart-io/goagent/interfaces.Runnable`      | v0.10.0+ | Deprecated (remove v1.0.0) |
| `github.com/kart-io/goagent/retrieval.VectorStore` | `github.com/kart-io/goagent/interfaces.VectorStore`   | v0.10.0+ | Deprecated (remove v1.0.0) |
| `github.com/kart-io/goagent/memory.Manager`        | `github.com/kart-io/goagent/interfaces.MemoryManager` | v0.10.0+ | Deprecated (remove v1.0.0) |

## Why This Change?

The refactoring addresses several critical issues:

1. **Interface Duplication**: VectorStore was defined in 2+ locations with incompatible definitions
2. **Import Complexity**: Circular dependencies and unclear boundaries
3. **Maintenance Burden**: Scattered interface definitions made updates difficult

## Migration Steps

### Step 1: Update Interface Imports (Recommended)

**Before**:

```go
import (
    "github.com/kart-io/goagent/core"
    "github.com/kart-io/goagent/retrieval"
)

func MyFunction(agent core.Agent, store retrieval.VectorStore) { ... }
```

**After**:

```go
import (
    "github.com/kart-io/goagent/interfaces"
)

func MyFunction(agent interfaces.Agent, store interfaces.VectorStore) { ... }
```

### Step 2: Compatibility Period

Type aliases ensure your existing code continues to work:

```go
// This still works (but is deprecated)
import "github.com/kart-io/goagent/core"
var agent core.Agent  // Points to interfaces.Agent via type alias
```

### Step 3: Automated Migration (Optional)

Use the provided script to automate import updates:

```bash
# Coming soon: scripts/migrate-interfaces.sh
```

## Compatibility Matrix

| Version | Old Imports        | New Imports | Breaking Changes |
| ------- | ------------------ | ----------- | ---------------- |
| v0.9.x  | Works              | N/A         | None             |
| v0.10.x | Works (deprecated) | Works       | None             |
| v0.11.x | Works (deprecated) | Works       | None             |
| v0.12.x | Works (deprecated) | Works       | None             |
| v1.0.0  | Removed            | Works       | Yes (planned)    |

## FAQ

### Q: Do I need to update my code immediately?

No. Type aliases provide full backward compatibility. Update at your convenience before v1.0.0.

### Q: Will my tests break?

No. All existing tests should pass without modification.

### Q: When will old imports stop working?

Old imports will be removed in v1.0.0 (minimum 4 minor versions away).

## Support

For issues or questions:

- GitHub Issues: https://github.com/kart-io/k8s-agent/issues
- Documentation: pkg/agent/docs/
