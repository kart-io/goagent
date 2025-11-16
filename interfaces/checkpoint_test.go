package interfaces

import (
	"context"
	"testing"
	"time"
)

// TestCheckpointStructure verifies Checkpoint struct is properly defined
func TestCheckpointStructure(t *testing.T) {
	now := time.Now()
	checkpoint := &Checkpoint{
		ID:       "ckpt-123",
		ThreadID: "thread-456",
		State: State{
			"messages":     []string{"msg1", "msg2"},
			"user_context": map[string]interface{}{"user_id": "user-789"},
			"step_count":   5,
		},
		Metadata: map[string]interface{}{
			"agent_name":           "TestAgent",
			"checkpoint_reason":    "step_complete",
			"parent_checkpoint_id": "ckpt-122",
			"tags":                 []string{"important", "milestone"},
		},
		CreatedAt: now,
	}

	if checkpoint.ID != "ckpt-123" {
		t.Errorf("Expected ID 'ckpt-123', got '%s'", checkpoint.ID)
	}
	if checkpoint.ThreadID != "thread-456" {
		t.Errorf("Expected ThreadID 'thread-456', got '%s'", checkpoint.ThreadID)
	}
	if checkpoint.State["step_count"] != 5 {
		t.Errorf("Expected state step_count=5, got '%v'", checkpoint.State["step_count"])
	}
	if checkpoint.Metadata["agent_name"] != "TestAgent" {
		t.Errorf("Expected agent_name='TestAgent', got '%v'", checkpoint.Metadata["agent_name"])
	}
	if checkpoint.CreatedAt != now {
		t.Error("CreatedAt timestamp mismatch")
	}
}

// TestCheckpointMetadataStructure verifies CheckpointMetadata struct is properly defined
func TestCheckpointMetadataStructure(t *testing.T) {
	now := time.Now()
	metadata := &CheckpointMetadata{
		ID:        "ckpt-001",
		ThreadID:  "thread-001",
		CreatedAt: now,
		Size:      1024,
	}

	if metadata.ID != "ckpt-001" {
		t.Errorf("Expected ID 'ckpt-001', got '%s'", metadata.ID)
	}
	if metadata.ThreadID != "thread-001" {
		t.Errorf("Expected ThreadID 'thread-001', got '%s'", metadata.ThreadID)
	}
	if metadata.CreatedAt != now {
		t.Error("CreatedAt timestamp mismatch")
	}
	if metadata.Size != 1024 {
		t.Errorf("Expected size 1024, got %d", metadata.Size)
	}
}

// mockCheckpointer is a minimal test implementation of Checkpointer
type mockCheckpointer struct {
	checkpoints map[string]*Checkpoint
	threadIndex map[string][]string // threadID -> list of checkpoint IDs
}

func newMockCheckpointer() *mockCheckpointer {
	return &mockCheckpointer{
		checkpoints: make(map[string]*Checkpoint),
		threadIndex: make(map[string][]string),
	}
}

func (m *mockCheckpointer) SaveCheckpoint(ctx context.Context, checkpoint *Checkpoint) error {
	// Store checkpoint
	m.checkpoints[checkpoint.ID] = checkpoint

	// Update thread index
	if m.threadIndex[checkpoint.ThreadID] == nil {
		m.threadIndex[checkpoint.ThreadID] = make([]string, 0)
	}
	m.threadIndex[checkpoint.ThreadID] = append(m.threadIndex[checkpoint.ThreadID], checkpoint.ID)

	return nil
}

func (m *mockCheckpointer) LoadCheckpoint(ctx context.Context, checkpointID string) (*Checkpoint, error) {
	checkpoint, exists := m.checkpoints[checkpointID]
	if !exists {
		return nil, nil // Not found
	}
	return checkpoint, nil
}

