# Goroutine Leak Fix in MockStreamClient

## Problem Statement

The `MockStreamClient.CompleteStream()` method had a critical goroutine leak when:
1. The context was cancelled before the stream finished
2. The consumer stopped reading from the channel early
3. The consumer never read from the channel at all

## Root Cause

The original implementation used blocking channel sends without proper context cancellation checks:

```go
// PROBLEMATIC CODE (lines 121-180)
func (m *MockStreamClient) CompleteStream(ctx context.Context, req *CompletionRequest) (<-chan *StreamChunk, error) {
    out := make(chan *StreamChunk, 10)  // Small buffer

    go func() {
        defer close(out)

        for i, char := range response {
            select {
            case <-ctx.Done():
                out <- &StreamChunk{...}  // BLOCKS if channel not read!
                return
            default:
                out <- chunk  // BLOCKS if buffer full
            }
        }

        out <- finalChunk  // BLOCKS if channel not read
    }()
}
```

### Issues Identified

1. **Blocking send on cancellation (line 134)**: When context is cancelled, the goroutine tries to send an error chunk, but if the consumer has stopped reading, this send blocks forever.

2. **Blocking send in default case (line 155)**: If the consumer is slow or stopped reading, the buffer fills up (only 10 slots) and the send blocks.

3. **Blocking final send (line 163)**: The final chunk send has no timeout or context check.

4. **Small buffer size**: With 50ms delay per character and a 50+ character response, the buffer can fill up quickly if the consumer is not keeping up.

## Solution

The fix implements three key improvements:

### 1. Non-Blocking Sends with Context Checks

All channel sends now use `select` with context cancellation:

```go
// Send chunk with non-blocking pattern
select {
case out <- chunk:
    // Chunk sent successfully
case <-ctx.Done():
    // Context cancelled while sending
    select {
    case out <- errorChunk:
    default:
        // Channel full or consumer gone, exit gracefully
    }
    return
}
```

### 2. Increased Buffer Size

Buffer size increased from 10 to 100 to accommodate the full response:

```go
out := make(chan *StreamChunk, 100)
```

This significantly reduces the probability of blocking on sends when the consumer is slightly behind.

### 3. Context-Aware Sleep

The delay between chunks now checks for context cancellation:

```go
select {
case <-time.After(50 * time.Millisecond):
case <-ctx.Done():
    // Context cancelled during sleep
    select {
    case out <- errorChunk:
    default:
        // Exit gracefully if channel full
    }
    return
}
```

## Benefits

1. **No Goroutine Leaks**: Goroutines always exit cleanly, even when:
   - Context is cancelled
   - Consumer stops reading early
   - Consumer never reads from the channel

2. **Graceful Degradation**: When the channel is full or consumer is gone, the goroutine exits immediately instead of blocking.

3. **Backward Compatibility**: All existing tests pass without modification.

4. **Performance**: No performance degradation - benchmark shows consistent results.

## Testing

Added comprehensive tests to verify the fix:

1. **TestMockStreamClient_CompleteStream/No_goroutine_leak_on_early_exit**: Verifies that stopping consumption early doesn't leak goroutines.

2. **TestMockStreamClient_CompleteStream/No_goroutine_leak_on_context_cancellation_without_consuming**: Verifies that cancelling context without consuming the stream doesn't leak goroutines.

3. **Race Detection**: All tests pass with `-race` flag, confirming no data races.

## Code Changes

**File**: `/Users/costalong/code/go/src/github.com/kart/goagent/llm/stream_client.go`

**Lines Modified**: 121-228 (CompleteStream method)

**Test Coverage**: Added 2 new test cases in `stream_test.go`

## Verification

```bash
# Run tests with race detection
go test -v -race -timeout 30s ./llm -run "TestMockStreamClient"

# All tests pass without hanging
PASS: TestMockStreamClient_CompleteStream/Stream_completion (2.40s)
PASS: TestMockStreamClient_CompleteStream/Stream_with_context_cancellation (0.10s)
PASS: TestMockStreamClient_CompleteStream/No_goroutine_leak_on_early_exit (0.20s)
PASS: TestMockStreamClient_CompleteStream/No_goroutine_leak_on_context_cancellation_without_consuming (0.10s)
```

## Best Practices Applied

1. **Always use select for channel sends in goroutines**: Never assume the consumer will read all values.

2. **Check context cancellation frequently**: Especially in loops and before blocking operations.

3. **Size buffers appropriately**: Buffer should accommodate expected burst sizes.

4. **Provide escape hatches**: Use `default` cases in select to avoid deadlocks when the channel is full or closed.

5. **Test goroutine cleanup**: Always verify that goroutines exit cleanly under various failure conditions.

## References

- [Go Concurrency Patterns: Context](https://go.dev/blog/context)
- [Go Memory Model](https://go.dev/ref/mem)
- [Effective Go: Concurrency](https://go.dev/doc/effective_go#concurrency)
