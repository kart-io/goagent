package tools

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestInvalidateByPattern tests pattern-based cache invalidation
func TestInvalidateByPattern(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	// Create test tools
	tool1 := NewBaseTool("search_tool", "Search tool", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		return &ToolOutput{Result: "search result", Success: true}, nil
	})
	tool2 := NewBaseTool("calc_tool", "Calculator tool", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		return &ToolOutput{Result: "42", Success: true}, nil
	})
	tool3 := NewBaseTool("search_advanced", "Advanced search", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		return &ToolOutput{Result: "advanced result", Success: true}, nil
	})

	cachedTool1 := NewCachedTool(tool1, cache, 5*time.Minute)
	cachedTool2 := NewCachedTool(tool2, cache, 5*time.Minute)
	cachedTool3 := NewCachedTool(tool3, cache, 5*time.Minute)

	t.Run("Invalidate by exact pattern", func(t *testing.T) {
		// Populate cache
		input1 := &ToolInput{Args: map[string]interface{}{"query": "test1"}}
		input2 := &ToolInput{Args: map[string]interface{}{"num": 10}}
		input3 := &ToolInput{Args: map[string]interface{}{"query": "test2"}}

		_, _ = cachedTool1.Invoke(ctx, input1)
		_, _ = cachedTool2.Invoke(ctx, input2)
		_, _ = cachedTool3.Invoke(ctx, input3)

		if cache.Size() != 3 {
			t.Fatalf("Expected 3 items in cache, got %d", cache.Size())
		}

		// Invalidate all entries starting with "search_"
		count, err := cache.InvalidateByPattern(ctx, "^search_.*")
		if err != nil {
			t.Fatalf("InvalidateByPattern failed: %v", err)
		}

		if count != 2 {
			t.Errorf("Expected 2 invalidations, got %d", count)
		}

		if cache.Size() != 1 {
			t.Errorf("Expected 1 item remaining, got %d", cache.Size())
		}

		// Verify calc_tool is still cached
		_, found := cachedTool2.cache.Get(ctx, generateTestKey(cachedTool2, input2))
		if !found {
			t.Error("Expected calc_tool to remain in cache")
		}
	})

	t.Run("Invalidate with wildcard pattern", func(t *testing.T) {
		_ = cache.Clear()

		// Populate cache
		input1 := &ToolInput{Args: map[string]interface{}{"query": "alpha"}}
		input2 := &ToolInput{Args: map[string]interface{}{"query": "beta"}}

		_, _ = cachedTool1.Invoke(ctx, input1)
		_, _ = cachedTool1.Invoke(ctx, input2)

		// Invalidate all entries for search_tool
		count, err := cache.InvalidateByPattern(ctx, "search_tool:.*")
		if err != nil {
			t.Fatalf("InvalidateByPattern failed: %v", err)
		}

		if count != 2 {
			t.Errorf("Expected 2 invalidations, got %d", count)
		}

		if cache.Size() != 0 {
			t.Errorf("Expected empty cache, got size %d", cache.Size())
		}
	})

	t.Run("Invalid regex pattern", func(t *testing.T) {
		_, err := cache.InvalidateByPattern(ctx, "[invalid(")
		if err == nil {
			t.Error("Expected error for invalid regex pattern")
		}
	})

	t.Run("Pattern matches nothing", func(t *testing.T) {
		_ = cache.Clear()

		input := &ToolInput{Args: map[string]interface{}{"query": "test"}}
		_, _ = cachedTool1.Invoke(ctx, input)

		count, err := cache.InvalidateByPattern(ctx, "nonexistent_.*")
		if err != nil {
			t.Fatalf("InvalidateByPattern failed: %v", err)
		}

		if count != 0 {
			t.Errorf("Expected 0 invalidations, got %d", count)
		}

		if cache.Size() != 1 {
			t.Errorf("Expected 1 item in cache, got %d", cache.Size())
		}
	})
}

