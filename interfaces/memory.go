package interfaces

import (
	"context"
	"time"
)

// MemoryManager is the canonical interface for agent memory management.
//
// This interface provides a unified abstraction for managing different types
// of memory (conversation history, case-based reasoning, key-value storage).
//
// Implementation locations:
//   - memory.DefaultManager - Default implementation with conversation and case memory
//   - memory.MemoryVectorStore - Vector-based memory store
//
// Previously defined in: memory/manager.go as Manager
// This is now the single source of truth.
//
// See: pkg/agent/memory/manager.go for concrete implementations
type MemoryManager interface {
	// AddConversation stores a conversation turn.
	//
	// This is used to maintain conversation history for context-aware
	// agent interactions. Each conversation turn is associated with
	// a session ID for grouping related conversations.
	//
	// Example:
	//   conv := &Conversation{
	//     SessionID: "session-123",
	//     Role:      "user",
	//     Content:   "Hello, how are you?",
	//     Timestamp: time.Now(),
	//   }
	//   err := manager.AddConversation(ctx, conv)
	AddConversation(ctx context.Context, conv *Conversation) error

	// GetConversationHistory retrieves conversation history for a session.
	//
	// Returns the most recent conversations up to the specified limit.
	// Conversations are typically returned in chronological order.
	//
	// Parameters:
	//   - sessionID: The session identifier
	//   - limit: Maximum number of conversations to return (0 or negative for all)
	//
	// Returns:
	//   - Conversation slice (may be empty if no history exists)
	//   - Error if retrieval fails
	GetConversationHistory(ctx context.Context, sessionID string, limit int) ([]*Conversation, error)

	// ClearConversation removes conversation history for a session.
	//
	// This is useful for starting fresh conversations or implementing
	// privacy features like "forget this conversation".
	//
	// Parameters:
	//   - sessionID: The session identifier to clear
	//
	// Returns:
	//   - Error if clearing fails (returns nil if session doesn't exist)
	ClearConversation(ctx context.Context, sessionID string) error

	// AddCase stores a case for case-based reasoning.
	//
	// Cases are examples of problems and solutions that agents can
	// reference when handling similar situations. This supports
	// learning from past experiences.
	//
	// The case's embedding vector should be pre-computed and included
	// in the Case struct for efficient similarity search.
	//
	// Example:
	//   case := &Case{
	//     Title:       "Database connection timeout",
	//     Problem:     "App fails to connect to database",
	//     Solution:    "Increase connection timeout to 30s",
	//     Category:    "infrastructure",
	//     Tags:        []string{"database", "timeout"},
	//     Embedding:   embeddings,  // Pre-computed vector
	//   }
	//   err := manager.AddCase(ctx, case)
	AddCase(ctx context.Context, caseMemory *Case) error

	// SearchSimilarCases finds similar cases using semantic search.
	//
	// This uses vector similarity (typically cosine similarity) to find
	// cases that are semantically similar to the query. The query is
	// embedded and compared against stored case embeddings.
	//
	// Parameters:
	//   - query: Natural language query describing the problem
	//   - limit: Maximum number of similar cases to return
	//
	// Returns:
	//   - Case slice ordered by similarity (highest first)
	//   - Each case includes a Similarity score (0.0-1.0)
	//   - Error if search fails
	//
	// Example:
	//   cases, err := manager.SearchSimilarCases(ctx, "database timeout issue", 3)
	//   for _, c := range cases {
	//     fmt.Printf("Similar case (%.2f): %s\n", c.Similarity, c.Title)
	//   }
	SearchSimilarCases(ctx context.Context, query string, limit int) ([]*Case, error)

	// Store persists arbitrary key-value data.
	//
	// This provides a general-purpose storage mechanism for agent state
	// or other data that doesn't fit into conversation or case memory.
	//
	// The value will be serialized internally, so it must be serializable.
	//
	// Parameters:
	//   - key: Unique identifier for the data
	//   - value: Data to store (must be serializable)
	//
	// Returns:
	//   - Error if storage fails
	Store(ctx context.Context, key string, value interface{}) error

	// Retrieve fetches stored data by key.
	//
	// Returns the stored value or an error if the key doesn't exist.
	// The caller is responsible for type assertion.
	//
	// Parameters:
	//   - key: The key to retrieve
	//
	// Returns:
	//   - The stored value (requires type assertion)
	//   - Error if key doesn't exist or retrieval fails
	//
	// Example:
	//   val, err := manager.Retrieve(ctx, "user_preferences")
	//   if err != nil {
	//     // Handle error
	//   }
	//   prefs, ok := val.(map[string]interface{})
	Retrieve(ctx context.Context, key string) (interface{}, error)

	// Delete removes stored data by key.
	//
	// This is idempotent - deleting a non-existent key is not an error.
	//
	// Parameters:
	//   - key: The key to delete
	//
	// Returns:
	//   - Error if deletion fails (returns nil if key doesn't exist)
	Delete(ctx context.Context, key string) error

	// Clear removes all memory.
	//
	// This is a destructive operation that clears:
	//   - All conversation history
	//   - All cases
	//   - All key-value storage
	//
	// Use with caution! This is typically used for:
	//   - Testing and development
	//   - Complete system resets
	//   - Data privacy compliance (e.g., GDPR right to be forgotten)
	//
	// Returns:
	//   - Error if clearing fails
	Clear(ctx context.Context) error
}

