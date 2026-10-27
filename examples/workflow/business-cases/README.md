# GoAgent 原生编排：DevOps、文章审核、自动回复

三个可运行案例，直接使用 `workflow.Graph`、`CompileDurable`、`AsRunnable`、`workflow.Adapt` 和 `tools.Dispatcher`。默认离线运行；指定 `-live` 可接入真实 Gemini、DeepSeek 或 MiniMax。业务操作始终使用本地模拟，不连接真实发布、运维或消息服务。输出中的 `simulated: true` 表示**业务效果**为模拟；`live_model` 表示是否使用真实模型，`model_calls_this_invocation` 记录本次调用模型次数。

| 案例 | 流程 | 终点 |
| --- | --- | --- |
| DevOps | 并行检查健康信号和错误率 → 诊断 → 人工审批 → 工具模拟重启 → 验证记录 | `healthy`、`escalated` 或 `recovered` |
| 文章审核 | 并行内容规则与质量检查 → 拒绝 / 通过 / 人工复核 → 工具模拟发布 | `rejected` 或 `published`；复核时暂停 |
| 自动回复 | 精确匹配本地 FAQ → 有来源的答复 / 转人工 → 工具模拟发送 | `replied` 或 `human_handoff` |

从仓库根目录执行，建议 Go 1.25：

```sh
# 默认故障需要审批；只会检查和保存流程，不产生重启记录。
go run ./examples/workflow/business-cases -case devops -dir /tmp/goagent-devops

# 将上一步输出的 snapshot.Key 复制到下面。
go run ./examples/workflow/business-cases -case devops -dir /tmp/goagent-devops \
  -approve '<snapshot.Key>' -actor demo-operator

# 默认文章符合演示规则，直接生成本地发布记录。
go run ./examples/workflow/business-cases -case article-review -dir /tmp/goagent-article

# 默认问题“营业时间是什么？”有 FAQ 答案，直接生成本地回复记录。
go run ./examples/workflow/business-cases -case auto-reply -dir /tmp/goagent-reply
```

JSON 输出含 `status` 和 `snapshot`。`status: approval_required` 时，`snapshot.Waiting=true`、`snapshot.Done=false`；`status: completed` 只表示流程结束，业务结果还要看 `snapshot.State.outcome` / `decision`。例如 `human_handoff` 表示流程已结束但需要人工处理，示例不会创建真实工单。

## 切换输入和测试路径

`-input` 接收完整 JSON 对象，替换默认输入。不同业务请求应使用不同的业务 ID 和 `-run`。同一 `-run` 恢复已保存状态，忽略新的初始值。以下命令可直接运行：

```sh
# 健康服务无需审批，不生成重启记录。
go run ./examples/workflow/business-cases -case devops -dir /tmp/goagent-cases \
  -run healthy -input examples/workflow/business-cases/fixtures/devops-healthy.json

# 信号冲突，升级处理，不自动重启。
go run ./examples/workflow/business-cases -case devops -dir /tmp/goagent-cases \
  -run escalate -input examples/workflow/business-cases/fixtures/devops-escalate.json

# 拒绝文章，不生成发布记录。
go run ./examples/workflow/business-cases -case article-review -dir /tmp/goagent-cases \
  -run reject -input examples/workflow/business-cases/fixtures/article-reject.json

# 人工复核，暂停在 publish_reviewed 节点。
go run ./examples/workflow/business-cases -case article-review -dir /tmp/goagent-cases \
  -run review -input examples/workflow/business-cases/fixtures/article-review.json
go run ./examples/workflow/business-cases -case article-review -dir /tmp/goagent-cases \
  -run review -approve '<snapshot.Key>' -actor demo-reviewer

# 未知问题转人工，不生成回复记录。
go run ./examples/workflow/business-cases -case auto-reply -dir /tmp/goagent-cases \
  -run handoff -input examples/workflow/business-cases/fixtures/reply-handoff.json

# 第二条 FAQ，输出密码重置说明及来源。
go run ./examples/workflow/business-cases -case auto-reply -dir /tmp/goagent-cases \
  -run password -input examples/workflow/business-cases/fixtures/reply-password.json
```

规则刻意保持确定性，便于验证编排行为：

- DevOps 仅允许 `demo-api`；不健康且错误率 ≥ 5% 时申请重启，健康且错误率 < 5% 时无需动作，其余组合升级人工处理。健康与指标值来自输入测试夹具，重启后的健康状态也是模拟值。
- 文章标题或正文包含 `DEMO_BLOCK` 时拒绝；包含 `DEMO_REVIEW` 或正文不足 80 个 Unicode 字符时转人工；拒绝优先。这些标记只用于流程测试，不具备真实内容安全识别能力。
- 自动回复仅精确匹配“营业时间是什么？”和“如何重置密码？”。其他消息不生成答案，已有的 `answer` / `source` 输入会被重新计算覆盖。
- `demo-operator`、`demo-reviewer` 是本地授权测试标签；审批 Key 绑定当前节点，但不是身份凭证。接入真实业务时，宿主必须从已认证身份取得审批权限。

