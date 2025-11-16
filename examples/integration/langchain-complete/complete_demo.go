package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/execution"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/store"
	"github.com/kart-io/goagent/store/memory"
	"github.com/kart-io/goagent/tools"
)

// ApplicationContext defines the application-specific context
// This will be available to all tools and middleware
type ApplicationContext struct {
	UserID       string
	UserName     string
	Organization string
	APIKey       string
	Tier         string // "free", "premium", "enterprise"
}

// CustomState extends the base AgentState with additional fields
type CustomState struct {
	*core.AgentState
	ConversationHistory []string
	ToolCallCount       int
	LastToolUsed        string
}

// NewCustomState creates a new custom state
func NewCustomState() *CustomState {
	return &CustomState{
		AgentState:          core.NewAgentState(),
		ConversationHistory: []string{},
		ToolCallCount:       0,
	}
}

func main() {
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║  LangChain-Inspired Complete Integration Example          ║")
	fmt.Println("║  Demonstrating All Features from Phases 1-3               ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Initialize components
	ctx := context.Background()

	// 1. Create LLM client (using mock for demo)
	llmClient := NewDemoLLMClient()

	// 2. Create custom context for the application
	appContext := ApplicationContext{
		UserID:       "user-789",
		UserName:     "Alice Johnson",
		Organization: "TechCorp",
		APIKey:       "sk-demo-key",
		Tier:         "premium",
	}

	// 3. Create custom state
	customState := NewCustomState()
	customState.Set("session_start", time.Now())
	customState.Set("user_preferences", map[string]interface{}{
		"language": "en",
		"timezone": "UTC",
		"theme":    "dark",
	})

	// 4. Create Store and Checkpointer
	store := memory.New()
	checkpointer := core.NewInMemorySaver()

	// Pre-populate store with some data
	_ = store.Put(ctx, []string{"users", appContext.UserID}, "profile", map[string]interface{}{
		"name":         appContext.UserName,
		"organization": appContext.Organization,
		"tier":         appContext.Tier,
		"credits":      1000,
	})

	// 5. Create custom tools
	searchTool := CreateSearchTool()
	calculatorTool := CreateCalculatorTool()
	weatherTool := CreateWeatherTool()
	databaseTool := CreateDatabaseTool(store)

	// 6. Create custom middleware

	// Tier-based rate limiter
	rateLimiter := createTierRateLimiter()

	// Dynamic prompt enhancer
	promptEnhancer := middleware.NewDynamicPromptMiddleware(func(req *middleware.MiddlewareRequest) string {
		// Enhance prompt based on user tier
		if ctx, ok := req.Runtime.(*execution.Runtime[ApplicationContext, *CustomState]); ok {
			tier := ctx.Context.Tier
			base := fmt.Sprintf("%v", req.Input)
			switch tier {
			case "enterprise":
				return fmt.Sprintf("[Premium AI Mode] %s [Extended Context Enabled]", base)
			case "premium":
				return fmt.Sprintf("[Enhanced Mode] %s", base)
			default:
				return fmt.Sprintf("[Standard Mode] %s", base)
			}
		}
		return fmt.Sprintf("%v", req.Input)
	})

	// Authentication middleware
	authMiddleware := middleware.NewAuthenticationMiddleware(func(ctx context.Context, req *middleware.MiddlewareRequest) (bool, error) {
		if runtime, ok := req.Runtime.(*execution.Runtime[ApplicationContext, *CustomState]); ok {
			// Validate API key
			if runtime.Context.APIKey == "" {
				return false, fmt.Errorf("API key required")
			}
			// Check tier permissions
			if runtime.Context.Tier == "free" {
				// Free tier limitations
				if runtime.State.ToolCallCount > 10 {
					return false, fmt.Errorf("free tier limit exceeded")
				}
			}
			return true, nil
		}
		return false, fmt.Errorf("invalid runtime context")
	})

	// Tool usage tracker middleware
	toolTracker := createToolTrackerMiddleware()

	// Response formatter middleware
	responseFormatter := core.NewTransformMiddleware(
		nil, // No input transform
		func(output interface{}) (interface{}, error) {
			// Format output with metadata
			return map[string]interface{}{
				"response":  output,
				"timestamp": time.Now().Format(time.RFC3339),
				"model":     "demo-model",
				"tier":      appContext.Tier,
			}, nil
		},
	)

	// 7. Build the agent using the Builder pattern
	fmt.Println("Building AI Agent with Complete Feature Set...")
	fmt.Println()

	agent, err := builder.NewAgentBuilder[ApplicationContext, *CustomState](llmClient).
		WithSystemPrompt("You are an advanced AI assistant with access to multiple tools. "+
			"You help users with various tasks including search, calculations, weather, and database queries. "+
			"Always be helpful, accurate, and efficient.").
		WithContext(appContext).
		WithState(customState).
		WithStore(store).
		WithCheckpointer(checkpointer).
		WithTools(searchTool, calculatorTool, weatherTool, databaseTool).
		WithMiddleware(
			middleware.NewLoggingMiddleware(func(msg string) {
				fmt.Printf("  [LOG] %s\n", msg)
			}),
			middleware.NewTimingMiddleware(),
			authMiddleware,
			rateLimiter,
			promptEnhancer,
			toolTracker,
			middleware.NewValidationMiddleware(
				func(req *middleware.MiddlewareRequest) error {
					// Validate input length
					if len(fmt.Sprintf("%v", req.Input)) > 5000 {
						return fmt.Errorf("input too long (max 5000 chars)")
					}
					return nil
				},
			),
			core.NewCacheMiddleware(30*time.Second),
			responseFormatter,
			core.NewCircuitBreakerMiddleware(3, 30*time.Second),
		).
		WithConfig(&builder.AgentConfig{
			MaxIterations:   10,
			Timeout:         30 * time.Second,
			EnableStreaming: false,
			EnableAutoSave:  true,
			SaveInterval:    10 * time.Second,
			MaxTokens:       2000,
			Temperature:     0.7,
			SessionID:       fmt.Sprintf("session-%s-%d", appContext.UserID, time.Now().Unix()),
			Verbose:         true,
		}).
		WithErrorHandler(func(err error) error {
			// Custom error handling
			log.Printf("Agent error: %v", err)
			// Transform error for user
			return fmt.Errorf("an error occurred while processing your request")
		}).
		WithMetadata("app_version", "1.0.0").
		WithMetadata("environment", "demo").
		Build()
	if err != nil {
		log.Fatalf("Failed to build agent: %v", err)
	}

	fmt.Println("Agent built successfully!")
	fmt.Println()

	// 8. Execute various tasks
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println("Executing Agent Tasks")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	// Task 1: Simple query (tests basic execution)
	fmt.Println("Task 1: Simple Query")
	fmt.Println("--------------------")
	result1, err := agent.Execute(ctx, "What is the capital of France?")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Result: %v\n", result1.Result)
	}
	fmt.Println()

	// Save checkpoint after first task
	_ = checkpointer.Save(ctx, agent.GetMetrics()["session_id"].(string), customState)

	// Task 2: Tool usage (tests tool selection and execution)
	fmt.Println("Task 2: Tool Usage - Calculation")
	fmt.Println("---------------------------------")
	result2, err := agent.ExecuteWithTools(ctx, "Calculate the compound interest on $10000 at 5% for 3 years")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Result: %v\n", result2.Result)
	}
	fmt.Println()

	// Task 3: Database query (tests store integration)
	fmt.Println("Task 3: Database Query")
	fmt.Println("-----------------------")
	result3, err := agent.ExecuteWithTools(ctx, "Get my user profile from the database")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Result: %v\n", result3.Result)
	}
	fmt.Println()

	// Task 4: Multiple tool usage (tests tool chaining)
	fmt.Println("Task 4: Multiple Tools")
	fmt.Println("-----------------------")
	result4, err := agent.ExecuteWithTools(ctx, "Search for Python tutorials, then calculate how many hours it takes to watch 10 tutorials of 45 minutes each")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Result: %v\n", result4.Result)
	}
	fmt.Println()

	// Task 5: Cached request (tests cache middleware)
	fmt.Println("Task 5: Cached Request (repeating Task 1)")
	fmt.Println("------------------------------------------")
	start := time.Now()
	result5, err := agent.Execute(ctx, "What is the capital of France?")
	duration := time.Since(start)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	} else {
		fmt.Printf("Result: %v\n", result5.Result)
		fmt.Printf("Duration: %v (should be faster due to cache)\n", duration)
	}
	fmt.Println()

	// 9. Display final statistics
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println("Session Statistics")
	fmt.Println("═══════════════════════════════════════════════════════════")

	metrics := agent.GetMetrics()
	finalState := agent.GetState()

	fmt.Printf("Session ID: %v\n", metrics["session_id"])
	fmt.Printf("Tools Available: %v\n", metrics["tools_count"])
	fmt.Printf("Tool Calls Made: %d\n", finalState.ToolCallCount)
	fmt.Printf("Last Tool Used: %s\n", finalState.LastToolUsed)

	// Check stored data
	storedProfile, _ := store.Get(ctx, []string{"users", appContext.UserID}, "profile")
	fmt.Printf("Stored User Profile: %v\n", storedProfile.Value)

	// Load checkpoint
	loadedState, err := checkpointer.Load(ctx, metrics["session_id"].(string))
	if err == nil && loadedState != nil {
		sessionStart, _ := loadedState.Get("session_start")
		fmt.Printf("Session Started: %v\n", sessionStart)
	}

	// 10. Graceful shutdown
	fmt.Println()
	fmt.Println("Shutting down agent...")
	if err := agent.Shutdown(ctx); err != nil {
		fmt.Printf("Shutdown error: %v\n", err)
	}

	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║  Complete Integration Example Finished Successfully! 🎉    ║")
	fmt.Println("║                                                            ║")
	fmt.Println("║  Demonstrated Features:                                   ║")
	fmt.Println("║  ✓ State Management (Phase 1)                            ║")
	fmt.Println("║  ✓ Runtime & Context (Phase 1)                           ║")
	fmt.Println("║  ✓ Store & Checkpointer (Phase 1)                        ║")
	fmt.Println("║  ✓ Middleware System (Phase 2)                           ║")
	fmt.Println("║  ✓ Agent Builder (Phase 3)                               ║")
	fmt.Println("║  ✓ Tool Integration                                      ║")
	fmt.Println("║  ✓ Error Handling                                        ║")
	fmt.Println("║  ✓ Caching & Performance                                 ║")
	fmt.Println("╚════════════════════════════════════════════════════════════╝")
}

