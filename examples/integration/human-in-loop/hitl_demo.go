package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kart-io/goagent/core"
)

func main() {
	fmt.Println("=== Human-in-the-Loop Pattern Demo ===")

	// Demo 1: Basic Interrupt and Approval
	demo1BasicInterrupt()

	fmt.Println()

	// Demo 2: Multiple Priority Levels
	demo2PriorityLevels()

	fmt.Println()

	// Demo 3: Human Input Collection
	demo3HumanInput()

	fmt.Println()

	// Demo 4: Conditional Interrupts with Rules
	demo4ConditionalInterrupts()

	fmt.Println()

	// Demo 5: Interrupt with State Persistence
	demo5StatePersistence()

	fmt.Println()

	// Demo 6: Interrupt Hooks and Monitoring
	demo6InterruptHooks()

	fmt.Println("\n=== Demo Complete ===")
}

// Demo 1: Basic interrupt requiring approval
func demo1BasicInterrupt() {
	fmt.Println("--- Demo 1: Basic Interrupt and Approval ---")

	manager := core.NewInterruptManager(nil)
	ctx := context.Background()

	// Create an interrupt requiring approval for a dangerous action
	interrupt := &core.Interrupt{
		Type:     core.InterruptTypeApproval,
		Priority: core.InterruptPriorityHigh,
		Message:  "Please approve: Delete production database",
		Context: map[string]interface{}{
			"database": "production_db",
			"action":   "delete",
			"risk":     "high",
		},
	}

	fmt.Println("Creating interrupt for dangerous operation...")
	fmt.Printf("  Type: %s\n", interrupt.Type)
	fmt.Printf("  Priority: %s\n", interrupt.Priority)
	fmt.Printf("  Message: %s\n", interrupt.Message)

	// Simulate human approval in background
	go func() {
		time.Sleep(500 * time.Millisecond)
		fmt.Println("\n[Human Reviewer] Reviewing interrupt...")
		fmt.Println("[Human Reviewer] Decision: APPROVED (with caution)")

		if err := manager.RespondToInterrupt(interrupt.ID, &core.InterruptResponse{
			Approved:    true,
			Reason:      "Approved by senior engineer for maintenance",
			RespondedBy: "admin@example.com",
		}); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
	}()

	// Wait for approval
	_, response, err := manager.CreateInterrupt(ctx, interrupt)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("\nReceived response:\n")
	fmt.Printf("  Approved: %v\n", response.Approved)
	fmt.Printf("  Reason: %s\n", response.Reason)
	fmt.Printf("  Responded by: %s\n", response.RespondedBy)

	if response.Approved {
		fmt.Println("  → Proceeding with database deletion")
	}
}

// Demo 2: Different priority levels and timeouts
func demo2PriorityLevels() {
	fmt.Println("--- Demo 2: Multiple Priority Levels ---")

	manager := core.NewInterruptManager(nil)
	ctx := context.Background()

	priorities := []core.InterruptPriority{
		core.InterruptPriorityCritical,
		core.InterruptPriorityHigh,
		core.InterruptPriorityMedium,
		core.InterruptPriorityLow,
	}

	// Create interrupts with different priorities
	for i, priority := range priorities {
		interrupt := &core.Interrupt{
			Type:     core.InterruptTypeApproval,
			Priority: priority,
			Message:  fmt.Sprintf("Action requiring %s priority approval", priority),
		}

		// Respond immediately to avoid blocking
		go func(idx int, intr *core.Interrupt) {
			time.Sleep(100 * time.Millisecond)
			_ = manager.RespondToInterrupt(intr.ID, &core.InterruptResponse{
				Approved:    true,
				Reason:      fmt.Sprintf("Auto-approved for demo %d", idx),
				RespondedBy: "system",
			})
		}(i, interrupt)

		// Create the interrupt
		_, _, _ = manager.CreateInterrupt(ctx, interrupt)

		fmt.Printf("Priority %s:\n", priority)
		fmt.Printf("  Message: %s\n", interrupt.Message)
		fmt.Printf("  Created: %v\n\n", interrupt.CreatedAt)
	}

	fmt.Printf("All %d priority levels demonstrated\n", len(priorities))
}