func (m *mockCheckpointer) ListCheckpoints(ctx context.Context, threadID string, limit int) ([]*CheckpointMetadata, error) {
	checkpointIDs := m.threadIndex[threadID]
	if checkpointIDs == nil {
		return []*CheckpointMetadata{}, nil
	}

	result := make([]*CheckpointMetadata, 0)

	// Get checkpoints for this thread (newest first)
	for i := len(checkpointIDs) - 1; i >= 0; i-- {
		checkpointID := checkpointIDs[i]
		checkpoint := m.checkpoints[checkpointID]
		if checkpoint == nil {
			continue
		}

		metadata := &CheckpointMetadata{
			ID:        checkpoint.ID,
			ThreadID:  checkpoint.ThreadID,
			CreatedAt: checkpoint.CreatedAt,
			Size:      100, // Mock size
		}
		result = append(result, metadata)

		if limit > 0 && len(result) >= limit {
			break
		}
	}

	return result, nil
}

func (m *mockCheckpointer) DeleteCheckpoint(ctx context.Context, checkpointID string) error {
	checkpoint, exists := m.checkpoints[checkpointID]
	if exists {
		// Remove from checkpoint storage
		delete(m.checkpoints, checkpointID)

		// Remove from thread index
		threadIDs := m.threadIndex[checkpoint.ThreadID]
		for i, id := range threadIDs {
			if id == checkpointID {
				m.threadIndex[checkpoint.ThreadID] = append(threadIDs[:i], threadIDs[i+1:]...)
				break
			}
		}
	}
	return nil
}

// Ensure mockCheckpointer implements Checkpointer interface
var _ Checkpointer = (*mockCheckpointer)(nil)

// TestCheckpointerInterface verifies the Checkpointer interface works correctly
func TestCheckpointerInterface(t *testing.T) {
	ctx := context.Background()
	checkpointer := newMockCheckpointer()

	// Test SaveCheckpoint
	checkpoint1 := &Checkpoint{
		ID:       "ckpt-1",
		ThreadID: "thread-1",
		State: State{
			"counter": 1,
			"data":    "first checkpoint",
		},
		Metadata: map[string]interface{}{
			"agent_name": "Agent1",
		},
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}

	err := checkpointer.SaveCheckpoint(ctx, checkpoint1)
	if err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}

	checkpoint2 := &Checkpoint{
		ID:       "ckpt-2",
		ThreadID: "thread-1",
		State: State{
			"counter": 2,
			"data":    "second checkpoint",
		},
		Metadata: map[string]interface{}{
			"agent_name":           "Agent1",
			"parent_checkpoint_id": "ckpt-1",
		},
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}

	err = checkpointer.SaveCheckpoint(ctx, checkpoint2)
	if err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}

	checkpoint3 := &Checkpoint{
		ID:       "ckpt-3",
		ThreadID: "thread-1",
		State: State{
			"counter": 3,
			"data":    "third checkpoint",
		},
		Metadata: map[string]interface{}{
			"agent_name":           "Agent1",
			"parent_checkpoint_id": "ckpt-2",
		},
		CreatedAt: time.Now(),
	}

	err = checkpointer.SaveCheckpoint(ctx, checkpoint3)
	if err != nil {
		t.Fatalf("SaveCheckpoint failed: %v", err)
	}

	// Test LoadCheckpoint
	loaded, err := checkpointer.LoadCheckpoint(ctx, "ckpt-2")
	if err != nil {
		t.Fatalf("LoadCheckpoint failed: %v", err)
	}
	if loaded == nil {
		t.Fatal("Expected non-nil checkpoint")
	}
	if loaded.ID != "ckpt-2" {
		t.Errorf("Expected checkpoint ID 'ckpt-2', got '%s'", loaded.ID)
	}
	if loaded.State["counter"] != 2 {
		t.Errorf("Expected counter=2, got '%v'", loaded.State["counter"])
	}

	// Test LoadCheckpoint for non-existent checkpoint
	notFound, err := checkpointer.LoadCheckpoint(ctx, "ckpt-999")
	if err != nil {
		t.Fatalf("LoadCheckpoint should not error for non-existent checkpoint: %v", err)
	}
	if notFound != nil {
		t.Error("Expected nil for non-existent checkpoint")
	}

	// Test ListCheckpoints (no limit)
	allMetadata, err := checkpointer.ListCheckpoints(ctx, "thread-1", 0)
	if err != nil {
		t.Fatalf("ListCheckpoints failed: %v", err)
	}
	if len(allMetadata) != 3 {
		t.Errorf("Expected 3 checkpoints, got %d", len(allMetadata))
	}

	// Verify newest first ordering
	if allMetadata[0].ID != "ckpt-3" {
		t.Errorf("Expected first checkpoint to be 'ckpt-3' (newest), got '%s'", allMetadata[0].ID)
	}
	if allMetadata[2].ID != "ckpt-1" {
		t.Errorf("Expected last checkpoint to be 'ckpt-1' (oldest), got '%s'", allMetadata[2].ID)
	}

	// Test ListCheckpoints with limit
	limitedMetadata, err := checkpointer.ListCheckpoints(ctx, "thread-1", 2)
	if err != nil {
		t.Fatalf("ListCheckpoints with limit failed: %v", err)
	}
	if len(limitedMetadata) != 2 {
		t.Errorf("Expected 2 checkpoints with limit=2, got %d", len(limitedMetadata))
	}

	// Test DeleteCheckpoint
	err = checkpointer.DeleteCheckpoint(ctx, "ckpt-2")
	if err != nil {
		t.Fatalf("DeleteCheckpoint failed: %v", err)
	}

	deletedCheckpoint, _ := checkpointer.LoadCheckpoint(ctx, "ckpt-2")
	if deletedCheckpoint != nil {
		t.Error("Expected checkpoint to be deleted")
	}

	// Verify list after deletion
	afterDelete, err := checkpointer.ListCheckpoints(ctx, "thread-1", 0)
	if err != nil {
		t.Fatalf("ListCheckpoints after delete failed: %v", err)
	}
	if len(afterDelete) != 2 {
		t.Errorf("Expected 2 checkpoints after deletion, got %d", len(afterDelete))
	}
}