// Tool implementations

func CreateSearchTool() interfaces.Tool {
	return tools.NewBaseTool(
		"search",
		"Search the web for information",
		`{"type": "object", "properties": {"query": {"type": "string"}}}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			query := input.Args["query"]
			// Simulate search
			results := []string{
				fmt.Sprintf("Result 1 for '%v': Introduction to %v", query, query),
				fmt.Sprintf("Result 2 for '%v': Advanced %v techniques", query, query),
				fmt.Sprintf("Result 3 for '%v': %v best practices", query, query),
			}
			return &interfaces.ToolOutput{
				Result:  results,
				Success: true,
			}, nil
		},
	)
}

func CreateCalculatorTool() interfaces.Tool {
	return tools.NewBaseTool(
		"calculator",
		"Perform mathematical calculations",
		`{"type": "object", "properties": {"expression": {"type": "string"}}}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			// Simplified calculator (in production, use proper expression parser)
			expression := fmt.Sprintf("%v", input.Args["expression"])

			// Example: compound interest calculation
			if expression == "compound_interest_10000_5_3" {
				principal := 10000.0
				rate := 0.05
				time := 3.0
				amount := principal * (1 + rate) * (1 + rate) * (1 + rate)
				interest := amount - principal
				return &interfaces.ToolOutput{
					Result: map[string]interface{}{
						"principal": principal,
						"rate":      rate * 100,
						"time":      time,
						"amount":    amount,
						"interest":  interest,
					},
					Success: true,
				}, nil
			}

			// Example: simple calculation
			if expression == "10*45/60" {
				result := 10.0 * 45.0 / 60.0
				return &interfaces.ToolOutput{
					Result:  fmt.Sprintf("%.2f hours", result),
					Success: true,
				}, nil
			}

			return &interfaces.ToolOutput{
				Result:  "Calculation performed",
				Success: true,
			}, nil
		},
	)
}

