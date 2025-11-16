// Package interfaces provides canonical interface definitions for the agent framework.
//
// All concrete implementations should reference these interfaces to ensure
// type compatibility across packages. This package serves as the single
// source of truth for all shared interfaces.
//
// Key Interfaces:
//   - Agent: Autonomous agent interface
//   - Runnable: Base execution interface
//   - Tool: Tool execution interface
//   - VectorStore: Vector storage and search
//   - MemoryManager: Memory management
//   - Checkpointer: State checkpointing
//   - Store: Key-value storage
//
// Backward Compatibility:
//
// Type aliases exist in original locations (retrieval/, memory/, core/)
// for backward compatibility. These will be removed in v1.0.0.
// See docs/refactoring/migration-guide.md for migration instructions.
package interfaces
