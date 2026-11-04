# 修改后的真实 Agent 验证

> 更新：按用户指定切换 MiniMax 国内端点后，原六条用例已全部通过，最新矩阵为 24/24。详见[国内重测报告](minimax-cn/REPORT.md)和[更新汇总](summary-after-cn.json)。以下保留首次国际端点验证及其 401 历史记录。

日期：2026-11-04。对当前未提交修复工作树进行了真实模型调用，使用已有 DeepSeek、Gemini、MiniMax 配置；没有用假模型替代响应。**18 个场景通过，MiniMax 的 6 个场景被 HTTP 401 认证错误阻断，整轮不判定为全部通过。** 业务输入均为合成数据，未接入真实支付、生产发布或用户消息系统。

共发出 **31 次提供方请求尝试**，在预先声明的 36 次上限以内：25 次获得成功模型终态和用量，6 次 MiniMax 请求返回 401。成功调用已知用量合计 **11,385 Token**；认证失败没有用量报告，不能将其视为已知零用量或据此计算费用。无自动重试，也没有切换模型将失败样本重跑成通过。逐组汇总见 [summary.json](summary.json)。

## 真实执行结果

| 场景 | 模型 | 结果 | 请求数 | 已知 Token |
|---|---|---|---|---|
| DevOps、文章审核、自动回复：六条用例 | deepseek-flash | 6/6 通过 | 6 | 2,006 |
| 相同六条业务用例 | gemini-2.5-flash | 6/6 通过 | 6 | 3,116 |
| 相同六条业务用例 | MiniMax-M3.1-Flash | 0/6，HTTP 401，认证阻断 | 6 | 未知 |
| 原生双工具、子进程退出及恢复 | deepseek-flash | 通过 | 2 | 1,249 |
| 原生双工具、子进程退出及恢复 | gemini-2.5-flash | 通过 | 2 | 1,090 |
| Harness 压缩与工具循环 | deepseek-flash | 通过 | 3（含摘要 1 次） | 2,154 |
| Agent → 真实模型重排 → 插件工具 → 回答 | deepseek-flash | 通过 | 3（含重排 1 次） | 890 |
| HTTP Native 绑定 → Agent → 插件工具 → 回答 | deepseek-flash | 通过 | 2 | 497 |
| Agent → PostgreSQL 双快照/双会话 → 重建回放 | deepseek-flash | 通过 | 1 | 383 |

### 业务决策与效果恢复

使用修复后的 `modelJudge`，经 `llm.Complete` 收集真实流式响应，进入实际 workflow 路由与 Dispatcher 工具路径。DeepSeek 和 Gemini 分别通过六条预先声明的用例：故障诊断需审批、冲突信号升级、正常文章发布、危险医疗误导拒绝、口语化问题匹配 FAQ，以及退款/密钥诱导消息转人工。

同时验证审批前无效果、拒绝分支无效果、模拟业务效果提交后恢复、完成后重放不再请求模型。报告保留模型决策、事实来源、用量及错误；业务动作仅写入隔离本地账本。报告：[DeepSeek](deepseek-business.json)、[Gemini](gemini-business.json)、[MiniMax](minimax-business.json)。

MiniMax 六次均由当前配置端点返回 HTTP 401；日志见 [minimax-business.log](minimax-business.log)。这是本轮认证失败的证据，不足以判断密钥过期、区域或账号权限的具体原因；本轮没有修改凭据、另投其他端点或更换模型。MiniMax 的本轮真实语义与工具能力没有通过认证后的验证。

### 原生工具调用与真实进程恢复

DeepSeek 和 Gemini 均由模型原生选择 `record_A`、`record_B`。第二个本地效果提交后，子进程按预设条件以 86 退出，再以新进程恢复；模型请求按阶段为 **[1,1,0]**。工具尝试为 A、B、B，但业务效果只有两条，子键不同；完成后的第三个进程不新增请求或效果。意图内退出码 86 是测试条件。

证据：[DeepSeek 原生循环](deepseek-native.json)、[Gemini 原生循环](gemini-native.json)。这验证本地持久化和协议续接，不证明任意外部 API 恰好执行一次。

### Harness、重排和入口绑定

[集成结果](integration-results.json)运行真实 Builder、Plugin、Harness compaction 和当前检索实现：

