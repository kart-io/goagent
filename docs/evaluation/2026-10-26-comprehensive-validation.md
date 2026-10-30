# 全量功能验证

日期：2026-10-26。范围为当时工作区主模块、真实服务集成和可离线执行的示例。Go 1.25.3，darwin/arm64，CGO 开启。全量测试不带 -short，启用竞态检测和 atomic 覆盖率；没有调用线上模型。

## 结果

| 检查 | 结果 |
|---|---|
| 主模块全部 189 个包的构建/测试检查 | 退出码 0；187 个包 pass，2 个纯配置/常量包无测试 |
| 实际有测试的主模块包 | 60 个 |
| 主模块顶层测试 | 2,715 通过，12 跳过，0 失败 |
| 计入子用例 | 4,731 通过，14 跳过，0 失败 |
| 全仓 go build、go vet | 退出码 0 |
| golangci-lint v2.6.2 | 0 issues |
| 离线冒烟 | 7 项通过，实际运行并检查输出标记 |
| CGO 关闭的 SQLite 测试选择 | 退出码 0；CGO-only 测试按构建约束排除/明确跳过 |
| 独立代码审查 | 生产修复无遗留重要问题；测试清理及 CGO 可移植性意见已处理 |

通过数来自 go test -json，而不是统计函数名。子用例不是独立生产样本。通过仅说明运行的检查成功，不证明全部业务路径正确。

## 实际执行的范围

- Agent/Builder：原生工具循环、终态直返、请求预算、状态/记忆、检索来源、回调、中间件与事件流。
- workflow：图编译、分支、显式并行 Clone/Merge、循环上限、适配、异常终态、取消和竞态。
- 检索/文档：BM25、混合、向量、重排序及真实 PDF 加载、按页读取与切分。
- 基础能力：规划、推理策略、反思执行、工具注册/并发/缓存、存储、检查点、性能工具、可观测性、分布式模块的现有自动测试。
- MCP：官方 SDK HTTP 和真实 stdio 子进程互操作。
- 真实服务：临时 PostgreSQL 命名空间 CRUD、隔离/取消与重连；Redis CRUD 与检查点重连；NATS 点对点/广播/退订；Qdrant CRUD/批处理、业务 ID、元数据与 UUID 兼容。
- 示例：workflow 同步/事件流、文本切分、Generator、Skill 协作、Mock InvokeFast 演示、计算器。均先编译为临时二进制再在临时目录运行；冒烟不代替各模块的语义断言。

服务分别使用 postgres:16-alpine、redis:7-alpine、nats:2.11.10-alpine 和 qdrant/qdrant:v1.16.2，均为独立临时容器；没有操作已有业务容器或数据库。Redis 7 不支持客户端新握手扩展时出现自动降级日志，CRUD 和检查点断言通过。

## 发现并修复

1. **Qdrant 业务 ID 无法写入**：直接发送 doc1 等字符串导致服务器 UUID 解析失败。非 UUID 业务 ID 现在确定性映射到 UUID，Add/Delete 共用映射，原 ID 保留于 payload；已有 UUID 点保持可寻址。
2. **Qdrant 元数据丢失**：按值是否为零判断字段类型会把 false、0、空字符串变成 nil。改按 protobuf oneof 类型解码，并阻止 metadata 覆盖保留的 id/page_content。写入和删除等待服务确认完成，避免紧接着读到未完成的操作；集合初始化失败释放客户端。
3. **PostgreSQL 忽略取消**：已取消的 Put 仍返回成功并写入。所有 GORM 数据操作现传入 WithContext(ctx)，离线无查询回归和真实服务取消回归均通过。
4. **SQLite 的声明驱动无法使用**：schema 声明 sqlite，注册名为 sqlite3。新增别名映射并保留 sqlite3 兼容；成功结果显式设置 Success=true。此前误以为缺驱动而跳过的 SQL 注入实库测试现已运行。
5. **构造测试错误依赖真实密钥**：3 个不请求模型的 LLM 配置测试改为测试凭据和支持的 Provider 构造入口。旧 llm.NewClientWithOptions 本身为显式不支持的历史入口，已标记弃用并添加其限制回归；没有把该占位入口包装为已实现功能。

