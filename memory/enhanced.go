// Package memory provides enhanced memory capabilities for agents
package memory

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/kart-io/goagent/core"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/utils/json"
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
	embedder     TextEmbedder
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

// NewHierarchicalMemoryWithContext creates a new hierarchical memory system with a parent context
func NewHierarchicalMemoryWithContext(parentCtx context.Context, vectorStore VectorStore, opts ...MemoryOption) *HierarchicalMemory {
	ctx, cancel := context.WithCancel(parentCtx)
	m := &HierarchicalMemory{
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
	// 容量来自选项，必须在选项应用后构造
	m.shortTerm = NewShortTermMemory(m.shortTermCapacity)

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

// TextEmbedder 把文本转换为向量，如 llm.EmbeddingClient（目前只有 providers.OpenAIProvider 声明支持，
// 固定使用 text-embedding-ada-002，1536 维）；向量维度须与向量存储一致。
// 比 Embedder 少了 EmbedBatch，因为 provider 客户端不实现批量接口；包内 SimpleEmbeddingModel
// 返回 []float32，不满足本接口。
type TextEmbedder interface {
	Embed(ctx context.Context, text string) ([]float64, error)
}

// WithEmbedder 让 StoreTyped 为可提取文本的内容生成向量并写入向量存储，使其能被 VectorSearch 检索。
func WithEmbedder(e TextEmbedder) MemoryOption {
	return func(m *HierarchicalMemory) {
		m.embedder = e
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
	// Embed 可能是网络调用，必须在持锁前完成
	embedding, err := m.embed(ctx, value)
	if err != nil {
		return agentErrors.Wrap(err, agentErrors.CodeRetrievalEmbedding, "failed to generate memory embedding").
			WithComponent("hierarchical_memory").
			WithOperation("store_typed").
			WithContext("key", key)
	}

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
		Embedding:   embedding,
	}

	// Store based on type
	switch memType {
	case MemoryTypeLongTerm, MemoryTypeEpisodic, MemoryTypeSemantic, MemoryTypeProcedural:
		err = m.longTerm.Store(ctx, entry)
	default:
		err = m.storeShortTerm(ctx, entry)
	}
	if err != nil {
		return err
	}
	// 用无法生成向量的内容覆盖同一 key 时，旧向量不能继续指向新内容
	if m.embedder != nil && len(embedding) == 0 {
		m.syncVector(ctx, key)
	}
	return nil
}

// storeShortTerm 让短期条目也立即可被 VectorSearch 检索，并同步被容量淘汰条目的向量。
func (m *HierarchicalMemory) storeShortTerm(ctx context.Context, entry *MemoryEntry) error {
	if m.vectorStore != nil && len(entry.Embedding) > 0 {
		if err := m.vectorStore.Add(ctx, entry.ID, toFloat64(entry.Embedding), entry.Metadata); err != nil {
			return agentErrors.Wrap(err, agentErrors.CodeStateSave, "failed to index short-term memory").
				WithComponent("hierarchical_memory").
				WithOperation("store_typed").
				WithContext("key", entry.ID)
		}
	}

	m.syncEvicted(ctx, m.shortTerm.storeEvicting(entry))
	return nil
}

// syncEvicted 只处理带向量的被淘汰条目；不带向量的条目从未写入向量存储。
func (m *HierarchicalMemory) syncEvicted(ctx context.Context, evicted *MemoryEntry) {
	if evicted != nil && len(evicted.Embedding) > 0 {
		m.syncVector(ctx, evicted.ID)
	}
}

// syncVector 在条目离开某一层或被覆盖后，让向量存储中该 ID 的向量与仍持有它的条目一致：
// 有持有者则重写其向量（恢复归属），否则删除，避免孤儿向量占用搜索名额。
func (m *HierarchicalMemory) syncVector(ctx context.Context, id string) {
	if m.vectorStore == nil {
		return
	}
	if holder := m.vectorHolder(ctx, id); holder != nil {
		if err := m.vectorStore.Add(ctx, id, toFloat64(holder.Embedding), holder.Metadata); err != nil {
			// 向量数据库写入失败，记录但继续
			fmt.Fprintf(os.Stderr, "vector store restore failed (id=%s): %v\n", id, err)
		}
		return
	}
	if err := m.vectorStore.Delete(ctx, id); err != nil {
		// 向量数据库删除失败，记录但继续
		fmt.Fprintf(os.Stderr, "vector store delete failed (id=%s): %v\n", id, err)
	}
}

// vectorHolder 返回该 ID 下带向量的条目，短期优先，与 VectorSearch 的查找顺序一致。
func (m *HierarchicalMemory) vectorHolder(ctx context.Context, id string) *MemoryEntry {
	if e, err := m.shortTerm.Get(ctx, id); err == nil && len(e.Embedding) > 0 {
		return e
	}
	if e, err := m.longTerm.Get(ctx, id); err == nil && len(e.Embedding) > 0 {
		return e
	}
	return nil
}

// embed 未配置 embedder 或内容无法提取文本时返回 nil，此时条目照常写入但不建向量索引。
func (m *HierarchicalMemory) embed(ctx context.Context, value interface{}) ([]float32, error) {
	if m.embedder == nil {
		return nil, nil
	}
	text, ok := embeddingText(value)
	if !ok {
		return nil, nil
	}
	vec, err := m.embedder.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	return toFloat32(vec), nil
}

// embeddingText 依次支持 string、[]byte、fmt.Stringer，其余类型取 JSON 序列化结果；
// nil 与 nil 指针/map/slice 等视为无法提取（nil 指针的 String() 可能 panic）。
func embeddingText(value interface{}) (string, bool) {
	if isNil(value) {
		return "", false
	}
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case []byte:
		text = string(v)
	case fmt.Stringer:
		text = v.String()
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		text = string(data)
	}
	return text, text != ""
}

func isNil(value interface{}) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan:
		return rv.IsNil()
	}
	return false
}

