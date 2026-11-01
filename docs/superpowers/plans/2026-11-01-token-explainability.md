# Token Explainability Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Preserve provider-reported reasoning/tool-input tokens, actual response model and explicit reasoning configuration through the existing usage path.

**Architecture:** Extend the single TokenUsage / Fields evidence model. Structured providers populate explicit evidence, Builder journals the same response metadata, and the existing ledger/hook aggregate without deriving totals.

**Tech Stack:** Go 1.25, installed go-openai and Google GenAI SDKs.

**Spec:** docs/evaluation/2026-11-01-token-difference-analysis/REPORT.md (recommendations approved by user).

## Global Constraints

- One API/accounting implementation; no version switches or parallel ledgers.
- Never infer unreported reasoning/tool tokens from total minus other fields.
- Never add token subcategories to supplier TotalTokens.
- Configuration records explicit request settings, not guessed supplier defaults.
- Preserve existing local changes and unrelated sandbox research.

## Review Focus

Unknown vs reported zero; partial cumulative frames; durable replay identity; overflow/negative evidence; unsupported explicit configuration must not be ignored.

### Task 1: Evidence and aggregation

- [x] Add failing tests for new reasoning/tool-input fields, masks, validation, merges and aggregation.
- [x] Extend interfaces.TokenUsage, llm.Usage, usage.Fields, ledger summaries and Builder merging.
- [x] Verify unknown fields, immutable evidence, overflow and replay.

### Task 2: Provider metadata and reasoning configuration

- [x] Add failing protocol tests for response model, reported detail fields and explicit request settings.
- [x] Extract Gemini thoughts/tool-use input and OpenAI-compatible reasoning details; preserve response model across stream frames.
- [x] Add value-based reasoning config, request overrides, provider-specific validation and wire serialization.
- [x] Verify configurations are not silently ignored; no default inference.

### Task 3: Runtime propagation

- [x] Test Builder hook and durable replay of metadata/detail evidence.
- [x] Propagate requested config and actual response model through existing operation journal and hooks.
- [x] Verify request settings remain immutable and original replay metadata is retained.

### Task 4: Examples, documentation and live verification

- [x] Extend real-model example output with detail availability, response model and requested settings.
- [x] Run focused tests/race, full module tests, and bounded real Gemini/DeepSeek/MiniMax calls.
- [x] Document interpretation and preserve all failures/evidence without replacing prior reports.

Ruling: DeepSeek 的本轮完整任务因模型额外工具参数失败，保留已知消费；单 Agent 成功验证实际模型及推理字段，不把失败伪装成完整任务成功。Gemini/MiniMax 完整任务通过。
