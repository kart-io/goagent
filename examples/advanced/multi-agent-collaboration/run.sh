#!/bin/bash

# Multi-Agent Collaboration Example Runner
# This script demonstrates the multi-agent system with different configurations

echo "========================================"
echo "Multi-Agent Collaboration Example"
echo "========================================"
echo ""

# Check if Ollama is running
if command -v ollama &> /dev/null; then
    if ollama list &> /dev/null; then
        echo "✓ Ollama detected and running"
        echo "  Available models:"
        ollama list | head -5
        echo ""
        echo "Running with Ollama..."

        # Allow user to specify model
        if [ -n "$OLLAMA_MODEL" ]; then
            echo "Using model: $OLLAMA_MODEL"
        else
            echo "Using default model: llama2"
            echo "Tip: Set OLLAMA_MODEL to use a different model"
            echo "Example: export OLLAMA_MODEL=qwen2"
        fi

        go run main_simple.go
        exit 0
    else
        echo "ℹ️  Ollama installed but not running"
        echo "  Start it with: ollama serve"
        echo ""
    fi
fi

# Check if any LLM provider is configured
if [ -n "$OPENAI_API_KEY" ]; then
    echo "✓ OpenAI API key detected"
    echo "Running with OpenAI provider..."
    go run main_simple.go
elif [ -n "$GEMINI_API_KEY" ]; then
    echo "✓ Gemini API key detected"
    echo "Running with Gemini provider..."
    go run main_simple.go
else
    echo "ℹ️  No LLM provider configured"
    echo "Running demonstration mode..."
    echo ""
    go run demo.go
fi

echo ""
echo "========================================"
echo "Example Complete"
echo "========================================"
echo ""
echo "To run with Ollama (recommended - local, no API key):"
echo "1. Install Ollama: https://ollama.ai"
echo "2. Start Ollama: ollama serve"
echo "3. Pull a model: ollama pull llama2 (or qwen2, deepseek-coder, etc.)"
echo "4. Run: ./run.sh"
echo ""
echo "To run with cloud LLM providers:"
echo "1. Set OPENAI_API_KEY or GEMINI_API_KEY environment variable"
echo "2. Run: ./run.sh"
echo ""
echo "To run the demo without LLM:"
echo "Run: go run demo.go"
echo ""