func toFloat64(v []float32) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = float64(x)
	}
	return out
}

// Get retrieves a value from memory
func (m *HierarchicalMemory) Get(ctx context.Context, key string) (interface{}, error) {
	// 更新访问计数并可能晋升条目，属于写操作
	m.mu.Lock()
	defer m.mu.Unlock()

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
			m.syncEvicted(ctx, m.shortTerm.storeEvicting(entry))
		}

		return entry.Content, nil
	}

	return nil, agentErrors.New(agentErrors.CodeStoreNotFound, "memory not found").
		WithComponent("hierarchical_memory").
		WithOperation("get").
		WithContext("key", key)
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
		return nil, agentErrors.New(agentErrors.CodeInvalidConfig, "vector store not configured").
			WithComponent("hierarchical_memory").
			WithOperation("vector_search")
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
		return nil, agentErrors.Wrap(err, agentErrors.CodeRetrievalSearch, "vector search failed").
			WithComponent("hierarchical_memory").
			WithOperation("vector_search")
	}

	// Retrieve memory entries
	var entries []*MemoryEntry
	for _, result := range results {
		// Score 是相似度，越大越相似
		if result.Score < threshold {
			continue
		}
		id := result.ID
		// 优先返回向量真正所属的条目，再回退到同 ID 的任意条目
		entry := m.vectorHolder(ctx, id)
		if entry == nil {
			if e, err := m.shortTerm.Get(ctx, id); err == nil {
				entry = e
			} else if e, err := m.longTerm.Get(ctx, id); err == nil {
				entry = e
			}
		}
		if entry != nil {
			entries = append(entries, withSimilarity(entry, result.Score))
		}
	}

	// Limit results
	if len(entries) > limit {
		entries = entries[:limit]
	}

	return entries, nil
}

// withSimilarity 返回带分数的副本：读锁下并发搜索不能写共享条目的 Metadata。
func withSimilarity(entry *MemoryEntry, score float64) *MemoryEntry {
	cp := *entry
	cp.Metadata = make(map[string]interface{}, len(entry.Metadata)+1)
	for k, v := range entry.Metadata {
		cp.Metadata[k] = v
	}
	cp.Metadata["similarity_score"] = score
	return &cp
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
			return agentErrors.Wrap(err, agentErrors.CodeStateSave, "failed to consolidate memory").
				WithComponent("hierarchical_memory").
				WithOperation("consolidate").
				WithContext("entry_id", entry.ID)
		}

		// Store in vector database if available
		if m.vectorStore != nil && len(entry.Embedding) > 0 {
			// Convert float32 to float64
			embedding64 := make([]float64, len(entry.Embedding))
			for i, v := range entry.Embedding {
				embedding64[i] = float64(v)
			}
			if err := m.vectorStore.Add(ctx, entry.ID, embedding64, entry.Metadata); err != nil {
				// 向量数据库添加失败，记录但继续
				fmt.Fprintf(os.Stderr, "vector store add failed during consolidation (entry_id=%s): %v\n", entry.ID, err)
			}
		}

		// Remove from short-term
		if err := m.shortTerm.Remove(ctx, entry.ID); err != nil {
			// 短期记忆移除失败，记录但继续
			fmt.Fprintf(os.Stderr, "short-term memory removal failed (entry_id=%s): %v\n", entry.ID, err)
		}
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

	// 另一层仍持有同 ID 条目时保留其向量，否则删除
	for _, id := range append(shortTermForgotten, longTermForgotten...) {
		m.syncVector(ctx, id)
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
		return agentErrors.New(agentErrors.CodeStoreNotFound, "one or both memories not found").
			WithComponent("hierarchical_memory").
			WithOperation("associate")
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
		return nil, agentErrors.New(agentErrors.CodeStoreNotFound, "memory not found").
			WithComponent("hierarchical_memory").
			WithOperation("get_associated").
			WithContext("id", id)
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
		if err := m.vectorStore.Clear(ctx); err != nil {
			// 向量数据库清空失败，记录但继续
			fmt.Fprintf(os.Stderr, "vector store clear failed: %v\n", err)
		}
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
			m.maintain()
		}
	}
}

// maintain 执行一次后台整理：VectorStore 是宿主提供的实现，没有调用方接收错误，
// panic 交给全局 PanicHandler 记录，后台循环继续下一轮。
func (m *HierarchicalMemory) maintain() {
	defer core.ReportPanic(m.ctx, "hierarchical_memory", "maintain")

	// Consolidate memories
	_ = m.Consolidate(m.ctx)

	// Forget unimportant memories
	_ = m.Forget(m.ctx, m.importanceThreshold)
}
