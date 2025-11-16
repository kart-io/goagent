# GoAgent Examples

This directory contains examples demonstrating the agent framework capabilities, organized by complexity level.

## Structure

### basic/ - Single-Feature Examples

Simple, focused examples demonstrating individual features:

- Simple agent creation
- Tool usage
- Chain construction
- Agent with memory
- Ollama integration
- Provider consistency
- All supported providers
- **Smart Agent with Tools (NEW)** - Time retrieval and API calls

**Best for**: First-time users, learning basics

### advanced/ - Multi-Feature Examples

Examples combining multiple features:

- Streaming execution
- Multi-mode streaming
- Observability integration
- ReAct agents
- Parallel execution
- Tool runtime
- Tool selector

**Best for**: Intermediate users, production patterns

### integration/ - Full-System Examples

Complete system integration examples:

- LangChain-inspired workflows
- Multi-agent systems
- Human-in-the-loop
- Pre-configured agents

**Best for**: Advanced users, architecture reference

## Running Examples

```bash
# Navigate to example directory
cd examples/basic/01-simple-agent/

# Run example
go run simple_agent.go
```

## Prerequisites

- Go 1.25.0+
- Required dependencies installed (`go mod download`)
- Environment variables configured (if needed)
