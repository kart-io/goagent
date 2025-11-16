// Package memory provides enhanced memory capabilities for agents
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// MemoryType represents the type of memory
type MemoryType string

const (
	MemoryTypeShortTerm  MemoryType = "short_term"
	MemoryTypeLongTerm   MemoryType = "long_term"
	MemoryTypeEpisodic   MemoryType = "episodic"
	MemoryTypeSemantic   MemoryType = "semantic"
	MemoryTypeProcedural MemoryType = "procedural"
)

// MemoryEntry represents a single memory entry
type MemoryEntry struct {
	ID          string                 `json:"id"`
	Type        MemoryType             `json:"type"`
	Content     interface{}            `json:"content"`
	Embedding   []float32              `json:"embedding,omitempty"`
	Timestamp   time.Time              `json:"timestamp"`
	LastAccess  time.Time              `json:"last_access"`
	AccessCount int                    `json:"access_count"`
	Importance  float64                `json:"importance"`
	Decay       float64                `json:"decay"`
	Tags        []string               `json:"tags,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Related     []string               `json:"related,omitempty"` // IDs of related memories
}

// StoreOptions provides options for storing memory
type StoreOptions struct {
	TTL      time.Duration          // Time to live
	Priority float64                // Priority for retrieval
	Tags     []string               // Tags for categorization
	Metadata map[string]interface{} // Additional metadata
}

// EnhancedMemory provides advanced memory capabilities
type EnhancedMemory interface {
	// Store memory with specific type
	StoreTyped(ctx context.Context, key string, value interface{}, memType MemoryType, opts StoreOptions) error

	// Retrieve memories by type
	GetByType(ctx context.Context, memType MemoryType, limit int) ([]*MemoryEntry, error)

	// Search memories using vector similarity
	VectorSearch(ctx context.Context, embedding []float32, limit int, threshold float64) ([]*MemoryEntry, error)

	// Consolidate short-term to long-term memory
	Consolidate(ctx context.Context) error

	// Forget old or unimportant memories
	Forget(ctx context.Context, threshold float64) error

	// Get memory statistics
	GetStats() *MemoryStats

	// Create associations between memories
	Associate(ctx context.Context, id1, id2 string, strength float64) error

	// Get associated memories
	GetAssociated(ctx context.Context, id string, limit int) ([]*MemoryEntry, error)
}

// MemoryStats contains memory statistics
type MemoryStats struct {
	TotalEntries      int            `json:"total_entries"`
	ShortTermCount    int            `json:"short_term_count"`
	LongTermCount     int            `json:"long_term_count"`
	EpisodicCount     int            `json:"episodic_count"`
	SemanticCount     int            `json:"semantic_count"`
	ProceduralCount   int            `json:"procedural_count"`
	TotalSize         int64          `json:"total_size"`
	LastConsolidation time.Time      `json:"last_consolidation"`
	AccessPatterns    map[string]int `json:"access_patterns"`
}

// HierarchicalMemory implements a hierarchical memory system
type HierarchicalMemory struct {
	shortTerm    *ShortTermMemory
	longTerm     *LongTermMemory
	vectorStore  VectorStore
	consolidator *MemoryConsolidator
	mu           sync.RWMutex

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wg     sync.WaitGroup

	// Configuration
	shortTermCapacity      int
	consolidationThreshold float64
	decayRate              float64
	importanceThreshold    float64
}

// NewHierarchicalMemory creates a new hierarchical memory system
func NewHierarchicalMemory(vectorStore VectorStore, opts ...MemoryOption) *HierarchicalMemory {
	ctx, cancel := context.WithCancel(context.Background())
	m := &HierarchicalMemory{
		shortTerm:              NewShortTermMemory(100),
		longTerm:               NewLongTermMemory(vectorStore),
		vectorStore:            vectorStore,
		consolidator:           NewMemoryConsolidator(),
		ctx:                    ctx,
		cancel:                 cancel,
		done:                   make(chan struct{}),
		shortTermCapacity:      100,
		consolidationThreshold: 0.7,
		decayRate:              0.01,
		importanceThreshold:    0.3,
	}

	for _, opt := range opts {
		opt(m)
	}

	// Start background consolidation with proper lifecycle management
	m.wg.Add(1)
	go m.backgroundConsolidation()

	return m
}

// MemoryOption configures memory
type MemoryOption func(*HierarchicalMemory)

// WithShortTermCapacity sets short-term memory capacity
func WithShortTermCapacity(capacity int) MemoryOption {
	return func(m *HierarchicalMemory) {
		m.shortTermCapacity = capacity
	}
}

// WithDecayRate sets memory decay rate
func WithDecayRate(rate float64) MemoryOption {
	return func(m *HierarchicalMemory) {
		m.decayRate = rate
	}
}

// Shutdown gracefully shuts down the memory system
func (m *HierarchicalMemory) Shutdown(ctx context.Context) error {
	// Signal shutdown
	m.cancel()
	close(m.done)

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Store stores a value in memory
func (m *HierarchicalMemory) Store(ctx context.Context, key string, value interface{}, opts StoreOptions) error {
	return m.StoreTyped(ctx, key, value, MemoryTypeShortTerm, opts)
}

// StoreTyped stores memory with specific type
func (m *HierarchicalMemory) StoreTyped(ctx context.Context, key string, value interface{}, memType MemoryType, opts StoreOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create memory entry
	entry := &MemoryEntry{
		ID:          key,
		Type:        memType,
		Content:     value,
		Timestamp:   time.Now(),
		LastAccess:  time.Now(),
		AccessCount: 0,
		Importance:  m.calculateImportance(value),
		Decay:       1.0,
		Tags:        opts.Tags,
		Metadata:    make(map[string]interface{}),
	}

	// Generate embedding if vector store is available
	// TODO: GenerateEmbedding method is not available in current VectorStore interface
	// if m.vectorStore != nil {
	// 	embedding, err := m.vectorStore.GenerateEmbedding(ctx, value)
	// 	if err == nil {
	// 		entry.Embedding = embedding
	// 	}
	// }

	// Store based on type
	switch memType {
	case MemoryTypeShortTerm:
		return m.shortTerm.Store(ctx, entry)
	case MemoryTypeLongTerm, MemoryTypeEpisodic, MemoryTypeSemantic, MemoryTypeProcedural:
		return m.longTerm.Store(ctx, entry)
	default:
		// Default to short-term
		return m.shortTerm.Store(ctx, entry)
	}
}

// Get retrieves a value from memory
func (m *HierarchicalMemory) Get(ctx context.Context, key string) (interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check short-term first
	if entry, err := m.shortTerm.Get(ctx, key); err == nil {
		m.updateAccess(entry)
		return entry.Content, nil
	}

	// Check long-term
	if entry, err := m.longTerm.Get(ctx, key); err == nil {
		m.updateAccess(entry)

		// Promote frequently accessed long-term memories to short-term
		if entry.AccessCount > 10 {
			_ = m.shortTerm.Store(ctx, entry)
		}

		return entry.Content, nil
	}

	return nil, fmt.Errorf("memory not found: %s", key)
}

// Search searches for memories matching a query
func (m *HierarchicalMemory) Search(ctx context.Context, query string, limit int) ([]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []interface{}

	// Search short-term
	shortTermResults, _ := m.shortTerm.Search(ctx, query, limit/2)
	results = append(results, shortTermResults...)

	// Search long-term
	longTermResults, _ := m.longTerm.Search(ctx, query, limit/2)
	results = append(results, longTermResults...)

	// Limit results
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// VectorSearch searches memories using vector similarity
func (m *HierarchicalMemory) VectorSearch(ctx context.Context, embedding []float32, limit int, threshold float64) ([]*MemoryEntry, error) {
	if m.vectorStore == nil {
		return nil, fmt.Errorf("vector store not configured")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Search in vector store
	// Convert float32 to float64
	embedding64 := make([]float64, len(embedding))
	for i, v := range embedding {
		embedding64[i] = float64(v)
	}

	results, err := m.vectorStore.Search(ctx, embedding64, limit*2)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	// Retrieve memory entries
	var entries []*MemoryEntry
	for _, result := range results {
		id := result.ID
		// Try to get from short-term
		if entry, err := m.shortTerm.Get(ctx, id); err == nil {
			entry.Metadata["similarity_score"] = result.Score
			entries = append(entries, entry)
			continue
		}

		// Try to get from long-term
		if entry, err := m.longTerm.Get(ctx, id); err == nil {
			entry.Metadata["similarity_score"] = result.Score
			entries = append(entries, entry)
		}
	}

	// Limit results
	if len(entries) > limit {
		entries = entries[:limit]
	}

	return entries, nil
}

// GetByType retrieves memories by type
func (m *HierarchicalMemory) GetByType(ctx context.Context, memType MemoryType, limit int) ([]*MemoryEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var entries []*MemoryEntry

	// Get from short-term
	shortTermEntries := m.shortTerm.GetByType(memType, limit)
	entries = append(entries, shortTermEntries...)

	// Get from long-term if needed
	if len(entries) < limit {
		remaining := limit - len(entries)
		longTermEntries := m.longTerm.GetByType(memType, remaining)
		entries = append(entries, longTermEntries...)
	}

	return entries, nil
}

// Consolidate consolidates short-term to long-term memory
func (m *HierarchicalMemory) Consolidate(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get important short-term memories
	candidates := m.shortTerm.GetConsolidationCandidates(m.consolidationThreshold)

	// Consolidate to long-term
	for _, entry := range candidates {
		// Update type to long-term
		if entry.Type == MemoryTypeShortTerm {
			entry.Type = MemoryTypeLongTerm
		}

		// Store in long-term
		if err := m.longTerm.Store(ctx, entry); err != nil {
			return fmt.Errorf("failed to consolidate memory %s: %w", entry.ID, err)
		}

		// Store in vector database if available
		if m.vectorStore != nil && len(entry.Embedding) > 0 {
			// Convert float32 to float64
			embedding64 := make([]float64, len(entry.Embedding))
			for i, v := range entry.Embedding {
				embedding64[i] = float64(v)
			}
			_ = m.vectorStore.Add(ctx, entry.ID, embedding64, entry.Metadata)
		}

		// Remove from short-term
		_ = m.shortTerm.Remove(ctx, entry.ID)
	}

	return nil
}

// Forget removes old or unimportant memories
func (m *HierarchicalMemory) Forget(ctx context.Context, threshold float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Apply decay to all memories
	m.applyDecay()

	// Forget short-term memories below threshold
	shortTermForgotten := m.shortTerm.Forget(threshold)

	// Forget long-term memories below threshold (more conservative)
	longTermForgotten := m.longTerm.Forget(threshold * 0.5)

	// Remove from vector store
	if m.vectorStore != nil {
		for _, id := range append(shortTermForgotten, longTermForgotten...) {
			_ = m.vectorStore.Delete(ctx, id)
		}
	}

	return nil
}

// Associate creates associations between memories
func (m *HierarchicalMemory) Associate(ctx context.Context, id1, id2 string, strength float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Find memories
	var entry1, entry2 *MemoryEntry

	// Check short-term
	if e, err := m.shortTerm.Get(ctx, id1); err == nil {
		entry1 = e
	} else if e, err := m.longTerm.Get(ctx, id1); err == nil {
		entry1 = e
	}

	if e, err := m.shortTerm.Get(ctx, id2); err == nil {
		entry2 = e
	} else if e, err := m.longTerm.Get(ctx, id2); err == nil {
		entry2 = e
	}

	if entry1 == nil || entry2 == nil {
		return fmt.Errorf("one or both memories not found")
	}

	// Create bidirectional association
	if entry1.Related == nil {
		entry1.Related = []string{}
	}
	if entry2.Related == nil {
		entry2.Related = []string{}
	}

	entry1.Related = append(entry1.Related, id2)
	entry2.Related = append(entry2.Related, id1)

	// Store association strength in metadata
	if entry1.Metadata == nil {
		entry1.Metadata = make(map[string]interface{})
	}
	if entry2.Metadata == nil {
		entry2.Metadata = make(map[string]interface{})
	}

	entry1.Metadata[fmt.Sprintf("association_%s", id2)] = strength
	entry2.Metadata[fmt.Sprintf("association_%s", id1)] = strength

	return nil
}

// GetAssociated retrieves associated memories
func (m *HierarchicalMemory) GetAssociated(ctx context.Context, id string, limit int) ([]*MemoryEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Find source memory
	var source *MemoryEntry
	if e, err := m.shortTerm.Get(ctx, id); err == nil {
		source = e
	} else if e, err := m.longTerm.Get(ctx, id); err == nil {
		source = e
	}

	if source == nil {
		return nil, fmt.Errorf("memory not found: %s", id)
	}

	// Get associated memories
	var associated []*MemoryEntry
	for _, relatedID := range source.Related {
		if len(associated) >= limit {
			break
		}

		if e, err := m.shortTerm.Get(ctx, relatedID); err == nil {
			associated = append(associated, e)
		} else if e, err := m.longTerm.Get(ctx, relatedID); err == nil {
			associated = append(associated, e)
		}
	}

	return associated, nil
}

// GetStats returns memory statistics
func (m *HierarchicalMemory) GetStats() *MemoryStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &MemoryStats{
		TotalEntries:   m.shortTerm.Size() + m.longTerm.Size(),
		ShortTermCount: m.shortTerm.Size(),
		LongTermCount:  m.longTerm.Size(),
		AccessPatterns: make(map[string]int),
	}

	// Count by type
	for _, entry := range m.shortTerm.GetAll() {
		switch entry.Type {
		case MemoryTypeEpisodic:
			stats.EpisodicCount++
		case MemoryTypeSemantic:
			stats.SemanticCount++
		case MemoryTypeProcedural:
			stats.ProceduralCount++
		}
	}

	for _, entry := range m.longTerm.GetAll() {
		switch entry.Type {
		case MemoryTypeEpisodic:
			stats.EpisodicCount++
		case MemoryTypeSemantic:
			stats.SemanticCount++
		case MemoryTypeProcedural:
			stats.ProceduralCount++
		}
	}

	return stats
}

// Clear clears all memory
func (m *HierarchicalMemory) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.shortTerm.Clear()
	m.longTerm.Clear()

	if m.vectorStore != nil {
		// Clear vector store
		_ = m.vectorStore.Clear(ctx)
	}

	return nil
}

// Helper methods

func (m *HierarchicalMemory) calculateImportance(value interface{}) float64 {
	// Simple importance calculation based on content size and structure
	data, _ := json.Marshal(value)
	size := len(data)

	// Base importance on size (normalized)
	importance := float64(size) / 1000.0
	if importance > 1.0 {
		importance = 1.0
	}

	// Adjust based on type
	switch v := value.(type) {
	case map[string]interface{}:
		// Complex structures are more important
		importance *= 1.2
	case []interface{}:
		// Lists are moderately important
		importance *= 1.1
	case string:
		// Long strings might be important
		if len(v) > 100 {
			importance *= 1.1
		}
	}

	// Normalize
	if importance > 1.0 {
		importance = 1.0
	}

	return importance
}

func (m *HierarchicalMemory) updateAccess(entry *MemoryEntry) {
	entry.LastAccess = time.Now()
	entry.AccessCount++

	// Boost importance based on access
	entry.Importance = entry.Importance * 1.1
	if entry.Importance > 1.0 {
		entry.Importance = 1.0
	}
}

func (m *HierarchicalMemory) applyDecay() {
	// Apply decay to short-term memories
	for _, entry := range m.shortTerm.GetAll() {
		timeSinceAccess := time.Since(entry.LastAccess)
		decayFactor := 1.0 - (m.decayRate * timeSinceAccess.Hours())
		if decayFactor < 0 {
			decayFactor = 0
		}
		entry.Decay = decayFactor
		entry.Importance *= decayFactor
	}

	// Apply slower decay to long-term memories
	for _, entry := range m.longTerm.GetAll() {
		timeSinceAccess := time.Since(entry.LastAccess)
		decayFactor := 1.0 - (m.decayRate * 0.1 * timeSinceAccess.Hours())
		if decayFactor < 0 {
			decayFactor = 0
		}
		entry.Decay = decayFactor
		entry.Importance *= decayFactor
	}
}

func (m *HierarchicalMemory) backgroundConsolidation() {
	defer m.wg.Done()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return // Clean shutdown
		case <-ticker.C:
			// Consolidate memories
			_ = m.Consolidate(m.ctx)

			// Forget unimportant memories
			_ = m.Forget(m.ctx, m.importanceThreshold)
		}
	}
}