func CreateWeatherTool() interfaces.Tool {
	return tools.NewBaseTool(
		"weather",
		"Get weather information for a location",
		`{"type": "object", "properties": {"location": {"type": "string"}}}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			location := input.Args["location"]
			// Simulate weather data
			return &interfaces.ToolOutput{
				Result: map[string]interface{}{
					"location":    location,
					"temperature": "22°C",
					"condition":   "Partly cloudy",
					"humidity":    "65%",
					"wind":        "10 km/h",
				},
				Success: true,
			}, nil
		},
	)
}

func CreateDatabaseTool(st store.Store) interfaces.Tool {
	return tools.NewBaseTool(
		"database",
		"Query the database for user information",
		`{"type": "object", "properties": {"query": {"type": "string"}}}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			// Access runtime context if available
			if input.Context != nil {
				// Get user profile from store
				if runtime, ok := input.Context.Value("runtime").(*execution.Runtime[ApplicationContext, *CustomState]); ok {
					userID := runtime.Context.UserID
					profile, err := st.Get(ctx, []string{"users", userID}, "profile")
					if err == nil {
						return &interfaces.ToolOutput{
							Result:  profile.Value,
							Success: true,
						}, nil
					}
				}
			}

			return &interfaces.ToolOutput{
				Result:  "Database query executed",
				Success: true,
			}, nil
		},
	)
}