// Demo 3: Collecting human input
func demo3HumanInput() {
	fmt.Println("--- Demo 3: Human Input Collection ---")

	manager := core.NewInterruptManager(nil)
	ctx := context.Background()

	// Create interrupt requesting human input
	interrupt := &core.Interrupt{
		Type:     core.InterruptTypeInput,
		Priority: core.InterruptPriorityMedium,
		Message:  "Please provide configuration values",
		Context: map[string]interface{}{
			"required_fields": []string{"api_key", "region", "timeout"},
		},
	}

	fmt.Println("Requesting human input for configuration...")
	fmt.Printf("  Required fields: api_key, region, timeout\n")

	// Simulate human providing input
	go func() {
		time.Sleep(500 * time.Millisecond)
		fmt.Println("\n[Human Operator] Providing configuration...")

		_ = manager.RespondToInterrupt(interrupt.ID, &core.InterruptResponse{
			Approved:    true,
			RespondedBy: "operator@example.com",
			Input: map[string]interface{}{
				"api_key": "sk-1234567890abcdef",
				"region":  "us-west-2",
				"timeout": 30,
			},
		})
	}()

	_, response, err := manager.CreateInterrupt(ctx, interrupt)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("\nReceived configuration:\n")
	for key, value := range response.Input {
		if key == "api_key" {
			fmt.Printf("  %s: %s (redacted)\n", key, "sk-****")
		} else {
			fmt.Printf("  %s: %v\n", key, value)
		}
	}
}

// Demo 4: Conditional interrupts with rules
func demo4ConditionalInterrupts() {
	fmt.Println("--- Demo 4: Conditional Interrupts with Rules ---")

	manager := core.NewInterruptManager(nil)
	executor := core.NewInterruptableExecutor(manager, nil)
	ctx := context.Background()

	// Add rule: interrupt on high-cost operations
	executor.AddInterruptRule(core.InterruptRule{
		Name: "high_cost_check",
		Condition: func(ctx context.Context, state core.State) bool {
			cost, ok := state.Get("estimated_cost")
			if !ok {
				return false
			}
			return cost.(float64) > 1000.0
		},
		CreateInterrupt: func(ctx context.Context, state core.State) *core.Interrupt {
			cost, _ := state.Get("estimated_cost")
			return &core.Interrupt{
				Type:     core.InterruptTypeReview,
				Priority: core.InterruptPriorityHigh,
				Message:  fmt.Sprintf("High cost operation detected: $%.2f", cost.(float64)),
				Context: map[string]interface{}{
					"cost": cost,
				},
			}
		},
	})

	// Add rule: interrupt on sensitive data access
	executor.AddInterruptRule(core.InterruptRule{
		Name: "sensitive_data_check",
		Condition: func(ctx context.Context, state core.State) bool {
			sensitive, ok := state.Get("accessing_sensitive_data")
			return ok && sensitive.(bool)
		},
		CreateInterrupt: func(ctx context.Context, state core.State) *core.Interrupt {
			return &core.Interrupt{
				Type:     core.InterruptTypeApproval,
				Priority: core.InterruptPriorityCritical,
				Message:  "Approval required: Accessing sensitive customer data",
			}
		},
	})

	// Test state that triggers high cost rule
	state1 := core.NewAgentState()
	state1.Set("estimated_cost", 1500.0)
	state1.Set("operation", "data_migration")

	fmt.Println("Checking interrupts for high-cost operation...")
	interrupts, _ := executor.CheckInterrupts(ctx, state1)
	fmt.Printf("  Triggered interrupts: %d\n", len(interrupts))
	if len(interrupts) > 0 {
		fmt.Printf("  Message: %s\n", interrupts[0].Message)
	}

	// Test state that triggers sensitive data rule
	state2 := core.NewAgentState()
	state2.Set("accessing_sensitive_data", true)
	state2.Set("data_type", "customer_pii")

	fmt.Println("\nChecking interrupts for sensitive data access...")
	interrupts2, _ := executor.CheckInterrupts(ctx, state2)
	fmt.Printf("  Triggered interrupts: %d\n", len(interrupts2))
	if len(interrupts2) > 0 {
		fmt.Printf("  Message: %s\n", interrupts2[0].Message)
		fmt.Printf("  Priority: %s\n", interrupts2[0].Priority)
	}

	// Test normal state (no interrupts)
	state3 := core.NewAgentState()
	state3.Set("estimated_cost", 50.0)
	state3.Set("operation", "report_generation")

	fmt.Println("\nChecking interrupts for normal operation...")
	interrupts3, _ := executor.CheckInterrupts(ctx, state3)
	fmt.Printf("  Triggered interrupts: %d\n", len(interrupts3))
}