缺陷均先通过实际失败或新增失败回归复现，再修复验证。初次全量覆盖率生成还暴露本机 PATH/GOROOT 指向不同 Go 版本的问题；最终验证同时对齐两者，未修改全局配置。原始初次失败记录保留在临时产物目录，最终结果没有将失败重分类为成功。

## 覆盖率和边界

- 全仓语句覆盖率：**40.3%**，包括大量仅编译而未自动执行的示例和命令代码。
- 排除 examples、cmd、testing 测试辅助包后的库代码：**69.1%**。过滤仅改变统计口径，没有改变原始 profile。
- workflow：87.7%；retrieval：82.0%；agentbench：89.1%；Builder：68.1%；core/durable：76.7%。

仍未达到项目文档所列的所有包 80% 目标。测试通过不能代替未覆盖的错误分支、长会话模型质量、真实业务题集验收或生产流量压测。当前没有线上模型供应商质量/费用排名、多副本 Qdrant 一致性、NATS 持久化投递和跨主机接管验证。

## 包级统计

下表的通过数包含子用例；覆盖率为该包自己的自动测试覆盖，不计人工冒烟运行覆盖。只有测试代码、没有生产语句的 integration 包不产生 profile 行，但其真实服务测试已执行。

| 包 | 通过 | 跳过 | 语句覆盖率 |
|---|---:|---:|---:|
| `agents` | 114 | 0 | 79.5% |
| `agents/base` | 40 | 0 | 29.9% |
| `agents/cot` | 65 | 0 | 80.2% |
| `agents/executor` | 36 | 0 | 65.2% |
| `agents/got` | 32 | 0 | 68.0% |
| `agents/metacot` | 30 | 0 | 66.9% |
| `agents/pot` | 38 | 0 | 65.6% |
| `agents/react` | 31 | 0 | 85.3% |
| `agents/sot` | 24 | 0 | 74.3% |
| `agents/specialized` | 122 | 0 | 94.6% |
| `agents/tot` | 48 | 0 | 73.9% |
| `builder` | 197 | 0 | 68.1% |
| `cache` | 41 | 0 | 89.6% |
| `core` | 275 | 0 | 70.6% |
| `core/checkpoint` | 100 | 0 | 90.8% |
| `core/durable` | 3 | 0 | 76.7% |
| `core/execution` | 15 | 0 | 88.0% |
| `core/middleware` | 124 | 0 | 88.7% |
| `core/state` | 86 | 0 | 87.1% |
| `distributed` | 142 | 0 | 96.4% |
| `document` | 101 | 0 | 82.1% |
| `errors` | 73 | 0 | 73.7% |
| `evaluation` | 2 | 0 | 91.2% |
| `evaluation/agentbench` | 58 | 0 | 89.1% |
| `interfaces` | 41 | 0 | 0.0% |
| `llm` | 40 | 2 | 59.0% |
| `llm/cache` | 24 | 0 | 55.0% |
| `llm/providers` | 378 | 0 | 72.2% |
| `mcp/remote` | 2 | 0 | 71.4% |
| `mcp/toolbox` | 12 | 0 | 49.7% |
| `memory` | 213 | 0 | 85.1% |
| `middleware` | 22 | 0 | 29.7% |
| `multiagent` | 99 | 0 | 76.2% |
| `observability` | 250 | 0 | 93.1% |
| `parsers` | 20 | 0 | 31.2% |
| `performance` | 115 | 1 | 75.9% |
| `planning` | 119 | 0 | 79.4% |
| `prompt` | 163 | 0 | 80.7% |
| `reflection` | 2 | 0 | 20.6% |
| `retrieval` | 305 | 0 | 82.0% |
| `store` | 19 | 0 | 82.6% |
| `store/adapters` | 12 | 0 | 33.1% |
| `store/memory` | 19 | 0 | 89.0% |
| `store/postgres` | 25 | 1 | 68.6% |
| `store/redis` | 25 | 0 | 88.8% |
| `stream` | 176 | 0 | 48.8% |
| `tools` | 262 | 6 | 66.3% |
| `tools/compute` | 59 | 0 | 86.7% |
| `tools/http` | 32 | 0 | 98.6% |
| `tools/middleware` | 38 | 0 | 86.8% |
| `tools/plugingen` | 28 | 0 | 78.5% |
| `tools/practical` | 234 | 0 | 61.0% |
| `tools/search` | 21 | 0 | 89.2% |
| `tools/shell` | 21 | 0 | 94.9% |
| `utils` | 67 | 0 | 97.7% |
| `utils/httpclient` | 22 | 0 | 93.5% |
| `utils/json` | 27 | 0 | 59.2% |
| `workflow` | 39 | 0 | 87.7% |