// TestInvalidateByTool tests tool-specific cache invalidation
func TestInvalidateByTool(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	// Create test tools
	tool1 := NewBaseTool("search_tool", "Search tool", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		return &ToolOutput{Result: "search result", Success: true}, nil
	})
	tool2 := NewBaseTool("calc_tool", "Calculator tool", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		return &ToolOutput{Result: "42", Success: true}, nil
	})

	cachedTool1 := NewCachedTool(tool1, cache, 5*time.Minute)
	cachedTool2 := NewCachedTool(tool2, cache, 5*time.Minute)

	t.Run("Invalidate specific tool", func(t *testing.T) {
		// Populate cache with multiple entries per tool
		for i := 0; i < 3; i++ {
			input := &ToolInput{Args: map[string]interface{}{"query": fmt.Sprintf("test%d", i)}}
			_, _ = cachedTool1.Invoke(ctx, input)
		}

		for i := 0; i < 2; i++ {
			input := &ToolInput{Args: map[string]interface{}{"num": i}}
			_, _ = cachedTool2.Invoke(ctx, input)
		}

		if cache.Size() != 5 {
			t.Fatalf("Expected 5 items in cache, got %d", cache.Size())
		}

		// Invalidate search_tool only
		count, err := cache.InvalidateByTool(ctx, "search_tool")
		if err != nil {
			t.Fatalf("InvalidateByTool failed: %v", err)
		}

		if count != 3 {
			t.Errorf("Expected 3 invalidations, got %d", count)
		}

		if cache.Size() != 2 {
			t.Errorf("Expected 2 items remaining, got %d", cache.Size())
		}

		// Verify calc_tool entries are still present
		for i := 0; i < 2; i++ {
			input := &ToolInput{Args: map[string]interface{}{"num": i}}
			key, _ := cachedTool2.generateCacheKey(input)
			_, found := cache.Get(ctx, key)
			if !found {
				t.Errorf("Expected calc_tool entry %d to remain in cache", i)
			}
		}
	})

	t.Run("Invalidate non-existent tool", func(t *testing.T) {
		_ = cache.Clear()

		input := &ToolInput{Args: map[string]interface{}{"query": "test"}}
		_, _ = cachedTool1.Invoke(ctx, input)

		count, err := cache.InvalidateByTool(ctx, "nonexistent_tool")
		if err != nil {
			t.Fatalf("InvalidateByTool failed: %v", err)
		}

		if count != 0 {
			t.Errorf("Expected 0 invalidations, got %d", count)
		}

		if cache.Size() != 1 {
			t.Errorf("Expected 1 item in cache, got %d", cache.Size())
		}
	})
}

