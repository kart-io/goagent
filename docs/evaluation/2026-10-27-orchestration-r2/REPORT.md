# 可组合编排 R2 交付验证

> 清理说明：本目录的测试日志及 Python 脚本已删除；历史测试结论和现存 JSON/Markdown 汇总保留。

日期：2026-10-27。实现代码：`772925f`（含性能修正与整分支审查修复）；源摘要见 [manifest.json](manifest.json)。基线：`3bfeea70fcce35c3e925a295cc42546c1ec80b1b`。文档/初次对测输入单独记录为 `b60e033`，不改变生产基线。

## 功能证据

| 边界 | 验证 |
| --- | --- |
| 根预算 | 并发最多准入、重复键、真实进程退出后保留预留、配置冲突、Save 失败无准入 |
| 用量 | 父子争用、摘要计量、失败回合已知用量、completed 回放不重复请求/累加、登记失败后从已保存证据修复 |
| 工具 | 显式 Schema/codec/version、opaque completed/failed/pending、深度 8/9、同 Dispatcher 重入、全部 owned 清理路径 |
| 子会话 | agent-tool 并发真实 MemoryManager 的独立 SessionID 历史、复用 SessionID 负例 |
| 上下文 | 深复制与嵌套字段篡改拒绝、配置指纹、先保存引用再请求、保存失败与恢复、不同配置拒绝旧意图、Unicode 分页 |
| 恢复 | context-recovery 与 composed-order 真实子进程退出 86 后重新运行；后者新增调用 4→0、预留保持 4、已知 token 保持 8 |
| 观察 | 实际订单节点/工具/模型继承同一 OTel trace；节点 panic 不记录成功；Recorder 有界、错误可见；HTML 转义和浏览器检查 |
| 流原型 | Source 返回前预览到达的屏障、Map/Fold 错误不能被 Source 吞掉、晚错不调度图下游、大小限制、并发 accumulator 独立 |
| 兼容 | 旧 fixture 和全量测试通过；无配置不追加预算/策略意图；主模块未添加外部编排依赖 |

浏览器预览已检查循环/分支拓扑、等待/失败/回放颜色和丢弃计数。报告为只读，无脚本或外部资源。真实远端模型未调用；没有据此声明线上模型质量或生产环境性能。

## 检查环境

主验证工具链 Go 1.25.0 darwin/arm64，Apple M4 Pro；另外以本机 Go 1.27.2 运行过全量 race 测试。CI 同版本 golangci-lint v2.6.2，在 Go 1.25 loader 下运行。系统预置 GOROOT 指向旧 Go 1.27.1，所有命令先 unset GOROOT；未修改用户全局环境。

全量 race、build、lint、导入规则退出码均为 0。具体命令：

```sh
env -u GOROOT GOTOOLCHAIN=go1.25.0 GO_TEST_MODE=true go test -race -timeout 10m ./...
env -u GOROOT GOTOOLCHAIN=go1.25.0 go build ./...
env -u GOROOT GOTOOLCHAIN=go1.25.0 /tmp/goagent-r2-tools/golangci-lint run ./...
env -u GOROOT GOTOOLCHAIN=go1.25.0 bash scripts/verify_imports.sh
```

全量测试、构建、lint、导入检查和对测模块契约的原始命令日志已清理，历史结论保留。Go 1.27 不能被旧 lint 的 export-data 解析器正确读取，此工具兼容错误通过使用声明的 Go 1.25 loader 解决，不是项目源码告警。Eino 仍只出现在隔离对测模块中；该模块 tidy 补充现有 OTel API 的间接依赖。

## 同机前后性能与 Eino 参照

两个版本使用相同 benchmark 源、Go 1.25.0、GOMAXPROCS=4、本地确定性模型、Eino v0.9.22。构造不计时，四个场景各跑 5 次、每次 benchtime=300ms，取 5 次中位数。先后串行执行，对测期间不运行编译、测试或其他基准；不能当作并发吞吐、RSS、持久化或远端模型性能。

| 框架/场景 | 前 ns/op | 后 ns/op | 时间变化 | B/op 前→后 | allocs/op 前→后 |
| --- | ---: | ---: | ---: | ---: | ---: |
| goagent/graph_8 | 770 | 783.9 | +1.8% | 2049 → 2049 | 32 → 32 |
| goagent/branch_loop_8 | 827 | 790.2 | -4.4% | 2049 → 2049 | 32 → 32 |
| goagent/agent_tool | 9639 | 10643 | +10.4% | 8092 → 8152 | 100 → 104 |
| goagent/stream_64 | 47049 | 49312 | +4.8% | 22810 → 22863 | 192 → 194 |
| eino/graph_8 | 14952 | 14969 | +0.1% | 33392 → 33392 | 358 → 358 |
| eino/branch_loop_8 | 13019 | 13280 | +2.0% | 30264 → 30264 | 339 → 339 |
| eino/agent_tool | 62744 | 64617 | +3.0% | 87898 → 87897 | 1348 → 1348 |
| eino/stream_64 | 33247 | 33440 | +0.6% | 57909 → 57909 | 783 → 783 |

前后对测数值见 [结构化汇总](benchmark-summary.json)，原始命令日志已清理。Eino 的小幅波动说明存在机器噪声，不能将几个百分点外推成确定的收益。

初测发现默认路径不必要分配 observe.Event、序列化持久意图并生成请求 UUID：图 770→1247 ns、32→41 allocs，工具循环 9639→18050 ns。保留 [初测汇总](benchmark-summary-initial.json)，初测原始日志已清理。修正后未启用观察不分配事件、未启用持久化不序列化意图，图分配回到基线；中间修正版的原始命令日志已清理。

最终图执行约 +1.8%、分支循环约 -4.4%（含噪声），分配不变；工具循环仍约 +10.4%（约 1 µs、+60 B/+4 allocs），64 块流约 +4.8%（约 +54 B/+2 allocs）。剩余开销来自统一调用计数、执行范围和上下文传递，未通过牺牲预算/错误证据移除。该回退明确记录，不预设 5% SLA。GoAgent 在这组图和工具微基准较快，Eino 在 64 块流的总耗时较快；不能声称整体优于其他框架。

复跑：

```sh
cd evaluation/frameworkbench
env -u GOROOT GOTOOLCHAIN=go1.25.0 GOMAXPROCS=4 go test -run '^$' \
  -bench '^BenchmarkFramework$' -benchmem -benchtime=300ms -count=5
```

流原型的 32 项 Fold 中位数 338.1 ns/op、416 B/op、11 allocs/op；结果保留于本报告，原始命令日志已清理。它没有旧同等 API，不能算作前后性能提升。采用结论为保留 experimental 包验证只读增量语义，不发布完整四范式。

## 交付范围与运行限制

R2 T1–T10 首轮范围已实现。完整四范式、反射 Mapping、可恢复子 Agent/嵌套审批、持久摘要、通用 Debugger.Run 继续推迟。模型型工具的 ModelInvoking 标记、会话/后端归属和自定义策略版本仍是受信宿主责任。合作式 context 不能强杀任意不响应取消的函数。

预算账本、引用及模型操作不构成跨文件事务。保留 pending/failed 和原版本 worker，不退款或删除日志来强制重试。迁移与 API 示例见 [可组合编排指南](../../guides/COMPOSABLE_ORCHESTRATION.md)。

最终整分支独立审查发现的 4 个 Important 均已用失败回归修复，最终全量 race 再次通过。详见 [审查与修复记录](FINAL_REVIEW.md)。审查前的原始性能快照日志已清理；主表为审查修复后复跑 5 次的数据。
