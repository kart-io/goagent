# Multi-Agent Collaboration Example

Three agents built with `builder.NewAgentBuilder[any, core.State]` run in sequence on one task ("Analyze website performance data and optimize loading times for an e-commerce platform"). Each agent's output is serialized to JSON and passed as the next agent's input.

| Agent | Tools (`tools.NewBaseTool`) |
| --- | --- |
| Analysis | `data_analysis` (`data`), `summarize` (`text`) |
| Strategy | `formulate_strategy` (`analysis`), `prioritize_tasks` (`tasks`) |
| Execution | `http_request` (`method`, `url`, optional `headers`, `body`), `execute_command` (`command`), `file_operations` (`operation`: read/write/list/delete, `path`, `content`) |

`http_request` sends real HTTP requests through `utils/httpclient`. `execute_command` and `file_operations` only return simulated results and never touch the system.

## Files

- `main_simple.go`: entry point and all agents/tools.
- `demo_standalone.go`: `RunDemo()`, an LLM-free walkthrough of the same flow. `main` does not call it.
- `run.sh`, `setup_ollama.sh`: helper scripts that detect Ollama and run the example.

## LLM selection

`initializeLLMClient` picks the first available provider:

1. `OLLAMA_MODEL` set: Ollama with that model.
2. A local Ollama answering a test request within 2 seconds: Ollama with `llama2`.
3. `OPENAI_API_KEY`: OpenAI `gpt-3.5-turbo`.
4. `GEMINI_API_KEY`: Gemini `gemini-pro`.

Otherwise it exits with an error; there is no no-LLM mode.

## Run

```bash
cd examples/advanced/multi-agent-collaboration

OLLAMA_MODEL=qwen2 go run .
# or
OPENAI_API_KEY=... go run .
```

`run.sh` exports `OLLAMA_MODEL` (default `llama3`) when Ollama is running, then runs `go run .`. `setup_ollama.sh` pulls a model if none of `llama2`/`qwen2`/`deepseek-coder`/`mistral` is installed, then runs `go run main_simple.go`.

The program prints each agent's result (structured results truncated to 20 lines) and a closing summary.
