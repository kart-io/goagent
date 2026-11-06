# 三项真实模型失败的接入修复

2026-11-07，基线 `497a491fcae22c9981c08450a258d25d61621992`。实现提交 `d8850bc`。本轮处理两项最终回执格式失败，以及 MiniMax 精确小数的传输问题。Gemini 计费阻断按用户要求暂不处理，未创建 Gemini 客户端或发送请求。

四项真实验证全部通过，共 **7 次生成请求、7 次 HTTP 200、4582 个已知 Token**。这是一组定向回归，不代表模型普遍成功率。MiniMax 原数值模式的上游舍入仍然存在；本轮解决方式是显式采用字符串业务协议并严格核验，不声称恢复已丢失的小数位。

## 原因与实现

- DeepSeek 和 MiniMax 的历史最终回答均返回了包含回执的完整 JSON，违反“只输出回执”的原契约。新增共享验收器 [ToolContract](../../../evaluation/agentbench/tool_contract.go)：严格检查最终文本，借助既有 `WithTurnStopping` 最多追加一次纠正，仍不符合时返回错误。没有从 JSON 提取字段充当正确模型答案。
- MiniMax 原始 `input_json_delta` 已把 `0.1234567890123456789` 改成 `0.12345678901234568`。工具 Schema 将 `id`、`rate` 明确声明为字符串，Dispatcher 在授权与效果执行前核对原文。新流程两个字符串完整保留；旧数值样本仍作为拒绝执行的负对照。
- 复用 Builder、根预算、Dispatcher 和 FileStore，不修改 Provider 或框架默认行为。MiniMax 继续使用 `NewAnthropicWithOptions`，实际端点为 `https://api.minimax.cn/anthropic/v1/messages`，模型为 `MiniMax-M3.1-Flash`，完整原生内容与签名逐字段回传。

`MaxIterations=5` 为停止钩子预留逻辑步数，独立根预算、客户端计数与 HTTP 传输计数限制每条新流程最多 3 次请求、1 次工具执行。返回宿主前再调用 `CheckAnswer`；不能仅增加逻辑步数而不设置硬预算。验收器配置变更后必须更新持久化 scope/预算版本，避免复用旧策略终态。

## 真实结果

| 场景 | 新请求 | 工具效果 | 结果 |
|---|---:|---:|---|
| DeepSeek 历史格式失败追加纠正 | 1 | 0 | 保留原错误回答与工具结果，纠正后只输出原回执 |
| MiniMax 历史格式失败追加纠正 | 1 | 0 | 保留原生状态，纠正后只输出原回执 |
| DeepSeek 新数值工具流程 | 3 | 1 | 原精度保留，第一次回答全 JSON，经一次纠正输出回执 |
| MiniMax 新字符串工具流程 | 2 | 1 | 完整保留精确参数，首次回答即为回执 |
| 两条新流程分别重建并重放 | 0 + 0 | 无新增 | 终态、预算及唯一工具效果保持一致 |

新流程的回执随机生成，首次请求未提供回执；模型只能从实际工具结果取得它。没有使用 `WithReturnDirectTools`。历史纠正只追加原错误 assistant 消息和纠正 user 消息，原始请求历史与期望回执不变，不重新执行工具。纠正请求属于显式追加生成，全部计入 7 次请求，无自动重试。本轮上限 8 次，未用满。

输入仍为 `id=9007199254740993`、`rate=0.1234567890123456789`、`label=<测试&>`；DeepSeek 保持 JSON 数字，MiniMax 改为字符串。工具效果是本地合成记录，没有连接真实订单、支付或身份服务。

两项完整流程都重新创建 Builder、Dispatcher、根预算与 FileStore 后重放，禁止网络上限为 0，未产生重复效果尝试。此处验证已完成终态的持久化重放；此前的进程中断恢复记录见[MiniMax 原报告](../2026-11-07-minimax-anthropic/REPORT.md)。

## 本地验证与证据

先验证旧停止策略下两项格式回归失败，再实现一次纠正；定向 race 连续 10 次通过。负对照包括上游已截短数值、字符串 Schema 下误传数字、错误字符串，均在工具效果前拒绝。完整流程验证纠正、严格回执、重放与第二次格式失败的终止。

```sh
GO_TEST_MODE=true go test -race -count=10 -timeout=180s ./evaluation/agentbench -run '^TestToolContract'
GO_TEST_MODE=true go test -race -count=1 -timeout=180s -coverprofile=.claude/remaining-live-20261107/coverage.out -covermode=atomic ./...
bash scripts/check_coverage.sh .claude/remaining-live-20261107/coverage.out
make build vet test-frameworkbench
make lint
go run ./scripts/apicheck
python3 docs/evaluation/2026-11-07-live-contract-remediation/verify.py
```

以上 Go 命令使用项目工具链 1.27.3。全仓 race、构建、vet、lint、独立 frameworkbench race、依赖边界与覆盖率门槛均通过，库覆盖率 79.9%，详情见归档 `coverage-gates.log`；包含示例的原始覆盖率分母不同，不混用。独立只读审查没有发现需修复问题，审查者另跑定向测试通过，未调用真实模型。

[摘要](summary.json)、[原始证据压缩包](raw-evidence.json.gz)、[逐文件校验清单](manifest.json)、[探针源码](probe.go.txt)、[驱动源码](run.py)、[离线核对程序](verify.py)。压缩包包含全部本轮请求、响应、终态、预算、效果、检查日志与历史负对照，记录路径脱敏并扫描凭据，未记录认证头。探针读取源码期间使用的共享验收器也存有快照。

驱动中的历史来源路径位于本地 `.claude/`；复现实测需先从压缩包 `historical/` 恢复请求、响应和场景文件到对应路径，再复制探针为 `.go` 文件编译。实时重跑会新增费用，离线核对无需凭据。MiniMax key 仅从既有配置映射到子进程环境，未修改全局配置。原历史[编解码报告](../2026-11-06-codec-live/REPORT.md)和 MiniMax 报告保持不变。

流式中间文本可能在停止校验前已输出，调用方应以通过校验的最终结果作为权威答案。原 MiniMax 数值模式仍不可用于本组精确小数；需要该模式时须供应商另行解决，不能关闭精度检查。Gemini 的 402 状态本轮未复验。
