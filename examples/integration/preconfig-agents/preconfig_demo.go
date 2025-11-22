package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/constants"
)

// MockLLMClient implements a simple mock LLM client for demonstration
type MockLLMClient struct {
	provider string
}

func NewMockLLMClient() *MockLLMClient {
	return &MockLLMClient{
		provider: "mock",
	}
}

func (m *MockLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// Simulate LLM response based on request
	response := "This is a mock response from the LLM."

	// Customize response based on message content
	if len(req.Messages) > 0 {
		lastMsg := req.Messages[len(req.Messages)-1]
		response = fmt.Sprintf("Mock LLM response to: %s", lastMsg.Content)
	}

	return &llm.CompletionResponse{
		Content:    response,
		Model:      "mock-model",
		TokensUsed: 50,
		Provider:   m.provider,
	}, nil
}

func (m *MockLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return m.Complete(ctx, &llm.CompletionRequest{Messages: messages})
}

func (m *MockLLMClient) Provider() constants.Provider {
	return constants.ProviderCustom
}

func (m *MockLLMClient) IsAvailable() bool {
	return true
}

func main() {
	fmt.Println("=== Pre-configured Agent Templates Demo ===")

	// Create a mock LLM client for demonstration
	llmClient := NewMockLLMClient()
	ctx := context.Background()

	// 1. Analysis Agent Demo
	fmt.Println("1. Analysis Agent")
	fmt.Println("   Optimized for data analysis and report generation")
	fmt.Println("   - Temperature: 0.1 (very consistent)")
	fmt.Println("   - MaxIterations: 20 (extended reasoning)")
	demoAnalysisAgent(ctx, llmClient)
	fmt.Println()

	// 2. Workflow Agent Demo
	fmt.Println("2. Workflow Agent")
	fmt.Println("   Optimized for multi-step workflow orchestration")
	fmt.Println("   - MaxIterations: 15")
	fmt.Println("   - EnableAutoSave: true")
	demoWorkflowAgent(ctx, llmClient)
	fmt.Println()

	// 3. Monitoring Agent Demo
	fmt.Println("3. Monitoring Agent")
	fmt.Println("   Optimized for continuous system monitoring")
	fmt.Println("   - MaxIterations: 100 (long-running)")
	fmt.Println("   - Rate limiting and caching enabled")
	demoMonitoringAgent(ctx, llmClient)
	fmt.Println()

	// 4. Research Agent Demo
	fmt.Println("4. Research Agent")
	fmt.Println("   Optimized for information gathering and synthesis")
	fmt.Println("   - MaxTokens: 4000 (comprehensive reports)")
	fmt.Println("   - Temperature: 0.5 (balanced)")
	demoResearchAgent(ctx, llmClient)
	fmt.Println()

	// 5. Comparison with existing agents
	fmt.Println("5. Comparison with Existing Pre-configured Agents")
	demoExistingAgents(ctx, llmClient)

	fmt.Println("\n=== Demo Complete ===")
}

func demoAnalysisAgent(ctx context.Context, llmClient llm.Client) {
	// Create data source
	dataSource := map[string]interface{}{
		"type":    "sales_data",
		"records": 1000,
		"period":  "Q4 2024",
	}

	// Create analysis agent
	agent, err := builder.AnalysisAgent(llmClient, dataSource)
	if err != nil {
		log.Printf("   Error creating analysis agent: %v\n", err)
		return
	}

	// Execute analysis
	output, err := agent.Execute(ctx, "Analyze Q4 2024 sales trends and identify key insights")
	if err != nil {
		log.Printf("   Error executing analysis: %v\n", err)
		return
	}

	fmt.Printf("   Input: Analyze Q4 2024 sales trends\n")
	fmt.Printf("   Output: %s\n", output.Result)
	fmt.Printf("   Config: Temperature=%.1f, MaxIterations=%d\n",
		agent.GetMetrics()["temperature"],
		agent.GetMetrics()["max_iterations"])
}

