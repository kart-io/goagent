# GoAgent / DeepSeek 退款客服案例实测

2026-10-27 的真实模型测试中，GoAgent 完成 12 次运行，答案与调用契约均通过，共 21 个模型请求，所有运行用量完整。

本目录仅保留原始测试会话中的 GoAgent 记录。清理时按框架筛选记录，未重新请求模型、未剔除 GoAgent 失败样本、未重新测量时间。run-context.json 中的起止时间是原始整个测试会话的时间，不是这 12 次运行的连续耗时。

题集是三道人工编写的退款示例，不含真实客户数据。每种配置重复三次，不能视为 12 个独立业务样本。

## 配置

| 项目 | 原始运行设置 |
|---|---|
| 模型 | DeepSeek 官方 API，`deepseek-flash`，服务端默认 thinking 设置 |
| 源码 | 基于 `335d5c6446e0271af379f77aae4874118761af41` 的未提交工作区，并非该提交的干净版本 |
| Go | go1.27.1 linux/amd64 |
| 任务 | 三题；退款状态分别执行普通循环和直返；每个配置重复三次 |
| 预算 | 每次运行最多 2 次请求，每请求最多 2,048 输出 Token，观察用量阈值 8,000 |
| 超时与温度 | 90 秒；温度请求值 0.2，生效情况取决于服务端模式 |

## 结果

| 指标 | 实测值 |
|---|---:|
| 答案与调用契约通过 | 12/12 |
| 模型请求 | 21 |
| 输入 Token | 7,083 |
| 输出 Token | 536 |
| 总 Token | 7,619 |
| 用量完整 | 12/12 |

| 场景 | 预期结果 | 通过 | 每次模型/工具调用 | 三次总 Token | P50 / P95（秒） |
|---|---|---:|---|---:|---:|
| 退款期限提取 | `7`，排除配送天数 `3` 的干扰 | 3/3 | 2 / 1 | 2,010 | 2.526 / 2.749 |
| 退款状态：普通循环 | 原样返回状态文本 | 3/3 | 2 / 1 | 2,095 | 2.496 / 2.725 |
| 退款状态：直返 | 原样返回状态文本 | 3/3 | 1 / 1 | 1,337 | 1.501 / 1.528 |
| 手续费资料缺失 | `UNKNOWN`，没有误用国内运费 `0` | 3/3 | 2 / 1 | 2,177 | 2.616 / 3.877 |

状态文本：`订单 DEMO-1002：退款已受理，预计 3 个工作日到账。`

状态直返减少一次模型请求。每组只有三次运行，P95 为组内最大值，不能据此判断长期尾延迟或纯框架性能。Token 数据未包含缓存命中计价明细，不直接换算成账单金额。

## 继续使用案例

题集现位于主模块 [refund-cases.json](../../../evaluation/agentbench/testdata/refund-cases.json)。在仓库根目录执行离线回归，无需凭据：

```sh
go test -race -count=1 -v ./evaluation/agentbench -run '^TestRefundScenario$'
```

通过主模块评测命令再次调用真实模型（输出目录必须不存在）：

```sh
OPENAI_API_KEY="$DEEPSEEK_API_KEY" \
OPENAI_BASE_URL=https://api.deepseek.com \
OPENAI_MODEL=deepseek-flash \
go run ./evaluation/agentbench/cmd/stress -live \
  -dataset evaluation/agentbench/testdata/refund-cases.json \
  -mode all -repeats 3 -workers 1 -timeout 90s \
  -out /tmp/goagent-refund-live
```

此命令使用现有 stress 入口，其每请求输出上限为 512 Token，与原始记录的 2,048 不同；它会产生新的测试结果，不是原始实验的完全复现。去掉 `-live` 即仅生成计划，不请求模型。

## 数据与边界

- [dataset.json](dataset.json)：原始题集及标签。
- [results.jsonl](results.jsonl)：保留的全部 12 次 GoAgent 运行。
- [summary.json](summary.json)：按场景和直返配置汇总。
- [manifest.json](manifest.json)：原始参数、题集哈希及记录筛选说明。
- [run-context.json](run-context.json)：原始会话时间、源码状态及筛选说明。

此案例尚未覆盖生产检索、带业务参数的工具、写操作授权与幂等、图恢复或大规模负载，不替代生产验收。
