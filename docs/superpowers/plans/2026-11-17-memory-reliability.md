# 长期记忆可靠性验证 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在客服示例上实现并运行 A（真实场景回归）、B（故障与并发）、C（memeval 基准）三部分验证，结果写入 `docs/evaluation/2026-11-17-memory-reliability/`。

**Architecture:** A、B 是 goagent-examples `feat/cs-memory` worktree 中 `orchestration/customer-service` 包的 Go 测试，复用 `hostEnv`、`newCustomer`、`turnJobKeys` 等现有测试辅助，经 `scripts/with-stores.sh` 使用本地 docker 存储。C 不改 memeval，只运行它并用一个只读辅助程序（存为 `.go.txt`）做重新评审与跨运行配对。所有代码都不进生产路径。

**Tech Stack:** Go 1.27、testify、goagent `memory`（经 go.work 用本地代码）、contrib pgstore 与 qdrant、DeepSeek、Gemini 嵌入、docker。

**Spec:** `docs/superpowers/specs/2026-11-17-memory-reliability-design.md`（goagent `test/memory-reliability`，423cc3bf）

## Global Constraints

- 被测代码：goagent `test/memory-reliability`（master + `feat/memory-save-replaces` + `fix/deepseek-dsml-toolcalls`），经 `goagent-examples-cs-mem/go.work` 使用；报告写明确切提交。
- 示例仓库路径：`/home/hellotalk/code/go/src/github.com/kart-io/goagent-examples-cs-mem`，分支 `feat/cs-memory`；包目录下文中简称 `cs/`（即 `orchestration/customer-service/`）。
- 不改生产代码（`cs/` 下非 `_test.go` 文件、`model.go` 的话术表），唯一例外是 `cs/scripts/with-stores.sh` 导出容器名。
- 测试命令统一前缀 `TMPDIR=/tmp taskset -c 2,3,6-27`，`go test -p 1`；不用 `-mod=mod`，不 `go mod tidy`，不改 go.mod/go.sum。
- 不发版、不打 tag、不推送、不合并到 master；只按路径 `git add`，不用 `git add -A`。
- 示例仓库主 worktree 中用户未提交的文件、contrib 的两个 worktree 都不碰。
- API Key、连接串不写入文件、日志、提交或报告；JSONL 与报告里只出现提供方名与模型名。
- 测试表驱动；注释、提交信息、报告用简体中文。
- 不修复发现的缺陷，只记录；不为通过而放宽断言或挑选运行结果。
- 对话模型 DeepSeek（`deepseek-flash`，`GOAGENT_LIVE_PROVIDER` 默认值），嵌入 `gemini-embedding-001`，A 与 C 的评审都用 DeepSeek。

## Review Focus

1. A 运行中提供方返回 429、5xx 或超时：这一次记为 `infra`，不计入通过率分母，测试不失败，其余重复继续。由 Task 4 的 `TestScenarioSummary` 覆盖。
2. A 中通用成员追问客户（工单停在 waiting_customer）：`driveTicket` 用固定话术最多补答 2 次，场景继续。由 Task 3 保持 `TestLiveMemory` 的既有行为。
3. 抽取任务在一次 sweep 后仍未结束：`settle` 反复 sweep，直到相关轮次的任务都进入终态（done、failed 或 dropped），上限 5 次；超出上限记为 `extract` 失败，不卡住测试。由 Task 3 的 `settle` 负责。
4. B 的 pg-outage 断言失败或 panic 后容器仍处于暂停：`t.Cleanup` 总会执行 `docker unpause`，后续测试照常可用。由 Task 6 的 pg-outage 子测试负责。
5. 多次运行写同一个输出目录：A 的输出目录按运行开始时间分子目录，旧结果不会被覆盖。由 Task 4 的 `scenarioOutDir` 负责。

---

### Task 1: 把 DSML 修复合入测试分支

**Files:** 无新增；goagent 合并提交。

- [ ] **Step 1: 合并**

```bash
cd /home/hellotalk/code/go/src/github.com/kart-io/goagent
git checkout test/memory-reliability
git merge --no-ff fix/deepseek-dsml-toolcalls -m "merge: 合入 DSML 工具调用还原，作为可靠性验证的被测代码"
```

期望：无冲突（fix 分支只改 `llm/providers/` 与 `CHANGELOG.md`）。若 `CHANGELOG.md` 冲突，两边条目都保留。

- [ ] **Step 2: 验证**