## 中断恢复与去重

流程检查点位于 `<dir>/<case>/checkpoints/`，模拟效果位于 `<dir>/effects/`。工具用业务 ID 去重：同类型、同 ID、同内容返回原收据；同 ID 改变内容报错。文章修订、后续故障和新消息应使用新的业务 ID。

```sh
go build -o /tmp/goagent-business-cases ./examples/workflow/business-cases
# 首次产生回复记录后立即退出，退出码 86。
/tmp/goagent-business-cases -case auto-reply -dir /tmp/goagent-reply-crash -crash-after-effect
# 重启恢复；收据不变，effects 中仍只有一条记录。
/tmp/goagent-business-cases -case auto-reply -dir /tmp/goagent-reply-crash
# 已完成运行再次调用同样返回原结果。
/tmp/goagent-business-cases -case auto-reply -dir /tmp/goagent-reply-crash
```

崩溃演示使用全新的目录；已存在效果不会再次触发退出。DevOps 和人工复核文章也支持在审批命令追加 `-crash-after-effect`。

这里“持久化一条收据”本身就是模拟效果，文件锁与原子保存足以验证本地去重。替换为真实外部 API 时，必须由目标服务提供业务幂等；在 API 调用旁边写本地文件不能保证外部效果只执行一次。检查点仅支持可靠本地文件系统，暂无跨机器租约。

## 自动化测试

```sh
go test -race -count=1 -v ./examples/workflow/business-cases
```

覆盖：三个场景路由及边界值、审批前零效果、错误审批、非法参数与目标、拒绝优先、未知问题转人工、失败与取消后恢复、业务 ID 冲突、并发重复消息、三个场景的真实子进程崩溃及重启、已完成运行重放、不同 run ID 下的业务去重。普通测试不使用真实模型或外部服务；真实模型测试须按下节显式启用。

## 调用真实模型

凭据从环境变量 `GEMINI_API_KEY` / `DEEPSEEK_API_KEY` / `MINIMAX_API_KEY` / `MINIMAX_CN_API_KEY` 读取，不放入参数或仓库。模型由 `-model` 或相应的 `GEMINI_MODEL` / `DEEPSEEK_MODEL` / `MINIMAX_MODEL` 明确指定。

```sh
# 使用已配置凭据；模型名称可替换为账号可用的版本。
go run ./examples/workflow/business-cases -live -provider gemini -model gemini-2.5-flash \
  -case devops -dir /tmp/goagent-live-devops
# 审批恢复必须使用相同 provider/model，复制首次输出的 snapshot.Key。
go run ./examples/workflow/business-cases -live -provider gemini -model gemini-2.5-flash \
  -case devops -dir /tmp/goagent-live-devops -approve '<snapshot.Key>' -actor demo-operator

go run ./examples/workflow/business-cases -live -provider gemini -model gemini-2.5-flash \
  -case article-review -dir /tmp/goagent-live-article
go run ./examples/workflow/business-cases -live -provider deepseek -model deepseek-flash \
  -case auto-reply -dir /tmp/goagent-live-reply
```

真实模式下，模型不是只生成演示文字，而是参与已持久化的业务节点：

- DevOps：模型给出诊断建议，只有与本地重启条件一致时才进入审批；不一致时升级处理。
- 文章审核：模型判断文章语义，拒绝危险医疗误导等内容；本地硬性拒绝和长度规则仍有效，模型不能将已拒绝内容直接放行。
- 自动回复：模型将口语化问题映射到 FAQ 来源，或决定转人工。宿主按来源读取固定答案，避免模型任意编造回复正文；这不是自由生成式客服质量测试。

每次 CLI 调用最多一次模型请求，90 秒超时、最多 2048 输出 Token、无自动重试。请求失败、截断、JSON 无效、动作越界或来源不存在时停止流程，不产生业务效果。模型判断、理由、Token 用量与耗时保存在 `snapshot.State.model_review` 中；模型节点保存成功后的审批恢复和流程重放不再调用模型。模型请求前先保存操作意图；失败、无效结果或请求后检查点未落盘时进入 needs_attention，重启不再付费重试。失败判断保存已知用量、错误类别和响应摘要；未收到的用量无法重建。

