# 工具调用级恢复与真实模型验证

日期：2026-10-27。基线：`15d275430e382a938f931e60d56be107589f3a64`，结果来自其后的本地工作区改动。Go 1.25；没有引入 Eino 依赖。本报告验证原生工具协议、恢复和效果去重，不作模型质量排名。

## 本次实现

- `core/durable.Do`：具名子操作意图/结果日志、稳定子键、尝试次数、完成复用、失败类别与已知用量保留。模型请求后结果未落盘时停止自动重试，保留 pending/ambiguous。
- Dispatcher：宿主 CallID 派生独立工具键；已完成调用跳过，未确定调用仅在显式恢复且有预算时重试，效果需目标端原子去重。空 CallID 保持旧行为。
- Builder：模型回合先持久化，再执行工具；整轮参数预检，保留原生调用 ID 和续接状态。未接入调用日志的裸 Tool、未检查点化的 ContextPolicy 明确拒绝。
- 业务判断：无效输出也记录用量、响应摘要、needs_attention 状态；重启不会重新请求同一个失败模型操作。
- `AdaptStream`：中间类型化 Data/Event 显式转换，只有合法最终结果推进图；映射失败取消上游。MiniMax 缓冲输出声明 final/steps 能力。

## 最终 v2 案例

同一个持久 Agent 节点调用 record_A、record_B，参数各为限定枚举 stage。模型可以同一轮按 A、B 请求两个工具。在 B 的本地效果提交之后、工具结果检查点之前，子进程直接 `os.Exit(86)`。第二个进程恢复，第三个进程重放已完成运行。

| 模型 | 结果 | 首次 / 恢复 / 完成重放请求数 | 工具实际尝试 | 最终效果 | 已知 tokens |
|---|---|---|---|---|---|---:|
| Gemini gemini-2.5-flash | 通过 | 1 / 1 / 0 | A、B、B | 2 条、不同键 | 911 |
| DeepSeek deepseek-flash | 通过 | 1 / 1 / 0 | A、B、B | 2 条、不同键 | 1245 |
| MiniMax MiniMax-M3.1-Flash | 通过 | 1 / 1 / 0 | A、B、B | 2 条、不同键 | 1491 |

最终 v2 共 6 次真实请求，3647 tokens。恢复只请求最终答案，A 的工具函数不重跑；B 使用原子键去重读取旧效果。模型最终答案均包含两条回执。完整断言检查图完成、工具执行次数、效果文件数量、两个不同子键、请求预算和完成重放。

证据：[Gemini](gemini-v2.json)、[DeepSeek](deepseek-v2.json)、[MiniMax](minimax-v2.json)。JSON 不包含密钥、模型续接内部数据或完整业务对话。

## 首次失败与改进

初版 v1 使用无参数工具 `{}`。Gemini、MiniMax 通过；DeepSeek 返回额外字段 `key`，Dispatcher 按 `additionalProperties:false` 拒绝，零工具执行、零效果。第一次请求的 449 tokens 保留在 [deepseek.json](deepseek.json)，失败日志未被覆盖。该版本模型计划仅检查对象结构，因此当时模型操作为 completed，错误发生在 Dispatcher 参数校验处。

检查适配层后，schema 通过原生请求路径传给模型，没有发现代码添加 key 参数。本次未弱化校验：案例改用明确的 stage 枚举，并增加模型回合的整轮 schema 预检。现在若第二个工具参数无效，第一个工具也不会执行，整个模型操作保存 failed、保留用量，重放返回 ErrOperationAttention。该负面场景通过离线回归测试；新版三模型使用相同 v2 案例重新验证。单次通过不代表模型以后不会生成无效参数。

v1 总计 5 次请求、2626 tokens；全部尝试合计 11 次请求、6273 tokens。未自动重试失败样本；v2 是代码与案例修改后的独立运行。保留 v1 的 gemini.json、deepseek.json、minimax.json 与日志。

## 离线验证

- `GO_TEST_MODE=true go test -race -timeout 10m ./...`：通过，65 个有测试的包。
- `go build ./...`：通过；第三方 sqlite3 C 编译有 const 限定符警告，详见 build.log。
- `golangci-lint run ./...`：0 issues。
- `bash scripts/verify_imports.sh`：0 findings。
- 最后一次操作日志保存失败/解锁错误处理调整，另跑 core/durable race 和 lint；见 storage-final.log、storage-lint.log。

新增验证包括：并发重复操作只执行一次、不同 CallID 不混用回执、参数/策略变化拒绝恢复、取消后失败证据保存、意图保存失败时零调用、请求后结果保存失败时阻止重复付费、无效 JSON/截断/流提前结束用量保留、无效第二个工具参数导致整轮零效果、进程重启保留原生 reasoning 续接、类型化流预览不提前推进图、映射失败取消上游。

## 重现

```sh
go test -race ./core/durable ./tools ./builder ./workflow ./examples/workflow/business-cases
GEMINI_MODEL=gemini-2.5-flash bash scripts/business_tool_loop_live.sh gemini /tmp/new-gemini-loop.json
DEEPSEEK_MODEL=deepseek-flash bash scripts/business_tool_loop_live.sh deepseek /tmp/new-deepseek-loop.json
MINIMAX_MODEL=MiniMax-M3.1-Flash bash scripts/business_tool_loop_live.sh minimax /tmp/new-minimax-loop.json
```

先通过环境配置对应 API key。每次真实案例最多 4 个请求、单请求最多 2048 输出 tokens，报告路径必须不存在。普通测试不调用真实模型。

## 边界

业务效果均为本地模拟，未操作真实运维、文章发布或消息服务。FileStore 是单机持久化，外部效果 exactly-once 仍需业务事务与幂等。模型失败后保存已知用量；未收到或来不及保存的用量无法重建。工具结果必须 JSON 往返，模型/提示词/策略变化必须提升图 Version。外部记忆、检索和中间件须由宿主保证恢复时稳定。

MiniMax 是案例级、缓冲的原生工具适配器，不是完整全局 Provider 或 token SSE 实现；按[官方原生工具续接约定](https://platform.minimax.io/docs/api-reference/text-openai-api)保留 reasoning_content。跨节点 Collect/Transform、分布式调度仍不在本次实现范围。