Run: `TMPDIR=/tmp taskset -c 2,3,6-27 go test -p 1 ./llm/providers/ ./memory/... && (cd ../goagent-examples-cs-mem && TMPDIR=/tmp taskset -c 2,3,6-27 go build ./orchestration/customer-service/)`
Expected: PASS，构建无错误。

- [ ] **Step 3: 在 ledger 记下合并提交号**（`git rev-parse HEAD`），报告的“代码版本”用它。

### Task 2: with-stores.sh 导出容器名

**Files:**
- Modify: `cs/scripts/with-stores.sh`（`export CS_MEMORY_QDRANT_ADDR=…` 之后）
- Modify: `cs/README.md` 第 213 行附近（说明新增的两个变量）

- [ ] **Step 1: 先确认现在没有导出**

Run: `cd goagent-examples-cs-mem && orchestration/customer-service/scripts/with-stores.sh sh -c 'echo "pg=${CS_MEMORY_PG_CONTAINER:-无} qd=${CS_MEMORY_QDRANT_CONTAINER:-无}"'`
Expected: `pg=无 qd=无`

- [ ] **Step 2: 加两行导出** `CS_MEMORY_PG_CONTAINER="$pg"`、`CS_MEMORY_QDRANT_CONTAINER="$qdrant"`，在脚本头注释里补一句用途：故障测试只暂停或删除这两个容器。

- [ ] **Step 3: 验证**

Run: `orchestration/customer-service/scripts/with-stores.sh sh -c 'docker inspect -f "{{.State.Running}}" "$CS_MEMORY_PG_CONTAINER" "$CS_MEMORY_QDRANT_CONTAINER"'`
Expected: 两行 `true`。脚本退出后 `docker ps -a --filter name=cs-mem-` 不再列出这两个容器。

- [ ] **Step 4: 提交**

```bash
git add orchestration/customer-service/scripts/with-stores.sh orchestration/customer-service/README.md
git commit -m "test(customer-service): with-stores.sh 导出容器名，供故障测试只操作自己的容器"
```

### Task 3: 可复用的工单驱动与记忆状态辅助

把 `TestLiveMemory` 里驱动工单走到终态的 `say` 闭包提成函数，供 A 复用；补上读取记忆状态、等待抽取完成的辅助。A 的场景要记录错误而不是立即失败，所以这些辅助返回 error，不调用 `require`。

**Files:**
- Modify: `cs/host_test.go`（新增 `newHostEnvAt`，`newHostEnv` 改为调用它）
- Modify: `cs/live_test.go`（`TestLiveMemory` 改用 `driveTicket`）
- Create: `cs/memstate_test.go`

**Interfaces:**
- Produces:
  - `func newHostEnvAt(t *testing.T, dir string, mutate func(*HostConfig)) *hostEnv`：与 `newHostEnv` 相同，只是 `dir` 由调用方给出；`newHostEnv(t, m)` 等价于 `newHostEnvAt(t, t.TempDir(), m)`。
  - `type ticketEvidence struct { ID, Status string; FollowUps, Approvals int }`，JSON 标签沿用 `TestLiveMemory` 现有的。
  - `func driveTicket(ctx context.Context, e *hostEnv, customer, text string) (ticketEvidence, error)`：行为与现有 `say` 相同（`Action` 须为 `started`；追问最多 2 次，补答话术不变；审核依次试 `lead-1`、`fin-1`；须到终态），违反时返回带工单 ID 的错误。
  - `func validRecords(ctx context.Context, m *memory.Memory) ([]memory.Record, error)`：用 `Recall(ctx, memory.Query{K: 1000})`（空文本时只按新近性排序、返回全部有效记录）取出当前命名空间的全部有效记录。
  - `func settle(ctx, e *hostEnv, cctx context.Context, tickets []string) (SweepReport, error)`：反复 `e.host.Sweep`（最多 5 次），直到这些工单在 `refundName`、`generalName` 两个会话中各轮的抽取任务都是 `JobDone`、`JobFailed` 或 `JobDropped`；会话不存在（`ErrSessionNotFound`）就跳过。超过上限时返回错误，列出仍未结束的任务 Key。返回最后一次 sweep 的报告。
  - `turnJobKeys` 改为返回 `([]string, error)` 的 `sessionJobKeys`，现有调用处包一层 `require.NoError`。不改任何断言。

- [ ] **Step 1: 先写 `validRecords` 与 `settle` 的离线测试**，放在 `memstate_test.go`，用 `newMemoryEnv`（离线脚本模型，需要容器）：

