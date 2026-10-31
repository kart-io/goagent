# 新增 Plugin 与 ACP 设计调研

日期：2026-10-31。本地基线 `125978c`，远端代码基线 `1603ab4`。范围为最近新增的 plugin 实现与 ACP 设计/实施计划，不包含实现 ACP 或修改产品行为。

后续状态：用户授权后已修订 ACP 设计与实施计划，落实本文建议，见[修订交付与验证](../evaluation/2026-10-31-acp-contract-review/README.md)。以下问题保留为修订前的发现记录；SDK 缺陷本身仍存在，随后执行阶段已拉取 contrib，确认远端存在 ACP 实现并在其基线上修复剩余问题；实际代码与测试结果见 ../../../goagent-contrib/docs/ACP_CONTRACT_FIXES.md。此处“尚未实现”等表述保留调研时本机可见范围，不作为当前 contrib 状态。

## 结论与交付状态

| 内容 | 当前状态 | 判断 |
| --- | --- | --- |
| plugin 能力包 | 已实现，18 个顶层测试，包含 Agent 集成 | 与 GoAgent 通用框架定位一致 |
| ACP Agent 端 | 当前主仓仅新增设计与实施计划 | 方向合理，计划存在需要先修正的问题 |
| ACP 编辑器联调 | 本机没有计划中的 goagent-contrib 工作副本 | 未验证，不能宣称已支持编辑器接入 |

本次不是完整代码审计。已读 plugin 的类型、资源、Host、Install 与测试；对 ACP 逐项核对现有流式事件、环境契约、会话日志和固定版本 SDK，另以独立模块运行两个协议行为探针。

## Plugin 的设计与价值

`Plugin.Contribute` 把工具、提示词、PreStep、请求钩子、恢复策略、中间件和回调打包。`Host` 负责依赖排序、共享资源、启动回滚和逆序停止；`Install` 先收集并验证贡献，再写入 Builder，可通过 `Dispatch` 为工具统一接入管线和持久记录。

依赖方向是 `plugin → builder/tools/core`，Builder 无须依赖 plugin，也无须经过 Harness。它适合订单、检索、编码、数据分析等能力包。插件不是子 Agent、Workflow 或动态加载系统；业务仍需自行组合这些对象。

需要保留的使用边界：

- Host 单次生命周期，停止前须先停止使用其资源的 Agent。失败的插件 Start 不会调用自身 Stop，须自行清理部分初始化资源。
- 同一个 Builder 只安装一次；Install 的“不改 Builder”承诺不回滚 Contribute 自身的外部副作用。与宿主已有能力的重名仍可能在 Build 阶段发现。
- Only 选择安装贡献，不等于停止其他已由 Host 启动的插件。依赖资源与能力安装是两个层面。
- 插件版本仅随其 PreStep 身份进入对应持久执行意图；没有 PreStep 的插件不能据此获得整体配置版本校验。新增或升级工具、提示词、请求钩子不能笼统宣称由插件版本全部保护。
- 回滚使用 WithoutCancel，避免启动 context 取消阻断释放；但没有统一清理超时，插件 Stop 必须避免无限阻塞。

来源：[类型](../../plugin/plugin.go)、[Host](../../plugin/host.go)、[Install](../../plugin/install.go)、[集成测试](../../plugin/agent_test.go)。

## ACP 的正确位置

计划将 ACP 放在 goagent-contrib 独立模块：编辑器经 JSON-RPC/stdio 驱动普通 GoAgent Agent，适配层转发流式文本、工具通知、审批与会话历史。文件和终端可转发给客户端，业务能力由 Factory 和工具提供。

该方向不会将 GoAgent 限定为编码框架。Harness 是可选工具与配置来源，不应成为 ACP Factory 的必经入口。ACP 也不提供沙盒隔离；终端具体运行在哪以及网络能力必须由实际客户端/部署配置决定，不能只因采用 ACP 就固定宣称 Remote=false、Network=true。

## 需要先修正的问题

### P1：同会话 prompt 替换后可能丢失取消能力（已复现）

SDK v0.13.5 在新 prompt 到达时取消旧 prompt，并将新 cancel 写入按 SessionID 索引的表；旧 prompt 返回时，无条件删除该 SessionID。这样会删除新 prompt 的 cancel，新任务后续收到 session/cancel 时没有可调用的 cancel。

独立探针通过信号通道固定“第二个已进入、然后第一个返回、最后 cancel”的顺序，确认新任务 context 未取消。计划中的会话互斥只串行 Runner 执行，无法修正 SDK 表项被错误删除的问题。适配层须管理自己的每轮执行取消句柄，或采用已修正且重新验证的 SDK；不能继续让 Cancel 为空方法。

