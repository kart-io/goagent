package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ShortTermMemory implements short-term/working memory
type ShortTermMemory struct {
	entries  map[string]*MemoryEntry
	order    []string // Maintain insertion order
	capacity int
	mu       sync.RWMutex
}

// NewShortTermMemory creates a new short-term memory
func NewShortTermMemory(capacity int) *ShortTermMemory {
	return &ShortTermMemory{
		entries:  make(map[string]*MemoryEntry),
		order:    make([]string, 0, capacity),
		capacity: capacity,
	}
}

// Store stores an entry in short-term memory
func (m *ShortTermMemory) Store(ctx context.Context, entry *MemoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists
	if _, exists := m.entries[entry.ID]; exists {
		// Update existing entry
		m.entries[entry.ID] = entry
		return nil
	}

	// Check capacity
	if len(m.entries) >= m.capacity {
		// Evict least recently used
		m.evictLRU()
	}

	// Store entry
	m.entries[entry.ID] = entry
	m.order = append(m.order, entry.ID)

	return nil
}

// Get retrieves an entry from short-term memory
func (m *ShortTermMemory) Get(ctx context.Context, id string) (*MemoryEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, exists := m.entries[id]
	if !exists {
		return nil, fmt.Errorf("not found in short-term memory: %s", id)
	}

	return entry, nil
}

// Search searches for entries in short-term memory
func (m *ShortTermMemory) Search(ctx context.Context, query string, limit int) ([]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []interface{}

	// Simple string matching - could be enhanced with better search
	for _, entry := range m.entries {
		if containsQuery(entry, query) {
			results = append(results, entry.Content)
			if len(results) >= limit {
				break
			}
		}
	}

	return results, nil
}

// GetByType retrieves entries by memory type
func (m *ShortTermMemory) GetByType(memType MemoryType, limit int) []*MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var entries []*MemoryEntry

	for _, entry := range m.entries {
		if entry.Type == memType {
			entries = append(entries, entry)
			if len(entries) >= limit {
				break
			}
		}
	}

	return entries
}

// GetConsolidationCandidates gets memories ready for consolidation
func (m *ShortTermMemory) GetConsolidationCandidates(threshold float64) []*MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var candidates []*MemoryEntry

	for _, entry := range m.entries {
		// Check importance and access patterns
		if entry.Importance >= threshold || entry.AccessCount > 5 {
			candidates = append(candidates, entry)
		}
	}

	// Sort by importance
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Importance > candidates[j].Importance
	})

	return candidates
}

// Remove removes an entry from short-term memory
func (m *ShortTermMemory) Remove(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.entries, id)

	// Remove from order
	newOrder := make([]string, 0, len(m.order)-1)
	for _, oid := range m.order {
		if oid != id {
			newOrder = append(newOrder, oid)
		}
	}
	m.order = newOrder

	return nil
}

// Forget removes memories below importance threshold
func (m *ShortTermMemory) Forget(threshold float64) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var forgotten []string

	for id, entry := range m.entries {
		if entry.Importance < threshold {
			delete(m.entries, id)
			forgotten = append(forgotten, id)
		}
	}

	// Update order
	if len(forgotten) > 0 {
		newOrder := make([]string, 0, len(m.order))
		for _, id := range m.order {
			if _, exists := m.entries[id]; exists {
				newOrder = append(newOrder, id)
			}
		}
		m.order = newOrder
	}

	return forgotten
}

// GetAll returns all entries
func (m *ShortTermMemory) GetAll() []*MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := make([]*MemoryEntry, 0, len(m.entries))
	for _, entry := range m.entries {
		entries = append(entries, entry)
	}

	return entries
}

// Size returns the number of entries
func (m *ShortTermMemory) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// Clear clears all entries
func (m *ShortTermMemory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries = make(map[string]*MemoryEntry)
	m.order = make([]string, 0, m.capacity)
}

// evictLRU evicts the least recently used entry
func (m *ShortTermMemory) evictLRU() {
	if len(m.order) == 0 {
		return
	}

	// Find entry with lowest importance and access
	var evictID string
	minScore := float64(1000000)

	for _, id := range m.order {
		entry := m.entries[id]
		// Score based on importance, access count, and time
		timeFactor := time.Since(entry.LastAccess).Hours()
		score := entry.Importance*10 + float64(entry.AccessCount) - timeFactor

		if score < minScore {
			minScore = score
			evictID = id
		}
	}

	if evictID != "" {
		delete(m.entries, evictID)

		// Remove from order
		newOrder := make([]string, 0, len(m.order)-1)
		for _, id := range m.order {
			if id != evictID {
				newOrder = append(newOrder, id)
			}
		}
		m.order = newOrder
	}
}