```go
func TestMemStateHelpers(t *testing.T) {
	e, _ := newMemoryEnv(t)
	c, cctx := newCustomer()
	r := e.message(msgID(), c, "订单 A1001 申请退款 30 元，只退回原支付方式")
	_, err := settle(context.Background(), e, cctx, []string{r.TicketID})
	require.NoError(t, err)
	recs, err := validRecords(cctx, e.host.memory.Memory)
	require.NoError(t, err)
	require.Contains(t, contents(recs, func(r memory.Record) string { return r.Content }), "客户要求退款只退回原支付方式")
	_, other := newCustomer()
	empty, err := validRecords(other, e.host.memory.Memory)
	require.NoError(t, err)
	require.Empty(t, empty)
}
```

- [ ] **Step 2: 运行，确认因未定义而编译失败**

Run: `cd goagent-examples-cs-mem && TMPDIR=/tmp taskset -c 2,3,6-27 orchestration/customer-service/scripts/with-stores.sh go test -p 1 -run TestMemStateHelpers ./orchestration/customer-service/`
Expected: 编译失败，`undefined: settle`

- [ ] **Step 3: 实现上面 Interfaces 中的全部函数**，并把 `TestLiveMemory` 的 `say` 改为 `ev, err := driveTicket(...); require.NoError(t, err); tickets = append(tickets, &ev)`。

- [ ] **Step 4: 跑全部记忆测试（离线），并编译 live 测试**

Run: `TMPDIR=/tmp taskset -c 2,3,6-27 orchestration/customer-service/scripts/with-stores.sh go test -p 1 -race ./orchestration/customer-service/`
Expected: PASS（live 测试因没有 `GOAGENT_LIVE` 而跳过）。

- [ ] **Step 5: 用真实模型跑一次 `TestLiveMemory`，确认重构没有改变行为**（约 20 次调用）

Run: `GOAGENT_LIVE=1 TMPDIR=/tmp taskset -c 2,3,6-27 orchestration/customer-service/scripts/with-stores.sh go test -p 1 -run '^TestLiveMemory$' -v ./orchestration/customer-service/`
Expected: PASS，输出含 `LIVE_EVIDENCE`。

- [ ] **Step 6: 提交**

```bash
git add orchestration/customer-service/host_test.go orchestration/customer-service/live_test.go orchestration/customer-service/memory_test.go orchestration/customer-service/memstate_test.go
git commit -m "test(customer-service): 提出工单驱动与记忆状态辅助，供多场景回归复用"
```

### Task 4: A 的评审与汇总（离线可测部分）

**Files:**
- Create: `cs/scenario_judge_test.go`

**Interfaces:**
- Produces:
  - `type judgeKind string`，取值 `judgeGeneric`、`judgeUpdate`、`judgeTemporal`、`judgeAbstain`，分别对应 `memory/memeval.JudgePrompt` 的通用、knowledge-update、temporal-reasoning、abstain 四个模板。
  - `func judgePrompt(kind judgeKind, question, expect, reply string) string`：把这四个英文模板的判定口径改写成中文，固定在代码中；要求评审第一行只输出“对”或“错”，第二行给一句理由。
  - `func parseVerdict(out string) (ok bool, reason string, err error)`：取第一个非空行，去掉首尾空白与标点；“对”“正确”“是”为真，“错”“错误”“否”为假，其他返回错误。理由取其余行，拼成一行。
  - `type scenarioResult struct`，JSONL 一行一个，字段：`Scenario, Repeat, Customers []string, Tickets []ticketEvidence, Question, Expect, Reply, RecallBlock string, Records []memory.Record, StateOK bool, StateNote string, Invariant bool, JudgeOK bool, JudgeReason, Infra string, ModelCalls int64, Seconds float64`，标签用 snake_case。`Infra` 非空表示这一次因提供方或环境错误作废。
  - `type scenarioRow struct { Scenario string; N, Infra, StatePass, JudgePass, BothPass int; Lo, Hi float64 }`，`Lo, Hi` 是 `BothPass/N` 的 Wilson 95% 区间。
  - `func summarizeScenarios(rs []scenarioResult) []scenarioRow`：按场景名排序；`Infra` 非空的不计入 `N` 与各通过数，只计入 `Infra`。
  - `func scenarioMarkdown(rows []scenarioRow) string`：表头 `| 场景 | 有效次数 | infra | 状态通过 | 评审通过 | 两层通过 | 通过率 | 95% 区间 |`。
  - `func scenarioOutDir() (string, error)`：取 `CS_SCENARIO_OUT`（默认 `/tmp/cs-scenarios`），在它下面创建 `20061102-150405` 格式的时间子目录并返回。

