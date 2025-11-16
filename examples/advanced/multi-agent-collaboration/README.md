# Multi-Agent Collaboration Example

This example demonstrates a sophisticated multi-agent system where three specialized agents work together to solve complex tasks. Each agent has a specific role and set of tools, showcasing the power of agent specialization and coordination.

## 🎯 Overview

The system consists of three specialized agents:

### 1. 🔍 Analysis Agent
- **Role**: Analyzes data and identifies patterns
- **Tools**:
  - `data_analysis`: Analyze data and extract insights
  - `summarize`: Create concise summaries
- **Capabilities**: Pattern recognition, data interpretation, insight extraction

### 2. 📋 Strategy Agent
- **Role**: Formulates optimal approaches based on analysis
- **Tools**:
  - `formulate_strategy`: Create strategic approaches
  - `prioritize_tasks`: Prioritize tasks by impact and effort
- **Capabilities**: Strategic planning, task prioritization, approach optimization

### 3. ⚡ Execution Agent
- **Role**: Executes strategies with tools and external APIs
- **Tools**:
  - `http_request`: Make HTTP API calls
  - `execute_command`: Run system commands (simulated for safety)
  - `file_operations`: Perform file operations
- **Capabilities**: API integration, command execution, file management

## 🚀 Features

- **Sequential Workflow**: Agents work in sequence, each building on the previous agent's output
- **Tool Integration**: Each agent has specialized tools for their domain
- **HTTP Support**: Execution agent can interact with external APIs
- **Error Handling**: Graceful error handling at each step
- **Flexible LLM Support**: Works with OpenAI, Gemini, or Ollama

## 📦 Installation

Ensure you have the GoAgent framework installed:

```bash
go get github.com/kart-io/goagent
```

## 🔧 Configuration

The example prioritizes local execution with Ollama, then falls back to cloud providers:

### Option 1: Ollama (Recommended - Local, No API Key)

```bash
# 1. Install Ollama from https://ollama.ai

# 2. Start Ollama service
ollama serve

# 3. Pull a model (choose one)
ollama pull llama2          # Default, balanced
ollama pull qwen2           # Good for Chinese and English
ollama pull deepseek-coder  # Excellent for coding tasks
ollama pull mistral         # Fast and efficient

# 4. Run the example
./run.sh

# Optional: Specify a different model
export OLLAMA_MODEL=qwen2
./run.sh
```

### Option 2: Cloud Providers (OpenAI or Gemini)

```bash
# OpenAI
export OPENAI_API_KEY="your-openai-api-key"
./run.sh

# OR Gemini
export GEMINI_API_KEY="your-gemini-api-key"
./run.sh
```

### Option 3: Demo Mode (No LLM)

```bash
# Run demonstration without any LLM
go run demo.go
```

## 🎮 Usage

Run the example:

```bash
cd examples/advanced/multi-agent-collaboration
go run main.go
```

## 📊 Example Workflow

Given a task like "Analyze website performance data and optimize loading times":

1. **Analysis Agent** examines the task:
   - Identifies data points and patterns
   - Assesses complexity
   - Extracts key themes and risks
   - Highlights opportunities

2. **Strategy Agent** formulates an approach:
   - Creates phased implementation plan
   - Prioritizes tasks by impact
   - Defines success metrics
   - Allocates resources

3. **Execution Agent** implements the strategy:
   - Makes API calls for data collection
   - Processes files and data
   - Executes optimization commands
   - Reports results

## 🛠️ Tools Reference

### Analysis Tools

```go
// Data Analysis Tool
{
  "name": "data_analysis",
  "input": {
    "data": "string"  // Data to analyze
  },
  "output": {
    "data_points": "number",
    "complexity": "string",
    "key_themes": ["string"],
    "risks": ["string"],
    "opportunities": ["string"]
  }
}

// Summarize Tool
{
  "name": "summarize",
  "input": {
    "text": "string"  // Text to summarize
  },
  "output": {
    "summary": "string",
    "word_count": "string",
    "key_points": "string"
  }
}
```

### Strategy Tools

