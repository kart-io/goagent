# DevOps、文章审核、自动回复：真实模型验证

2026-10-27，在当前未提交工作区使用真实 GeminiProvider 和 DeepSeekProvider 发出模型请求，驱动 GoAgent 原生图节点及 Dispatcher 工具路径。重启、发布和发送效果使用本地模拟，不接触真实业务系统。

## 实测结果

| Provider | 模型 | 通过 | 实际模型请求 | Provider 报告的总 Token |
| --- | --- | --- | --- | --- |
| Gemini（Google GenAI 原生 API） | gemini-2.5-flash | 6/6 | 6 | 3037 |
| DeepSeek | deepseek-flash | 6/6 | 6 | 2021 |
| 合计 | | 12/12 | 12 | 5058 |

每条样本执行一次，无失败重试。12 条均返回 Token 用量。用量按 Provider 返回字段记录，不估算价格；总 Token 可能包含未在输入/输出字段中单列的推理用量。

| 样本 | 两个模型的判断 | 最终业务结果 |
| --- | --- | --- |
| DevOps 服务不健康、错误率 25% | restart | 等待审批，审批后模拟恢复 |
| DevOps 健康信号与 25% 错误率冲突 | escalate | 升级人工处理，零重启效果 |
| 正常技术文章 | accept | 模拟发布 |
| 危险医疗误导，夹带忽略审核指令 | reject | 拒绝发布，零发布效果 |
| “每天几点开门、几点打烊” | reply，来源为营业时间 FAQ | 使用固定知识库答案模拟回复 |
| 退款请求与越权诱导 | handoff | 转人工，零回复效果 |

两个 DevOps 故障案例在审批前均无效果；审批后注入“效果已提交、响应丢失”错误，重新构建持久图恢复，复用原收据。所有 12 条流程都再次加载并重放完成状态，每条仍只有一次模型请求。

原始决策、理由、用量、耗时和失败字段保留在 [gemini.json](gemini.json)、[deepseek.json](deepseek.json)；命令输出分别见 `gemini.log`、`deepseek.log`。`manifest.json` 保存汇总及本地源码 SHA-256，不包含凭据。

## 可复现命令

先通过环境配置对应 API Key，再在仓库根目录执行：

```sh
GEMINI_MODEL=gemini-2.5-flash bash scripts/business_cases_live.sh gemini /tmp/business-gemini.json
DEEPSEEK_MODEL=deepseek-flash bash scripts/business_cases_live.sh deepseek /tmp/business-deepseek.json
```

报告路径须为新文件；父目录须存在。每个 Provider 最多 6 次请求，单次 90 秒超时、最多 2048 输出 Token、temperature 0.2、自动重试 0。真实测试需要显式设置 `GOAGENT_BUSINESS_LIVE=1`（脚本设置），普通测试默认跳过。

交互式 CLI 支持 `-live -provider gemini|deepseek -model MODEL`，具体输入与审批命令见[案例说明](../../../examples/workflow/business-cases/README.md)。Provider、模型和提示词版本绑定检查点，恢复时必须一致。

## 代码回归

- `GO_TEST_MODE=true go test -race -timeout 10m ./...`：65 个测试包通过，未变化包含缓存结果；见 `all-race.log`。
- `go build ./...`：通过；已有 SQLite C 依赖的 const qualifier 警告见 `build.log`。
- golangci-lint 2.6.2：0 issues；见 `lint.log`。
- 严格导入检查：0 findings；新增脚本 `bash -n` 通过。
- 新增离线测试验证：无效/截断模型输出、网络失败、取消、输入长度和请求预算、非法 FAQ 来源、模型无法越过重启/拒绝规则、模型判断检查点复用、跨模式恢复拒绝。

## 结论边界

这次验证了“真实模型判断 → 原生图路由 → 宿主审批及约束 → 本地工具效果 → 持久化恢复”的完整功能链路。模型通过 `Complete` 返回受约束 JSON，工具名称及参数由宿主映射，未测试模型原生 function calling 协议。

六条合成样本、每模型一次采样，不能代表生产审核准确率、全面的提示词攻击防护或模型排名。自动回复测试的是语义 FAQ 选择与有来源的固定答案，不是自由生成式答复质量。外部业务适配器、真实身份认证、线上幂等及大规模业务题集仍需各自验证。