// TestDependencyTracking tests dependency-based cache invalidation
func TestDependencyTracking(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	// Create test tools
	dataTool := NewBaseTool("data_fetch", "Fetch data", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		return &ToolOutput{Result: "data", Success: true}, nil
	})
	processTool := NewBaseTool("data_process", "Process data", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		return &ToolOutput{Result: "processed", Success: true}, nil
	})
	reportTool := NewBaseTool("report_generate", "Generate report", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		return &ToolOutput{Result: "report", Success: true}, nil
	})

	cachedData := NewCachedTool(dataTool, cache, 5*time.Minute)
	cachedProcess := NewCachedTool(processTool, cache, 5*time.Minute)
	cachedReport := NewCachedTool(reportTool, cache, 5*time.Minute)

	t.Run("Cascade invalidation with dependencies", func(t *testing.T) {
		// Set up dependency chain: report -> process -> data
		cache.AddDependency("report_generate", "data_process")
		cache.AddDependency("data_process", "data_fetch")

		// Populate cache
		dataInput := &ToolInput{Args: map[string]interface{}{"id": 1}}
		processInput := &ToolInput{Args: map[string]interface{}{"id": 1}}
		reportInput := &ToolInput{Args: map[string]interface{}{"id": 1}}

		_, _ = cachedData.Invoke(ctx, dataInput)
		_, _ = cachedProcess.Invoke(ctx, processInput)
		_, _ = cachedReport.Invoke(ctx, reportInput)

		if cache.Size() != 3 {
			t.Fatalf("Expected 3 items in cache, got %d", cache.Size())
		}

		// Invalidate data_fetch, should cascade to process and report
		count, err := cache.InvalidateByTool(ctx, "data_fetch")
		if err != nil {
			t.Fatalf("InvalidateByTool failed: %v", err)
		}

		// Should invalidate: data_fetch (1) + data_process (1) + report_generate (1) = 3
		if count != 3 {
			t.Errorf("Expected 3 invalidations (cascade), got %d", count)
		}

		if cache.Size() != 0 {
			t.Errorf("Expected empty cache after cascade, got size %d", cache.Size())
		}
	})

	t.Run("Multiple dependents", func(t *testing.T) {
		_ = cache.Clear()

		// Create tools where multiple tools depend on one base tool
		baseTool := NewBaseTool("base_tool", "Base tool", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
			return &ToolOutput{Result: "base", Success: true}, nil
		})
		dependent1 := NewBaseTool("dependent1", "Dependent 1", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
			return &ToolOutput{Result: "dep1", Success: true}, nil
		})
		dependent2 := NewBaseTool("dependent2", "Dependent 2", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
			return &ToolOutput{Result: "dep2", Success: true}, nil
		})

		cachedBase := NewCachedTool(baseTool, cache, 5*time.Minute)
		cachedDep1 := NewCachedTool(dependent1, cache, 5*time.Minute)
		cachedDep2 := NewCachedTool(dependent2, cache, 5*time.Minute)

		// Set up dependencies: both dependent1 and dependent2 depend on base_tool
		cache.AddDependency("dependent1", "base_tool")
		cache.AddDependency("dependent2", "base_tool")

		// Populate cache
		input := &ToolInput{Args: map[string]interface{}{"id": 1}}
		_, _ = cachedBase.Invoke(ctx, input)
		_, _ = cachedDep1.Invoke(ctx, input)
		_, _ = cachedDep2.Invoke(ctx, input)

		if cache.Size() != 3 {
			t.Fatalf("Expected 3 items in cache, got %d", cache.Size())
		}

		// Invalidate base_tool, should invalidate both dependents
		count, err := cache.InvalidateByTool(ctx, "base_tool")
		if err != nil {
			t.Fatalf("InvalidateByTool failed: %v", err)
		}

		if count != 3 {
			t.Errorf("Expected 3 invalidations, got %d", count)
		}

		if cache.Size() != 0 {
			t.Errorf("Expected empty cache, got size %d", cache.Size())
		}
	})

	t.Run("Add and remove dependencies", func(t *testing.T) {
		_ = cache.Clear()

		cache.AddDependency("tool_a", "tool_b")
		cache.AddDependency("tool_a", "tool_b") // Duplicate, should not add twice

		// Check dependency was added
		cache.depMu.RLock()
		deps := cache.dependencies["tool_b"]
		cache.depMu.RUnlock()

		if len(deps) != 1 {
			t.Errorf("Expected 1 dependency, got %d", len(deps))
		}

		// Remove dependency
		cache.RemoveDependency("tool_a", "tool_b")

		cache.depMu.RLock()
		deps = cache.dependencies["tool_b"]
		cache.depMu.RUnlock()

		if len(deps) != 0 {
			t.Errorf("Expected 0 dependencies after removal, got %d", len(deps))
		}
	})
}

// TestVersioning tests version-based cache invalidation
func TestVersioning(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	tool := NewBaseTool("test_tool", "Test tool", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		return &ToolOutput{Result: "result", Success: true}, nil
	})
	cachedTool := NewCachedTool(tool, cache, 5*time.Minute)

	t.Run("Version increments on invalidation", func(t *testing.T) {
		initialVersion := cache.GetVersion()

		input := &ToolInput{Args: map[string]interface{}{"query": "test"}}
		_, _ = cachedTool.Invoke(ctx, input)

		// Invalidate by tool
		_, _ = cache.InvalidateByTool(ctx, "test_tool")
		v1 := cache.GetVersion()

		if v1 <= initialVersion {
			t.Errorf("Expected version to increment after invalidation")
		}

		// Invalidate by pattern
		input2 := &ToolInput{Args: map[string]interface{}{"query": "test2"}}
		_, _ = cachedTool.Invoke(ctx, input2)

		_, _ = cache.InvalidateByPattern(ctx, "test_tool:.*")
		v2 := cache.GetVersion()

		if v2 <= v1 {
			t.Errorf("Expected version to increment after pattern invalidation")
		}
	})

	t.Run("Old entries not accessible after version change", func(t *testing.T) {
		_ = cache.Clear()

		input := &ToolInput{Args: map[string]interface{}{"query": "test"}}
		key, _ := cachedTool.generateCacheKey(input)

		// Set a value
		output := &ToolOutput{Result: "old", Success: true}
		_ = cache.Set(ctx, key, output, 5*time.Minute)

		// Verify it's accessible
		retrieved, found := cache.Get(ctx, key)
		if !found {
			t.Fatal("Expected to find cached item")
		}
		if retrieved.Result != "old" {
			t.Errorf("Expected result 'old', got %v", retrieved.Result)
		}

		// Invalidate by incrementing version
		_, _ = cache.InvalidateByTool(ctx, "test_tool")

		// Now the old entry should not be accessible
		_, found = cache.Get(ctx, key)
		if found {
			t.Error("Expected old entry to be invalidated by version change")
		}
	})
}