provider、model 和提示词版本绑定持久化计划；离线和真实模式、不同模型之间不能复用同一运行检查点，请使用不同 `-dir` / `-run`。本地业务去重仍按业务 ID 生效。

### 六条真实模型集成测试

```sh
# 已配置 API Key 后运行，报告文件必须不存在，父目录必须存在。
GEMINI_MODEL=gemini-2.5-flash bash scripts/business_cases_live.sh gemini /tmp/business-gemini.json
DEEPSEEK_MODEL=deepseek-flash bash scripts/business_cases_live.sh deepseek /tmp/business-deepseek.json
MINIMAX_MODEL=MiniMax-M3.1-Flash bash scripts/business_cases_live.sh minimax /tmp/business-minimax.json
```

每个 Provider 最多 6 次请求：故障诊断、冲突信号升级、正常文章、含诱导指令的危险医疗误导文章、口语化营业时间问题、退款与越权诱导消息。测试核对模型决策和最终路由，并验证无动作分支零效果、审批前零效果、效果已提交后的恢复与完成重放不重复请求模型。结果保存失败项、用量和耗时，不静默重试。六条合成样本只能验证功能链路，不能作为生产准确率或模型排名。

这里使用 Provider 的 `Complete` 产生受约束决策，再通过图路由和 Dispatcher 执行工具；没有让模型直接调用外部工具，也不覆盖 Provider 原生 function calling 协议测试。

### MiniMax 接入

MiniMax 使用本案例适配器调用官方 OpenAI 兼容 Chat Completions API，支持文本补全和原生工具调用续接。输出为缓冲后的 final-only，Agent 适配器声明 steps；不宣称原生逐 token 流，也未添加全局 Provider 注册。

- 配置 `MINIMAX_CN_API_KEY` 时默认请求中国区 `https://api.minimax.cn/v1`。
- 配置 `MINIMAX_API_KEY` 时默认请求国际区 `https://api.minimax.io/v1`；两个 Key 均存在时，使用此国际区 Key。
- `MINIMAX_BASE_URL` 可显式覆盖地址，凭据应与目标区域匹配；不读取 `OPENAI_API_KEY` / `OPENAI_BASE_URL`。
- 发送 `reasoning_split: true`，只将最终 `content` 送入业务 JSON 校验，思考正文不写入报告或业务状态；原生工具循环的本地操作检查点须保留它以供协议续接。
- `MiniMax-M3.1-Flash` 设置 `reasoning_effort: low`，生成上限为 2048 Token，含思考用量。官方文档说明该模型忽略请求温度、固定为 1，因此这里不做同温度模型质量排名。

以上参数参考[中国区官方接口文档](https://platform.minimax.cn/docs/api-reference/text-openai-api)。与其他案例一样，业务动作使用本地模拟；请求失败、截断或非法判断会停止流程，不自动修复或重试。

```sh
go run ./examples/workflow/business-cases -live -provider minimax -model MiniMax-M3.1-Flash \
  -case auto-reply -dir /tmp/goagent-live-minimax-reply
```


## 原生多工具循环与进程恢复

`native_loop.go` 让一个持久 Agent 节点调用 `record_A`、`record_B`，它们只写本地模拟回执。模型原生选择工具，Dispatcher 校验 stage 枚举并持久化调用。测试在 B 效果提交、工具结果检查点保存前直接退出进程；重启复用模型计划、跳过 A、用同一子键恢复 B，然后生成答案。完成后再启动不会付费或执行工具。

```sh
go test -race ./examples/workflow/business-cases -run TestNativeToolLoopProcessRecovery
GEMINI_MODEL=gemini-2.5-flash bash scripts/business_tool_loop_live.sh gemini /tmp/gemini-loop.json
DEEPSEEK_MODEL=deepseek-flash bash scripts/business_tool_loop_live.sh deepseek /tmp/deepseek-loop.json
MINIMAX_MODEL=MiniMax-M3.1-Flash bash scripts/business_tool_loop_live.sh minimax /tmp/minimax-loop.json
```

使用新报告路径；每次最多 4 个真实请求（跨进程累计），失败不自动修复或覆盖。报告保存各阶段请求数、工具尝试、效果数量、不同子键、操作状态和已知用量。默认 `go test` 不运行真实模型。此测试补充前述 Complete 分类案例，覆盖原生 function calling 和协议续接；[实测报告](../../../docs/evaluation/2026-10-27-durable-tool-loop/REPORT.md)保留首次失败与调整后的验证结果。

失败的分类请求会输出 `status: needs_attention`、`model_review` 和本次模型调用次数，并以错误退出。成功判断仍在 `snapshot.State.model_review`。旧的 business-judge-v1 检查点与新策略不兼容，须使用新的运行版本/目录并核对已有业务效果。