## 明确跳过的测试

以下 14 项未计为通过。PostgreSQL 的历史构造占位测试虽仍跳过，新 integration 包已另行测试真实构造、迁移和持久化；历史失效 API 测试不代表现有实现保留这些能力。

| 包 | 测试 | 原因 |
|---|---|---|
| `examples` | `TestSupervisorAgent_DeepSeek_Optimized` | supervisor_agent_example_optimized_test.go:20: DEEPSEEK_API_KEY environment variable not set |
| `examples` | `TestSupervisorAgent_MultipleScenarios/simple_task` | supervisor_agent_example_optimized_test.go:178: Skipping table-driven test - requires mock LLM implementation |
| `examples` | `TestSupervisorAgent_MultipleScenarios/multi_agent_task` | supervisor_agent_example_optimized_test.go:178: Skipping table-driven test - requires mock LLM implementation |
| `examples` | `TestSupervisorAgent_DeepSeek_Example` | supervisor_agent_example_test.go:106: DEEPSEEK_API_KEY environment variable not set |
| `llm` | `TestExampleOpenAIOptions` | integration_test.go:247: Example only |
| `llm` | `TestExampleConfigWithOptions` | integration_test.go:272: Example only |
| `performance` | `TestCachedAgent_MaxSizeEviction` | cache_test.go:252: SimpleCache 不支持 maxSize 驱逐策略，已简化为仅支持 TTL |
| `store/postgres` | `TestNew` | postgres_test.go:45: Requires real PostgreSQL connection |
| `tools` | `TestToolExecutor_Timeout` | parallel_test.go:374: Skipping flaky timeout test - behavior varies by execution environment |
| `tools` | `TestInvalidateByPattern` | tool_cache_invalidation_test.go:18: SimpleToolCache 已删除模式失效功能,CachedTool使用独立缓存实例 |
| `tools` | `TestInvalidateByTool` | tool_cache_invalidation_test.go:144: SimpleToolCache 已删除工具失效功能,CachedTool使用独立缓存实例 |
| `tools` | `TestDependencyTracking` | tool_cache_invalidation_test.go:230: SimpleToolCache 已删除依赖追踪功能 |
| `tools` | `TestVersioning` | tool_cache_invalidation_test.go:367: SimpleToolCache 已删除版本号失效功能 |
| `tools` | `TestInvalidationStatistics` | tool_cache_invalidation_test.go:442: SimpleToolCache 已简化统计功能 |

## 复现与产物

配置隔离测试服务后，参见 [真实服务验证说明](https://github.com/kart-io/goagent/blob/v0.16.1/integration/README.md)（该目录已在 v0.17.0 删除）。本轮主命令：

```sh
GO_TEST_MODE=true GOMAXPROCS=2 go test -json -p 2 -race \
  -covermode=atomic -coverprofile=/tmp/goagent-full.coverage.out \
  -count=1 -timeout 300s ./...
```

真实服务端点、PDF 路径和移除线上密钥的进程环境按 integration README 配置。

机器可读结果：[summary JSON](2026-10-26-comprehensive-summary.json)。本次完整 JSONL、覆盖率 HTML/profile、初次失败记录与冒烟日志保存在 `/tmp/goagent-full-validation-20261026/`，未写入仓库。
