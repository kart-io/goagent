package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/execution"
	"github.com/kart-io/goagent/core/state"
	"github.com/kart-io/goagent/store"
	"github.com/kart-io/goagent/store/memory"
	"github.com/kart-io/goagent/testing/mocks"
)

// TestContext provides a complete test environment
type TestContext struct {
	Ctx          context.Context
	Cancel       context.CancelFunc
	State        *state.AgentState
	Store        store.Store
	Checkpointer checkpoint.Checkpointer
	MockLLM      *mocks.MockLLMClient
	MockTools    map[string]*mocks.MockTool
	T            *testing.T
}

// NewTestContext creates a new test context
func NewTestContext(t *testing.T) *TestContext {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	tc := &TestContext{
		Ctx:          ctx,
		Cancel:       cancel,
		State:        state.NewAgentState(),
		Store:        memory.New(),
		Checkpointer: checkpoint.NewInMemorySaver(),
		MockLLM:      mocks.NewMockLLMClient(),
		MockTools:    make(map[string]*mocks.MockTool),
		T:            t,
	}

	// Register cleanup
	t.Cleanup(func() {
		tc.Cancel()
	})

	return tc
}

// AddMockTool adds a mock tool to the context
func (tc *TestContext) AddMockTool(name, description string) *mocks.MockTool {
	tool := mocks.NewMockTool(name, description)
	tc.MockTools[name] = tool
	return tool
}

// CreateRuntime creates a test runtime
func (tc *TestContext) CreateRuntime(sessionID string) *execution.Runtime[TestAppContext, *state.AgentState] {
	appCtx := TestAppContext{
		UserID:   "test-user",
		UserName: "Test User",
	}

	return execution.NewRuntime(appCtx, tc.State, tc.Store, tc.Checkpointer, sessionID)
}

// TestAppContext is a simple application context for testing
type TestAppContext struct {
	UserID   string
	UserName string
	Metadata map[string]interface{}
}

// AssertNoError asserts that an error is nil
func AssertNoError(t *testing.T, err error, msgAndArgs ...interface{}) {
	t.Helper()
	if err != nil {
		msg := "Unexpected error"
		if len(msgAndArgs) > 0 {
			msg = fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...)
		}
		t.Fatalf("%s: %v", msg, err)
	}
}

// AssertError asserts that an error is not nil
func AssertError(t *testing.T, err error, msgAndArgs ...interface{}) {
	t.Helper()
	if err == nil {
		msg := "Expected error but got nil"
		if len(msgAndArgs) > 0 {
			msg = fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...)
		}
		t.Fatal(msg)
	}
}

// AssertEqual asserts that two values are equal
func AssertEqual(t *testing.T, expected, actual interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if expected != actual {
		msg := "Values not equal"
		if len(msgAndArgs) > 0 {
			msg = fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...)
		}
		t.Fatalf("%s: expected=%v, actual=%v", msg, expected, actual)
	}
}

// AssertNotNil asserts that a value is not nil
func AssertNotNil(t *testing.T, value interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if value == nil {
		msg := "Expected non-nil value"
		if len(msgAndArgs) > 0 {
			msg = fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...)
		}
		t.Fatal(msg)
	}
}

// AssertNil asserts that a value is nil
func AssertNil(t *testing.T, value interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	if value != nil {
		msg := fmt.Sprintf("Expected nil but got %v", value)
		if len(msgAndArgs) > 0 {
			msg = fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...)
		}
		t.Fatal(msg)
	}
}

// AssertTrue asserts that a value is true
func AssertTrue(t *testing.T, value bool, msgAndArgs ...interface{}) {
	t.Helper()
	if !value {
		msg := "Expected true but got false"
		if len(msgAndArgs) > 0 {
			msg = fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...)
		}
		t.Fatal(msg)
	}
}

// AssertFalse asserts that a value is false
func AssertFalse(t *testing.T, value bool, msgAndArgs ...interface{}) {
	t.Helper()
	if value {
		msg := "Expected false but got true"
		if len(msgAndArgs) > 0 {
			msg = fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...)
		}
		t.Fatal(msg)
	}
}

// AssertContains asserts that a string contains a substring
func AssertContains(t *testing.T, haystack, needle string, msgAndArgs ...interface{}) {
	t.Helper()
	if !contains(haystack, needle) {
		msg := fmt.Sprintf("String does not contain substring: haystack=%q, needle=%q", haystack, needle)
		if len(msgAndArgs) > 0 {
			msg = fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...)
		}
		t.Fatal(msg)
	}
}

// AssertEventually asserts that a condition is eventually true
func AssertEventually(t *testing.T, condition func() bool, timeout time.Duration, msgAndArgs ...interface{}) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	msg := fmt.Sprintf("Condition not met within %v", timeout)
	if len(msgAndArgs) > 0 {
		msg = fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...)
	}
	t.Fatal(msg)
}

// WaitForCondition waits for a condition to be true
func WaitForCondition(ctx context.Context, condition func() bool, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if condition() {
				return nil
			}
		}
	}
}

// RunParallel runs test functions in parallel
func RunParallel(t *testing.T, tests map[string]func(t *testing.T)) {
	for name, test := range tests {
		name := name
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			test(t)
		})
	}
}

// contains checks if a string contains a substring
func contains(haystack, needle string) bool {
	return len(needle) == 0 || len(haystack) >= len(needle) && (haystack == needle || haystack[:len(needle)] == needle || haystack[len(haystack)-len(needle):] == needle || containsMiddle(haystack, needle))
}

// containsMiddle checks if needle is in the middle of haystack
func containsMiddle(haystack, needle string) bool {
	for i := 1; i < len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// CreateTestAgent creates a test agent with mocks
type TestAgentConfig struct {
	Name         string
	SystemPrompt string
	Tools        []string
	MaxIter      int
}

// DefaultTestAgentConfig returns default test agent config
func DefaultTestAgentConfig() TestAgentConfig {
	return TestAgentConfig{
		Name:         "test-agent",
		SystemPrompt: "You are a test agent",
		Tools:        []string{"calculator", "search"},
		MaxIter:      5,
	}
}