// Custom middleware implementations

func createTierRateLimiter() core.Middleware {
	limits := map[string]int{
		"free":       10,
		"premium":    100,
		"enterprise": 1000,
	}

	// TODO: Could enhance to use tier-specific limits
	_ = limits
	return core.NewRateLimiterMiddleware(100, 1*time.Minute)
}

func createToolTrackerMiddleware() core.Middleware {
	return &toolTrackerMiddleware{
		BaseMiddleware: core.NewBaseMiddleware("tool-tracker"),
	}
}

type toolTrackerMiddleware struct {
	*core.BaseMiddleware
}

func (m *toolTrackerMiddleware) OnBefore(ctx context.Context, request *middleware.MiddlewareRequest) (*middleware.MiddlewareRequest, error) {
	// Pass through
	return request, nil
}

func (m *toolTrackerMiddleware) OnAfter(ctx context.Context, response *middleware.MiddlewareResponse) (*middleware.MiddlewareResponse, error) {
	// Track tool usage in state
	if response.State != nil {
		if state, ok := response.State.(*CustomState); ok {
			// Check if a tool was used (simplified check)
			if response.Metadata != nil {
				if toolName, ok := response.Metadata["tool_used"].(string); ok {
					state.ToolCallCount++
					state.LastToolUsed = toolName
				}
			}
		}
	}
	return response, nil
}

// Demo LLM Client

type DemoLLMClient struct{}

func NewDemoLLMClient() *DemoLLMClient {
	return &DemoLLMClient{}
}

func (c *DemoLLMClient) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	// Simulate LLM responses based on input
	input := ""
	for _, msg := range req.Messages {
		if msg.Role == "user" {
			input = msg.Content
			break
		}
	}

	response := "I understand your request."

	// Provide specific responses for demo queries
	switch input {
	case "What is the capital of France?":
		response = "The capital of France is Paris."
	case "Calculate the compound interest on $10000 at 5% for 3 years":
		response = "I'll calculate the compound interest for you. Using the calculator tool..."
	case "Get my user profile from the database":
		response = "I'll retrieve your user profile from the database..."
	case "Search for Python tutorials, then calculate how many hours it takes to watch 10 tutorials of 45 minutes each":
		response = "I'll search for Python tutorials and calculate the total viewing time..."
	}

	return &llm.CompletionResponse{
		Content:    response,
		Model:      "demo-model",
		TokensUsed: len(response) / 4, // Rough estimate
	}, nil
}

func (c *DemoLLMClient) Chat(ctx context.Context, messages []llm.Message) (*llm.CompletionResponse, error) {
	return c.Complete(ctx, &llm.CompletionRequest{Messages: messages})
}

func (c *DemoLLMClient) Provider() llm.Provider {
	return llm.ProviderCustom
}

func (c *DemoLLMClient) IsAvailable() bool {
	return true
}