// LongTermMemory implements long-term/persistent memory
type LongTermMemory struct {
	entries     map[string]*MemoryEntry
	vectorStore VectorStore
	index       map[MemoryType][]string // Type index for fast lookup
	mu          sync.RWMutex
}

// NewLongTermMemory creates a new long-term memory
func NewLongTermMemory(vectorStore VectorStore) *LongTermMemory {
	return &LongTermMemory{
		entries:     make(map[string]*MemoryEntry),
		vectorStore: vectorStore,
		index:       make(map[MemoryType][]string),
	}
}

// Store stores an entry in long-term memory
func (m *LongTermMemory) Store(ctx context.Context, entry *MemoryEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Store entry
	m.entries[entry.ID] = entry

	// Update index
	if m.index[entry.Type] == nil {
		m.index[entry.Type] = []string{}
	}

	// Check if already in index
	found := false
	for _, id := range m.index[entry.Type] {
		if id == entry.ID {
			found = true
			break
		}
	}
	if !found {
		m.index[entry.Type] = append(m.index[entry.Type], entry.ID)
	}

	// Store in vector database if available
	if m.vectorStore != nil && len(entry.Embedding) > 0 {
		// Convert float32 to float64 for VectorStore interface
		embedding64 := make([]float64, len(entry.Embedding))
		for i, v := range entry.Embedding {
			embedding64[i] = float64(v)
		}
		return m.vectorStore.Add(ctx, entry.ID, embedding64, entry.Metadata)
	}

	return nil
}

// Get retrieves an entry from long-term memory
func (m *LongTermMemory) Get(ctx context.Context, id string) (*MemoryEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, exists := m.entries[id]
	if !exists {
		return nil, fmt.Errorf("not found in long-term memory: %s", id)
	}

	return entry, nil
}

// Search searches for entries in long-term memory
func (m *LongTermMemory) Search(ctx context.Context, query string, limit int) ([]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var results []interface{}

	// Search using vector store if available
	// TODO: GenerateEmbedding method is not available in current VectorStore interface
	// if m.vectorStore != nil {
	// 	embedding, err := m.vectorStore.GenerateEmbedding(ctx, query)
	// 	if err == nil {
	// 		ids, _, err := m.vectorStore.Search(ctx, embedding, limit, 0.5)
	// 		if err == nil {
	// 			for _, id := range ids {
	// 				if entry, exists := m.entries[id]; exists {
	// 					results = append(results, entry.Content)
	// 				}
	// 			}
	// 			return results, nil
	// 		}
	// 	}
	// }

	// Fallback to simple string matching
	for _, entry := range m.entries {
		if containsQuery(entry, query) {
			results = append(results, entry.Content)
			if len(results) >= limit {
				break
			}
		}
	}

	return results, nil
}

// GetByType retrieves entries by memory type
func (m *LongTermMemory) GetByType(memType MemoryType, limit int) []*MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var entries []*MemoryEntry

	// Use index for faster lookup
	if ids, exists := m.index[memType]; exists {
		for _, id := range ids {
			if entry, exists := m.entries[id]; exists {
				entries = append(entries, entry)
				if len(entries) >= limit {
					break
				}
			}
		}
	}

	return entries
}

// Forget removes memories below importance threshold
func (m *LongTermMemory) Forget(threshold float64) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var forgotten []string

	for id, entry := range m.entries {
		// More conservative forgetting for long-term memory
		if entry.Importance < threshold && entry.AccessCount < 2 {
			delete(m.entries, id)
			forgotten = append(forgotten, id)

			// Remove from index
			if ids, exists := m.index[entry.Type]; exists {
				newIDs := make([]string, 0, len(ids))
				for _, oid := range ids {
					if oid != id {
						newIDs = append(newIDs, oid)
					}
				}
				m.index[entry.Type] = newIDs
			}
		}
	}

	return forgotten
}

// GetAll returns all entries
func (m *LongTermMemory) GetAll() []*MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries := make([]*MemoryEntry, 0, len(m.entries))
	for _, entry := range m.entries {
		entries = append(entries, entry)
	}

	return entries
}

