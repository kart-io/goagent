# DeepSeek 隔离订单后端实测

2026-10-27，本轮使用 DeepSeek 原生 Provider，在按实际参数查询的隔离订单后端上完成 12 次运行。答案、工具参数及调用契约全部通过，用量完整 12/12；共 16 次模型请求、7,068 Token（输入 6,500，输出 568）。全部记录保留，没有重试筛选。

## 题集与结果

六道合成题，每题重复两次。题目共享少量合成订单，不构成独立生产验收样本。使用 `contract` 模式，查询直返四题，事实推理两题，配置在运行前确定。

| 场景 | 答案/参数/契约通过 | 模型请求 | 总 Token |
|---|---:|---:|---:|
| 租户 A 同号订单 | 2/2 | 2 | 1,000 |
| 租户 B 同号订单 | 2/2 | 2 | 1,000 |
| 跨租户不可见 | 2/2 | 2 | 1,020 |
| 不存在的订单 | 2/2 | 2 | 990 |
| 退款期限提取 | 2/2 | 4 | 1,500 |
| 资料不足拒答 | 2/2 | 4 | 1,558 |

租户由宿主配置，模型只能传 `order_id`。同订单号在两个租户下返回不同记录；跨租户与不存在均返回 `NOT_FOUND`，共 4 次 lookup miss。后端不读取参数标签或标准答案，未将全部数据库记录发送模型。离线测试另覆盖额外 tenant_id 注入、参数类型错误、同租户查错对象、取消和无效题集拒绝。

此结果验证测试后端的参数路由和租户隔离，不代表真实身份认证、生产订单权限或支付接口已接入。当前仍未收到脱敏业务题集。退款写入与审批恢复由独立的本地案例测试，不与模型成绩混算。

## 运行配置

- 原生 Provider：DeepSeek；官方默认端点；模型 `deepseek-flash`。
- 每任务最多 2 次请求，每请求最多 2,048 输出 Token，观察用量阈值 8,000，超时 90 秒，单并发；计划上限 24 次请求。
- 温度请求值 0.2，未显式配置 thinking；服务端模式由供应商决定。
- 基于 `c9d4e6c` 的未提交工作区，Go 1.27.1；精确题集哈希与配置见 manifest。这里的旧提交号不是已包含本轮变更的干净版本。
- 未采集计费和缓存价格，不能直接换算费用；题集、后端、运行模式与前轮不同，不作框架性能排名。

## 复跑

设置 `DEEPSEEK_API_KEY`，在仓库根目录运行；结果目录必须不存在。去掉 `-live` 可只生成计划。

```sh
go run ./evaluation/agentbench/cmd/stress -live \
  -provider deepseek -model deepseek-flash \
  -dataset evaluation/agentbench/testdata/tenant-order-cases.json \
  -mode contract -repeats 2 -workers 1 \
  -max-calls 2 -max-output-tokens 2048 -token-stop 8000 -timeout 90s \
  -out /tmp/goagent-tenant-orders-live
```

材料：[题集](dataset.json)、[配置](manifest.json)、[原始结果](results.jsonl)、[汇总](summary.json)。退款恢复与业务约束见[案例说明](../../../examples/workflow/refund-recovery/README.md)，Provider 检查范围见[能力矩阵](../../guides/PROVIDER_CAPABILITIES.md)。

## 工程验证

[退款业务约束实跑记录](refund-guards-smoke.json)验证本地演示身份拒绝、退款提交后退出码 86、重启恢复、不同执行 ID 的同业务申请去重、累计退款超额拒绝和订单版本变更拒绝。最终账本仅一笔，金额 6,000 分。有效期到期及并发额度竞争另由可控时钟和独立数据库连接的回归测试覆盖。

Go 1.25 全仓短测试通过；相关包与退款案例竞态测试、`go vet ./...`、`go build ./...` 均通过。Provider 矩阵验证 9 个基础实现与 2 个包装器，不把构造器/接口测试计为供应商线上验证。SQLite 驱动保留上游 C const 限定符编译警告，不影响测试结果。
