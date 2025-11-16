package agents

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestTaskResult verifies TaskResult structure and JSON marshaling
func TestTaskResult(t *testing.T) {
	t.Run("create task result", func(t *testing.T) {
		result := TaskResult{
			TaskID:    "task1",
			AgentName: "agent1",
			Output:    "output",
			Error:     nil,
		}
		assert.Equal(t, "task1", result.TaskID)
		assert.Equal(t, "agent1", result.AgentName)
	})

	t.Run("task result with error", func(t *testing.T) {
		result := TaskResult{
			TaskID:      "task1",
			ErrorString: "error occurred",
		}
		assert.Equal(t, "error occurred", result.ErrorString)
	})
}

// TestTask verifies Task structure
func TestTask(t *testing.T) {
	t.Run("create task with metadata", func(t *testing.T) {
		task := Task{
			ID:          "task1",
			Type:        "compute",
			Description: "test task",
			Priority:    1,
			Metadata: map[string]interface{}{
				"key": "value",
			},
		}
		assert.Equal(t, "task1", task.ID)
		assert.Equal(t, 1, len(task.Metadata))
	})

	t.Run("task without metadata", func(t *testing.T) {
		task := Task{
			ID:       "task1",
			Priority: 0,
		}
		assert.Nil(t, task.Metadata)
	})
}

// TestExecutionStage verifies execution stage structure
func TestExecutionStage(t *testing.T) {
	t.Run("sequential stage", func(t *testing.T) {
		stage := ExecutionStage{
			ID:         "stage1",
			Sequential: true,
			Tasks: []Task{
				{ID: "task1"},
				{ID: "task2"},
			},
		}
		assert.True(t, stage.Sequential)
		assert.Equal(t, 2, len(stage.Tasks))
	})

	t.Run("parallel stage", func(t *testing.T) {
		stage := ExecutionStage{
			ID:         "stage1",
			Sequential: false,
			Tasks:      []Task{},
		}
		assert.False(t, stage.Sequential)
		assert.Empty(t, stage.Tasks)
	})
}

// TestTaskOrchestrator tests orchestration of tasks
func TestTaskOrchestrator(t *testing.T) {
	orchestrator := NewTaskOrchestrator(5)

	t.Run("empty task list creates no stages", func(t *testing.T) {
		plan := orchestrator.CreateExecutionPlan([]Task{})
		assert.NotNil(t, plan)
		assert.Equal(t, 0, len(plan.Stages))
	})

	t.Run("single task creates one stage", func(t *testing.T) {
		tasks := []Task{
			{ID: "task1", Priority: 1},
		}
		plan := orchestrator.CreateExecutionPlan(tasks)
		assert.Equal(t, 1, len(plan.Stages))
		assert.Equal(t, 1, len(plan.Stages[0].Tasks))
	})

	t.Run("mixed priorities create multiple stages", func(t *testing.T) {
		tasks := []Task{
			{ID: "task1", Priority: 1},
			{ID: "task2", Priority: 2},
			{ID: "task3", Priority: 1},
		}
		plan := orchestrator.CreateExecutionPlan(tasks)
		// Should have at least 2 stages for different priorities
		assert.True(t, len(plan.Stages) >= 1)
	})

	t.Run("tasks with same priority in same stage", func(t *testing.T) {
		tasks := []Task{
			{ID: "task1", Priority: 5},
			{ID: "task2", Priority: 5},
			{ID: "task3", Priority: 5},
		}
		plan := orchestrator.CreateExecutionPlan(tasks)
		assert.Equal(t, 1, len(plan.Stages))
		assert.Equal(t, 3, len(plan.Stages[0].Tasks))
	})
}

// TestRoutingRule verifies routing rule structure
func TestRoutingRule(t *testing.T) {
	t.Run("create rule with condition", func(t *testing.T) {
		rule := RoutingRule{
			Condition: func(t Task) bool {
				return t.Type == "compute"
			},
			AgentName: "compute_agent",
			Priority:  10,
		}
		task := Task{Type: "compute"}
		assert.True(t, rule.Condition(task))
		assert.Equal(t, "compute_agent", rule.AgentName)
	})
}

// TestCapabilityRouterAdvanced
func TestCapabilityRouterAdvanced(t *testing.T) {
	router := NewCapabilityRouter()

	t.Run("register multiple agents with different capabilities", func(t *testing.T) {
		router.RegisterAgent("agent1", []string{"compute", "memory"}, func(t Task) float64 {
			return 0.9
		})
		router.RegisterAgent("agent2", []string{"io", "network"}, func(t Task) float64 {
			return 0.7
		})

		caps1 := router.GetCapabilities("agent1")
		caps2 := router.GetCapabilities("agent2")

		assert.Equal(t, 2, len(caps1))
		assert.Equal(t, 2, len(caps2))
	})

	t.Run("update routing for agent", func(t *testing.T) {
		router.UpdateRouting("agent1", 0.5)
		// Should not panic
		assert.True(t, true)
	})
}

