# 框架性能对测报告

> 清理说明：本目录的测试日志及 Python 脚本已删除；历史测试结论和现存 JSON/Markdown 汇总保留。

日期：2026-10-27。有效数据为 v2；初轮无效数据保留在 [preliminary](../2026-10-27-framework-performance/INVALID.md)。

## 结论与适用范围

在本机、已构建的暖执行、本地即时模型任务下，GoAgent 的小图、条件循环和一次工具循环开销低于 Eino ADK/compose 的对应入口；64 段文本流中，Eino 的总完成时间中位数更低，GoAgent 的首段到达更早且本次尾延迟更低。GoAgent 在四项 Go 场景中分配字节与次数均较少。

这是指定 API、配置与工作负载的测量，不是框架综合排名。Eino compose 与 GoAgent Graph 的通用性、状态表示和调度机制不同；本次未验证两者所有功能等价。Python 对照包括语言、对象模型和运行时差异，不将其时间差单独归因于框架实现。

## 环境与版本

- Apple M4 Pro，arm64，14 个逻辑 CPU，48 GiB 内存，macOS 26.7.1。
- Go 1.27.2，GOMAXPROCS=4；GoAgent 本地实现基线 `3bfeea70fcce35c3e925a295cc42546c1ec80b1b`；本轮没有修改其生产实现。
- Eino v0.9.22，使用当前 ADK ChatModelAgent/Runner 与 compose.Graph；没有使用旧 ReAct 入口。
- CPython 3.14.8，LangGraph 1.2.18、LangChain 1.4.7、OpenAI Agents SDK 0.24.0、Google ADK Python 2.13.0。
- 所有 Go 对象在同一独立 benchmark module 中解析依赖，因此 Eino 的部分传递依赖由 MVS 选为与 GoAgent 共用版本，详见 build-info；这不等于 Eino 独立默认依赖树。
- 本机为有其他桌面应用运行的开发机，没有独占 CPU/亲和性设置。测量期间未并行运行编译、测试或另一项 benchmark；前后没有系统热限制警告。记录重复间波动，不把单机样本当生产 SLA。

完整工具链、时间、source SHA256、依赖锁与调度参数见 [manifest](manifest.json)、[Go build info](go-build-info.txt)、[requirements.lock](../../../evaluation/frameworkbench/requirements.lock)。

## 测量方法

1. 真实执行框架公有入口，模拟的只是模型响应和业务工具数据。无 API Key、远端推理和 HTTP/SSE；Python connect 被禁用，外部 tracing 关闭/noop。
2. 图最终值均为 8。工具任务均为模型规划 → lookup(order_id=DEMO-1001) → 回执 30 → 模型校验回执并回答 30，恰好两次模型调用、一次工具调用，未使用直接返回优化。
3. 流任务均为一次模型请求、64 个已就绪的 x 文本增量、最终 64 字符。最终快照不再次累加。首段延迟指消费者观察到第一个文本增量，不是网络或推理 TTFT。
4. 构造、图编译、导入和 warmup 不计时。ADK 必需的新内存会话在 prepare/cleanup 阶段创建/删除，不计入耗时；run_async 内部会话处理仍计时。所有框架均无持久 checkpointer/会话历史累积。
5. GoAgent Invoke 使用其原生流执行源收集最终结果；Eino 非流 Agent 使用 Generate。这是当前推荐入口的实现差异，没有人工替换 GoAgent 为更快的旧 Execute。
6. Python 工具、图节点与路由均为原生 async，避免微小同步回调触发线程池往返；每个 job 一个 event loop，未逐操作 asyncio.run，asyncio debug 明确关闭并逐组断言。Python GC 保持开启。
7. 16 个 framework/scenario 组合，每组合 5 个独立进程，每进程 warmup 50 次、正式 400 次，共 80 组、32,000 次正式操作；所有操作都检查答案、模型/工具次数或增量数。调度按固定 seed 在每轮随机打乱，串行执行。
8. 下表 P50/P95 是先计算每进程分位数，再取五轮的中位数；P95 使用 nearest rank。重复范围是五轮 P50 的最小/最大，不是置信区间。未进行显著性检验，不将纳秒级差异直接推广。
9. Go 分配数据单独使用 testing.B，benchmem、300ms、5 次；ns/op 是该 pass 的均摊时间，与逐操作延迟 P50 不混为一个指标。构造均在 ResetTimer 前。