### P1：session/close 只删 map，没有取消任务及释放资源（已核对 SDK，部分行为已复现）

计划中的 CloseSession 只删除 Session；SDK 对 CloseSession 仅分派调用，并不自动执行 session/cancel。探针确认关闭响应返回后，原 prompt context 仍未取消。

适配层须取消并等待在途执行，随后清理会话资源；需明确 Factory 返回资源的关闭归属。仅丢弃 Runner 引用不会释放 MCP 连接、plugin.Host 等资源。重复 load 同 ID 替换 Session，也需要同样的处理。

[官方关闭会话约定](https://agentclientprotocol.com/protocol/v1/session-setup#closing-active-sessions)要求取消正在进行的工作并释放资源；固定版本的实现见 [SDK 分派源码](https://github.com/coder/acp-go-sdk/blob/v0.13.5/agent_gen.go)。

### P1：终端清理 context 过早创建（计划静态缺陷）

exec.go 示例在 CreateTerminal 后立即创建 5 秒 cleanup context，然后才等待命令完成。命令超过 5 秒再取消或正常结束时，Kill/Release 使用的 context 已过期；错误又被忽略，会造成取消与清理没有完成却被当作已处理。

应在进入清理阶段时创建独立且有期限的 context；记录清理失败，并在取消时核对退出状态。测试应让命令运行超过清理期限，再取消，而非只测立即取消。

### P1：FileSystem 实施计划与设计不一致（计划静态缺陷）

设计要求客户端同时声明读写才转发；计划使用 `ReadTextFile || WriteTextFile` 分别转发，重新引入编辑器缓冲区与磁盘版本混用。

此外，设计承诺创建父目录、IfVersion 对不存在文件返回 ErrStale、目录目标报错；示例实现没有补齐这些分支，Perm 也被忽略。并发的“读取版本→写入”没有协调，不能满足参与同一实现的写者之间的条件写原子性。编辑器外部写入无法协调可作为已声明限制；适配器内部并发仍需独立处理。

应先明确文本文件适配器支持范围并统一规格/实现/测试。不得把新建父目录测试改成预建目录来绕过 WriteFile 契约。

### P2：只处理 text delta，会丢失仅含 done.Output 的回答（计划静态缺陷）

现有 ExecuteStream 明确允许缓存中间件只产生 done，而最终答案在 done.Output。计划忽略 done.Output，因此该路径会返回 end_turn，但编辑器没有答案。

应记录是否发出答案，并为没有文本增量的最终输出补发；已流式发送的文本与最终输出不一致时也需明确呈现策略，避免重复追加。需覆盖缓存命中与普通流两条路径。

### P2：会话列表依赖连接内 cwd 记录，重启后丢失元数据

计划对未知 cwd 返回空字符串，指定 cwd 过滤时仍纳入未知条目。其结果是跨连接列表无法可靠定位工作区，也无法完成明确的工作区筛选。建议独立持久化会话 ID、cwd 与更新时间，再实现分页及过滤；不要把消息存储自动视为完整会话目录。

## 本地验证与复跑

1. `env -u GOROOT go test -race ./plugin -count=1`：通过，耗时 1.893 秒；涵盖现有 18 个顶层测试，不代表全仓回归。
2. `python3 docs/evaluation/2026-10-31-acp-contract-review/reproduce.py`：通过，两个探针均重现待处理行为，结果见 [原始日志](../evaluation/2026-10-31-acp-contract-review/result.log)。这里的 PASS 表示“成功观察到问题”，不是“ACP 取消契约通过”。
3. 下载并只读检查 SDK v0.13.5，没有修改主项目 go.mod/go.sum；探针在临时模块执行，结束自动删除临时目录。
4. ACP 计划尚未落地，因此没有宣称其编译通过、端到端协议通过或真实编辑器联调通过。

本机未提供仓库准则列出的 sequential-thinking、shrimp-task-manager、desktop-commander、context7、github.search_code 工具；采用显式职责/契约分析、rg/源码读取、官方协议页面、固定 SDK 源码和本地可复跑探针补偿。未运行第三方仓库脚本。

## 执行顺序建议

先修订 ACP 规格与计划：取消所有权、会话清理协议、终端清理期限、文件系统契约和最终答案映射。随后在独立模块实现，并优先加入上述缺陷对应的失败测试；通过 io.Pipe 假编辑器协议测试后，再进行真实编辑器联调。Plugin 可继续用于业务能力封装，暂不因 ACP 修改 GoAgent 核心接口。

定性评审（不是统计正确率）：Plugin 代码质量 90、测试覆盖 90、规范遵循 90，需求匹配 93、架构一致 94、风险边界 88，综合 91/100，本轮范围通过；ACP 计划综合 74/100，建议修订后执行。