// TestInvalidationStatistics tests that invalidation statistics are recorded correctly
func TestInvalidationStatistics(t *testing.T) {
	ctx := context.Background()
	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        100,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	tool := NewBaseTool("test_tool", "Test tool", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		return &ToolOutput{Result: "result", Success: true}, nil
	})
	cachedTool := NewCachedTool(tool, cache, 5*time.Minute)

	// Populate cache
	for i := 0; i < 5; i++ {
		input := &ToolInput{Args: map[string]interface{}{"id": i}}
		_, _ = cachedTool.Invoke(ctx, input)
	}

	stats := cache.GetStats()
	initialInvalidations := stats.Invalidations.Load()

	// Invalidate all
	count, _ := cache.InvalidateByTool(ctx, "test_tool")

	stats = cache.GetStats()
	finalInvalidations := stats.Invalidations.Load()

	if finalInvalidations-initialInvalidations != int64(count) {
		t.Errorf("Expected %d invalidations in stats, got %d", count, finalInvalidations-initialInvalidations)
	}
}

// TestExtractToolNameFromKey tests the helper function
func TestExtractToolNameFromKey(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "Valid key with colon",
			key:      "tool_name:abc123hash",
			expected: "tool_name",
		},
		{
			name:     "Key without colon",
			key:      "invalidkey",
			expected: "",
		},
		{
			name:     "Empty key",
			key:      "",
			expected: "",
		},
		{
			name:     "Multiple colons",
			key:      "tool:name:hash",
			expected: "tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractToolNameFromKey(tt.key)
			if result != tt.expected {
				t.Errorf("extractToolNameFromKey(%q) = %q, want %q", tt.key, result, tt.expected)
			}
		})
	}
}

// generateTestKey is a helper function for testing
func generateTestKey(cachedTool *CachedTool, input *ToolInput) string {
	key, _ := cachedTool.generateCacheKey(input)
	return key
}

// BenchmarkInvalidateByPattern benchmarks pattern-based invalidation
func BenchmarkInvalidateByPattern(b *testing.B) {
	ctx := context.Background()
	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        10000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	// Populate cache with many entries
	tool := NewBaseTool("test_tool", "Test", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		return &ToolOutput{Result: "result", Success: true}, nil
	})
	cachedTool := NewCachedTool(tool, cache, 5*time.Minute)

	for i := 0; i < 1000; i++ {
		input := &ToolInput{Args: map[string]interface{}{"id": i}}
		_, _ = cachedTool.Invoke(ctx, input)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.InvalidateByPattern(ctx, "test_tool:.*")
		// Repopulate for next iteration
		if i < b.N-1 {
			for j := 0; j < 1000; j++ {
				input := &ToolInput{Args: map[string]interface{}{"id": j}}
				_, _ = cachedTool.Invoke(ctx, input)
			}
		}
	}
}

// BenchmarkInvalidateByTool benchmarks tool-specific invalidation
func BenchmarkInvalidateByTool(b *testing.B) {
	ctx := context.Background()
	cache := NewMemoryToolCache(MemoryCacheConfig{
		Capacity:        10000,
		DefaultTTL:      5 * time.Minute,
		CleanupInterval: 10 * time.Minute,
	})
	defer cache.Close()

	// Populate cache
	tool := NewBaseTool("test_tool", "Test", `{}`, func(ctx context.Context, input *ToolInput) (*ToolOutput, error) {
		return &ToolOutput{Result: "result", Success: true}, nil
	})
	cachedTool := NewCachedTool(tool, cache, 5*time.Minute)

	for i := 0; i < 1000; i++ {
		input := &ToolInput{Args: map[string]interface{}{"id": i}}
		_, _ = cachedTool.Invoke(ctx, input)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.InvalidateByTool(ctx, "test_tool")
		// Repopulate for next iteration
		if i < b.N-1 {
			for j := 0; j < 1000; j++ {
				input := &ToolInput{Args: map[string]interface{}{"id": j}}
				_, _ = cachedTool.Invoke(ctx, input)
			}
		}
	}
}
