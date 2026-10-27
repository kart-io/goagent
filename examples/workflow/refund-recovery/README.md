# 退款审批与崩溃恢复

这是可在本地运行的合成案例：查询订单 → 检查退款条件 → 等待审批 → 写入模拟退款账本 → 保存完成检查点。没有真实支付接口，不会转移资金。模型参数提取另由 [DeepSeek 原生工具评测](../../../docs/evaluation/2026-10-27-deepseek-native-refund/REPORT.md)验证；本案例的退款条件和金额由确定性代码检查，不调用模型。

## 执行与故障注入

从仓库根目录执行，需要启用 CGO 的 Go 环境与 C 编译器（SQLite 驱动已在主模块依赖中）：

```sh
go build -o /tmp/goagent-refund ./examples/workflow/refund-recovery
demo_dir=$(mktemp -d /tmp/goagent-refund.XXXXXX)

# 新建申请：执行只读检查图，输出待审批状态及订单、金额。
/tmp/goagent-refund -dir "$demo_dir" -run request-1 > "$demo_dir/waiting.json"
cat "$demo_dir/waiting.json"

# 查看状态后，取出等待步骤的键，显式批准这个合成申请。
approval_key=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["snapshot"]["key"])' "$demo_dir/waiting.json")

# 模拟退款已提交、检查点尚未保存时进程退出：预期退出码 86。
/tmp/goagent-refund -dir "$demo_dir" -run request-1 \
  -approve "$approval_key" -crash-after-refund

# 新进程复用相同申请 ID，恢复已持久化的审批与幂等键。
/tmp/goagent-refund -dir "$demo_dir" -run request-1

# 再次执行仍返回同一收据，不新增退款。
/tmp/goagent-refund -dir "$demo_dir" -run request-1
```

第一步输出 `status=approval_required`、`snapshot.next=1`，账本尚无退款。故障注入后账本有一笔退款，但检查点仍指向待完成的退款步骤。恢复后 `status=completed`、`snapshot.next=2`，返回原退款收据，账本仍只有一笔。

默认订单 `DEMO-1001`：支付 9,900 分、签收 3 天、币种 CNY。演示政策允许签收 7 天内、金额大于零且不超过订单支付额。`-order DEMO-EXPIRED`、不存在的订单、超过支付额的 `-amount-cents` 均在审批和退款前失败。

`-order` 与 `-amount-cents` 仅用于新申请；同一 `-run` 已持久化的订单与金额不会被恢复命令覆盖。再次使用旧审批键会被拒绝，恢复已批准步骤无需再次传 `-approve`。

## 恢复边界

- `workflow.Graph[refundState]` 运行两个只读节点；`durable.Runner` 在整个检查图完成后保存业务状态。图执行过程中失败会重跑整个只读检查步骤，**没有增加任意图节点级恢复能力**。
- 模拟外部退款服务使用独立 SQLite 数据库，唯一幂等键对应固定的订单、金额、币种和收据。重复请求返回原收据；同键不同参数报错。退款数据库与检查点不共享事务，专门暴露真实的副作用歧义窗口。
- Durable 步骤仍是至少一次执行。账本去重使本案例同一申请只有一笔记录，不意味着任意外部 API 都恰好执行一次。
- 业务申请必须稳定复用 `-run`。换一个 ID 会创建另一个申请；案例未实现跨申请累计退款限额、退款撤销、审批过期或订单状态变化后的重新校验。
- `-approve` 是本地演示入口。审批键不是身份凭证；生产宿主必须验证审批人权限，并把授权绑定到具体申请、金额和流程版本。状态目录按可信本地存储处理，不防止有文件访问权的操作者篡改。
- 依赖可靠本地文件锁、原子替换与 SQLite 持久化，不提供跨主机接管。步骤实现变化时必须更新 Runner.Version，旧计划不能静默加载。

## 自动验证

```sh
go test -race -count=1 -v ./examples/workflow/refund-recovery
```

覆盖独立进程间审批等待、错误审批拒绝、账本提交后退出码 86、恢复和重复恢复、已提交检查图不重跑、恢复输入不覆盖已审批订单、八个并发同键请求去重、同键不同参数拒绝，以及不满足政策的申请无副作用。