## 同语言 Go 对照

单位为微秒，越低越好。P50/P95 口径见上文。

| 场景 | GoAgent P50 | Eino P50 | GoAgent P95 | Eino P95 | 较大/较小耗时比 |
| --- | ---: | ---: | ---: | ---: | --- |
| 8 节点串行图 | 0.500 | 10.979 | 1.708 | 27.000 | Eino / GoAgent = 21.96 |
| 8 次条件循环 | 0.541 | 10.209 | 1.542 | 25.042 | Eino / GoAgent = 18.87 |
| 两轮模型＋一次工具 | 9.229 | 53.917 | 14.917 | 129.500 | Eino / GoAgent = 5.84 |
| 64 段文本流 | 43.667 | 25.791 | 55.750 | 79.584 | GoAgent / Eino = 1.69 |

耗时比是较大耗时除以较小耗时，不等于整个真实业务会得到同倍数加速。

### 首段与完成时间

| 框架 | 首段 P50（µs） | 完成 P50（µs） | 完成 P95（µs） |
| --- | ---: | ---: | ---: |
| goagent | 10.333 | 43.667 | 55.750 |
| eino | 21.979 | 25.791 | 79.584 |

这组结果表明“首段快”和“总完成快”应分别评价。64 段都已就绪，不代表真实服务端按时间产生增量时也有同样差距。

### Go 分配数据

每项为五次 testing.B 的中位数；B/op 是分配流量，不是峰值 RSS 或保留堆大小。

| 场景 | GoAgent ns/op | Eino ns/op | GoAgent B/op | Eino B/op | GoAgent allocs/op | Eino allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 8 节点串行图 | 709.0 | 14593.0 | 2049 | 33392 | 32 | 358 |
| 8 次条件循环 | 734.2 | 12874.0 | 2049 | 30264 | 32 | 339 |
| 两轮模型＋一次工具 | 9375.0 | 60279.0 | 7938 | 87967 | 97 | 1345 |
| 64 段文本流 | 44744.0 | 31083.0 | 22808 | 57909 | 192 | 783 |

[分配汇总](go-allocations-summary.json) 保留五轮结果，原始分配日志已清理。流场景再次表现为 GoAgent 分配较少、完成时间更高，不能以分配量单独推导吞吐。

## Python 与跨运行时观察

单位为微秒。LangGraph 用 StateGraph 的 async 图；Agent 任务用 LangChain create_agent，不将手写 model/tool 循环冒充预构建 Agent。空白表示本次未测，不代表框架不支持。

| 框架 | 8 节点图 P50 | 条件循环 P50 | 工具循环 P50 | 64 段流 P50 | 首段 P50 |
| --- | ---: | ---: | ---: | ---: | ---: |
| langgraph | 1520.104 | 2282.271 | — | — | — |
| langchain | — | — | 1586.583 | 1461.771 | 1066.562 |
| openai_agents | — | — | 1971.583 | 1220.062 | 747.230 |
| google_adk | — | — | 1988.000 | 5625.354 | 5103.667 |

Python ADK 包含运行事件与会话服务路径；各 SDK 的协议模型、钩子和工具校验机制不同。这些数值适合估算本地应用入口的固定开销，不能作为 GoAgent 在相同功能下比 Python 框架快多少倍的结论，也不能评价语言无关的算法效率。

## 重复间波动

各行是五个独立进程 P50 的最小到最大，单位微秒。