- [ ] **Step 1: 写测试 `TestJudgePrompt`、`TestParseVerdict`、`TestScenarioSummary`**（表驱动，不需要容器与网络）

```go
func TestParseVerdict(t *testing.T) {
	tests := []struct{ in string; ok, wantErr bool }{
		{"对\n答复给出了上海浦东", true, false},
		{"错。\n答复仍说北京", false, false},
		{"  正确：理由", true, false},
		{"否", false, false},
		{"我认为……", false, true},
		{"", false, true},
	}
	// 逐条断言 ok 与是否出错
}

func TestScenarioSummary(t *testing.T) {
	rs := []scenarioResult{
		{Scenario: "update", StateOK: true, JudgeOK: true},
		{Scenario: "update", StateOK: true, JudgeOK: false},
		{Scenario: "update", Infra: "deepseek: 429"},
		{Scenario: "abstain", StateOK: true, JudgeOK: true},
	}
	rows := summarizeScenarios(rs)
	require.Equal(t, "abstain", rows[0].Scenario)
	u := rows[1]
	require.Equal(t, []int{2, 1, 2, 1, 1}, []int{u.N, u.Infra, u.StatePass, u.JudgePass, u.BothPass})
	require.InDelta(t, 0.0945, rows[1].Lo, 0.001) // Wilson(1,2) 下界
	require.InDelta(t, 0.9055, rows[1].Hi, 0.001)
}
```

`TestJudgePrompt` 对四种 kind 断言：结果含问题、标准答案、答复原文；update 含“以更新后的答案为准”的意思；temporal 含“相差一天不扣分”；abstain 含“指出无法回答或请对方提供”。

- [ ] **Step 2: 运行确认失败**

Run: `TMPDIR=/tmp taskset -c 2,3,6-27 go test -p 1 -run 'TestJudgePrompt|TestParseVerdict|TestScenarioSummary' ./orchestration/customer-service/`
Expected: 编译失败，`undefined: judgePrompt`

- [ ] **Step 3: 实现 Interfaces 中的函数**；Wilson 区间公式与 `memory/memeval/score.go` 的 `wilson` 相同（z=1.96），N=0 时区间为 0,0。

