#!/bin/bash

# Quick setup script for running multi-agent example with Ollama

echo "========================================"
echo "🤖 Multi-Agent Ollama Setup"
echo "========================================"
echo ""

# Check if Ollama is installed
if ! command -v ollama &> /dev/null; then
    echo "❌ Ollama not found. Please install it first:"
    echo "   Visit: https://ollama.ai"
    echo ""
    echo "   On macOS: brew install ollama"
    echo "   On Linux: curl -fsSL https://ollama.ai/install.sh | sh"
    exit 1
fi

echo "✓ Ollama is installed"

# Check if Ollama is running
if ! ollama list &> /dev/null; then
    echo ""
    echo "⚠️  Ollama service not running. Starting it now..."
    ollama serve &
    sleep 3
fi

echo "✓ Ollama service is running"
echo ""

# List available models
echo "📦 Available models:"
ollama list 2>/dev/null | head -10
echo ""

# Check for recommended models
MODELS=("llama2" "qwen2" "deepseek-coder" "mistral")
FOUND_MODEL=""

for model in "${MODELS[@]}"; do
    if ollama list 2>/dev/null | grep -q "$model"; then
        FOUND_MODEL=$model
        break
    fi
done

if [ -z "$FOUND_MODEL" ]; then
    echo "📥 No suitable model found. Pulling llama2..."
    ollama pull llama2
    FOUND_MODEL="llama2"
else
    echo "✓ Found model: $FOUND_MODEL"
fi

echo ""
echo "========================================"
echo "🚀 Running Multi-Agent Example"
echo "========================================"
echo ""

# Set the model and run
export OLLAMA_MODEL=$FOUND_MODEL
echo "Using model: $OLLAMA_MODEL"
echo ""

# Run the example
go run main_simple.go

echo ""
echo "========================================"
echo "✨ Example Complete!"
echo "========================================"
echo ""
echo "Tips:"
echo "• Try different models: export OLLAMA_MODEL=qwen2"
echo "• Pull more models: ollama pull deepseek-coder"
echo "• List models: ollama list"
echo ""