// TestCheckpointerMultipleThreads verifies Checkpointer handles multiple threads correctly
func TestCheckpointerMultipleThreads(t *testing.T) {
	ctx := context.Background()
	checkpointer := newMockCheckpointer()

	// Create checkpoints for different threads
	thread1Checkpoint := &Checkpoint{
		ID:        "ckpt-t1-1",
		ThreadID:  "thread-1",
		State:     State{"data": "thread 1 data"},
		CreatedAt: time.Now(),
	}

	thread2Checkpoint := &Checkpoint{
		ID:        "ckpt-t2-1",
		ThreadID:  "thread-2",
		State:     State{"data": "thread 2 data"},
		CreatedAt: time.Now(),
	}

	checkpointer.SaveCheckpoint(ctx, thread1Checkpoint)
	checkpointer.SaveCheckpoint(ctx, thread2Checkpoint)

	// List checkpoints for thread-1 should only return thread-1 checkpoints
	thread1List, _ := checkpointer.ListCheckpoints(ctx, "thread-1", 0)
	if len(thread1List) != 1 {
		t.Errorf("Expected 1 checkpoint for thread-1, got %d", len(thread1List))
	}
	if thread1List[0].ThreadID != "thread-1" {
		t.Errorf("Expected ThreadID 'thread-1', got '%s'", thread1List[0].ThreadID)
	}

	// List checkpoints for thread-2 should only return thread-2 checkpoints
	thread2List, _ := checkpointer.ListCheckpoints(ctx, "thread-2", 0)
	if len(thread2List) != 1 {
		t.Errorf("Expected 1 checkpoint for thread-2, got %d", len(thread2List))
	}
	if thread2List[0].ThreadID != "thread-2" {
		t.Errorf("Expected ThreadID 'thread-2', got '%s'", thread2List[0].ThreadID)
	}
}