- Harness Agent 先调用 `load_evidence` 获得长报告，PreStep 经真实摘要模型收集流并压缩。第二次 Agent 请求实际含 `[此前对话的摘要]`，工具恰好一次，最终仍返回工具中的 `LIVE-COMPACT-7419`。两次 Agent 请求受根预算限制，摘要的额外一次请求由独立上限计数器跟踪。
- `AsRunnable().Invoke` 的真实 Agent 调用插件工具；工具调用真实 LLM 重排器，选择退款政策而非配送政策。最终答案准确复制工具回执 `LIVE-HTTP-7391`。
- `httpagent.Native(agent.ExecuteStream).Run` 同样由真实模型调用插件工具并返回相同回执，工具恰好执行一次，两轮模型请求。此项验证的是 HTTP 模块的绑定层，没有把它描述为实际 HTTP/SSE 网络协议验收。

每类客户端在发送前检查独立请求上限，合计最多 8 次，单次输出上限 1024 Token、60 秒超时。探针源码与模块配置见 [integration-probe.go.txt](integration-probe.go.txt)、[integration-probe.mod.txt](integration-probe.mod.txt)，运行结果退出 0。它验证了本轮修改的流式收集与压缩组件；plan-agent 专用摘要转发包装器、后台到期竞争和 effects 排序仍由此前确定性/竞态回归覆盖，没有声称在本轮分别进行过真实模型测试。

### 真实 Agent 与 PostgreSQL

使用已有 postgres:16-alpine 镜像创建独立临时容器，仅绑定 localhost。真实 DeepSeek 模型自主选择 `persist_receipt` 工具一次，工具调用最新 `SaveAll` 写两个快照、`AppendAll` 写两个会话，序号均为 1，并立即读取验证。

Agent 的操作日志使用 PostgreSQL `session.DurableStore`，根预算也持久化在 PostgreSQL。关闭并重建连接池、Store、SessionStore、Provider、预算控制器和 Agent 后，以相同执行键调用，返回相同结果；新增模型请求、工具效果、业务事件均为 **0**，日志仍为 4 条边界事件，用量仍为 383 Token。客户端与 HTTP transport 在发送前均拒绝第二次真实请求。

证据：[结构化结果](pg-agent-drbazf-executed-result.json)、[数据库证据](pg-agent-drbazf-database-evidence.txt)、[探针源码](pg-agent-drbazf-source.txt)、[详细报告](pg-agent-drbazf-report.txt)。临时容器已删除。前两次仅因临时目录中的凭据未继承而在前置检查退出，均为 0 次请求，原始记录保留；成功调用没有重试。

这里是同一测试进程内重建所有存储/Agent 对象，不是 PostgreSQL 或 OS 进程崩溃测试；真实 OS 子进程恢复在上面的原生循环用例完成。SaveAll 与 AppendAll 各自原子，不是跨两种 API 的单一事务。SQL 失败回滚、序号冲突、租约接管和数据库重启由此前[实库修复验证](../2026-11-04-stability/REPAIR.md)覆盖。

## 可复现入口与边界

业务与原生循环使用 examples 仓库的既有入口；新报告路径必须不存在，不能覆盖本轮结果：

```sh
cd ../goagent-examples
DEEPSEEK_MODEL=deepseek-flash bash scripts/business_cases_live.sh deepseek /tmp/NEW-deepseek-business.json
GEMINI_MODEL=gemini-2.5-flash bash scripts/business_cases_live.sh gemini /tmp/NEW-gemini-business.json
DEEPSEEK_MODEL=deepseek-flash bash scripts/business_tool_loop_live.sh deepseek /tmp/NEW-deepseek-native.json
GEMINI_MODEL=gemini-2.5-flash bash scripts/business_tool_loop_live.sh gemini /tmp/NEW-gemini-native.json
```

每套业务最多 6 请求；每套原生循环最多 4 请求。API Key 由环境读取，不写入命令或结果。正确配置工具链的机器可直接运行；本机执行时仅对子进程取消不匹配的 GOROOT。探针临时模块使用本地 replace，未修改生产源码或已有模块配置。

测试使用 Go 1.27.2 darwin/arm64 和当前未提交修复工作树，版本及源码摘要见 [tested-source-hashes.json](tested-source-hashes.json)。产物检查未发现当前已配置 API Key/Access Token 原值；原始模型思考文本不写入报告，只保留已知用量。

这些是合成场景的小样本功能验证，没有生产准确率、性能、HA 或费用结论。MiniMax 认证未通过，不能把本轮汇总写成全部模型通过。未使用无效模型回复作为成功结果，也没有改写上次已归档的退款错误声明。