func demoWorkflowAgent(ctx context.Context, llmClient llm.Client) {
	// Create workflow definitions
	workflows := map[string]interface{}{
		"deployment": []string{
			"validate_code",
			"run_tests",
			"build_image",
			"deploy_staging",
			"integration_test",
			"deploy_production",
		},
		"rollback": []string{
			"stop_new_deployment",
			"restore_previous_version",
			"verify_health",
		},
	}

	// Create workflow agent
	agent, err := builder.WorkflowAgent(llmClient, workflows)
	if err != nil {
		log.Printf("   Error creating workflow agent: %v\n", err)
		return
	}

	// Execute workflow
	input := map[string]interface{}{
		"workflow": "deployment",
		"target":   "production",
		"version":  "v2.3.0",
	}

	output, err := agent.Execute(ctx, input)
	if err != nil {
		log.Printf("   Error executing workflow: %v\n", err)
		return
	}

	fmt.Printf("   Input: Execute deployment workflow for v2.3.0\n")
	fmt.Printf("   Output: %s\n", output.Result)

	// Check workflow status in state
	state := agent.GetState()
	status, _ := state.Get("workflow_status")
	fmt.Printf("   Status: %v\n", status)
}

func demoMonitoringAgent(ctx context.Context, llmClient llm.Client) {
	checkInterval := 30 * time.Second

	// Create monitoring agent
	agent, err := builder.MonitoringAgent(llmClient, checkInterval)
	if err != nil {
		log.Printf("   Error creating monitoring agent: %v\n", err)
		return
	}

	// Simulate monitoring check
	metrics := map[string]interface{}{
		"cpu_usage":    75.5,
		"memory_usage": 82.3,
		"disk_usage":   45.0,
		"requests_sec": 1250,
		"error_rate":   0.02,
	}

	input := map[string]interface{}{
		"metrics":   metrics,
		"threshold": "check for anomalies",
	}

	output, err := agent.Execute(ctx, input)
	if err != nil {
		log.Printf("   Error executing monitoring: %v\n", err)
		return
	}

	fmt.Printf("   Input: Check system metrics\n")
	fmt.Printf("   Metrics: CPU=%.1f%%, Memory=%.1f%%, ErrorRate=%.2f%%\n",
		metrics["cpu_usage"], metrics["memory_usage"], metrics["error_rate"].(float64)*100)
	fmt.Printf("   Output: %s\n", output.Result)

	// Display monitoring status
	state := agent.GetState()
	status, _ := state.Get("monitoring_status")
	interval, _ := state.Get("check_interval")
	fmt.Printf("   Status: %v, Interval: %v\n", status, interval)
}

func demoResearchAgent(ctx context.Context, llmClient llm.Client) {
	// Define research sources
	sources := []string{
		"https://arxiv.org/cs/AI",
		"https://scholar.google.com",
		"https://paperswithcode.com",
		"https://huggingface.co/papers",
	}

	// Create research agent
	agent, err := builder.ResearchAgent(llmClient, sources)
	if err != nil {
		log.Printf("   Error creating research agent: %v\n", err)
		return
	}

	// Execute research
	query := "Latest developments in Large Language Models for 2024"
	output, err := agent.Execute(ctx, query)
	if err != nil {
		log.Printf("   Error executing research: %v\n", err)
		return
	}

	fmt.Printf("   Input: %s\n", query)
	fmt.Printf("   Sources: %d configured\n", len(sources))
	fmt.Printf("   Output: %s\n", output.Result)

	// Check research status
	state := agent.GetState()
	researchStatus, _ := state.Get("research_status")
	sourcesCount, _ := state.Get("sources_count")
	fmt.Printf("   Status: %v, Sources Count: %v\n", researchStatus, sourcesCount)
}

func demoExistingAgents(ctx context.Context, llmClient llm.Client) {
	fmt.Println("   Existing Pre-configured Agents:")

	// QuickAgent
	fmt.Println("\n   a) QuickAgent - Simple agent with minimal config")
	quickAgent, _ := builder.QuickAgent(llmClient, "You are a helpful assistant")
	quickOutput, _ := quickAgent.Execute(ctx, "Hello!")
	fmt.Printf("      Output: %s\n", quickOutput.Result)

	// RAGAgent
	fmt.Println("\n   b) RAGAgent - Retrieval-Augmented Generation")
	ragAgent, _ := builder.RAGAgent(llmClient, nil)
	_ = ragAgent // Use the variable
	fmt.Printf("      Temperature: %.1f (optimized for accuracy)\n", 0.3)
	fmt.Printf("      MaxTokens: %d\n", 3000)

	// ChatAgent
	fmt.Println("\n   c) ChatAgent - Conversational chatbot")
	chatAgent, _ := builder.ChatAgent(llmClient, "Alice")
	state := chatAgent.GetState()
	userName, _ := state.Get("user_name")
	fmt.Printf("      User: %v\n", userName)
	fmt.Printf("      Streaming Enabled: true\n")

	fmt.Println("\n   All agents demonstrate different optimization patterns!")
}