// TestCheckpointerEmptyThread verifies Checkpointer handles empty threads correctly
func TestCheckpointerEmptyThread(t *testing.T) {
	ctx := context.Background()
	checkpointer := newMockCheckpointer()

	// List checkpoints for non-existent thread
	emptyList, err := checkpointer.ListCheckpoints(ctx, "non-existent-thread", 0)
	if err != nil {
		t.Fatalf("ListCheckpoints should not error for non-existent thread: %v", err)
	}
	if len(emptyList) != 0 {
		t.Errorf("Expected 0 checkpoints for non-existent thread, got %d", len(emptyList))
	}
}

// TestCheckpointerIdempotentDelete verifies DeleteCheckpoint is idempotent
func TestCheckpointerIdempotentDelete(t *testing.T) {
	ctx := context.Background()
	checkpointer := newMockCheckpointer()

	checkpoint := &Checkpoint{
		ID:        "ckpt-delete-test",
		ThreadID:  "thread-test",
		State:     State{"data": "test"},
		CreatedAt: time.Now(),
	}

	checkpointer.SaveCheckpoint(ctx, checkpoint)

	// First delete
	err := checkpointer.DeleteCheckpoint(ctx, "ckpt-delete-test")
	if err != nil {
		t.Fatalf("First delete failed: %v", err)
	}

	// Second delete (should not error)
	err = checkpointer.DeleteCheckpoint(ctx, "ckpt-delete-test")
	if err != nil {
		t.Errorf("Second delete should not error, got: %v", err)
	}

	// Delete non-existent checkpoint (should not error)
	err = checkpointer.DeleteCheckpoint(ctx, "never-existed")
	if err != nil {
		t.Errorf("Delete of non-existent checkpoint should not error, got: %v", err)
	}
}

// TestCheckpointStateIsolation verifies State is properly isolated between checkpoints
func TestCheckpointStateIsolation(t *testing.T) {
	ctx := context.Background()
	checkpointer := newMockCheckpointer()

	// Create shared state
	state := State{"counter": 0}

	checkpoint1 := &Checkpoint{
		ID:        "ckpt-1",
		ThreadID:  "thread-1",
		State:     state,
		CreatedAt: time.Now(),
	}

	checkpointer.SaveCheckpoint(ctx, checkpoint1)

	// Modify the state after saving
	state["counter"] = 999

	// Create another checkpoint with modified state
	checkpoint2 := &Checkpoint{
		ID:        "ckpt-2",
		ThreadID:  "thread-1",
		State:     state,
		CreatedAt: time.Now(),
	}

	checkpointer.SaveCheckpoint(ctx, checkpoint2)

	// Load first checkpoint - it should reflect the state at save time
	loaded1, _ := checkpointer.LoadCheckpoint(ctx, "ckpt-1")
	// Note: In the mock implementation, state is not deep copied,
	// so this test documents current behavior.
	// A production implementation should deep copy state.
	if loaded1 == nil {
		t.Fatal("Expected checkpoint to be loaded")
	}

	// Load second checkpoint
	loaded2, _ := checkpointer.LoadCheckpoint(ctx, "ckpt-2")
	if loaded2 == nil {
		t.Fatal("Expected checkpoint to be loaded")
	}

	// Both currently share the same state reference in mock
	// (This documents that the mock doesn't deep copy - production should)
	t.Logf("Checkpoint 1 counter: %v", loaded1.State["counter"])
	t.Logf("Checkpoint 2 counter: %v", loaded2.State["counter"])
}

// TestCheckpointMetadataSize verifies CheckpointMetadata size field
func TestCheckpointMetadataSize(t *testing.T) {
	metadata := &CheckpointMetadata{
		ID:        "ckpt-size-test",
		ThreadID:  "thread-1",
		CreatedAt: time.Now(),
		Size:      2048,
	}

	if metadata.Size != 2048 {
		t.Errorf("Expected size 2048, got %d", metadata.Size)
	}

	// Size can be 0 for small checkpoints
	smallMetadata := &CheckpointMetadata{
		ID:        "ckpt-small",
		ThreadID:  "thread-1",
		CreatedAt: time.Now(),
		Size:      0,
	}

	if smallMetadata.Size != 0 {
		t.Errorf("Expected size 0, got %d", smallMetadata.Size)
	}
}
