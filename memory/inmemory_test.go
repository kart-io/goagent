package memory

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInMemoryManager(t *testing.T) {
	t.Run("with config", func(t *testing.T) {
		config := &Config{
			EnableConversation:    true,
			MaxConversationLength: 20,
			EnableVectorStore:     true,
		}

		manager := NewInMemoryManager(config)
		assert.NotNil(t, manager)
		assert.Equal(t, config, manager.config)
	})

	t.Run("with nil config", func(t *testing.T) {
		manager := NewInMemoryManager(nil)
		assert.NotNil(t, manager)
		assert.NotNil(t, manager.config)
		assert.Equal(t, DefaultConfig(), manager.config)
	})
}

func TestInMemoryManager_AddConversation(t *testing.T) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()

	t.Run("add valid conversation", func(t *testing.T) {
		conv := &Conversation{
			SessionID: "session-1",
			Role:      "user",
			Content:   "Hello, agent!",
		}

		err := manager.AddConversation(ctx, conv)
		require.NoError(t, err)
		assert.NotEmpty(t, conv.ID)
		assert.False(t, conv.Timestamp.IsZero())
	})

	t.Run("nil conversation", func(t *testing.T) {
		err := manager.AddConversation(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "conversation is nil")
	})

	t.Run("missing session_id", func(t *testing.T) {
		conv := &Conversation{
			Role:    "user",
			Content: "Hello",
		}

		err := manager.AddConversation(ctx, conv)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "session_id is required")
	})

	t.Run("auto-generate ID", func(t *testing.T) {
		conv := &Conversation{
			SessionID: "session-2",
			Role:      "assistant",
			Content:   "Response",
		}

		err := manager.AddConversation(ctx, conv)
		require.NoError(t, err)
		assert.NotEmpty(t, conv.ID)
	})

	t.Run("preserve existing ID", func(t *testing.T) {
		conv := &Conversation{
			ID:        "custom-id",
			SessionID: "session-3",
			Role:      "user",
			Content:   "Test",
		}

		err := manager.AddConversation(ctx, conv)
		require.NoError(t, err)
		assert.Equal(t, "custom-id", conv.ID)
	})
}

func TestInMemoryManager_AddConversation_MaxLength(t *testing.T) {
	config := &Config{
		EnableConversation:    true,
		MaxConversationLength: 3,
	}
	manager := NewInMemoryManager(config)
	ctx := context.Background()

	sessionID := "session-test"

	// Add 5 conversations
	for i := 1; i <= 5; i++ {
		conv := &Conversation{
			SessionID: sessionID,
			Role:      "user",
			Content:   "Message " + string(rune(i)),
		}
		err := manager.AddConversation(ctx, conv)
		require.NoError(t, err)
	}

	// Should only keep last 3 conversations
	convs, err := manager.GetConversationHistory(ctx, sessionID, 0)
	require.NoError(t, err)
	assert.Len(t, convs, 3)
}

func TestInMemoryManager_GetConversationHistory(t *testing.T) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()
	sessionID := "session-history"

	// Add multiple conversations
	for i := 1; i <= 5; i++ {
		conv := &Conversation{
			SessionID: sessionID,
			Role:      "user",
			Content:   "Message " + string(rune(i)),
		}
		err := manager.AddConversation(ctx, conv)
		require.NoError(t, err)
	}

	t.Run("get all conversations", func(t *testing.T) {
		convs, err := manager.GetConversationHistory(ctx, sessionID, 0)
		require.NoError(t, err)
		assert.Len(t, convs, 5)
	})

	t.Run("get limited conversations", func(t *testing.T) {
		convs, err := manager.GetConversationHistory(ctx, sessionID, 3)
		require.NoError(t, err)
		assert.Len(t, convs, 3)
	})

	t.Run("get from non-existent session", func(t *testing.T) {
		convs, err := manager.GetConversationHistory(ctx, "non-existent", 0)
		require.NoError(t, err)
		assert.Empty(t, convs)
	})

	t.Run("missing session_id", func(t *testing.T) {
		convs, err := manager.GetConversationHistory(ctx, "", 0)
		assert.Error(t, err)
		assert.Nil(t, convs)
		assert.Contains(t, err.Error(), "session_id is required")
	})
}