// Demo 5: Interrupt with state persistence
func demo5StatePersistence() {
	fmt.Println("--- Demo 5: Interrupt with State Persistence ---")

	checkpointer := core.NewInMemorySaver()
	manager := core.NewInterruptManager(checkpointer)
	ctx := context.Background()

	// Create state with important data
	state := core.NewAgentState()
	state.Set("workflow_step", 5)
	state.Set("processed_items", 1250)
	state.Set("current_batch", "batch_042")

	interrupt := &core.Interrupt{
		Type:     core.InterruptTypeDecision,
		Priority: core.InterruptPriorityMedium,
		Message:  "Decide: Continue processing or pause for maintenance?",
		State:    state,
		Context: map[string]interface{}{
			"progress": "62.5%",
		},
	}

	fmt.Println("Creating interrupt with state preservation...")
	fmt.Printf("  Workflow step: %v\n", state.Snapshot()["workflow_step"])
	fmt.Printf("  Processed items: %v\n", state.Snapshot()["processed_items"])

	// Simulate decision-making
	go func() {
		time.Sleep(500 * time.Millisecond)
		fmt.Println("\n[Decision Maker] Reviewing workflow state...")
		fmt.Println("[Decision Maker] Decision: CONTINUE processing")

		_ = manager.RespondToInterrupt(interrupt.ID, &core.InterruptResponse{
			Approved:    true,
			Reason:      "System stable, continue to completion",
			RespondedBy: "ops_manager",
		})
	}()

	_, _, err := manager.CreateInterrupt(ctx, interrupt)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Verify state was saved
	savedState, err := checkpointer.Load(ctx, fmt.Sprintf("interrupt_%s", interrupt.ID))
	if err == nil {
		fmt.Printf("\nState successfully persisted:\n")
		fmt.Printf("  Workflow step: %v\n", savedState.Snapshot()["workflow_step"])
		fmt.Printf("  Processed items: %v\n", savedState.Snapshot()["processed_items"])
		fmt.Println("  → Can resume from this point if needed")
	}
}

// Demo 6: Interrupt hooks for monitoring
func demo6InterruptHooks() {
	fmt.Println("--- Demo 6: Interrupt Hooks and Monitoring ---")

	manager := core.NewInterruptManager(nil)
	ctx := context.Background()

	createdCount := 0
	resolvedCount := 0

	// Set up monitoring hooks
	manager.OnInterruptCreated(func(i *core.Interrupt) {
		createdCount++
		fmt.Printf("[Monitor] Interrupt created: %s\n", i.ID)
		fmt.Printf("  Type: %s, Priority: %s\n", i.Type, i.Priority)
		fmt.Printf("  Total created: %d\n", createdCount)
	})

	manager.OnInterruptResolved(func(i *core.Interrupt, r *core.InterruptResponse) {
		resolvedCount++
		fmt.Printf("\n[Monitor] Interrupt resolved: %s\n", i.ID)
		fmt.Printf("  Approved: %v\n", r.Approved)
		fmt.Printf("  Response time: %v\n", r.RespondedAt.Sub(i.CreatedAt))
		fmt.Printf("  Total resolved: %d\n", resolvedCount)
	})

	// Create and resolve multiple interrupts
	interrupts := []*core.Interrupt{
		{
			Type:     core.InterruptTypeApproval,
			Priority: core.InterruptPriorityHigh,
			Message:  "Approval needed for deployment",
		},
		{
			Type:     core.InterruptTypeReview,
			Priority: core.InterruptPriorityMedium,
			Message:  "Code review required",
		},
	}

	for i, interrupt := range interrupts {
		// Respond in background
		go func(idx int, intr *core.Interrupt) {
			time.Sleep(300 * time.Millisecond)
			_ = manager.RespondToInterrupt(intr.ID, &core.InterruptResponse{
				Approved:    idx == 0, // Approve first, reject second
				Reason:      fmt.Sprintf("Response for interrupt %d", idx),
				RespondedBy: "reviewer",
			})
		}(i, interrupt)

		_, _, _ = manager.CreateInterrupt(ctx, interrupt)
	}

	fmt.Printf("\n[Summary]\n")
	fmt.Printf("  Total interrupts created: %d\n", createdCount)
	fmt.Printf("  Total interrupts resolved: %d\n", resolvedCount)
}
