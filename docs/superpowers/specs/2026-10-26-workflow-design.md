# 通用流程编排

用户要求继续补充通用流程编排。新增 workflow 包，复用 core.Runnable，不引入新的主模块依赖，也不改写已有执行入口。

## 方案与范围

采用同一泛型状态 T 的显式图；异构 Agent 通过业务适配转换为 T。比 any 节点保持更好的类型约束，也避免本轮实现复杂字段映射与动态类型编译器。编译结果为 core.StreamRunnable[T,T]，支持 Invoke、Stream、Batch、Pipe、配置和回调。

提供 AddNode、AddRunnable、Adapt、AddEdge、AddBranch、AddParallel 和 Compile。Adapt 显式映射 Runnable[I,O] 与图状态，待底层流合法结束后转换最终输出，中间事件透传。START/END 为保留名称；每个节点配置一条普通边或一个条件分支，分支返回的目标必须提前声明。普通多入边为路径汇合，不作为 AND 屏障；并行汇合必须通过 AddParallel 显式声明。

Compile 检查入口唯一、节点与边引用、重复与冲突、不可达节点、所有节点存在通向 END 的结构路径。允许有出口的循环，MaxSteps 默认 100，MaxConcurrency 默认 4；配置负值拒绝。循环超限、未声明的动态分支目标、节点/路由/合并错误与 panic 均失败，不发成功最终块。

编译时复制图定义，后续 Builder 修改不影响已编译结果。每次执行的状态、步骤和并行结果独立；调用方保证节点资源可并发使用。T 按值传递，包含 map、slice、pointer 的状态不会自动深拷贝。并行节点必须提供 Clone，所有分支输入在启动 worker 前准备，并由应用负责深复制可变数据。

并行节点采用受限 worker，先完成的分支不决定结果顺序；Merge 接收按分支注册顺序排列的结果，只有所有分支成功才运行。失败立即取消其余分支并停止新任务，等待已启动任务退出；业务函数必须遵守 context。分支 panic 转为错误。事件经调度协程串行发送，避免并发调用 core StreamSource.emit。

StreamMode 为 steps。图透传节点原有事件，但不把节点 Done 当作图 Done；节点状态只在终态成功后推进。对缺少终态、重复终态或终态后错误的自定义 Runnable 明确失败。串行节点 emit node_start/node_end，并行分支 emit branch_start/branch_end，Step 标识节点或节点/分支。图完成后恰好一个成功 Done。提前停止读取必须取消 context。

## 验收

先写行为测试，覆盖路径选择、编译拒绝、循环上限、并行真实重叠/并发上限/顺序/取消/panic/深复制、节点流与虚假成功、图快照、批处理和回调。编译并运行无需模型的示例；整仓短测试、workflow race。只读审查后修复重要问题。持久化恢复、字段映射和跨节点 Collect/Transform 留作明确的后续能力。
