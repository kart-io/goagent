package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/examples/multiagent/common"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/kart-io/goagent/multiagent"
)

// SimpleWorkerAgent is a simple worker that echoes the task
type SimpleWorkerAgent struct {
	*core.BaseAgent
	role multiagent.Role
}

func NewSimpleWorkerAgent(name string) *SimpleWorkerAgent {
	return &SimpleWorkerAgent{
		BaseAgent: core.NewBaseAgent(name, "Simple Worker", []string{"work"}),
		role:      multiagent.RoleWorker,
	}
}

func (a *SimpleWorkerAgent) GetRole() multiagent.Role     { return a.role }
func (a *SimpleWorkerAgent) SetRole(role multiagent.Role) { a.role = role }
func (a *SimpleWorkerAgent) ReceiveMessage(ctx context.Context, msg multiagent.Message) error {
	return nil
}

func (a *SimpleWorkerAgent) SendMessage(ctx context.Context, msg multiagent.Message) error {
	return nil
}

func (a *SimpleWorkerAgent) Vote(ctx context.Context, proposal interface{}) (bool, error) {
	return true, nil
}

func (a *SimpleWorkerAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	// Simulate work
	time.Sleep(500 * time.Millisecond)
	return &multiagent.Assignment{
		AgentID:   a.Name(),
		Role:      a.role,
		Subtask:   task.Input,
		Status:    multiagent.TaskStatusCompleted,
		Result:    fmt.Sprintf("%s completed: %v", a.Name(), task.Input),
		StartTime: time.Now(),
		EndTime:   time.Now(),
	}, nil
}

// LLMLeaderAgent uses DeepSeek to generate plans
type LLMLeaderAgent struct {
	*SimpleWorkerAgent
	llmClient llm.Client
}

func NewLLMLeaderAgent(name string, client llm.Client) *LLMLeaderAgent {
	agent := &LLMLeaderAgent{
		SimpleWorkerAgent: NewSimpleWorkerAgent(name),
		llmClient:         client,
	}
	agent.SetRole(multiagent.RoleLeader)
	return agent
}

func (a *LLMLeaderAgent) Collaborate(ctx context.Context, task *multiagent.CollaborativeTask) (*multiagent.Assignment, error) {
	fmt.Printf("[%s] Generating plan using DeepSeek...\n", a.Name())

	// Construct prompt for DeepSeek
	prompt := fmt.Sprintf(`You are a leader agent. Your goal is to break down the following task into subtasks for workers.
Task: %s
Available Workers: worker-1, worker-2

Return a JSON object with the following structure:
{
  "tasks": [
    {
      "worker_id": "worker-1",
      "task": "description of task for worker 1"
    },
    {
      "worker_id": "worker-2",
      "task": "description of task for worker 2"
    }
  ],
  "strategy": "parallel"
}
Do not include any markdown formatting (like `+"```json"+`) or explanations, just the raw JSON string.`, task.Input)

	// Call LLM
	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			llm.UserMessage(prompt),
		},
		Temperature: 0.1, // Low temperature for deterministic output
	}

	resp, err := a.llmClient.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	content := resp.Content
	// Clean up markdown code blocks if present (just in case)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	fmt.Printf("[%s] LLM Response: %s\n", a.Name(), content)

	// Parse JSON to verify it's valid (optional, system will also parse it)
	var plan map[string]interface{}
	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response as JSON: %w", err)
	}

	return &multiagent.Assignment{
		AgentID:   a.Name(),
		Role:      multiagent.RoleLeader,
		Result:    plan, // Return the parsed map
		Status:    multiagent.TaskStatusCompleted,
		StartTime: time.Now(),
		EndTime:   time.Now(),
	}, nil
}

func main() {
	// Check API Key
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		log.Fatal("DEEPSEEK_API_KEY environment variable is required")
	}

	// Initialize logger
	logger := common.NewSimpleLogger()

	// Create system
	system := multiagent.NewMultiAgentSystem(logger)

	// Register workers
	worker1 := NewSimpleWorkerAgent("worker-1")
	worker2 := NewSimpleWorkerAgent("worker-2")
	if err := system.RegisterAgent("worker-1", worker1); err != nil {
		log.Fatalf("Failed to register worker-1: %v", err)
	}
	if err := system.RegisterAgent("worker-2", worker2); err != nil {
		log.Fatalf("Failed to register worker-2: %v", err)
	}

	// Initialize DeepSeek Client
	client, err := providers.NewDeepSeekWithOptions(
		llm.WithAPIKey(apiKey),
		llm.WithModel("deepseek-chat"),
		llm.WithJSONResponse(), // Force JSON output for the leader
		llm.WithSystemPrompt("你是一个智能的领导者Agent。请始终以JSON格式输出任务分配计划。"),
	)
	if err != nil {
		log.Fatalf("Failed to create DeepSeek client: %v", err)
	}

	// Register Leader
	leader := NewLLMLeaderAgent("leader", client)
	if err := system.RegisterAgent("leader", leader); err != nil {
		log.Fatalf("Failed to register leader: %v", err)
	}

	// Create task
	task := &multiagent.CollaborativeTask{
		ID:          "task-1",
		Type:        multiagent.CollaborationTypeHierarchical,
		Description: "Analyze yearly data",
		Input:       "Analyze the sales data for Q1 and Q2 2024.",
	}

	fmt.Println("Starting Hierarchical Task with DeepSeek Leader...")
	result, err := system.ExecuteTask(context.Background(), task)
	if err != nil {
		log.Fatalf("Task failed: %v", err)
	}

	fmt.Printf("Task Status: %s\n", result.Status)
	fmt.Printf("Results: %v\n", result.Results)
}