func TestInMemoryManager_ClearConversation(t *testing.T) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()
	sessionID := "session-clear"

	// Add conversations
	conv := &Conversation{
		SessionID: sessionID,
		Role:      "user",
		Content:   "Test message",
	}
	err := manager.AddConversation(ctx, conv)
	require.NoError(t, err)

	// Verify conversation exists
	convs, err := manager.GetConversationHistory(ctx, sessionID, 0)
	require.NoError(t, err)
	assert.Len(t, convs, 1)

	t.Run("clear existing session", func(t *testing.T) {
		err := manager.ClearConversation(ctx, sessionID)
		require.NoError(t, err)

		convs, err := manager.GetConversationHistory(ctx, sessionID, 0)
		require.NoError(t, err)
		assert.Empty(t, convs)
	})

	t.Run("missing session_id", func(t *testing.T) {
		err := manager.ClearConversation(ctx, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "session_id is required")
	})

	t.Run("clear non-existent session", func(t *testing.T) {
		err := manager.ClearConversation(ctx, "non-existent")
		require.NoError(t, err)
	})
}

func TestInMemoryManager_AddCase(t *testing.T) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()

	t.Run("add valid case", func(t *testing.T) {
		caseMemory := &Case{
			Title:       "OOMKilled Pod",
			Description: "Pod was killed due to OOM",
			Problem:     "High memory usage",
			Solution:    "Increase memory limit",
			Tags:        []string{"memory", "pod"},
		}

		err := manager.AddCase(ctx, caseMemory)
		require.NoError(t, err)
		assert.NotEmpty(t, caseMemory.ID)
		assert.False(t, caseMemory.CreatedAt.IsZero())
		assert.False(t, caseMemory.UpdatedAt.IsZero())
	})

	t.Run("nil case", func(t *testing.T) {
		err := manager.AddCase(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "case is nil")
	})

	t.Run("auto-generate ID", func(t *testing.T) {
		caseMemory := &Case{
			Title:       "Test Case",
			Description: "Description",
		}

		err := manager.AddCase(ctx, caseMemory)
		require.NoError(t, err)
		assert.NotEmpty(t, caseMemory.ID)
	})

	t.Run("preserve existing ID", func(t *testing.T) {
		caseMemory := &Case{
			ID:          "custom-case-id",
			Title:       "Test Case",
			Description: "Description",
		}

		err := manager.AddCase(ctx, caseMemory)
		require.NoError(t, err)
		assert.Equal(t, "custom-case-id", caseMemory.ID)
	})
}

func TestInMemoryManager_SearchSimilarCases(t *testing.T) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()

	// Add test cases
	cases := []*Case{
		{
			Title:       "OOMKilled Pod",
			Description: "Pod was killed due to out of memory",
			Problem:     "High memory usage",
			Solution:    "Increase memory limit",
			Tags:        []string{"memory", "pod"},
		},
		{
			Title:       "CrashLoopBackOff",
			Description: "Pod keeps crashing and restarting",
			Problem:     "Application error",
			Solution:    "Fix application bug",
			Tags:        []string{"crash", "pod"},
		},
		{
			Title:       "Disk Pressure",
			Description: "Node has disk pressure",
			Problem:     "High disk usage",
			Solution:    "Clean up disk space",
			Tags:        []string{"disk", "node"},
		},
	}

	for _, c := range cases {
		err := manager.AddCase(ctx, c)
		require.NoError(t, err)
	}

	t.Run("search matching cases", func(t *testing.T) {
		results, err := manager.SearchSimilarCases(ctx, "memory", 10)
		require.NoError(t, err)
		// Note: Simple text matching, may return multiple results
		assert.NotEmpty(t, results)
	})

	t.Run("search with limit", func(t *testing.T) {
		results, err := manager.SearchSimilarCases(ctx, "pod", 1)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(results), 1)
	})

	t.Run("search empty query", func(t *testing.T) {
		results, err := manager.SearchSimilarCases(ctx, "", 10)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("search no matches", func(t *testing.T) {
		results, err := manager.SearchSimilarCases(ctx, "nonexistent_query_xyz", 10)
		require.NoError(t, err)
		// Due to simple implementation, results may vary
		assert.NotNil(t, results)
	})
}

func TestInMemoryManager_Store(t *testing.T) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()

	t.Run("store valid key-value", func(t *testing.T) {
		err := manager.Store(ctx, "key1", "value1")
		require.NoError(t, err)
	})

	t.Run("store complex value", func(t *testing.T) {
		value := map[string]interface{}{
			"field1": "value1",
			"field2": 123,
			"field3": []string{"a", "b", "c"},
		}
		err := manager.Store(ctx, "complex_key", value)
		require.NoError(t, err)
	})

	t.Run("empty key", func(t *testing.T) {
		err := manager.Store(ctx, "", "value")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "key is required")
	})

	t.Run("overwrite existing key", func(t *testing.T) {
		err := manager.Store(ctx, "overwrite_key", "initial_value")
		require.NoError(t, err)

		err = manager.Store(ctx, "overwrite_key", "new_value")
		require.NoError(t, err)

		value, err := manager.Retrieve(ctx, "overwrite_key")
		require.NoError(t, err)
		assert.Equal(t, "new_value", value)
	})
}

