# 人工审批暂停：持久图的审批节点与幂等打款

```text
assess ──退款且 ≤500──▶ pay_small ──▶ END
   │    ──退款且 >500──▶ pay_large（ApprovalNodes，暂停等待审批）──▶ END
   └────拒绝──────────▶ END
```

- `assess` 节点里的 Agent 只判断申请是否符合政策。打款金额始终取申请单，不采信模型输出的数字。
- 两个打款节点写入模拟账本。账本以申请单号为唯一键，重复调用返回原收据；同一单号金额不一致时报错。

| 申请 | 情况 | 应得路径 |
| --- | --- | --- |
| R-SMALL | 199 元，未拆封，第 3 天 | 直接打款 |
| R-LARGE | 2899 元，未拆封，第 5 天 | 暂停 → 审批 → 打款 |
| R-REJECT | 459 元，已穿两周，第 25 天 | 拒绝，不打款 |

```sh
dir=$(mktemp -d)
go run ./examples/orchestration/live/approval -dir "$dir" -request R-LARGE            # Status=waiting，记下 ApprovalKey
go run ./examples/orchestration/live/approval -dir "$dir" -request R-LARGE -approve <ApprovalKey>
```

大额流程每一步都在独立子进程中执行：

1. 开始：返回 `waiting`，账本为空。
2. 不带审批键再次推进：仍为 `waiting`，0 次请求。
3. 用错误的审批键：被拒，不打款。
4. 批准后打款写入账本，但在检查点保存前退出。
5. 恢复：不需要再次审批，评估节点不重新请求模型，账本去重返回原收据，不再付款。
6. 回放：结果与第 5 步完全相同。

同一套断言既用脚本化模型离线跑（`TestScriptedApprovalFlows`），也用真实模型跑（`TestLiveApprovalFlows`）。

审批键不是身份凭证。真实系统要先认证、鉴权，再调用审批，并且外部支付要按业务单号做幂等。
