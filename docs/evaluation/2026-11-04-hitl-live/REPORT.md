# 真实模型 HITL / Checkpoint 验证

2026-11-04 10:47:10–10:47:23 UTC（新加坡时间 18:47）。Gemini 与 DeepSeek 的暂停、人工修改、落盘后退出及恢复均通过；**综合验收未全部通过**，Gemini 最终通知错误声称已完成退款。

| 模型 | 实际请求 | 报告 Token | 执行检查 | 内容检查 |
|---|---:|---:|---|---|
| gemini-2.5-flash | 3 | 385 | 通过 | 失败：声称退款已完成 |
| deepseek-flash | 3 | 831 | 通过 | 本样例通过 |
| 合计 | 6 | 1,216 | 28 个阶段符合预期 | 综合失败 |

Token 取每个审批/拒绝场景的最终持久预算，不累加重复读取的快照。6 条用量均已知。DeepSeek 的 372 reasoning tokens 已包含在 completion 中，不重复相加。未计算金额费用。

## 场景与证据

订单 `DEMO-HITL-01`、金额 88 元均为合成数据，人工输入由脚本模拟。真实模型先生成待审批草稿，人工将金额改为 66 元，再由模型生成通知。没有执行支付或退款，也没有连接生产身份系统。

每个 Provider 独立运行以下 14 个阶段，每阶段是新进程：

| 阶段 | 预期及实测 |
|---|---|
| 01 起草 | 1 次真实调用；保存草稿后进入 Waiting |
| 02 等待重放、03 查询 | 0 次调用；状态和预算不变 |
| 04 错误 Key、05 未授权身份、06 非法金额 | 拒绝，退出码 1；0 次调用；原检查点不变 |
| 07 接受修改并退出 | 先保存 66 元与放行标记，再真实 `os.Exit(86)` |
| 08 新进程查询 | 66 元保留、尚未完成；预算仍只有起草的 1 次调用 |
| 09 恢复生成 | 仅新增 1 次调用；采用 66 元与正确订单号；完成 |
| 10 完成重放 | 0 次调用；状态和预算不变 |
| 11 过期响应 | 拒绝，退出码 1；0 次调用 |
| 12 新场景起草 | 1 次真实调用，进入 Waiting |
| 13 人工拒绝、14 拒绝后重放 | 0 次调用；完成拒绝分支，无最终通知 |

每家审批分支累计 2 次模型请求，拒绝分支累计 1 次。意图内的退出码 1 / 86 是测试条件，不是隐藏的异常。原始阶段输出、检查点和预算分别保存在 [gemini](gemini/) 与 [deepseek](deepseek/) 目录，汇总见 [results.json](results.json) 和 [run.log](run.log)。

## 内容失败与评分更正

提示要求使用人工修改后的金额，并明确禁止声称真实支付已经发生。Gemini 返回：

> 您的订单 DEMO-HITL-01 已完成退款，退款金额为 66 元。

金额正确，但实际没有退款动作，因此此通知不满足事实约束。DeepSeek 返回：

> 您的订单 DEMO-HITL-01 经人工审核，退款金额确定为 66 元，我们将按流程处理。

初版自动驱动检查了暂停/恢复行为、订单和金额，却漏查退款完成声明，因此原始 `results.json.passed=true` **只表示原执行与数值断言通过**。人工复核发现该遗漏后补充了内容检查，使用已保存输出生成 [review.json](review.json)，综合结果为 `passed=false`，离线复核退出码为 1。未追加模型请求、未改写原始响应或原始评分。

保留 [driver-original.py](driver-original.py) 与 [probe.go.txt](probe.go.txt) 作为本轮实际运行代码。当前 [驱动](../../../scripts/hitl_live.py) 将 `execution_passed` 和内容检查分开，后续运行以综合结果决定退出码。补充规则只识别此中文固定样例中的退款完成措辞，不代表通用语义核验。本轮逐条检查了两条最终通知。业务中的完成通知应绑定支付系统的真实结果，必要时采用确定性模板或再次审核。

## 环境、复现与归档

Go `go1.27.3 linux/amd64`，本地 FileStore；在本次新增 HITL 实现的未提交工作树上测试，工程修复基线为 `a5f5eaa3`。不是已发布版本的验收。源文件哈希见 [tested-sources.json](tested-sources.json)，测试探针完整归档如上；没有修改模型提示后重跑。

使用仓库原生 Gemini / DeepSeek Provider，通过 Builder 和持久图执行。模型 ID 显式传入；温度 0，输出上限 512，单次模型超时 60 秒，无自动重试。Gemini thinking budget 为 0，DeepSeek 保留默认推理配置。API 调用计数来自客户端包装器，并与持久预算核对。密钥从环境读取，没有写入报告。

原始付费运行命令：

```sh
python3 scripts/hitl_live.py --live --out docs/evaluation/2026-11-04-hitl-live
```

再次进行真实验证应改用不存在的新目录；原始目录不可覆盖。只复核本轮保存结果：

```sh
python3 scripts/hitl_live.py --review docs/evaluation/2026-11-04-hitl-live
# 预期退出码 1：Gemini 内容失败；不调用模型
```

准备探针时曾使用不存在的 Builder 方法，构建失败发生在任何模型请求之前；已修复，初始诊断 [probe-compile-initial.log](probe-compile-initial.log) 和修复后 [probe-lint.log](probe-lint.log) 一并保留。最终编译、vet、lint 及离线评分验证见 [validation.log](validation.log)。原始校验清单另存为 [checksums-original.json](checksums-original.json)，最终 [checksums.json](checksums.json) 覆盖新增报告与复核资料。

本轮仅验证节点边界 HITL 与新进程恢复，没有测试模型流中途取消后的重发、嵌套 Agent 工具的真实模型续跑、多机共享存储或真实支付幂等。两个小样例不能推导通用模型准确率或生产可用性。