```go
// Strategy Formulation Tool
{
  "name": "formulate_strategy",
  "input": {
    "analysis": "string"  // Analysis results
  },
  "output": {
    "approach": "string",
    "phases": [{"phase": "string", "action": "string"}],
    "timeline": "string",
    "resources": ["string"],
    "success_metrics": ["string"]
  }
}

// Prioritization Tool
{
  "name": "prioritize_tasks",
  "input": {
    "tasks": ["string"]  // Tasks to prioritize
  },
  "output": [
    {
      "priority": "string",
      "task": "string",
      "impact": "string",
      "effort": "string"
    }
  ]
}
```

### Execution Tools

```go
// HTTP Request Tool
{
  "name": "http_request",
  "input": {
    "method": "GET|POST|PUT|DELETE|PATCH",
    "url": "string",
    "headers": {"key": "value"},  // Optional
    "body": {}  // Optional for POST/PUT/PATCH
  },
  "output": {
    "status_code": "number",
    "body": "string",
    "headers": {}
  }
}

// Command Executor Tool (Simulated)
{
  "name": "execute_command",
  "input": {
    "command": "string"
  },
  "output": {
    "command": "string",
    "status": "string",
    "output": "string"
  }
}

// File Operations Tool
{
  "name": "file_operations",
  "input": {
    "operation": "read|write|list|delete",
    "path": "string",
    "content": "string"  // For write operation
  },
  "output": {
    // Varies by operation
  }
}
```

## 🎯 Use Cases

This multi-agent system is perfect for:

1. **Data Processing Pipelines**
   - Analysis → Strategy → Execution flow
   - Automated data processing and reporting

2. **API Integration Tasks**
   - Fetch data from multiple sources
   - Process and analyze results
   - Execute actions based on findings

3. **System Automation**
   - Analyze system metrics
   - Plan optimization strategies
   - Execute improvements

4. **Business Process Automation**
   - Analyze business data
   - Formulate strategies
   - Implement solutions

## 🔄 Extending the System

### Adding New Agents

```go
// Create a new specialized agent
func createCustomAgent(llmClient llm.Client) (interfaces.Agent, error) {
    // Add custom tools
    customTool := &MyCustomTool{}

    registry := tools.NewRegistry()
    registry.Register(customTool)

    agent := agents.NewBasicAgent(
        "CustomAgent",
        llmClient,
        registry,
        agents.WithSystemPrompt("Your custom prompt"),
    )

    return agent, nil
}
```

### Adding New Tools

```go
// Implement the Tool interface
type MyCustomTool struct{}

func (t *MyCustomTool) Name() string {
    return "my_custom_tool"
}

func (t *MyCustomTool) Description() string {
    return "Description of what this tool does"
}

func (t *MyCustomTool) Execute(ctx context.Context, input interface{}) (interface{}, error) {
    // Tool implementation
    return result, nil
}

func (t *MyCustomTool) GetSchema() interface{} {
    // JSON Schema for input validation
    return schema
}
```

## 🔍 Debugging

Enable debug output by setting environment variables:

```bash
export DEBUG=true
export LOG_LEVEL=debug
```

## ⚠️ Safety Notes

- Command execution is **simulated** by default for safety
- File operations are **simulated** to prevent accidental modifications
- To enable actual execution, modify the tool implementations with appropriate security checks

## 📝 Output Example

```
=== Multi-Agent Collaboration Example ===

Task: Analyze website performance data and optimize loading times
========================================

🔍 Analysis Agent: Analyzing the task...
✓ Analysis completed: Identified 5 key performance metrics...

📋 Strategy Agent: Formulating strategy...
✓ Strategy formulated: 3-phase optimization plan created...

⚡ Execution Agent: Executing the strategy...
✓ Execution completed: Optimizations applied successfully...

📊 Final Results:
----------------------------------------
Analysis:
  {data_points: 15, complexity: medium, key_themes: [...]}
Strategy:
  {approach: phased_implementation, phases: [...]}
Execution:
  {status: success, optimizations_applied: 5}

✨ Task completed successfully with 12 steps
```

## 🤝 Contributing

Feel free to extend this example with:
- Additional agent types
- More sophisticated tools
- Real API integrations
- Database connections
- Advanced coordination patterns

## 📄 License

This example is part of the GoAgent framework and follows the same license.