// ConversationMemory is an optional specialized interface for conversation storage.
//
// Implementations that need fine-grained control over conversation memory
// can implement this interface in addition to MemoryManager.
//
// This interface is used internally by some memory implementations but is
// not required for basic MemoryManager usage.
type ConversationMemory interface {
	// Add adds a conversation to storage.
	Add(ctx context.Context, conv *Conversation) error

	// Get retrieves conversation history.
	Get(ctx context.Context, sessionID string, limit int) ([]*Conversation, error)

	// Clear clears conversation history for a session.
	Clear(ctx context.Context, sessionID string) error

	// Count returns the number of conversations in a session.
	Count(ctx context.Context, sessionID string) (int, error)
}

// Conversation represents a conversation turn.
//
// A conversation captures a single message in a multi-turn dialogue,
// including the role (user/assistant/system), content, and metadata.
type Conversation struct {
	// ID is a unique identifier for this conversation turn.
	//
	// May be auto-generated by the storage implementation.
	ID string `json:"id"`

	// SessionID groups related conversation turns.
	//
	// All conversations with the same SessionID are part of the
	// same dialogue session.
	SessionID string `json:"session_id"`

	// Role indicates who sent the message.
	//
	// Common values:
	//   - "user": Message from the user
	//   - "assistant": Message from the AI agent
	//   - "system": System message (e.g., instructions)
	Role string `json:"role"`

	// Content is the message content.
	//
	// This is typically text, but may include formatted content
	// depending on the implementation.
	Content string `json:"content"`

	// Timestamp is when the conversation occurred.
	//
	// Used for ordering and filtering conversations.
	Timestamp time.Time `json:"timestamp"`

	// Metadata contains additional context about the conversation.
	//
	// Optional. May include:
	//   - model: Which LLM model generated the response
	//   - tokens: Token count for this turn
	//   - language: Detected language
	//   - sentiment: Sentiment analysis results
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Case represents a stored case for reasoning.
//
// Cases capture problem-solution pairs that can be used for case-based
// reasoning. Each case includes semantic embeddings for similarity search.
type Case struct {
	// ID is a unique identifier for this case.
	//
	// May be auto-generated by the storage implementation.
	ID string `json:"id"`

	// Title is a brief summary of the case.
	//
	// Should be descriptive but concise (1-2 sentences).
	Title string `json:"title"`

	// Description provides more detail about the case.
	//
	// This can be a longer explanation of the context and scenario.
	Description string `json:"description"`

	// Problem describes the issue that was encountered.
	//
	// This should be detailed enough to match similar future problems.
	Problem string `json:"problem"`

	// Solution describes how the problem was resolved.
	//
	// This should include actionable steps and outcomes.
	Solution string `json:"solution"`

	// Category groups related cases.
	//
	// Examples: "infrastructure", "security", "performance", "bug-fix"
	Category string `json:"category"`

	// Tags are keywords for filtering and searching.
	//
	// Examples: ["database", "timeout", "postgres"]
	Tags []string `json:"tags,omitempty"`

	// Embedding is the semantic vector representation of this case.
	//
	// This is used for similarity search. Typically computed by
	// embedding the concatenation of Title + Problem + Solution.
	//
	// The dimension should match the embedding model being used
	// (e.g., 1536 for OpenAI text-embedding-ada-002).
	Embedding []float64 `json:"embedding,omitempty"`

	// Similarity is the similarity score (0.0-1.0) when this case
	// is returned from a search.
	//
	// Only populated in search results. Higher values indicate
	// greater similarity to the query.
	//
	// Not persisted in storage.
	Similarity float64 `json:"similarity,omitempty"`

	// CreatedAt is when the case was first added.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the case was last modified.
	//
	// Useful for tracking case evolution and versioning.
	UpdatedAt time.Time `json:"updated_at"`

	// Metadata contains additional information about the case.
	//
	// Optional. May include:
	//   - source: Where this case came from
	//   - author: Who created the case
	//   - success_rate: How often this solution works
	//   - related_cases: IDs of related cases
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}
