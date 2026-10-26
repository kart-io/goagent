# Eino 建议落地与验证

日期：2026-10-26。实现沿用 GoAgent，Eino 作为独立对照及编排验证模块；未迁移主执行底座。

后续同日已新增主模块 workflow 编排能力，见[通用流程编排验证](2026-10-26-workflow-validation.md)。本文记录此前 Eino 对照任务的交付与当时边界。

## 已交付

- README、快速入门、文档入口和 grounded RAG 示例统一推荐 AsRunnable。旧 Execute/ExecuteWithTools 保留兼容语义。
- Builder ContextPolicy 显式启用完整工具回合保留、大结果引用和 Unicode 分页读取；引用隔离到单次执行，最终回合恢复原始证据。
- 显式历史摘要，保留近期消息、当前问题及原始记忆；摘要请求计入请求预算和累计用量，缺失用量保留未知状态。
- Eino v0.9.22 + OpenAI 扩展 v0.1.13 独立模块，通过共享 RunWithExecutor 使用相同提示、工具数据、Meter、题集和评分。
- Eino 分支图可调用现有 GoAgent Runnable；验证两条路径、输入错误、业务失败和取消，不引入主模块依赖。
- 题集导入通过 DecodeSuite 从空结构解码，修正可选字段继承预生成题目的问题；原有 stress 命令也使用相同入口。

## 验证方法

```sh
GOMAXPROCS=2 GO_TEST_MODE=true go test -p 2 ./... -short -count=1 -timeout 120s
GOMAXPROCS=2 GO_TEST_MODE=true go test -p 2 -race ./builder ./evaluation/agentbench/... -short -count=1 -timeout 120s
cd evaluation/einocompare
GOMAXPROCS=2 go test -p 2 -race ./... -short -count=1 -timeout 120s
go run ./cmd/workflow
go run ./cmd/compare -count 24 -out /tmp/goagent-eino-preview
```

本机 Go 可执行文件为 1.27.2，环境 GOROOT 指向 1.27.1。验证命令仅对当前进程设置 `GOROOT=/opt/homebrew/opt/go/libexec`，未修改全局配置。主模块测试不会遍历独立模块，故分别运行。

主模块整仓短测试的 58 个有测试包通过；Builder/agentbench 与独立 Eino 模块的竞态检查通过。

CLI 另经受控本地 HTTP 上游验证：三条人工固定标签，覆盖无工具、依据工具资料回答、Unicode 多行原文直返。all 模式实际执行 8 条框架/配置记录，答案和调用契约全部通过；单请求预算的 baseline 模式实际执行 6 条记录，其中 4 条工具任务按预期失败，返回非零状态并完整保存失败记录、用量和汇总。首次文本时间包含工具直返；manifest 不包含凭据。此测试的上游为假模型，不构成模型质量或性能成绩。

## 审查修复

独立只读代码审查发现并已用失败测试复现后修复：摘要 Client 参与配置序列化可能暴露凭据、过期任务被评分成功、Eino lookup 包装器比 GoAgent 额外限制参数，以及 Eino 工具直返首次文本时间缺失。没有用放宽评分标准处理失败。

## 工程选择及边界

保留 GoAgent 底座可以复用既有工具、检索、记忆和评测，代价是主模块仍不具备 Eino 的通用 Graph 编排与流变换。当前适配验证仅连接最终结果，不等于迁移已经完成。

卸载存储选择执行内存，避免引入持久化生命周期和文件权限语义，代价是不减少进程内存，也不能跨进程恢复。最终回合恢复原文优先保证证据可读，代价是最终请求不保证变小。摘要失败明确报错，调用方须为摘要和业务执行共同配置预算。

尚无真实业务题集验收、线上 GoAgent/Eino 同条件结果、长会话质量与累计成本测量、Eino 图恢复/跨节点 Token 变换验证。因此不提供速度、成功率或费用的框架排名。