- [ ] **Step 4: 运行确认通过**（命令同 Step 2）。Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add orchestration/customer-service/scenario_judge_test.go
git commit -m "test(customer-service): 记忆场景回归的评审提示词、判定解析与通过率汇总"
```

### Task 5: A 的场景表与 `TestLiveMemoryScenarios`

**Files:**
- Create: `cs/scenarios_live_test.go`

**Interfaces:**
- Consumes: Task 3 的 `newHostEnvAt`、`driveTicket`、`validRecords`、`settle`；Task 4 的全部函数与类型。
- Produces: `TestLiveMemoryScenarios`。

**场景表**（话术与检查取自 spec 的 A 场景表，下表只列实现要用的关键词；每个场景每次重复使用新客户；每张工单之后执行 `settle`，pinchain 每步后 `settle` 两次）：

| name | judgeKind | 标准答案（给评审） | 状态检查（`validRecords` 的正文） | 不变量 |
|---|---|---|---|---|
| recall | generic | 针对 X200 给出固件升级方法，不再询问型号 | 有记录含 `X200` | 否 |
| multi | generic | A1002 是耳机，A1003 是充电器 | 有记录含 `耳机`，有记录含 `充电器` | 否 |
| update | update | 寄到上海浦东 | 没有记录含 `朝阳`，有记录含 `浦东` | 否 |
| temporal | temporal | 最早反馈的是耳机断连 | 有记录含 `断连`，有记录含 `发烫` | 否 |
| abstain | abstain | 没有该客户设备型号的记录，应说明不知道或请客户提供 | 没有记录含 `型号` | 否 |
| premise | generic | 记录中耳机是蓝色，应指出或确认颜色，不按红色作答 | 不检查（StateOK 为真） | 否 |
| isolate | generic | 没有该客户会员号的记录，不得给出 M-778899 | 客户 Q 的命名空间没有记录含 `778899`，`Pinned` 也没有 | 是 |
| forget | generic | 客户已要求删除备用电话，不应给出 13800001111 | 没有记录含 `13800001111` | 否 |
| pinchain | generic | 只用邮件联系 | 见下 | 是 |

pinchain 的状态检查：每张工单 `settle` 后把 `Pinned` 返回的 ID 都加入集合 S。最后对 S 中每个 ID 调用 `Get`，沿 `SupersededBy` 走到底：链上每条记录 `Pinned` 为真，末端 `Valid()`。另外最终的 `Pinned` 中至少有一条含 `邮件`。

运行方式：
- 门控与 `TestLiveMemory` 相同。`CS_SCENARIO_REPEATS` 默认 10；`CS_SCENARIO_ONLY` 是逗号分隔的场景名，为空时运行全部。
- 每个场景是一个 `t.Run`，内部调用 `t.Parallel()`，并发数用 `-parallel 3` 控制。每次重复是一个串行子测试 `r01`、`r02`……，各自调用 `newHostEnvAt(t, <输出目录>/<场景>-<rr>, …)`，所以运行记录 `runs/` 直接留在输出目录，供失败归类。
- 每次重复的最后一步是被评审的问题，答复取 `e.sent(support.KindCustomerReply)` 中发给该客户的最后一条。
- `RecallBlock` 取 `e.host.memory.Memory.RecallBlock(cctx, question)`。`Records` 取作答前的 `validRecords`；pinchain 另外附上 S 中各记录 `Get` 的结果。
- 评审客户端用 `livemodel.New(livemodel.Config{Provider: "deepseek", Model: "deepseek-flash"})` 单独创建，不和被测客户端共用调用计数。评审调用失败记为 `Infra`。
- `driveTicket`、`settle` 或提供方返回错误时，这一次记 `Infra`（写入错误文本，先经 `livemodel.Redact`），继续下一次重复。
- 每次结果追加到 `<输出目录>/results.jsonl`（多个 goroutine 写入，要加锁）。全部结束后把 `scenarioMarkdown` 写入 `<输出目录>/summary.md`，同时 `t.Log` 输出。
- 只有 `Invariant` 为真的场景状态检查失败时才调用 `t.Errorf`，其余场景的失败只体现在结果中。

- [ ] **Step 1: 实现 `TestLiveMemoryScenarios` 与场景表**

- [ ] **Step 2: 不设 `GOAGENT_LIVE` 时确认跳过、没有调用网络**

Run: `TMPDIR=/tmp taskset -c 2,3,6-27 go test -p 1 -run TestLiveMemoryScenarios -v ./orchestration/customer-service/`
Expected: `SKIP`

- [ ] **Step 3: 只跑 abstain、isolate 各 1 次做冒烟**（约 20 次调用）

Run: `GOAGENT_LIVE=1 CS_SCENARIO_REPEATS=1 CS_SCENARIO_ONLY=abstain,isolate TMPDIR=/tmp taskset -c 2,3,6-27 orchestration/customer-service/scripts/with-stores.sh go test -p 1 -parallel 3 -timeout 30m -run TestLiveMemoryScenarios -v ./orchestration/customer-service/`
Expected: PASS；输出目录下有 `results.jsonl`（2 行）、`summary.md`、`abstain-01/runs/`。用 `grep -c 'sk-' results.jsonl` 确认没有密钥（输出 0）。

- [ ] **Step 4: 提交**

```bash
git add orchestration/customer-service/scenarios_live_test.go
git commit -m "test(customer-service): 九个长期记忆真实场景回归（召回、更新、时间、弃答、隔离、遗忘、钉住链等）"
```

### Task 6: B 的 `TestMemoryFaults`

**Files:**
- Create: `cs/faults_test.go`

**Interfaces:**
- Consumes: Task 3 的 `newHostEnvAt`、`validRecords`、`settle`；现有的 `newCustomer`、`msgID`、`rememberPinned`、`pinned`、`respond`。
- Produces:
  - `func faultModel(onReconcile func()) *livemodel.Client`：包装 `respond`。抽取请求（系统提示以 `你从一轮对话中抽取` 开头）的对话里含 `记下：` 时，把 `记下：` 之后到行尾或引号为止的文本（正则 `记下：([^"\n]+)`）作为一条 fact 返回，重要度 0.8；整理请求（以 `你维护长期记忆` 开头）先调用 `onReconcile`（可为 nil），再交给 `respond`；其余请求直接交给 `respond`。
  - `TestMemoryFaultCrashChild`：只在设置了 `CS_FAULT_CHILD_DIR` 时运行，否则 `t.Skip`。它在该目录用 `newHostEnvAt` 打开宿主（`faultModel` 的 `onReconcile` 先创建 `CS_FAULT_CHILD_MARK` 文件，再 `select {}` 阻塞），然后调用 `Sweep`。

`TestMemoryFaults` 的子测试，串行执行（index-loss 会删除共用集合，pg-outage 会暂停共用容器）：

| 子测试 | 做法 | 断言 |
|---|---|---|
| concurrent | 同一客户两个 goroutine 同时 `Message`，分别发 `记下：会员等级是金卡`、`记下：常用快递是顺丰`；两个 goroutine 同时 `Sweep`；最后 `settle` | 两次 `Message` 和两次 `Sweep` 都没有错误；`validRecords` 中 `会员等级是金卡`、`常用快递是顺丰` 各恰好 1 条 |
| replay | `记下：生日是三月` 后 `settle`，记下记录数 n；再 `Sweep` 3 次；用同一个消息 ID 再发一次原消息，然后 `settle` | 记录数仍为 n；重复投递返回的 `Action` 不是 `started` |
| crash | `rememberPinned(…, "请记住：只用邮件联系我")`；`Message("邮件或短信都可以")`，不 sweep；`e.host.Close()`；用 `exec.Command(os.Args[0], "-test.run", "^TestMemoryFaultCrashChild$")` 启动子进程，环境带 `CS_FAULT_CHILD_DIR=e.dir` 与标记文件路径；60 秒内等到标记文件出现后 `Process.Kill()` 并 `Wait()`；`e.open()`，每 2 秒 `Sweep` 一次（忽略错误，等待租约过期），最长 3 分钟，直到该轮任务为 `JobDone` | 任务最终完成；`Pinned` 恰好 1 条，内容为 `客户接受邮件或短信联系`；原钉住记录无效，且 `SupersededBy` 指向这条记录；对所有经手的记录，失效原因为 superseded 时取代者存在 |
| pg-outage | 需要 `CS_MEMORY_PG_CONTAINER`，否则跳过。`记下：发票抬头是个人`；`docker pause`，并注册 `t.Cleanup(docker unpause)`；用 10 秒超时的上下文 `Sweep`；`docker unpause`；`settle` | 暂停期间 `Sweep` 返回错误；恢复后 `settle` 成功，`发票抬头是个人` 恰好 1 条 |
| index-loss | `记下：常住城市是杭州`、`记下：偏好晚上联系` 后 `settle`；`Recall(cctx, Query{Text: "杭州", K: 10})` 得到 ID 集合 A；用 `qc.NewClient` 连接 `CS_MEMORY_QDRANT_ADDR`，删除所有名称以 `cs_memory_` 开头的集合；再 `Recall` 一次，并发一条新消息；`Memory.Reindex(cctx, true)`；再 `Recall` 得到 B | 删除后的 `Recall` 与 `Message` 不 panic（返回错误可以接受，结果写入日志）；`Reindex` 没有错误；B 与 A 的 ID 集合相同 |
| ns-concurrent | 5 个客户并发，每个客户 `Message("记下：暗号是 K<i>")` 后各自 `Sweep`；最后 `settle` | 每个命名空间的 `validRecords` 只含自己的 `K<i>`，且恰好 1 条 |

- [ ] **Step 1: 实现 `faultModel` 与 `TestMemoryFaultCrashChild`，并先实现 concurrent、replay 两个子测试**

- [ ] **Step 2: 运行**

Run: `TMPDIR=/tmp taskset -c 2,3,6-27 orchestration/customer-service/scripts/with-stores.sh go test -p 1 -race -run 'TestMemoryFaults/(concurrent|replay)' -v ./orchestration/customer-service/`
Expected: 两个子测试都给出结果。失败按 spec 视为缺陷：保留输出，不改断言，在 ledger 记下现象。

- [ ] **Step 3: 实现 crash、pg-outage、index-loss、ns-concurrent**

- [ ] **Step 4: 全量运行 B 两次，确认结果可重复**

Run: `TMPDIR=/tmp taskset -c 2,3,6-27 orchestration/customer-service/scripts/with-stores.sh go test -p 1 -race -count 2 -run 'TestMemoryFaults' -v ./orchestration/customer-service/ 2>&1 | tee /tmp/b-faults.log`
Expected: 每个子测试在两次运行中结果一致。结束后 `docker ps --filter status=paused` 为空。

- [ ] **Step 5: 跑整个包的离线测试，确认没有误伤其他测试**

Run: `TMPDIR=/tmp taskset -c 2,3,6-27 orchestration/customer-service/scripts/with-stores.sh go test -p 1 -race ./orchestration/customer-service/...`
Expected: 除 B 中记录为缺陷的子测试外全部 PASS。

- [ ] **Step 6: README 补运行方式**：在 `cs/README.md` 第 272 行那一段后加一段，写 A（`TestLiveMemoryScenarios` 及三个环境变量与 `-parallel 3`）与 B（`TestMemoryFaults`，pg-outage 依赖容器名）的命令；然后提交：

```bash
git add orchestration/customer-service/faults_test.go orchestration/customer-service/README.md
git commit -m "test(customer-service): 长期记忆故障与并发测试（并发、重放、崩溃、Postgres 中断、索引丢失、多客户）"
```

### Task 7: C 的重新评审与跨运行对比辅助程序

**Files:**
- Create: `goagent/docs/evaluation/2026-11-17-memory-reliability/rejudge.go.txt`

**Interfaces:**
- 运行方式：`cp rejudge.go.txt /tmp/rejudge/main.go`，然后在 goagent 根目录执行 `go run /tmp/rejudge/main.go <参数>`（从 goagent 模块解析 import）。
- 参数：
  - `-baseline a.jsonl,b.jsonl`：G2 基线文件。
  - `-current x.jsonl`：本次 memeval 的结果。
  - `-judged out.jsonl`：重新评审后的基线，文件已存在时直接读取，不重复调用模型。
  - `-out compare.md`：对比结果。
  - `-concurrency 8`。
  - `-check`：只验证符号检验。
- 重新评审：对每条基线 `memeval.Result`，用 `memeval.Question{Text, Answer, Type, Abstain}` 与 `memeval.JudgePrompt` 构造提示词，调用 DeepSeek（温度 0）。解析方式与 `memory/memeval/run.go` 的判定逻辑相同，实现前先读 `run.go` 第 124–147 行并照抄。只替换 `Correct` 字段。失败最多重试 4 次，仍失败的题从配对中剔除并计数。
- 配对：键为 (`system`, `question_id`, `run`)，基线文件按顺序对应 run 1、run 2，本次结果按自身的 `run` 字段。本次对、基线错为胜，反之为负。精确双侧符号检验 `p = min(1, 2·Σ_{i≤min(w,l)} C(n,i)/2^n)`，其中 n=w+l。
- 输出表格：`| 方案 | 运行 | 本次准确率 | 基线(DeepSeek 重判)准确率 | 基线(gpt-4o 原判)准确率 | 胜 | 负 | p |`，另附一行说明 DeepSeek 与 gpt-4o 判定的一致率。

- [ ] **Step 1: 写程序，并实现 `-check`**：断言 `signP(19, 24)` 四舍五入后为 `0.542`（与 G2 README 的配对结果一致），以及 `signP(0, 0) == 1`。

- [ ] **Step 2: 运行 `-check`**

Run: `mkdir -p /tmp/rejudge && cp docs/evaluation/2026-11-17-memory-reliability/rejudge.go.txt /tmp/rejudge/main.go && go run /tmp/rejudge/main.go -check`
Expected: 输出 `ok`

- [ ] **Step 3: 提交**

```bash
git add docs/evaluation/2026-11-17-memory-reliability/rejudge.go.txt
git commit -m "docs(evaluation): memeval 基线重新评审与跨运行符号检验辅助程序"
```

### Task 8: 运行 C

- [ ] **Step 1: 下载数据集**到 `/tmp/memeval-data/`。LoCoMo 用 `snap-research/locomo` 的 `data/locomo10.json`，sha256 前缀须为 `79fa87e9`（与 G2 一致，不一致就停下报告）；LongMemEval 用 `xiaowu0162/longmemeval` 的 `longmemeval_oracle.json`。记下两个 sha256。

- [ ] **Step 2: 运行 LoCoMo**（在 goagent 根目录执行，输出到 `docs/evaluation/2026-11-17-memory-reliability/`）

```bash
TMPDIR=/tmp taskset -c 2,3,6-27 go run ./scripts/memeval -format locomo -data /tmp/memeval-data/locomo10.json -sample 200 -repeats 2 -concurrency 10 \
  -systems memory,graph -chat deepseek -judge deepseek -embed gemini:gemini-embedding-001 \
  -out docs/evaluation/2026-11-17-memory-reliability/locomo-200.md -results docs/evaluation/2026-11-17-memory-reliability/locomo-200.jsonl
```

Expected: 两个文件生成；JSONL 共 800 行（200 题 × 2 方案 × 2 次），题目 ID 集合与 G2 的 `locomo-200.jsonl` 相同（用 `jq -r .question_id | sort -u | diff` 核对）。

- [ ] **Step 3: 运行 LongMemEval oracle**：参数与上一步相同，改为 `-format longmemeval -data …/longmemeval_oracle.json -sample 100 -repeats 1`，输出文件名为 `lme-100.md`、`lme-100.jsonl`。

- [ ] **Step 4: 重新评审并对比**

Run: `go run /tmp/rejudge/main.go -baseline docs/evaluation/2026-11-13-memory-g2/locomo-200.jsonl,docs/evaluation/2026-11-13-memory-g2/locomo-200-r2.jsonl -current docs/evaluation/2026-11-17-memory-reliability/locomo-200.jsonl -judged docs/evaluation/2026-11-17-memory-reliability/g2-rejudged-deepseek.jsonl -out docs/evaluation/2026-11-17-memory-reliability/compare.md`
Expected: `compare.md` 有 memory、graph 各 2 行，记下 p 值。

- [ ] **Step 5: 提交**（先 `grep -l 'sk-' docs/evaluation/2026-11-17-memory-reliability/*` 确认没有密钥）

```bash
git add docs/evaluation/2026-11-17-memory-reliability/locomo-200.md docs/evaluation/2026-11-17-memory-reliability/locomo-200.jsonl docs/evaluation/2026-11-17-memory-reliability/lme-100.md docs/evaluation/2026-11-17-memory-reliability/lme-100.jsonl docs/evaluation/2026-11-17-memory-reliability/g2-rejudged-deepseek.jsonl docs/evaluation/2026-11-17-memory-reliability/compare.md
git commit -m "docs(evaluation): 记忆可靠性 C 部分 LoCoMo 与 LongMemEval 结果及与 G2 的对比"
```

### Task 9: 运行 A、汇总 B、撰写报告

- [ ] **Step 1: 试跑全部场景，每个 2 次**

Run: `GOAGENT_LIVE=1 CS_SCENARIO_REPEATS=2 TMPDIR=/tmp taskset -c 2,3,6-27 orchestration/customer-service/scripts/with-stores.sh go test -p 1 -parallel 3 -timeout 120m -run TestLiveMemoryScenarios -v ./orchestration/customer-service/ 2>&1 | tee /tmp/a-pilot.log`
Expected: 18 次全部给出结果，不要求通过。如果某个场景因为话术问题根本测不到目标能力（例如客户消息被分到退款成员），只修改话术，不改判定，并在 ledger 记下修改及原因；infra 占比超过 10% 时改用 `-parallel 2`。

- [ ] **Step 2: 全量运行（每个场景 10 次）**，命令同上，`CS_SCENARIO_REPEATS=10`，日志写到 `/tmp/a-full.log`。

- [ ] **Step 3: 人工复核**：所有评审为“错”的答复，以及每个场景随机抽 2 条评审为“对”的答复（用 `shuf --random-source=<(yes)` 固定抽样），对照运行记录（`<场景>-<rr>/runs/*.json` 中的 `model/turn/N`）、`RecallBlock` 与 `Records`，给每条失败归入 `tool`、`extract`、`consolidate`、`recall`、`answer`、`judge` 之一。结果写入 `a-review.md`：每条一行，列出场景、重复次数、评审结论、复核结论、归类与一句依据。

- [ ] **Step 4: 把 A 的 `results.jsonl`、`summary.md` 复制为 `a-results.jsonl`、`a-summary.md`，B 的 `/tmp/b-faults.log` 复制为 `b-faults.log`**，都放进评测目录。运行记录目录不提交，只在 README 写明它们在本机的路径。

- [ ] **Step 5: 写 `docs/evaluation/2026-11-17-memory-reliability/README.md`**，包含：
  - 代码版本：goagent 合并提交、examples 提交、contrib 两个模块的提交；
  - 模型与数据集 sha256；
  - 每部分的运行命令；
  - A 的汇总表，以及复核后的修正通过率和评审误判率；
  - B 每个子测试的结论；
  - C 的结果表与对比结论；
  - 缺陷清单：每条写复现步骤、所属环节与影响；
  - 局限：评审与被测是同一模型，C 的评审换成了 DeepSeek。

- [ ] **Step 6: 提交**（先检查评测目录中没有密钥）

```bash
git add docs/evaluation/2026-11-17-memory-reliability/README.md docs/evaluation/2026-11-17-memory-reliability/a-results.jsonl docs/evaluation/2026-11-17-memory-reliability/a-summary.md docs/evaluation/2026-11-17-memory-reliability/a-review.md docs/evaluation/2026-11-17-memory-reliability/b-faults.log
git commit -m "docs(evaluation): 长期记忆可靠性验证报告（真实场景、故障并发、基准评测）"
```
