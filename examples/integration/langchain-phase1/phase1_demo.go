package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/core/checkpoint"
	"github.com/kart-io/goagent/core/execution"
	"github.com/kart-io/goagent/core/state"
	"github.com/kart-io/goagent/store/memory"
)

// CustomContext represents application-specific context
type CustomContext struct {
	UserID   string
	UserName string
	Role     string
}

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║  LangChain-Inspired Phase 1 Features                      ║")
	fmt.Println("║  State, Runtime, Store, and Checkpointer                  ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// 1. State Management Demo
	fmt.Println("1. State Management Demo")
	fmt.Println("------------------------")
	stateDemo()

	// 2. Store Demo (Long-term Storage)
	fmt.Println("\n2. Store Demo (Long-term Storage)")
	fmt.Println("----------------------------------")
	storeDemo()

	// 3. Checkpointer Demo (Session Persistence)
	fmt.Println("\n3. Checkpointer Demo (Session Persistence)")
	fmt.Println("------------------------------------------")
	checkpointerDemo()

	// 4. Runtime Demo (Integrated Environment)
	fmt.Println("\n4. Runtime Demo (Integrated Environment)")
	fmt.Println("----------------------------------------")
	runtimeDemo()

	// 5. Complete Workflow Demo
	fmt.Println("\n5. Complete Workflow Demo")
	fmt.Println("-------------------------")
	completeWorkflowDemo()

	fmt.Println("\n╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║  All Examples Completed Successfully! 🎉                   ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
}

// stateDemo demonstrates State Management
func stateDemo() {
	// Create agent state
	state := state.NewAgentState()

	// Set various types of values
	state.Set("user_name", "Alice")
	state.Set("conversation_count", 5)
	state.Set("is_authenticated", true)
	state.Set("preferences", map[string]interface{}{
		"theme":    "dark",
		"language": "en",
	})

	// Get values with type-safe helpers
	userName, _ := state.GetString("user_name")
	count, _ := state.GetInt("conversation_count")
	isAuth, _ := state.GetBool("is_authenticated")

	fmt.Printf("  User: %s\n", userName)
	fmt.Printf("  Conversation Count: %d\n", count)
	fmt.Printf("  Authenticated: %v\n", isAuth)
	fmt.Printf("  State Size: %d keys\n", state.Size())

	// Batch update
	state.Update(map[string]interface{}{
		"last_activity": time.Now(),
		"session_id":    "sess-123",
	})

	// Take a snapshot
	snapshot := state.Snapshot()
	fmt.Printf("  Snapshot Keys: %v\n", state.Keys())
	fmt.Printf("  Snapshot Size: %d bytes\n", len(fmt.Sprintf("%v", snapshot)))

	// Clone state
	cloned := state.Clone()
	cloned.Set("cloned", true)
	fmt.Printf("  Original has 'cloned' key: %v\n", func() bool {
		_, ok := state.Get("cloned")
		return ok
	}())
}

// storeDemo demonstrates long-term storage
func storeDemo() {
	ctx := context.Background()
	store := memory.New()

	// Store user preferences
	userNamespace := []string{"users", "user123"}
	preferences := map[string]interface{}{
		"theme":          "dark",
		"notifications":  true,
		"language":       "en",
		"items_per_page": 20,
	}

	err := store.Put(ctx, userNamespace, "preferences", preferences)
	if err != nil {
		fmt.Printf("  Error storing preferences: %v\n", err)
		return
	}
	fmt.Println("  ✓ Stored user preferences")

	// Store conversation history
	convNamespace := []string{"conversations", "thread-456"}
	for i := 1; i <= 3; i++ {
		message := map[string]interface{}{
			"role":      "user",
			"content":   fmt.Sprintf("Message %d", i),
			"timestamp": time.Now(),
		}
		err = store.Put(ctx, convNamespace, fmt.Sprintf("msg-%d", i), message)
		if err != nil {
			fmt.Printf("  Error storing message: %v\n", err)
			return
		}
	}
	fmt.Println("  ✓ Stored conversation history")

	// Retrieve data
	storedPrefs, err := store.Get(ctx, userNamespace, "preferences")
	if err != nil {
		fmt.Printf("  Error retrieving preferences: %v\n", err)
		return
	}
	fmt.Printf("  Retrieved preferences: created=%v, updated=%v\n",
		storedPrefs.Created.Format("15:04:05"),
		storedPrefs.Updated.Format("15:04:05"))

	// List all conversation messages
	msgKeys, err := store.List(ctx, convNamespace)
	if err != nil {
		fmt.Printf("  Error listing messages: %v\n", err)
		return
	}
	fmt.Printf("  Conversation has %d messages: %v\n", len(msgKeys), msgKeys)

	// Search with metadata
	fmt.Printf("  Total items in store: %d\n", store.Size())
	fmt.Printf("  Active namespaces: %v\n", store.Namespaces())
}

// checkpointerDemo demonstrates session persistence
func checkpointerDemo() {
	ctx := context.Background()
	checkpointer := checkpoint.NewInMemorySaver()

	// Session 1: Initial conversation
	threadID1 := "thread-001"
	state1 := state.NewAgentState()
	state1.Set("user", "Alice")
	state1.Set("topic", "Kubernetes deployment")
	state1.Set("message_count", 3)

	err := checkpointer.Save(ctx, threadID1, state1)
	if err != nil {
		fmt.Printf("  Error saving checkpoint: %v\n", err)
		return
	}
	fmt.Printf("  ✓ Saved checkpoint for %s\n", threadID1)

	// Update the session
	state1.Set("message_count", 5)
	state1.Set("last_topic", "Service mesh")
	err = checkpointer.Save(ctx, threadID1, state1)
	if err != nil {
		fmt.Printf("  Error updating checkpoint: %v\n", err)
		return
	}
	fmt.Println("  ✓ Updated checkpoint")

	// Session 2: Different conversation
	threadID2 := "thread-002"
	state2 := state.NewAgentState()
	state2.Set("user", "Bob")
	state2.Set("topic", "Monitoring")
	err = checkpointer.Save(ctx, threadID2, state2)
	if err != nil {
		fmt.Printf("  Error saving checkpoint: %v\n", err)
		return
	}
	fmt.Printf("  ✓ Saved checkpoint for %s\n", threadID2)

	// List all checkpoints
	infos, err := checkpointer.List(ctx)
	if err != nil {
		fmt.Printf("  Error listing checkpoints: %v\n", err)
		return
	}
	fmt.Printf("  Active checkpoints: %d\n", len(infos))
	for _, info := range infos {
		fmt.Printf("    - %s (created: %v, updated: %v)\n",
			info.ThreadID,
			info.CreatedAt.Format("15:04:05"),
			info.UpdatedAt.Format("15:04:05"))
	}

	// Resume session 1
	loadedState, err := checkpointer.Load(ctx, threadID1)
	if err != nil {
		fmt.Printf("  Error loading checkpoint: %v\n", err)
		return
	}
	user, _ := loadedState.Get("user")
	msgCount, _ := loadedState.Get("message_count")
	fmt.Printf("  ✓ Resumed session for user=%v, messages=%v\n", user, msgCount)

	// Get history for thread 1
	history, err := checkpointer.GetHistory(ctx, threadID1)
	if err != nil {
		fmt.Printf("  Error getting history: %v\n", err)
		return
	}
	fmt.Printf("  Session history: %d previous states\n", len(history))
}

// runtimeDemo demonstrates the Runtime environment
func runtimeDemo() {
	ctx := context.Background()

	// Create components
	customCtx := CustomContext{
		UserID:   "user-123",
		UserName: "Alice",
		Role:     "admin",
	}
	state := state.NewAgentState()
	state.Set("initialized", true)
	store := memory.New()
	checkpointer := checkpoint.NewInMemorySaver()

	// Create runtime
	runtime := execution.NewRuntime(customCtx, state, store, checkpointer, "session-789")
	fmt.Printf("  Created runtime for session: %s\n", runtime.SessionID)
	fmt.Printf("  User: %s (Role: %s)\n", runtime.Context.UserName, runtime.Context.Role)

	// Use runtime with metadata
	runtime = runtime.WithMetadata("source", "demo")
	runtime = runtime.WithMetadata("version", "1.0")
	fmt.Printf("  Added metadata: %v\n", runtime.Metadata)

	// NOTE: Tool with runtime example temporarily disabled
	// The generic type system and tool runtime integration have changed
	// and need to be updated for the new architecture

	/*
		// Define a tool that uses runtime
		// Note: Use the concrete state.AgentState type from the state package
		getUserInfoTool := func(ctx context.Context, input string, rt interface{}) (string, error) {
			// Type assert to the runtime with concrete types
			runtime, ok := rt.(*execution.Runtime[CustomContext, *state.AgentState])
			if !ok {
				return "", fmt.Errorf("invalid runtime type")
			}

			// Access user context
			userName := runtime.Context.UserName
			userRole := runtime.Context.Role

			// Update state
			runtime.State.Set("last_tool_call", "getUserInfo")
			runtime.State.Set("last_input", input)

			// Store user activity
			activityNamespace := []string{"activity", runtime.Context.UserID}
			activity := map[string]interface{}{
				"tool":      "getUserInfo",
				"input":     input,
				"timestamp": time.Now(),
			}
			err := runtime.Store.Put(ctx, activityNamespace, "last_activity", activity)
			if err != nil {
				return "", err
			}

			return fmt.Sprintf("User: %s, Role: %s, Query: %s", userName, userRole, input), nil
		}

		// Create and execute tool
		tool := execution.NewToolWithRuntime("getUserInfo", "Gets user information", getUserInfoTool, runtime)
		result, err := tool.Execute(ctx, "account details")
		if err != nil {
			fmt.Printf("  Error executing tool: %v\n", err)
			return
		}
		fmt.Printf("  Tool result: %s\n", result)
	*/

	fmt.Println("  [Tool with runtime example temporarily disabled - needs architecture update]")

	// Verify state was updated
	lastTool, _ := state.Get("last_tool_call")
	fmt.Printf("  State updated: last_tool_call=%v\n", lastTool)

	// Verify activity was stored
	activityKeys, _ := store.List(ctx, []string{"activity", "user-123"})
	fmt.Printf("  Activities stored: %v\n", activityKeys)
}

// completeWorkflowDemo shows all components working together
func completeWorkflowDemo() {
	ctx := context.Background()

	// Setup infrastructure
	store := memory.New()
	checkpointer := checkpoint.NewInMemorySaver()

	fmt.Println("  Simulating multi-turn conversation...")

	// Turn 1: User starts conversation
	sessionID := "conversation-001"
	userCtx := CustomContext{
		UserID:   "user-456",
		UserName: "Bob",
		Role:     "developer",
	}

	state := state.NewAgentState()
	state.Set("turn", 1)
	state.Set("topic", "container orchestration")

	runtime := execution.NewRuntime(userCtx, state, store, checkpointer, sessionID)
	fmt.Printf("\n  Turn 1: User=%s, Topic=%v\n", userCtx.UserName, "container orchestration")

	// Save checkpoint after turn 1
	err := runtime.SaveState(ctx)
	if err != nil {
		fmt.Printf("  Error saving state: %v\n", err)
		return
	}
	fmt.Println("  ✓ Checkpoint saved")

	// Turn 2: Continue conversation
	state.Set("turn", 2)
	state.Set("follow_up", "service mesh details")
	runtime.SaveState(ctx)
	fmt.Println("\n  Turn 2: Follow-up question about service mesh")
	fmt.Println("  ✓ Checkpoint saved")

	// Turn 3: Request specific information
	state.Set("turn", 3)
	state.Set("request", "Istio configuration example")
	runtime.SaveState(ctx)
	fmt.Println("\n  Turn 3: Requesting Istio examples")
	fmt.Println("  ✓ Checkpoint saved")

	// Simulate interruption and resume
	fmt.Println("\n  --- Session Interrupted ---")
	time.Sleep(100 * time.Millisecond)

	fmt.Println("\n  --- Resuming Session ---")
	loadedState, err := checkpointer.Load(ctx, sessionID)
	if err != nil {
		fmt.Printf("  Error loading state: %v\n", err)
		return
	}

	turn, _ := loadedState.Get("turn")
	topic, _ := loadedState.Get("topic")
	request, _ := loadedState.Get("request")

	fmt.Printf("  Resumed at turn %v\n", turn)
	fmt.Printf("  Original topic: %v\n", topic)
	fmt.Printf("  Last request: %v\n", request)

	// Get conversation history
	history, err := checkpointer.GetHistory(ctx, sessionID)
	if err != nil {
		fmt.Printf("  Error getting history: %v\n", err)
		return
	}
	fmt.Printf("  Conversation history: %d previous states\n", len(history))

	// Store conversation summary
	summaryNamespace := []string{"summaries", userCtx.UserID}
	summary := map[string]interface{}{
		"session_id":   sessionID,
		"user":         userCtx.UserName,
		"topic":        topic,
		"turns":        turn,
		"last_request": request,
		"completed":    time.Now(),
	}
	err = store.Put(ctx, summaryNamespace, sessionID, summary)
	if err != nil {
		fmt.Printf("  Error storing summary: %v\n", err)
		return
	}
	fmt.Println("  ✓ Conversation summary stored")

	// Final statistics
	fmt.Println("\n  Final Statistics:")
	fmt.Printf("    - Checkpoints: %d\n", checkpointer.Size())
	fmt.Printf("    - Store items: %d\n", store.Size())
	fmt.Printf("    - Store namespaces: %v\n", store.Namespaces())
}
