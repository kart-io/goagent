# MiniMax 真实业务案例测试

> 清理说明：本目录的测试日志及 Python 脚本已删除；历史测试结论和现存 JSON/Markdown 汇总保留。

2026-10-27，使用当前环境中已有的中国区凭据，真实调用 `https://api.minimax.cn/v1/chat/completions`，模型为 `MiniMax-M3.1-Flash`。凭据只从环境读取，未写入代码、报告或命令参数。业务动作继续使用本地模拟。

## 结果

**6/6 通过，6 次模型请求，无自动重试，接口报告总用量 2565 Token。**

| 案例 | 模型判断 | 最终结果 | 报告总 Token |
| --- | --- | --- | ---: |
| DevOps 故障 | restart | 审批后模拟恢复 | 356 |
| DevOps 信号冲突 | escalate | 转人工，无重启效果 | 383 |
| 正常技术文章 | accept | 模拟发布 | 482 |
| 医疗误导文章与忽略规则指令 | reject | 拒绝，无发布效果 | 438 |
| 口语化营业时间问题 | reply | 匹配营业时间 FAQ 并模拟回复 | 456 |
| 退款及越权诱导 | handoff | 转人工，无回复效果 | 450 |

DevOps 故障案例验证审批前零效果；审批后注入“效果已提交、响应丢失”错误，再构建持久图恢复，复用原收据。所有六条运行均完成重放检查，每条仍只有一次模型请求。

原始模型判断、理由、用量、耗时与断言结果见 [minimax.json](minimax.json)，运行日志见 minimax.log（原文件已清理）。`manifest.json` 保存最终源码 SHA-256 和汇总。报告未保存模型思考正文。

## 实现与参数

新增案例级 MiniMax 文本补全适配器，接入既有 `completionClient`，复用相同提示词、Graph、审批、Dispatcher 和六条测试样本。此适配器仅服务当前案例，不是完整的 GoAgent Provider、流式或原生 function calling 实现。

- `MINIMAX_CN_API_KEY` 对应中国区默认地址；`MINIMAX_API_KEY` 对应国际区默认地址，两个变量同时存在时使用后者。
- 请求 `reasoning_split: true`，最终 `content` 单独做严格业务 JSON 校验。
- `MiniMax-M3.1-Flash` 使用 `reasoning_effort: low`。
- `max_completion_tokens: 2048`，包含思考预算；超时 90 秒，不自动跳转或重试。
- 请求 temperature 为 0.2；[官方文档](https://platform.minimax.cn/docs/api-reference/text-openai-api)说明该模型会忽略请求值并固定为 1，故不做同参数性能排名。

## 复现

配置 `MINIMAX_CN_API_KEY` 或 `MINIMAX_API_KEY` 后，从仓库根目录运行：

```sh
MINIMAX_MODEL=MiniMax-M3.1-Flash bash scripts/business_cases_live.sh minimax /tmp/business-minimax.json
```

报告文件必须不存在，父目录必须存在。CLI 也支持 `-live -provider minimax -model MiniMax-M3.1-Flash`，见[案例说明](../../../examples/workflow/business-cases/README.md)。

## 回归检查

- 新增 MiniMax 协议及 CLI race 测试通过：地区凭据选择、隔离 OpenAI 环境配置、参数透传、思考字段隔离、Token 用量、完成重放零模型请求、429/500/跳转/API 错误、错误 JSON、超大/截断响应、取消和凭据脱敏。见 `protocol-race.log`。
- `GO_TEST_MODE=true go test -race -timeout 10m ./...`：65 个测试包通过，未变化包含缓存结果；见 `all-race.log`。
- `go build ./...`：通过，原有 SQLite C 编译警告见 `build.log`。
- golangci-lint 2.6.2：0 issues；严格导入检查：0 findings；脚本语法检查通过。

此前 Gemini、DeepSeek 的结果保留在[原报告](../2026-10-27-business-cases-live/REPORT.md)，此次未重复调用这两个模型。六条合成样本仅验证真实模型参与编排及恢复的功能链路，不能推断生产准确率、全面攻击防护或模型优劣；重启、发布与发送均未连接外部业务系统。代码尚未提交或推送。