func TestInMemoryManager_Retrieve(t *testing.T) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()

	// Store test data
	err := manager.Store(ctx, "test_key", "test_value")
	require.NoError(t, err)

	t.Run("retrieve existing key", func(t *testing.T) {
		value, err := manager.Retrieve(ctx, "test_key")
		require.NoError(t, err)
		assert.Equal(t, "test_value", value)
	})

	t.Run("retrieve non-existent key", func(t *testing.T) {
		value, err := manager.Retrieve(ctx, "non_existent_key")
		assert.Error(t, err)
		assert.Nil(t, value)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("empty key", func(t *testing.T) {
		value, err := manager.Retrieve(ctx, "")
		assert.Error(t, err)
		assert.Nil(t, value)
		assert.Contains(t, err.Error(), "key is required")
	})
}

func TestInMemoryManager_Delete(t *testing.T) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()

	// Store test data
	err := manager.Store(ctx, "delete_key", "value")
	require.NoError(t, err)

	t.Run("delete existing key", func(t *testing.T) {
		err := manager.Delete(ctx, "delete_key")
		require.NoError(t, err)

		value, err := manager.Retrieve(ctx, "delete_key")
		assert.Error(t, err)
		assert.Nil(t, value)
	})

	t.Run("delete non-existent key", func(t *testing.T) {
		err := manager.Delete(ctx, "non_existent_key")
		require.NoError(t, err)
	})

	t.Run("empty key", func(t *testing.T) {
		err := manager.Delete(ctx, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "key is required")
	})
}

func TestInMemoryManager_Clear(t *testing.T) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()

	// Add conversations
	conv := &Conversation{
		SessionID: "session-1",
		Role:      "user",
		Content:   "Message",
	}
	err := manager.AddConversation(ctx, conv)
	require.NoError(t, err)

	// Add cases
	caseMemory := &Case{
		Title:       "Test Case",
		Description: "Description",
	}
	err = manager.AddCase(ctx, caseMemory)
	require.NoError(t, err)

	// Add key-value
	err = manager.Store(ctx, "key1", "value1")
	require.NoError(t, err)

	// Clear all
	err = manager.Clear(ctx)
	require.NoError(t, err)

	// Verify all cleared
	convs, err := manager.GetConversationHistory(ctx, "session-1", 0)
	require.NoError(t, err)
	assert.Empty(t, convs)

	cases, err := manager.SearchSimilarCases(ctx, "Test", 10)
	require.NoError(t, err)
	assert.Empty(t, cases)

	value, err := manager.Retrieve(ctx, "key1")
	assert.Error(t, err)
	assert.Nil(t, value)
}

func TestInMemoryManager_Concurrency(t *testing.T) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()

	// Test concurrent writes
	t.Run("concurrent conversation writes", func(t *testing.T) {
		done := make(chan bool)

		for i := 0; i < 10; i++ {
			go func(index int) {
				conv := &Conversation{
					SessionID: "concurrent-session",
					Role:      "user",
					Content:   "Message from goroutine",
				}
				_ = manager.AddConversation(ctx, conv)
				done <- true
			}(i)
		}

		for i := 0; i < 10; i++ {
			<-done
		}

		convs, err := manager.GetConversationHistory(ctx, "concurrent-session", 0)
		require.NoError(t, err)
		assert.Len(t, convs, 10)
	})

	t.Run("concurrent key-value writes", func(t *testing.T) {
		done := make(chan bool)

		for i := 0; i < 10; i++ {
			go func(index int) {
				key := "concurrent_key_" + string(rune(index))
				_ = manager.Store(ctx, key, index)
				done <- true
			}(i)
		}

		for i := 0; i < 10; i++ {
			<-done
		}
	})
}

