# A2A 与 MCP 协议适配验收

2026-11-09，全部实现在当前 goagent 仓库；基线 `0f9f9698aeae84ec921dcdcbfddc08acd4fea4bb`。本轮未提交或推送。主代理最终门禁通过，独立审查9项发现全部关闭。

## 交付

- `management/protocol` 复用管理任务、Actor、幂等、审批和命令；A2A、既有 HTTP 与 MCP 共用一个 Service/Store/Worker。
- `management/a2a` 提供官方 Agent Card、JSON-RPC/SSE、任务身份映射、取消、等待与输入/审批恢复；`a2a/remote` 提供外部 Agent 发现与工具调用。
- `management/mcpserver` 将 Agent 暴露为工具，提供 task_get/task_wait/task_cancel/task_respond；支持2026 MRTR 和旧协议人工输入桥接。
- `mcp/remote` 提供可序列化检查点、换连接续接、有界输入回调、错误时保留检查点，以及宿主官方 SDK 配置入口。
- [接入指南](../../guides/PROTOCOL_ADAPTERS.md) 与[可编译装配示例](../../../management/protocol/example_test.go)包含生命周期、身份来源、工具参数和协议边界。

## 审查与修复

[独立深度审查](REVIEW.md)记录1项 Critical、8项 Warning及其触发、证据与关闭结果：入站数值精度/幂等、恢复首响应阻塞、输出模式交集、终态订阅拒绝、MCP能力声明与空对象兼容、Data媒体类型、管理分页默认值、ListTasks语义以及多轮等待消息身份。独立审查报告中的“最终门禁待主代理执行”是审查当时状态，由本报告和 checks.json 补全，不修改历史证据。

额外运行发现官方 SDK 的旧协议兼容中间件仅重入一次；服务端改用官方 Elicit 完成有界多轮桥接，连续两轮测试通过，并完成[增量独立复核](ADDITIONAL-REVIEW.md)。

首轮新增测试的 Config 必须完整设置 Limits 默认值、错误码需按 int64 比较；均已校正后通过。首轮 lint 报6处官方 roots/sampling API 弃用：这些仍处于至少十二个月支持期，已隔离在唯一的旧协议响应匹配函数内并注明局部豁免理由；最终 lint 为0。原始 lint 输出保留在 raw/lint-before-compat-annotation.txt，没有忽略整个包或关闭全局检查。

## 永久回归覆盖

| 场景 | 行为证据 |
| --- | --- |
| 三入口同任务 | A2A启动、MCP查询、HTTP回复，最终结果与Task ID一致 |
| 输入/审批 | accept/decline/cancel矩阵；普通Resume不能绕过审批；Worker停止并重启后可续接；重复回复不重复执行 |
| 连接与取消 | 流式断开不取消生产者；MCP取消后HTTP/A2A观察同一终态 |
| 数字与格式 | 精确字符串断言9007199254740993持久输入；相同messageId改成9007199254740992产生冲突；拒绝不支持的媒体/输出模式 |
| 恢复首响应 | 执行器被gate阻塞时，ReturnImmediately与stream首事件仍及时返回running |
| 分页与消息身份 | 管理默认页长1/上限2时可跨页投影审批；同修订wait ID稳定、后续修订ID不同；ListTasks明确拒绝 |
| MCP能力 | 未声明、空对象、仅URL、form；终态无需elicitation能力 |
| 续接权限 | 外租户、错误响应ID、篡改task_id、过期revision均拒绝且不新增执行 |
| 外部MCP桥接 | 检查点序列化再换连接续接；保留原始参数与两轮requestState；回调错误/取消和轮数上限保留检查点 |
| 旧协议 | 宿主ElicitationHandler调用成功；GoAgent服务端连续两次等待完整结束 |

## 运行结果

所有命令、工作目录、退出码和日志映射见 [checks.json](checks.json)。最终全量 race 使用 `-count=1`，69个含测试包通过；构建、vet、lint、导入约束、模块校验、独立frameworkbench模块均通过。整体覆盖率79.2%，所有现有分包门禁通过；详情见 [覆盖率门禁](raw/coverage-gates.txt)。

协议测试另用 coverpkg 跨包统计，避免集成测试执行到其他包却显示0覆盖率：

| 包 | 语句覆盖率 |
| --- | --- |
| management/protocol | 75.9% |
| management/a2a | 65.5% |
| management/mcpserver | 77.0% |
| a2a/remote | 59.1% |
| mcp/remote | 78.9% |
| 五包合计 | 72.4% |

覆盖率不是完整规范符合性证明。最终全量日志为 [test.txt](raw/test.txt)，定向日志为 [adapters.txt](raw/adapters.txt)。原始 coverage profile 使用 gzip 无损压缩归档。源码和文档校验值见 [SOURCE_SHA256SUMS](SOURCE_SHA256SUMS)，可在仓库根目录执行 `sha256sum -c docs/evaluation/2026-11-09-protocol-adapters/SOURCE_SHA256SUMS`。

## 明确边界

- 使用官方 SDK，不复刻协议栈。A2A支持单个文本/JSON part；不提供文件、多part、消息历史、推送或ListTasks。管理存储没有状态更新时间排序契约，不能将创建时间分页冒充 A2A 列表语义。
- 官方 A2A 客户端的返回 Metadata/Data 等 any 仍按 float64 解码，a2a/remote继承该边界；精确大整数/小数应以字符串传输。服务端入站原文和幂等比较已保真。
- 状态流基于持久快照轮询，可能合并中间修订，不是token流或完整事件重放。
- MCP长任务通过显式工具契约实现，没有宣告原生tasks扩展。新协议HTTP采用无状态transport；旧版反向输入需要有状态HTTP或stdio。用户认证和服务生命周期由宿主负责。
- 实测使用真实官方SDK和管理Worker，但Store是事务内存夹具；本轮没有执行生产数据库断电恢复、跨仓实库矩阵或外部平台认证验收，不将离线通过等同于这些能力。

主代理质量评估：代码94/100、测试92/100、规范94/100；需求匹配95/100、架构一致96/100、边界清晰94/100，综合94/100，建议通过。分数为审查判断；可复现证据以上述日志、永久测试与源码校验值为准。
