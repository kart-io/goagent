# 退款审批与崩溃恢复

这是可在本地运行的合成案例：查询订单 → 检查退款条件并注册业务申请 → 等待审批 → 复核订单和累计额度 → 写入模拟退款账本 → 保存完成检查点。没有真实支付接口，不会转移资金。模型参数提取另由 [DeepSeek 原生工具评测](../../../docs/evaluation/2026-10-27-deepseek-native-refund/REPORT.md)验证；本案例的退款条件和金额由确定性代码检查，不调用模型。

当前流程和账本为 v2：旧 v1 检查点/账本会明确拒绝加载，不自动覆盖或迁移。请使用新的演示目录，保留旧记录。

## 执行与故障注入

从仓库根目录执行，需要启用 CGO 的 Go 环境与 C 编译器（SQLite 驱动已在主模块依赖中）：

```sh
go build -o /tmp/goagent-refund ./examples/workflow/refund-recovery
demo_dir=$(mktemp -d /tmp/goagent-refund.XXXXXX)

# 新建申请：执行只读检查图，输出待审批状态及订单、金额。
/tmp/goagent-refund -dir "$demo_dir" -run execution-1 -request refund-1 > "$demo_dir/waiting.json"
cat "$demo_dir/waiting.json"

# 查看状态后，取出等待步骤的键，显式批准这个合成申请。
approval_key=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["snapshot"]["key"])' "$demo_dir/waiting.json")

# 模拟退款已提交、检查点尚未保存时进程退出：预期退出码 86。
/tmp/goagent-refund -dir "$demo_dir" -run execution-1 \
  -approve "$approval_key" -approver demo-reviewer -crash-after-refund

# 新进程复用相同申请 ID，恢复已持久化的审批与幂等键。
/tmp/goagent-refund -dir "$demo_dir" -run execution-1

# 再次执行仍返回同一收据，不新增退款。
/tmp/goagent-refund -dir "$demo_dir" -run execution-1
```

第一步输出 `status=approval_required`、`snapshot.next=1`，账本尚无退款。故障注入后账本有一笔退款，但检查点仍指向待完成的退款步骤。恢复后 `status=completed`、`snapshot.next=2`，返回原退款收据，账本仍只有一笔。

默认订单 `DEMO-1001`：支付 9,900 分、签收 3 天、币种 CNY。演示政策允许签收 7 天内、金额大于零且不超过订单支付额。`-order DEMO-EXPIRED`、不存在的订单、超过支付额的 `-amount-cents` 均在审批和退款前失败。

`-request` 是业务退款申请 ID，`-run` 是执行 ID。换一个执行 ID，仍需复用相同业务 ID 才能识别同一申请；不同业务 ID 的累计退款总额也不得超过订单支付额。默认业务 ID 是 `refund-request-1`。相同业务 ID 不允许改变订单或金额。

`-request`、`-order` 与 `-amount-cents` 仅用于新执行；同一 `-run` 已持久化的申请不会被恢复命令覆盖。再次使用旧审批键会被拒绝，恢复已批准步骤无需再次传 `-approve`。新执行复用已存在的业务申请时会加载原审批快照，不能重设有效期。

`-approval-ttl` 默认为 10 分钟，从首次准备业务申请开始计时。审批和退款执行时均检查有效期、订单版本、支付金额、币种和签收天数；发生变更或过期则拒绝尚未发生的退款，需要重新审查并创建新的业务申请。审批记录保存 `approved_by` 和 `approved_at`。已完成退款的重试优先返回原收据，即使审批后来过期，也不会新增退款或掩盖已发生的结果。

## 恢复边界

- `workflow.Graph[refundState]` 运行两个只读节点；`durable.Runner` 在整个检查图完成后保存业务状态。图执行过程中失败会重跑整个只读检查步骤，**没有增加任意图节点级恢复能力**。
- 模拟外部退款服务使用独立 SQLite 数据库，业务申请 ID 是退款记录唯一键，执行幂等键作为审计字段保留。重复业务申请返回原收据；同 ID 不同参数报错。累计额度检查与退款插入使用同一 `BEGIN IMMEDIATE` 事务，避免并发申请超额。退款数据库与检查点不共享事务，专门暴露真实的副作用歧义窗口。
- Durable 步骤仍是至少一次执行。账本去重使本案例同一申请只有一笔记录，不意味着任意外部 API 都恰好执行一次。
- 案例已覆盖跨申请累计限额、审批有效期和订单版本复核；不提供撤销、退款失败冲正、部分到账或生产支付对账。订单数据在 SQLite 的 `orders` 表中，测试通过修改版本模拟外部订单变更。
- `-approver demo-reviewer` 是本地演示授权规则，其他身份被拒绝；字符串参数本身不是身份认证。生产宿主必须先验证登录身份和权限，再调用审批边界。审批键同样不是身份凭证。状态目录按可信本地存储处理，不防止有文件访问权的操作者篡改。
- 依赖可靠本地文件锁、原子替换与 SQLite 持久化，不提供跨主机接管。步骤实现变化时必须更新 Runner.Version，旧计划不能静默加载。

## 自动验证

```sh
go test -race -count=1 -v ./examples/workflow/refund-recovery
```

覆盖独立进程间审批等待、错误审批键/身份拒绝、账本提交后退出码 86、恢复和重复恢复、跨执行 ID 的业务去重、已提交检查图不重跑、恢复输入不覆盖已审批订单、并发不同申请不超额、同 ID 不同参数拒绝、审批过期、订单变更复核，以及旧账本拒绝加载。

## 复用业务保护

幂等、审批、订单版本与累计金额检查已提取为 [`workflow/refund.Service`](../../../workflow/refund/README.md)。本例 `ledgerTx` 实现事务存储接口，`service()` 注入演示授权和七天规则；原 SQLite schema v2 与已有请求数据保持兼容。新效果执行前会再次检查授权，已提交回执的恢复不依赖审批仍然有效。生产宿主应实现真实身份授权与订单事务存储。

## 原生图与工具执行模式

新增 `-engine graph`：恢复申请 → 并行查询订单/校验申请 → 注册审核快照 → 审批 → Dispatcher 退款工具。所有执行均在 GoAgent 内部，工具获得节点的稳定 ExecutionKey。`-engine steps` 仍为旧默认模式，检查点目录分别是 graph-checkpoints 与 checkpoints，账本共享业务 ID 去重。

```sh
go run ./examples/workflow/refund-recovery -engine graph -dir /tmp/refund-native
# 复制输出的 snapshot.Key 到 KEY；演示审批人不是生产身份认证。
go run ./examples/workflow/refund-recovery -engine graph -dir /tmp/refund-native \
  -approve KEY -approver demo-reviewer -crash-after-refund
# 模拟退出码 86 后重启，只确认原退款，不重复写入。
go run ./examples/workflow/refund-recovery -engine graph -dir /tmp/refund-native
```

graph 模式输出 snapshot.State、snapshot.Waiting、snapshot.Done。完整约束见[原生编排文档](../../../docs/guides/NATIVE_ORCHESTRATION.md)。
