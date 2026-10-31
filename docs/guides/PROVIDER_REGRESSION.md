# Provider 持续回归

离线入口：

```sh
bash scripts/provider_regression.sh /tmp/provider-contracts.jsonl
```

向 master、develop 推送或发起 PR 时运行 `.github/workflows/provider-contracts.yml`（也可手动触发）；Go 1.25、race、short、count=1，失败不重试。原生 OpenAI/DeepSeek、Gemini 协议用独立 HTTP 测试服务覆盖工具结果续接、签名保留、截断与缺失终态、取消、用量和能力降级；脚本同时运行 `./builder` 与 `./evaluation/agentbench/...` 的契约测试；工作流另用 `scripts/apicheck` 检查推荐示例中的旧 API 候选。JSONL 在失败时仍上传。此步骤不使用真实 API 密钥。

真实入口明确指定供应商、模型及新输出目录：

```sh
GEMINI_MODEL=gemini-2.5-flash GEMINI_EMBEDDING_MODEL=gemini-embedding-001 \
  bash scripts/provider_live.sh gemini /tmp/gemini-new-results
DEEPSEEK_MODEL=deepseek-flash \
  bash scripts/provider_live.sh deepseek /tmp/deepseek-new-results
```

密钥从对应的 `GEMINI_API_KEY` / `DEEPSEEK_API_KEY` 环境变量读取。脚本也支持 `openai`，需 `OPENAI_MODEL` 与 `OPENAI_API_KEY`，未加入默认付费矩阵。

| 套件 | 题数 | 最多上游请求 | 限制 |
|---|---:|---:|---|
| Gemini 原生 | 6 | 10（8 生成 + 2 向量） | 每案例 90 秒，生成最多 2048 输出 Token |
| DeepSeek 租户订单 | 6 | 12 | 顺序执行，每案例最多 2 请求、90 秒，每请求最多 2048 输出 Token |

无自动重试，失败保存结果并返回非零。DeepSeek 的 8000 Token 停止阈值是每个任务的已观测阈值，不是全局费用上限。Gemini Embed 接口未提供用量，报告已知 Token 不包含向量请求；请求上限不等于金额上限。数据为本地合成只读订单，无真实订单或支付写入。

`provider-live.yml` 每周一 03:30 UTC 调度，也支持手动触发。启用前在仓库变量设置 `ENABLE_PROVIDER_LIVE=true`，在 `provider-live` environment 配置上述两个密钥和 `GEMINI_MODEL`、`GEMINI_EMBEDDING_MODEL`、`DEEPSEEK_MODEL` 变量。未启用时整个任务跳过；启用后缺少配置则失败，不将缺失密钥报告成测试通过。PR 不触发付费调用，也不接收密钥。工作流只读仓库权限、20 分钟超时、结果保留 14 天。

最新本地真实运行见[回归结果](../evaluation/2026-10-27-provider-regression/REPORT.md)。
