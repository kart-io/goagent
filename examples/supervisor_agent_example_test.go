package examples

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/kart-io/goagent/agents"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/providers"
	"github.com/stretchr/testify/mock"
)

// MockAgent is a mock implementation of the core.Agent interface for testing purposes.
type MockAgent struct {
	mock.Mock
}

func (m *MockAgent) Invoke(ctx context.Context, input *core.AgentInput) (*core.AgentOutput, error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*core.AgentOutput), args.Error(1)
}

func (m *MockAgent) Stream(ctx context.Context, input *core.AgentInput) (<-chan core.StreamChunk[*core.AgentOutput], error) {
	args := m.Called(ctx, input)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(<-chan core.StreamChunk[*core.AgentOutput]), args.Error(1)
}

func (m *MockAgent) Batch(ctx context.Context, inputs []*core.AgentInput) ([]*core.AgentOutput, error) {
	args := m.Called(ctx, inputs)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*core.AgentOutput), args.Error(1)
}

func (m *MockAgent) WithCallbacks(callbacks ...core.Callback) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	args := m.Called(callbacks)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(core.Runnable[*core.AgentInput, *core.AgentOutput])
}

func (m *MockAgent) WithConfig(config core.RunnableConfig) core.Runnable[*core.AgentInput, *core.AgentOutput] {
	args := m.Called(config)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(core.Runnable[*core.AgentInput, *core.AgentOutput])
}

func (m *MockAgent) GetConfig() core.RunnableConfig {
	args := m.Called()
	return args.Get(0).(core.RunnableConfig)
}

func (m *MockAgent) Pipe(next core.Runnable[*core.AgentOutput, interface{}]) core.Runnable[*core.AgentInput, interface{}] {
	args := m.Called(next)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(core.Runnable[*core.AgentInput, interface{}])
}

func (m *MockAgent) Capabilities() []string {
	return []string{}
}

func (m *MockAgent) Name() string {
	return ""
}

func (m *MockAgent) Description() string {
	return ""
}

func TestSupervisorAgent_DeepSeek_Example(t *testing.T) {
	// 1. Define the overall task
	complexTask := "Research the capital of France, find its current weather, and write a short summary."

	// 2. Create mock sub-agents with specific capabilities

	searchAgent := new(MockAgent)

	weatherAgent := new(MockAgent)

	summaryAgent := new(MockAgent)

	// 3. Create the DeepSeek LLM client

	// IMPORTANT: Set the DEEPSEEK_API_KEY environment variable to run this test.

	apiKey := os.Getenv("DEEPSEEK_API_KEY")

	if apiKey == "" {

		t.Skip("DEEPSEEK_API_KEY environment variable not set")

	}

	config := &llm.Config{

		APIKey: apiKey,

		Model: "deepseek-chat",
	}

	llm, err := providers.NewDeepSeek(config)

	if err != nil {

		t.Fatalf("Failed to create DeepSeek client: %v", err)

	}

	// 4. Create the SupervisorAgent

	agentConfig := agents.DefaultSupervisorConfig()

	agentConfig.AggregationStrategy = agents.StrategyHierarchy

	supervisor := agents.NewSupervisorAgent(llm, agentConfig)

	supervisor.AddSubAgent("search", searchAgent)

	supervisor.AddSubAgent("weather", weatherAgent)

	supervisor.AddSubAgent("summary", summaryAgent)

	// 5. Mock the responses from the sub-agents

	searchAgent.On("Invoke", mock.Anything, mock.Anything).Return(&core.AgentOutput{

		Result: "The capital of France is Paris.",
	}, nil).Once()

	weatherAgent.On("Invoke", mock.Anything, mock.Anything).Return(&core.AgentOutput{

		Result: "The weather in Paris is sunny with a temperature of 25°C.",
	}, nil).Once()

	summaryAgent.On("Invoke", mock.Anything, mock.Anything).Return(&core.AgentOutput{

		Result: "The capital of France is Paris, where it is currently sunny and 25°C.",
	}, nil).Once()

	// 8. Invoke the SupervisorAgent

	finalResult, err := supervisor.Invoke(context.Background(), &core.AgentInput{Task: complexTask})

	// 9. Print and verify the result

	if err != nil {

		t.Fatalf("Supervisor agent returned an error: %v", err)

	}

	fmt.Printf("Initial complex task: %s\n\n", complexTask)

	fmt.Println("Supervisor agent final aggregated result:")

	fmt.Printf("%+v\n", finalResult.Result)

	// Example of how you might access the data

	if result, ok := finalResult.Result.(map[string]interface{}); ok {

		if searchResult, ok := result["search"].(map[string]interface{}); ok {

			if results, ok := searchResult["results"].([]interface{}); ok {

				fmt.Println("\nSearch results:")

				for _, res := range results {

					fmt.Printf("- %s\n", res)

				}

			}

		}

		if weatherResult, ok := result["weather"].(map[string]interface{}); ok {

			if results, ok := weatherResult["results"].([]interface{}); ok {

				fmt.Println("\nWeather results:")

				for _, res := range results {

					fmt.Printf("- %s\n", res)

				}

			}

		}

	}

}
