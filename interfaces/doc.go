// Package interfaces provides canonical interface definitions for the agent framework.
//
// All concrete implementations should reference these interfaces to ensure
// type compatibility across packages. This package serves as the single
// source of truth for all shared interfaces.
//
// Key Interfaces:
//   - Tool: Tool execution interface
//   - DocumentRetriever: Evidence retrieval
//   - VectorStore: Vector storage and search
//   - MemoryManager: Memory management
//   - Store: Key-value storage
//
// Agent execution contracts live in core (core.Agent, core.Runnable) and
// durable execution in core/durable.
package interfaces
