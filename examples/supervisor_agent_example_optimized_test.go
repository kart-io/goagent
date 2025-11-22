package examples

import (
	"context"
	"fmt"
	"testing"

	"github.com/kart-io/goagent/agents"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/examples/testhelpers"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupervisorAgent_DeepSeek_Optimized(t *testing.T) {
	// Skip if API key not set
	testhelpers.SkipIfNoEnv(t, "DEEPSEEK_API_KEY")

	// Define test case
	testCase := struct {
		task             string
		searchResponse   string
		weatherResponse  string
		summaryResponse  string
		expectedSearches int
	}{
		task:             "Research the capital of France, find its current weather, and write a short summary.",
		searchResponse:   "The capital of France is Paris.",
		weatherResponse:  "The weather in Paris is sunny with a temperature of 25°C.",
		summaryResponse:  "The capital of France is Paris, where it is currently sunny and 25°C.",
		expectedSearches: 3, // search, weather, summary
	}

	// Create mock sub-agents using simplified pattern
	searchAgent := testhelpers.NewMockAgent("search")
	searchAgent.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
		return &core.AgentOutput{
			Result: testCase.searchResponse,
			Status: "success",
		}, nil
	})

	weatherAgent := testhelpers.NewMockAgent("weather")
	weatherAgent.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
		return &core.AgentOutput{
			Result: testCase.weatherResponse,
			Status: "success",
		}, nil
	})

	summaryAgent := testhelpers.NewMockAgent("summary")
	summaryAgent.SetInvokeFn(func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
		return &core.AgentOutput{
			Result: testCase.summaryResponse,
			Status: "success",
		}, nil
	})

	// Create DeepSeek LLM client
	apiKey := testhelpers.RequireEnv(t, "DEEPSEEK_API_KEY")
	config := &llm.LLMOptions{
		APIKey: apiKey,
		Model:  "deepseek-chat",
	}

	llmClient, err := providers.NewDeepSeek(config)
	require.NoError(t, err, "Failed to create DeepSeek client")

	// Create the SupervisorAgent with hierarchical strategy
	agentConfig := agents.DefaultSupervisorConfig()
	agentConfig.AggregationStrategy = agents.StrategyHierarchy

	supervisor := agents.NewSupervisorAgent(llmClient, agentConfig)
	supervisor.AddSubAgent("search", searchAgent)
	supervisor.AddSubAgent("weather", weatherAgent)
	supervisor.AddSubAgent("summary", summaryAgent)

	// Invoke the SupervisorAgent
	finalResult, err := supervisor.Invoke(context.Background(), &core.AgentInput{Task: testCase.task})
	require.NoError(t, err, "Supervisor agent returned an error")
	require.NotNil(t, finalResult, "Supervisor agent returned nil result")

	// Print results for visual verification
	fmt.Printf("Initial complex task: %s\n\n", testCase.task)
	fmt.Println("Supervisor agent final aggregated result:")
	fmt.Printf("%+v\n", finalResult.Result)

	// Verify result structure using helper functions
	resultMap, ok := finalResult.Result.(map[string]interface{})
	if !ok {
		t.Logf("Result is not a map, got type: %T, value: %+v", finalResult.Result, finalResult.Result)
		return // Not a structured result, skip detailed verification
	}

	// Verify search results if present
	if searchData, ok := testhelpers.GetMapValue[map[string]interface{}](t, resultMap, "search"); ok {
		if results, ok := testhelpers.GetMapValue[[]interface{}](t, searchData, "results"); ok {
			assert.NotEmpty(t, results, "Search results should not be empty")
			fmt.Println("\nSearch results:")
			for _, res := range results {
				fmt.Printf("- %s\n", res)
			}
		}
	}

	// Verify weather results if present
	if weatherData, ok := testhelpers.GetMapValue[map[string]interface{}](t, resultMap, "weather"); ok {
		if results, ok := testhelpers.GetMapValue[[]interface{}](t, weatherData, "results"); ok {
			assert.NotEmpty(t, results, "Weather results should not be empty")
			fmt.Println("\nWeather results:")
			for _, res := range results {
				fmt.Printf("- %s\n", res)
			}
		}
	}
}

// TestSupervisorAgent_MultipleScenarios demonstrates table-driven tests
func TestSupervisorAgent_MultipleScenarios(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	testCases := []struct {
		name            string
		task            string
		agentResponses  map[string]string
		expectedAgents  []string
		validateResults func(t *testing.T, result *core.AgentOutput)
	}{
		{
			name: "simple_task",
			task: "What is 2+2?",
			agentResponses: map[string]string{
				"calculator": "4",
			},
			expectedAgents: []string{"calculator"},
			validateResults: func(t *testing.T, result *core.AgentOutput) {
				require.NotNil(t, result, "Result should not be nil")
				assert.Equal(t, "success", result.Status, "Status should be success")
			},
		},
		{
			name: "multi_agent_task",
			task: "Search for Go programming language and summarize it",
			agentResponses: map[string]string{
				"search":  "Go is a statically typed, compiled programming language.",
				"summary": "Go is a modern programming language designed for simplicity and efficiency.",
			},
			expectedAgents: []string{"search", "summary"},
			validateResults: func(t *testing.T, result *core.AgentOutput) {
				require.NotNil(t, result, "Result should not be nil")
				assert.Equal(t, "success", result.Status, "Status should be success")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock agents
			mockAgents := make(map[string]*testhelpers.MockAgent)
			for agentName, response := range tc.agentResponses {
				agent := testhelpers.NewMockAgent(agentName)
				agent.SetInvokeFn(func(r string) func(context.Context, *core.AgentInput) (*core.AgentOutput, error) {
					return func(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
						return &core.AgentOutput{
							Result: r,
							Status: "success",
						}, nil
					}
				}(response))
				mockAgents[agentName] = agent
			}

			// Create a simple supervisor (without real LLM for table tests)
			// This is a simplified example - in practice you'd need a mock LLM
			t.Skip("Skipping table-driven test - requires mock LLM implementation")

			// The pattern would be:
			// supervisor := createSupervisorWithMocks(mockAgents)
			// result, err := supervisor.Invoke(context.Background(), &core.AgentInput{Task: tc.task})
			// require.NoError(t, err)
			// tc.validateResults(t, result)
		})
	}
}
