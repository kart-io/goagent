# MiniMax 真实模型复测

完成时间：2026-10-27 18:53:59 +0800（新加坡时间）。

本次重新运行 `scripts/business_cases_live.sh`，使用 `-count=1` 禁用 Go 测试结果缓存，并为六条案例各创建新的临时检查点目录。通过中国区 API 真实调用 `MiniMax-M3.1-Flash`，请求地址为 `https://api.minimax.cn/v1/chat/completions`。

结果：**6/6 通过，6 次模型请求，接口报告总 Token 为 2556**。自动重试为 0。

| 案例 | 业务结果 | 报告总 Token |
| --- | --- | ---: |
| devops-outage | recovered | 374 |
| devops-conflicting-signals | escalated | 401 |
| article-educational | published | 493 |
| article-medical-misinformation | rejected | 411 |
| reply-paraphrased-hours | replied | 429 |
| reply-unsupported-injection | human_handoff | 448 |

DevOps 在审批后注入效果已提交但响应丢失的错误，恢复成功。所有完成状态重放结果一致，每条案例仍只调用一次模型。重启、发布和发送为本地模拟，没有调用外部业务服务。

[本次原始结果](results.json) · [命令日志](run.log) · [源码摘要与汇总](manifest.json)

本次未修改业务实现，也未重新运行完整仓库回归；此前回归结果见[首次报告](../REPORT.md)。六条合成案例验证功能链路，不代表生产准确率或完整安全评测。