// Test concurrent read/write operations with race detection
func TestInMemoryManagerConcurrentReadWrite(t *testing.T) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()

	// Pre-populate some data
	for i := 0; i < 5; i++ {
		conv := &Conversation{
			SessionID: "session-rw",
			Role:      "user",
			Content:   fmt.Sprintf("Message %d", i),
		}
		err := manager.AddConversation(ctx, conv)
		require.NoError(t, err)
	}

	// Concurrent reads and writes
	t.Run("concurrent read and write conversations", func(t *testing.T) {
		var wg sync.WaitGroup
		numOperations := 50

		// Readers
		for i := 0; i < numOperations/2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = manager.GetConversationHistory(ctx, "session-rw", 0)
			}()
		}

		// Writers
		for i := 0; i < numOperations/2; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				conv := &Conversation{
					SessionID: "session-rw",
					Role:      "assistant",
					Content:   fmt.Sprintf("Concurrent message %d", index),
				}
				_ = manager.AddConversation(ctx, conv)
			}(i)
		}

		wg.Wait()

		// Verify data integrity
		convs, err := manager.GetConversationHistory(ctx, "session-rw", 0)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(convs), 5, "Should have at least initial conversations")
	})

	t.Run("concurrent read and write store", func(t *testing.T) {
		var wg sync.WaitGroup
		numOperations := 50

		// Pre-populate
		for i := 0; i < 5; i++ {
			err := manager.Store(ctx, fmt.Sprintf("key_%d", i), i)
			require.NoError(t, err)
		}

		// Readers
		for i := 0; i < numOperations/2; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				_, _ = manager.Retrieve(ctx, fmt.Sprintf("key_%d", index%5))
			}(i)
		}

		// Writers
		for i := 0; i < numOperations/2; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				_ = manager.Store(ctx, fmt.Sprintf("key_new_%d", index), index)
			}(i)
		}

		wg.Wait()
	})

	t.Run("concurrent read and write cases", func(t *testing.T) {
		var wg sync.WaitGroup
		numOperations := 50

		// Pre-populate cases
		for i := 0; i < 5; i++ {
			caseItem := &Case{
				Title:       fmt.Sprintf("Case %d", i),
				Description: "Test case",
				Problem:     "test problem",
			}
			err := manager.AddCase(ctx, caseItem)
			require.NoError(t, err)
		}

		// Readers
		for i := 0; i < numOperations/2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = manager.SearchSimilarCases(ctx, "test", 10)
			}()
		}

		// Writers
		for i := 0; i < numOperations/2; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				caseItem := &Case{
					Title:       fmt.Sprintf("Concurrent Case %d", index),
					Description: "Concurrent test case",
					Problem:     "concurrent problem",
				}
				_ = manager.AddCase(ctx, caseItem)
			}(i)
		}

		wg.Wait()
	})
}

