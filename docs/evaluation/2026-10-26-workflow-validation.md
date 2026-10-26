# 通用流程编排验证

日期：2026-10-26。新增主模块 workflow 包，保留原有 Builder、core 和独立 Eino 对照模块。

## 已实现

- Graph[T] 泛型状态图，Compile 验证入口、引用、后继、可达性和结构出口。
- 串行节点、显式条件路由、路径汇合、有界循环；MaxSteps 默认 100。
- AddParallel 显式 Clone/Merge、固定结果顺序和 worker 上限；MaxConcurrency 默认 4。
- AddRunnable 接入同状态节点或子图；Adapt 显式转换 Runnable[I,O] 与图状态，并保留事件。
- 编译结果为 core.StreamRunnable，支持 Invoke、Stream、Batch、Pipe、配置和回调。
- 节点/分支事件、事件作用域、严格终态检查，防止局部节点完成被当作整个图完成。
- 无需模型的示例包含快速路径、双来源并行检索、草稿审查循环与最终输出。

## 验证

```sh
GOMAXPROCS=2 GO_TEST_MODE=true go test -p 2 ./... -short -count=1 -timeout 120s
GOMAXPROCS=2 go test -p 2 -race ./workflow -count=10 -timeout 60s
go vet ./workflow ./examples/workflow
go run ./examples/workflow
go run ./examples/workflow -stream
cd evaluation/einocompare
GOMAXPROCS=2 go test -p 2 -race ./... -short -count=1 -timeout 120s
```

本机设置匹配工具链的进程级 GOROOT=/opt/homebrew/opt/go/libexec，未修改全局配置。主模块 59 个有测试包通过；workflow 的 19 个测试函数及子用例通过 10 轮竞态检查。独立 Eino 模块回归、go vet 和 diff 检查通过。

覆盖分支只执行选中路径、循环更新状态与超限、编译拒绝非法定义、图快照、批处理与回调、并行真实重叠与 worker 上限、状态复制、结果顺序、错误/panic/取消、事件作用域、缺失/重复终态与迟到错误、Adapt 输入/输出错误，以及 Builder Agent 的实际流式接入。SDK、模型、外部服务和图持久化恢复不属于此验证范围。

独立只读审查未发现 Critical、Important 或 Minor 问题。审查另以独立 race harness 验证 Adapt 在流关闭前不映射输出，以及 100 次事件背压下的并行取消能等待活动 worker 退出、不输出成功终态。

示例结果：检索路径保留两条来源，草稿执行两次后结束；quick 路径跳过检索和草稿。并行分支事件完成次序不固定，Merge 仍按注册顺序获取结果。

## 边界和取舍

统一状态 T 让输入输出保持类型约束，异构 Agent 通过 Adapt 显式转换；代价是暂不提供异构节点自动推断和字段映射 DSL。普通多入边只汇合被选中的路径，AND 汇合必须通过 AddParallel 表达。

Clone 由调用方实现，避免对任意 Go 对象做不可靠的自动深复制；代价是应用必须正确隔离 map/slice/pointer 的可变字段。并发上限作用于每个并行节点，不是所有图与 Batch 的全局额度。

图流声明为 steps，边传递节点成功的最终状态，允许透传节点原有文本事件；没有跨节点 Collect/Transform。节点用量按 Step 保留作用域，不自动累加全图用量。

执行不自动重试或回滚副作用。并行函数必须响应 context；忽略取消的函数可能阻止节点结束。当前没有持久化图恢复、分布式调度或跨主机接管，core/durable 仍用于显式顺序步骤恢复。