| 框架 / 场景 | P50 范围 |
| --- | ---: |
| eino / agent_tool | 52.917–56.916 |
| eino / branch_loop_8 | 10.000–11.562 |
| eino / graph_8 | 10.667–12.416 |
| eino / stream_64 | 25.625–29.146 |
| goagent / agent_tool | 9.041–10.208 |
| goagent / branch_loop_8 | 0.500–0.625 |
| goagent / graph_8 | 0.459–0.542 |
| goagent / stream_64 | 43.062–48.646 |
| google_adk / agent_tool | 1906.021–2051.833 |
| google_adk / stream_64 | 5505.979–5645.854 |
| langchain / agent_tool | 1551.438–1677.646 |
| langchain / stream_64 | 1435.917–1478.667 |
| langgraph / branch_loop_8 | 2274.375–2355.583 |
| langgraph / graph_8 | 1515.021–1524.021 |
| openai_agents / agent_tool | 1947.042–2078.396 |
| openai_agents / stream_64 | 1161.958–1330.479 |

## 对当前 R2 方案的反馈

- 保留同状态 Graph 和类型化 Adapt 的渐进方案：当前小图固定开销已经低，没有性能证据要求立即重写异构 DAG 或反射映射器。
- 流处理值得先做原型。本次 GoAgent 总完成时间落后而首段更早，需要同时评估首段、总时长、取消、背压和分配，不能仅优化一个指标。优先验证事件/channel 交接数量与同步 Fold 的取舍。
- 一次单独的 GoAgent 流 CPU profile 在调度/同步相关符号上有较多样本，见 [profile 摘要](goagent-stream-profile.txt) 与 [原始 profile](goagent-stream.cpu)。Darwin 的采样包含 runtime 等待相关符号，不能据此声称已精确归因到某个 channel 或业务函数；profile 不用于替代上述延迟数据。
- 根预算、持久引用与 OTel 新接入尚未实现，本次没有测到其新增成本。后续应以同一工具/流基线分别开启预算、上下文与观察测增量开销，不能将本次结果当作 R2 实现后的成绩。

## 正确性、问题记录与排除项

- 正式 v2 的 80/80 组、32,000/32,000 次操作通过输出与工作量契约。正确性单测覆盖复用多次调用，以及拒绝少调用/少增量的结果。
- 主项目 `GO_TEST_MODE=true go test -race -timeout 10m ./...` 通过；独立 Go module 的 race 测试通过；Python 5 项 unittest 通过。Python unittest 自身启用 debug 并出现依赖弃用/慢任务提示，这些不在性能计时进程内；正式行的 asyncio_debug 全为 false。
- 首次启动发现外部 GOROOT 指向 Go 1.27.1，而可执行文件是 1.27.2。只在子进程移除 GOROOT，未改用户全局配置。
- 无效初轮误设 PYTHONASYNCIODEBUG=0，实际开启 debug；发现后终止并保留部分原始数据。独立审查另发现同步 LangGraph 回调造成额外 executor 跳转，已统一为原生 async。修复后重新跑全部 80 组，未混用初轮结果。
- 无真实模型、付费、HTTP/SSE、冷启动、并发吞吐、持久化 I/O、崩溃恢复或服务器扩展性对测；Temporal 不进入这些纯进程内表格。Google ADK Go 也未测试。
- 主项目 go.mod/go.sum 未变；第三方依赖仅在独立模块和临时 Python venv。各框架没有为本测试修改源代码。

## 复跑与原始证据

[对测代码和命令](../../../evaluation/frameworkbench/README.md)。运行目录必须新建；不覆盖旧结果。主要证据：

- [每次操作延迟 results.jsonl](results.jsonl)、[summary.json](summary.json)、[随机顺序 schedule.json](schedule.json)。
- [manifest.json](manifest.json)、[分配汇总](go-allocations-summary.json)。
- 主项目测试、独立模块 race、Python 测试和 CPU profile 的原始命令日志已清理。
- 各 job 的 stderr 单独保留；只有依赖警告的行不作失败忽略，若真实退出非零将产生 failure.json 并停止采样。
