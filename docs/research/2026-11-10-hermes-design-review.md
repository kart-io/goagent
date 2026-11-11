# Hermes 图解全文阅读与 GoAgent 对照

日期：2026-11-10。性质：阅读记录与静态代码对照；不是功能验收报告。

## 1. 来源与阅读范围

用户提供的[《跨平台Agent怎么做的？Hermes深度拆解展开》](https://mp.weixin.qq.com/s/rbybfVn1Pxbup9pkfPwMWg)，账号「图灵AI进化论」，页面时间为 2026-09-29。通过浏览器读取页面及原图列表，下载并逐张查看全部 17 张原图。第 1～13 张包含架构内容，第 14～17 张是资料库展示，未提供额外 Hermes 实现细节。图片内容是二手图解，且页面注明含 AI 生成图片，不能直接当成源码契约。

原图 URL、尺寸和 SHA-256 见 [来源清单](2026-11-10-hermes-sources.json)。原图暂存在本机 `/tmp/goagent-hermes-review/`，未复制进仓库。以下均是阅读归纳，不是全文转录。

另核对官方 [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent/tree/a2fa0a4b649b7f3a771cf92f1ae43059eeec76e6)，固定提交 `a2fa0a4b649b7f3a771cf92f1ae43059eeec76e6`（提交时间 2026-11-10T14:57:54Z）。它晚于文章，不将当前源码的行为倒推为文章发表时的实现。本轮只读相关代码，未运行 Hermes。

本地对照基线：

- GoAgent HEAD：`96b816c3f51415af70c0904686bc4115d21c3cf4`，连同阅读时工作树。
- GoAgent Harness HEAD：`367330b2194713b2d94b6c567c75634c4f866cb5`，连同已有未提交修改。开始时已有 `preset/build.go`、`preset/builtin.go`、`preset/plan.go`、`integration/runtime_test.go` 修改；本轮不修改这些文件。
- 用户写作 `goagent-hermess`；当前找到的相邻仓库为 `goagent-harness`，本文按此对应，名称确认待用户纠正。

## 2. 全部图片逐项对照

| 页 | 阅读要点 | 本地事实与判断 | 设计落点 |
| --- | --- | --- | --- |
| 01 总图 | 入口、循环、上下文、模型、工具、环境、记忆、技能、网关、调度、安全和扩展相互协作 | 这些是职责，不能机械变成十二个新包 | 保持内核、Harness、宿主三层 |
| 02 入口 | CLI、Gateway、ACP、API、Batch 适配同一运行时 | 已有 management 的 HTTP/A2A/MCP 共用任务；ACP 是不同协议，不能由 A2A 支持推定已覆盖 | 新入口复用任务身份、版本和授权 |
| 03 循环 | 判断、工具、回填、续跑；有界重试和中断 | Builder、Handle、durable、budget 已覆盖主体；持久工具 error 仍不被 Builder 自动转换为反馈 | 保持恢复安全，逐工具分类错误 |
| 04 上下文 | 多来源组装、来源检查、稳定快照、可调试 | PromptSection 每轮固定，PromptContext 每步追加；Revision 可固定发布内容，但没有统一的来源解释报告 | 来源、权限、预算、摘要和冻结范围 |
| 05 模型 | 隔离 Provider/API 差异，按能力与成本选路 | 已有 Client、能力检查、RequestHook；未发现统一持久路由决策组件；持久 RequestError 不允许换客户端 | 先固定版本的确定性路由 |
| 06 工具 | 注册、可用性筛选、统一分发与结果反馈 | Registry、Dispatcher、Pipeline 已存在；展示、可用性与执行许可应各自校验 | 保留显式注册，统一准入 |
| 07 环境 | 工具语义与本机、容器、远程环境解耦 | 核心 harness/env；Harness 有 native、Docker、SSH 和 Provider/受管会话 | 补能力矩阵与同环境验收，无需移植六种后端 |
| 08 记忆 | 精炼事实、历史检索、上下文压缩各司其职 | core/session、现有对话 memory、Harness compaction 已存在；持久事实服务仅有待实施设计；有界跨会话检索仍需单列 | 四类状态分别设计，索引不作真源 |
| 09 技能 | 把成功方法变成可治理、按需加载的程序性记忆 | 核心 skills.Catalog 已实现；Harness 自带另一个解析器；尚无候选提炼、验证和发布闭环 | 先统一加载，再做候选生命周期 |
| 10 网关 | authorize、route、deliver，处理平台和会话差异 | management/protocol 已统一任务，management outbox 已提交终态通知；平台身份和 effects 交接去重属于宿主 | 复用 outbox，原子保存接纳收据与效果 |
| 11 调度 | 到点启动完整 Agent 任务，并把结果投递出去 | background 管子工作；effects 管投递；二者都不等于日历调度器 | 宿主 Schedule → 现有 management Task |
| 12 安全 | 入口、上下文、工具、技能、隔离分别设边界 | 已有授权、信任、审批、Guard、沙箱；内容扫描不能替代这些边界 | 内容审查绑定工件，权限仍在执行时落实 |
| 13 扩展 | 插件、MCP、外部记忆接入稳定边界 | plugin、mcp、skills、revision 已存在；事实记忆扩展设计待实现 | 不增加 Python 式动态插件内核 |
| 14 资料库 | 飞书知识库页面截图 | 非 Hermes 架构证据 | 无新增需求 |
| 15 资料库 | Agent 学习入口与资料分类 | 非 Hermes 架构证据 | 无新增需求 |
| 16 资料库 | 一般 Agent 分层文章截图 | 非 Hermes 具体实现证据 | 不据此增加模块 |
| 17 资料库 | RAG 基础与进阶资料截图 | 非 Hermes 具体实现证据 | RAG 继续独立于事实记忆 |

## 3. 官方源码补核与不能照搬之处

下表链接均固定到上述 SHA；只声明实际阅读到的行为。

| 官方文件 | 核对结果 | 对本项目的影响 |
| --- | --- | --- |
| [prompt_builder.py](https://github.com/NousResearch/hermes-agent/blob/a2fa0a4b649b7f3a771cf92f1ae43059eeec76e6/agent/prompt_builder.py) | 上下文内容扫描区分用户身份文件与项目文件；AGENTS 目录链从仓库根到 cwd | 本项目需声明来源和适用范围；不照搬文件名或把正则结果当信任证明 |
| [runtime_provider.py](https://github.com/NousResearch/hermes-agent/blob/a2fa0a4b649b7f3a771cf92f1ae43059eeec76e6/hermes_cli/runtime_provider.py) | CLI/Gateway/Cron 共享 Provider 解析；可按会话冻结配置，凭据仍由授权存储更新 | 固定路由配置与端点，凭据只保留受限引用 |
| [registry.py](https://github.com/NousResearch/hermes-agent/blob/a2fa0a4b649b7f3a771cf92f1ae43059eeec76e6/tools/registry.py) | schema、handler、工具集、可用性检查和 dispatch 集中管理，含作用域状态 | 复用本项目 Dispatcher；不引入 AST 扫描与隐式 import 注册 |
| [session_search_tool.py](https://github.com/NousResearch/hermes-agent/blob/a2fa0a4b649b7f3a771cf92f1ae43059eeec76e6/tools/session_search_tool.py) | 历史检索与窗口读取直接返回数据库消息；自动任务与交互记录有不同检索待遇 | 搜索投影返回原事件引用，避免定时报告挤占用户交互结果 |
| [memory_provider.py](https://github.com/NousResearch/hermes-agent/blob/a2fa0a4b649b7f3a771cf92f1ae43059eeec76e6/agent/memory_provider.py) | 静态提示与动态 recall 分开；多 profile 的实例和存储绑定有明确要求 | 沿用既有事实服务设计的可信 Scope，禁止进程全局可变租户绑定 |
| [skills_guard.py](https://github.com/NousResearch/hermes-agent/blob/a2fa0a4b649b7f3a771cf92f1ae43059eeec76e6/tools/skills_guard.py) | 静态规则与来源信任共同裁决，源码也标注动态语言写入的漏检 | 扫描仅提供证据，不授予工具、网络或文件权限 |
| [skill_manager_tool.py](https://github.com/NousResearch/hermes-agent/blob/a2fa0a4b649b7f3a771cf92f1ae43059eeec76e6/tools/skill_manager_tool.py) | 有写入暂存/审批路径；读到的 write gate 在导入失败时会放行 | 只借鉴候选与发布分开；本项目发布依赖缺失时应保持候选状态 |
| [session_identity.py](https://github.com/NousResearch/hermes-agent/blob/a2fa0a4b649b7f3a771cf92f1ae43059eeec76e6/gateway/session_identity.py) | 一次入口事件冻结传输身份、授权归属和运行归属，无法解析时拒绝 | 身份解析一次且来源可信；跨平台串联必须有宿主绑定 |
| [scheduler_tick.py](https://github.com/NousResearch/hermes-agent/blob/a2fa0a4b649b7f3a771cf92f1ae43059eeec76e6/cron/scheduler_tick.py)、[scheduler_delivery.py](https://github.com/NousResearch/hermes-agent/blob/a2fa0a4b649b7f3a771cf92f1ae43059eeec76e6/cron/scheduler_delivery.py) | tick 有准入和排他控制；投递会区分平台是否支持异步推送 | 调度、业务执行和结果投递分别建身份与状态 |
| [delivery_ledger.py](https://github.com/NousResearch/hermes-agent/blob/a2fa0a4b649b7f3a771cf92f1ae43059eeec76e6/gateway/delivery_ledger.py) | 保存最终响应投递义务，但账本错误不阻断发送；模糊发送可以带恢复标记重发 | 不照搬 best-effort 保证；本项目继续用 effects 的 attention/Reconcile 区分未知效果 |

## 4. 本地证据入口

- [循环契约](../../builder/loop_hooks.go)、[模型持久意图](../../builder/durable_turn.go)、[重试限制](../../builder/model_call.go)、[工具反馈](../../builder/tool_events.go)。
- [工具管线](../../tools/pipeline.go)、[Dispatcher](../../tools/dispatcher.go)、[环境能力](../../harness/env/README.md)。
- [技能加载](../guides/SKILLS.md)、[Catalog](../../skills/catalog.go)、[不可变修订](../guides/AGENT_REVISIONS.md)。
- [统一管理](../../management/README.md)、[现有 outbox](../../management/outbox.go)、[协议适配](../guides/PROTOCOL_ADAPTERS.md)、[会话 Store](../../core/session/store.go)。
- [长期记忆的删除优先](../design/MEMORY_GOVERNANCE.md)（待实施，取代存档于 tag `archive/persistent-factual-memory` 的事实记忆设计），不能算作已发布能力。
- Harness：`preset/skills.go`、`preset/envcontext.go`、`preset/build.go`、`preset/loop.go`、`background/README.md`、`effects/README.md` 及现有 Pi/dsh 采纳记录。

## 5. 设计结论

增量补齐契约，优先统一技能加载与来源解释，再完成固定路由、会话检索和技能候选闭环。跨平台网关与日历调度先做宿主组合规格，不下沉成新的核心运行循环。不把图解中的“可重试”扩展成未知副作用可重跑，也不把“同一个 Agent”解释为所有用户共享一份会话或权限。

后续具体设计与验收条件见 [Hermes 采纳设计](../superpowers/specs/2026-11-10-hermes-runtime-design.md)。

## 6. 设计评审与验证范围

独立 Builder 评审提出四个影响实施的问题，均已写回设计：复用 management 已有 outbox，并原子保存 effects 接纳收据；调度未知应答的恢复窗口与幂等保留期协调；Manifest 排除临时路径/动态诊断；固定旧版本计划遵守现有 agent.publish 授权。复核结论为无剩余重要未决问题。

文档验证覆盖新增 Markdown 的本地链接、代码围栏、空白格式，以及全部 17 张原图和 13 份已下载官方源码文件的 SHA-256。代码对照为静态阅读，未运行 Hermes、GoAgent 模型/数据库验收或声称新增功能已可用。本轮仅补设计与索引，保留开始时已有的代码与事实记忆草稿。
