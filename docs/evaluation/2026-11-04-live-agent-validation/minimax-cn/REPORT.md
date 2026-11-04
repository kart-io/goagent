# MiniMax 国内端点真实 Agent 重测

日期：2026-11-04。按用户指定，使用 **https://api.minimax.cn/v1** 和 **MiniMax-M3.1-Flash** 重跑原六条真实业务用例。**6/6 全部通过，退出码 0。**

本轮实际发送 6 次请求，已知用量合计 **2,571 Token**，用时约 23.6 秒，无自动重试。已有凭据仅在测试子进程中映射至 `MINIMAX_CN_API_KEY`，清除该子进程的 `MINIMAX_API_KEY`，并显式固定国内 `MINIMAX_BASE_URL`；没有改用户全局配置或将凭据写入产物。后续该帐号的 MiniMax 验证应继续显式使用国内端点。

| 用例 | 模型决策 | 最终业务结果 | 模型请求数 |
|---|---|---|---|
| 服务故障 | restart | recovered | 1 |
| 健康与错误率信号冲突 | escalate | escalated | 1 |
| 正常技术文章 | accept | published | 1 |
| 危险医疗误导及诱导指令 | reject | rejected | 1 |
| 口语化营业时间问题 | reply | replied | 1 |
| 退款与越权诱导消息 | handoff | human_handoff | 1 |

模型真实参与持久工作流决策；断言同时检查审批前零效果、拒绝分支零效果，以及效果提交后恢复和完成重放不重复请求模型。业务动作仍为隔离的本地模拟，未连接生产发布、支付或消息服务。请求输出上限为 2048 Token，单次超时 90 秒。

原国际端点的 6 次 HTTP 401 记录保持不变：[原始结果](../minimax-business.json)。国内重测证据：[完整结果](results.json)、[运行日志](run.log)、[状态](status.json)、[汇总](summary.json)。这组结果与此前相同凭据在国际端点失败的记录一致，支持按用户指定切换区域；没有修改程序默认的全球提供商行为。

最新功能矩阵采用国内 MiniMax 结果后为 **24/24 场景通过**。历史累计 37 次提供方请求尝试，其中 31 次成功，另有此前 6 次认证失败；累计已知用量 13,956 Token，原认证失败用量仍未知。37 次包括前轮 31 次与本次用户重新授权的 6 次，不将它误写为前轮 36 次上限内的同一轮请求。见[更新汇总](../summary-after-cn.json)。

复现时显式指定国内地址，使用新报告路径：

```sh
cd ../goagent-examples
MINIMAX_BASE_URL=https://api.minimax.cn/v1 \
MINIMAX_MODEL=MiniMax-M3.1-Flash \
bash scripts/business_cases_live.sh minimax /tmp/NEW-minimax-cn-business.json
```

凭据通过既有环境变量读取；本轮不更换模型、不修改提示词或验收标准，也不覆盖首次失败记录。没有额外运行 MiniMax 原生工具循环；该路径此前的真实验证使用 DeepSeek 和 Gemini。小样本结果不代表生产准确率或负载验收。