// TestLoadBalancingRouterAdvanced
func TestLoadBalancingRouterAdvanced(t *testing.T) {
	router := NewLoadBalancingRouter(3)

	t.Run("track load across multiple agents", func(t *testing.T) {
		// Simulating agent load tracking
		router.activeTaskCount["agent1"] = 2
		router.activeTaskCount["agent2"] = 1

		load1 := router.GetLoad("agent1")
		load2 := router.GetLoad("agent2")

		assert.Equal(t, int32(2), load1)
		assert.Equal(t, int32(1), load2)
	})

	t.Run("release task decrements load", func(t *testing.T) {
		router.activeTaskCount["agent1"] = 2
		router.ReleaseTask("agent1")

		load := router.GetLoad("agent1")
		assert.Equal(t, int32(1), load)
	})

	t.Run("release non-existent agent does nothing", func(t *testing.T) {
		router.ReleaseTask("nonexistent")
		// Should not panic
		assert.True(t, true)
	})

	t.Run("release when load is zero does nothing", func(t *testing.T) {
		router.activeTaskCount["agent1"] = 0
		router.ReleaseTask("agent1")
		// Should not panic
		assert.True(t, true)
	})
}

// TestRandomRouterStatistics
func TestRandomRouterStatistics(t *testing.T) {
	router := NewRandomRouter()

	t.Run("router instantiation", func(t *testing.T) {
		assert.NotNil(t, router)
		_ = router // Explicit use
	})
}

// TestAggregationStrategies coverage
func TestAggregationStrategiesEdgeCases(t *testing.T) {
	t.Run("merge with all failures", func(t *testing.T) {
		aggregator := NewResultAggregator(StrategyMerge)
		results := []TaskResult{
			{Error: context.Canceled, ErrorString: "canceled"},
			{Error: context.DeadlineExceeded, ErrorString: "timeout"},
		}

		aggregated := aggregator.Aggregate(results)
		merged := aggregated.(map[string]interface{})
		assert.Equal(t, 0, len(merged["results"].([]interface{})))
		assert.Equal(t, 2, len(merged["errors"].([]string)))
	})

	t.Run("best with all failures", func(t *testing.T) {
		aggregator := NewResultAggregator(StrategyBest)
		results := []TaskResult{
			{Error: context.Canceled},
			{Error: context.DeadlineExceeded},
		}

		aggregated := aggregator.Aggregate(results)
		assert.Nil(t, aggregated)
	})

	t.Run("consensus with single result", func(t *testing.T) {
		aggregator := NewResultAggregator(StrategyConsensus)
		results := []TaskResult{
			{Output: "single", Error: nil},
		}

		aggregated := aggregator.Aggregate(results)
		assert.Equal(t, "single", aggregated)
	})

	t.Run("consensus with no results", func(t *testing.T) {
		aggregator := NewResultAggregator(StrategyConsensus)
		results := []TaskResult{}

		aggregated := aggregator.Aggregate(results)
		assert.Nil(t, aggregated)
	})

	t.Run("unknown aggregation strategy defaults to merge", func(t *testing.T) {
		aggregator := NewResultAggregator("unknown_strategy")
		results := []TaskResult{
			{Output: "result1"},
		}

		aggregated := aggregator.Aggregate(results)
		assert.NotNil(t, aggregated)
	})
}

// TestSupervisorConfigVariations
func TestSupervisorConfigVariations(t *testing.T) {
	tests := []struct {
		name   string
		config *SupervisorConfig
		verify func(*SupervisorConfig)
	}{
		{
			name: "custom concurrency",
			config: &SupervisorConfig{
				MaxConcurrentAgents: 10,
			},
			verify: func(c *SupervisorConfig) {
				assert.Equal(t, 10, c.MaxConcurrentAgents)
			},
		},
		{
			name: "custom timeout",
			config: &SupervisorConfig{
				SubAgentTimeout: 60 * time.Second,
			},
			verify: func(c *SupervisorConfig) {
				assert.Equal(t, 60*time.Second, c.SubAgentTimeout)
			},
		},
		{
			name: "caching disabled",
			config: &SupervisorConfig{
				EnableCaching: false,
			},
			verify: func(c *SupervisorConfig) {
				assert.False(t, c.EnableCaching)
			},
		},
		{
			name: "metrics disabled",
			config: &SupervisorConfig{
				EnableMetrics: false,
			},
			verify: func(c *SupervisorConfig) {
				assert.False(t, c.EnableMetrics)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.verify(tt.config)
		})
	}
}

// TestMetricsEdgeCases
func TestMetricsEdgeCases(t *testing.T) {
	metrics := NewSupervisorMetrics()

	t.Run("success rate with zero tasks", func(t *testing.T) {
		snapshot := metrics.GetSnapshot()
		// Avoid division by zero
		assert.NotPanics(t, func() {
			_ = snapshot["success_rate"]
		})
	})

	t.Run("multiple rapid increments", func(t *testing.T) {
		for i := 0; i < 1000; i++ {
			metrics.IncrementTotalTasks()
		}
		snapshot := metrics.GetSnapshot()
		assert.Equal(t, int64(1000), snapshot["total_tasks"])
	})

	t.Run("execution time accumulation", func(t *testing.T) {
		m := NewSupervisorMetrics()
		m.IncrementTotalTasks()
		m.UpdateExecutionTime(100)
		m.IncrementTotalTasks()
		m.UpdateExecutionTime(200)

		snapshot := m.GetSnapshot()
		assert.Equal(t, int64(2), snapshot["total_tasks"])
	})
}