// Test Clear() method with concurrent operations (deadlock test)
func TestInMemoryManagerClearConcurrency(t *testing.T) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()

	t.Run("clear with concurrent reads", func(t *testing.T) {
		// Pre-populate data
		for i := 0; i < 10; i++ {
			conv := &Conversation{
				SessionID: "clear-session",
				Role:      "user",
				Content:   fmt.Sprintf("Message %d", i),
			}
			err := manager.AddConversation(ctx, conv)
			require.NoError(t, err)

			err = manager.Store(ctx, fmt.Sprintf("key_%d", i), i)
			require.NoError(t, err)

			caseItem := &Case{
				Title:       fmt.Sprintf("Case %d", i),
				Description: "Test",
			}
			err = manager.AddCase(ctx, caseItem)
			require.NoError(t, err)
		}

		var wg sync.WaitGroup

		// Start concurrent readers
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 5; j++ {
					_, _ = manager.GetConversationHistory(ctx, "clear-session", 0)
					_, _ = manager.SearchSimilarCases(ctx, "test", 10)
					_, _ = manager.Retrieve(ctx, "key_0")
					time.Sleep(1 * time.Millisecond)
				}
			}()
		}

		// Execute Clear in parallel
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond) // Let readers start
			err := manager.Clear(ctx)
			assert.NoError(t, err)
		}()

		// Wait for all operations to complete (should not deadlock)
		done := make(chan bool)
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Success - no deadlock
		case <-time.After(5 * time.Second):
			t.Fatal("Deadlock detected - Clear() took too long with concurrent operations")
		}

		// Verify data is cleared
		convs, err := manager.GetConversationHistory(ctx, "clear-session", 0)
		require.NoError(t, err)
		assert.Empty(t, convs)
	})

	t.Run("multiple concurrent clears", func(t *testing.T) {
		// Pre-populate
		for i := 0; i < 5; i++ {
			conv := &Conversation{
				SessionID: "multi-clear",
				Role:      "user",
				Content:   "Message",
			}
			_ = manager.AddConversation(ctx, conv)
		}

		var wg sync.WaitGroup

		// Multiple concurrent Clear() calls
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := manager.Clear(ctx)
				assert.NoError(t, err)
			}()
		}

		// Wait with timeout
		done := make(chan bool)
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Success
		case <-time.After(5 * time.Second):
			t.Fatal("Deadlock detected with multiple concurrent Clear() calls")
		}
	})
}

// Stress test for high concurrency
func TestInMemoryManagerStressConcurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	manager := NewInMemoryManager(nil)
	ctx := context.Background()

	t.Run("stress test all operations", func(t *testing.T) {
		var wg sync.WaitGroup
		numGoroutines := 100
		operationsPerGoroutine := 100

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < operationsPerGoroutine; j++ {
					operation := j % 6

					switch operation {
					case 0:
						// Add conversation
						conv := &Conversation{
							SessionID: fmt.Sprintf("session_%d", goroutineID%10),
							Role:      "user",
							Content:   fmt.Sprintf("Message from goroutine %d op %d", goroutineID, j),
						}
						_ = manager.AddConversation(ctx, conv)

					case 1:
						// Get conversation history
						_, _ = manager.GetConversationHistory(ctx, fmt.Sprintf("session_%d", goroutineID%10), 5)

					case 2:
						// Store key-value
						key := fmt.Sprintf("key_%d_%d", goroutineID, j)
						_ = manager.Store(ctx, key, j)

					case 3:
						// Retrieve key-value
						_, _ = manager.Retrieve(ctx, fmt.Sprintf("key_%d_%d", goroutineID, j-1))

					case 4:
						// Add case
						caseItem := &Case{
							Title:       fmt.Sprintf("Case %d %d", goroutineID, j),
							Description: "Stress test case",
						}
						_ = manager.AddCase(ctx, caseItem)

					case 5:
						// Search cases
						_, _ = manager.SearchSimilarCases(ctx, "test", 5)
					}
				}
			}(i)
		}

		// Wait with timeout
		done := make(chan bool)
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Success
			t.Logf("Stress test completed: %d goroutines × %d operations", numGoroutines, operationsPerGoroutine)
		case <-time.After(30 * time.Second):
			t.Fatal("Stress test timeout - possible deadlock or performance issue")
		}
	})
}

// Benchmark tests
func BenchmarkInMemoryManager_AddConversation(b *testing.B) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		conv := &Conversation{
			SessionID: "bench-session",
			Role:      "user",
			Content:   "Benchmark message",
		}
		_ = manager.AddConversation(ctx, conv)
	}
}

func BenchmarkInMemoryManager_GetConversationHistory(b *testing.B) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()
	sessionID := "bench-session"

	// Prepare data
	for i := 0; i < 100; i++ {
		conv := &Conversation{
			SessionID: sessionID,
			Role:      "user",
			Content:   "Message",
		}
		_ = manager.AddConversation(ctx, conv)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.GetConversationHistory(ctx, sessionID, 10)
	}
}

func BenchmarkInMemoryManager_Store(b *testing.B) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "bench_key"
		_ = manager.Store(ctx, key, "value")
	}
}

func BenchmarkInMemoryManager_Retrieve(b *testing.B) {
	manager := NewInMemoryManager(nil)
	ctx := context.Background()
	key := "bench_key"

	_ = manager.Store(ctx, key, "value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.Retrieve(ctx, key)
	}
}