// Size returns the number of entries
func (m *LongTermMemory) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.entries)
}

// Clear clears all entries
func (m *LongTermMemory) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.entries = make(map[string]*MemoryEntry)
	m.index = make(map[MemoryType][]string)
}

// MemoryConsolidator handles memory consolidation
type MemoryConsolidator struct {
	lastConsolidation  time.Time
	consolidationCount int
	mu                 sync.RWMutex
}

// NewMemoryConsolidator creates a new memory consolidator
func NewMemoryConsolidator() *MemoryConsolidator {
	return &MemoryConsolidator{
		lastConsolidation: time.Now(),
	}
}

// Consolidate performs memory consolidation
func (c *MemoryConsolidator) Consolidate(shortTerm []*MemoryEntry, longTerm *LongTermMemory) ([]*MemoryEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var consolidated []*MemoryEntry

	// Group related memories
	groups := c.groupRelatedMemories(shortTerm)

	// Consolidate each group
	for _, group := range groups {
		if len(group) > 1 {
			// Merge related memories
			merged := c.mergeMemories(group)
			consolidated = append(consolidated, merged)
		} else {
			// Single memory, just consolidate if important
			consolidated = append(consolidated, group[0])
		}
	}

	c.lastConsolidation = time.Now()
	c.consolidationCount++

	return consolidated, nil
}

// groupRelatedMemories groups related memories together
func (c *MemoryConsolidator) groupRelatedMemories(memories []*MemoryEntry) [][]*MemoryEntry {
	groups := make([][]*MemoryEntry, 0, len(memories))
	used := make(map[string]bool)

	for _, memory := range memories {
		if used[memory.ID] {
			continue
		}

		group := []*MemoryEntry{memory}
		used[memory.ID] = true

		// Find related memories
		for _, other := range memories {
			if used[other.ID] {
				continue
			}

			if c.areRelated(memory, other) {
				group = append(group, other)
				used[other.ID] = true
			}
		}

		groups = append(groups, group)
	}

	return groups
}

// areRelated checks if two memories are related
func (c *MemoryConsolidator) areRelated(m1, m2 *MemoryEntry) bool {
	// Check if explicitly related
	for _, relatedID := range m1.Related {
		if relatedID == m2.ID {
			return true
		}
	}

	// Check if they share tags
	for _, tag1 := range m1.Tags {
		for _, tag2 := range m2.Tags {
			if tag1 == tag2 {
				return true
			}
		}
	}

	// Check temporal proximity
	timeDiff := m1.Timestamp.Sub(m2.Timestamp)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}
	if timeDiff < 5*time.Minute {
		return true
	}

	return false
}

// mergeMemories merges a group of related memories
func (c *MemoryConsolidator) mergeMemories(group []*MemoryEntry) *MemoryEntry {
	if len(group) == 0 {
		return nil
	}
	if len(group) == 1 {
		return group[0]
	}

	// Create merged memory
	merged := &MemoryEntry{
		ID:          fmt.Sprintf("consolidated_%d", time.Now().Unix()),
		Type:        MemoryTypeLongTerm,
		Timestamp:   group[0].Timestamp,
		LastAccess:  time.Now(),
		AccessCount: 0,
		Tags:        []string{},
		Metadata:    make(map[string]interface{}),
		Related:     []string{},
	}

	// Merge content
	contents := make([]interface{}, 0, len(group))
	totalImportance := 0.0
	totalAccess := 0

	for _, memory := range group {
		contents = append(contents, memory.Content)
		totalImportance += memory.Importance
		totalAccess += memory.AccessCount

		// Merge tags
		for _, tag := range memory.Tags {
			if !containsString(merged.Tags, tag) {
				merged.Tags = append(merged.Tags, tag)
			}
		}

		// Keep track of original IDs
		merged.Related = append(merged.Related, memory.ID)
	}

	merged.Content = contents
	merged.Importance = totalImportance / float64(len(group))
	merged.AccessCount = totalAccess
	merged.Metadata["original_count"] = len(group)
	merged.Metadata["consolidation_time"] = time.Now()

	return merged
}

// Helper functions

func containsQuery(entry *MemoryEntry, query string) bool {
	// Simple string matching - could be enhanced
	contentStr := fmt.Sprintf("%v", entry.Content)
	return len(query) > 0 && len(contentStr) > 0 &&
		(contentStr == query || containsString([]string{contentStr}, query))
}

func containsString(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}
