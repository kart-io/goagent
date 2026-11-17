# 长期记忆可靠性验证（2026-11-17）

验证 goagent 长期记忆在三方面的可靠性：真实客服场景（A）、故障与并发（B）、公开基准与 G2 的对比（C）。设计见 `docs/superpowers/specs/2026-11-17-memory-reliability-design.md`，计划见 `docs/superpowers/plans/2026-11-17-memory-reliability.md`。

## 版本与环境

| 项 | 版本 |
|---|---|
| goagent（被测） | `51ce038d`（test/memory-reliability，合入 DSML 还原；之后的提交只改 docs） |
| goagent-examples | `2357dcc`（feat/cs-memory；A 场景代码定于 `0766af1`，B 在 `2357dcc`） |
| goagent-contrib | `349db79`（feat/memory-qdrant；durable/pgstore `ec87829`，memory/qdrant `349db79`），经 go.work 使用本地代码 |
| 对话与评审模型 | `deepseek/deepseek-flash`，温度 0 |
| 嵌入 | A：gemini（`newEmbedder` Live 模式）；B：离线哈希嵌入 256 维；C：`gemini:gemini-embedding-001` |
| 存储 | `scripts/with-stores.sh` 拉起的本地 docker Postgres 与 Qdrant，运行后删除 |
| LoCoMo | `locomo10.json`，sha256 `79fa87e90f04081343b8c8debecb80a9a6842b76a7aa537dc9fdf651ea698ff4`（与 G2 相同） |
| LongMemEval | HF `xiaowu0162/longmemeval-cleaned` 的 `longmemeval_oracle.json`，sha256 `821a2034d219ab45846873dd14c14f12cfe7776e73527a483f9dac095d38620c`（清洗版，数字可能与论文原版略有出入） |

测试均以 `TMPDIR=/tmp taskset -c 2,3,6-27` 运行（本机核 0、8 计算出错）。

## A 真实场景

命令（goagent-examples 根目录）：

```bash
GOAGENT_LIVE=1 CS_SCENARIO_REPEATS=10 TMPDIR=/tmp taskset -c 2,3,6-27 \
  orchestration/customer-service/scripts/with-stores.sh \
  go test -p 1 -parallel 3 -timeout 120m -run TestLiveMemoryScenarios -v ./orchestration/customer-service/
```

9 个场景各 10 次，90 次全部给出结果，infra 0。运行目录在本机 `/tmp/cs-scenarios/20261117-190225`（不提交），逐次结果见 `a-results.jsonl`，自动汇总见 `a-summary.md`，人工复核见 `a-review.md`。

| 场景 | 考察 | 自动两层通过 | 复核后通过 |
|---|---|---:|---:|
| abstain | 没有记录时不编造 | 10 | 10 |
| isolate | 客户之间不串记忆 | 10 | 10 |
| pinchain | 钉住记录被取代后链条完整 | 10 | 10 |
| multi | 多条事实同时召回 | 9 | 9 |
| recall | 跨工单召回 | 7 | 8 |
| premise | 纠正与记忆冲突的错误前提 | 7 | 7 |
| forget | 删除后不再给出 | 6 | 4 |
| temporal | 按时间先后作答 | 2 | 2 |
| update | 新事实取代旧事实 | 0 | 9 |
| 合计 | | 61/90（67.8%） | 69/90（76.7%） |

复核范围是全部 29 个失败项、每场景 2 个固定抽样的通过项，以及与 forget-01/02/03/10 答复同构却被自动判对的 forget-06/08，共 45 项。forget-06/08 都只说「无法查询，身份验证后自行查看」，从未说明号码已删除或不存在，复核改判为错。自动评审（状态检查或 LLM 评审任一层）与复核不一致 13 项，误判率 28.9%，主要来自 update 的状态检查：它用子串「朝阳」判断旧地址是否残留，而新事实本身会写「原地址北京朝阳已不再使用」，10 次中误判 9 次；复核确认没有一次是旧地址记录仍然有效。

复核后失败归类计数（按 `a-review.md` 的「归类」列；未复核项沿用自动判定，均为通过）：

| 环节 | 失败数 | 项 |
|---|---:|---|
| tool | 1 | premise-07 |
| extract | 4 | temporal-03/06/09/10（不能排除 consolidate noop） |
| consolidate | 0 | — |
| recall | 4 | temporal-01/02/04/05 |
| answer | 12 | forget-01/02/03/06/08/10、premise-04/09、recall-03/10、multi-04、update-04 |
| 合计（系统失败） | 21 | = 90 − 69 |
| judge（另一维度） | 13 | 自动评审误判：update-01~10、recall-02、forget-06/08；不计入上面的合计 |

isolate 只检查客户 Q 的命名空间没有 778899，本次运行时并未确认客户 P 真的存入了会员号，P 写入失败时也会空过。本次运行的证据是 P 确实存了：10 次 isolate 的运行记录全部含 `memory_save` 调用（`grep -l memory_save /tmp/cs-scenarios/20261117-190225/isolate-*/runs/*.json` 命中 11 个文件，覆盖 isolate-01~10）。正向检查（P 的有效或钉住记录含 778899，否则 state_ok 为假）是本次运行之后在 goagent-examples `a72abed` 补上的。

## B 故障与并发

命令：

```bash
TMPDIR=/tmp taskset -c 2,3,6-27 orchestration/customer-service/scripts/with-stores.sh \
  go test -p 1 -race -count 2 -run 'TestMemoryFaults' -v ./orchestration/customer-service/
```

离线脚本模型 + 真实 Postgres/Qdrant，两轮全部通过，日志见 `b-faults.log`。

| 子测试 | 故障 | 结论 |
|---|---|---|
| concurrent | 同一客户两条消息并发、两次 Sweep 并发 | 通过：每条事实恰好一条记录。同一客户同时只有一张未结束工单，第二条消息排队为同一工单的下一轮 |
| replay | 重复 Sweep 3 次、同一消息 ID 重放 | 通过：记录不重复 |
| crash | 子进程在整理（reconcile）中被 SIGKILL，重启后继续 | 通过：约 30 秒（worker 租约时长）后任务完成，旧记录被正确取代；被 kill 的那次不计入 Attempts |
| pg-outage | `docker pause` Postgres，10 秒期限内 Sweep | 通过：约 3.6 秒返回超时错误，恢复后状态一致 |
| ns-concurrent | 5 个客户并发 | 通过：命名空间互不串扰 |
| index-loss | 删除 Qdrant 集合后 `Reindex` | 通过：集合重建，点数 = 有效记录数，召回 ID 集合与故障前一致。集合缺失期间召回不报错（字符重叠打分兜底） |

## C 基准评测

命令（goagent 根目录）：

```bash
TMPDIR=/tmp taskset -c 2,3,6-27 go run ./scripts/memeval -format locomo -data /tmp/memeval-data/locomo10.json -sample 200 -repeats 2 \
  -concurrency 10 -systems memory,graph -chat deepseek -judge deepseek -embed gemini:gemini-embedding-001 \
  -out locomo-200.md -results locomo-200.jsonl
TMPDIR=/tmp taskset -c 2,3,6-27 go run ./scripts/memeval -format longmemeval -data /tmp/memeval-data/longmemeval_oracle.json -sample 100 -repeats 1 \
  （其余参数同上） -out lme-100.md -results lme-100.jsonl
mkdir -p /tmp/rejudge && cp rejudge.go.txt /tmp/rejudge/main.go
TMPDIR=/tmp taskset -c 2,3,6-27 go run /tmp/rejudge/main.go -baseline docs/evaluation/2026-11-13-memory-g2/locomo-200.jsonl,docs/evaluation/2026-11-13-memory-g2/locomo-200-r2.jsonl \
  -current locomo-200.jsonl -judged g2-rejudged-deepseek.jsonl -out compare.md
```

| 数据集 | memory | graph | memory 对 graph（胜/负，p） |
|---|---:|---:|---|
| LoCoMo 200 题 × 2 轮 | 48.8% | 57.2% | 33/67，p=0.001 |
| LongMemEval oracle 100 题 | 68.0% | 71.0% | 6/9，p=0.607 |

与 G2（2026-11-13）逐题配对。G2 原用 gpt-4o 评审，这里先用 DeepSeek 重判 G2 的答复（800 条，0 条失败，与 gpt-4o 一致率 98.2%），再做精确双侧符号检验：

| 方案 | 轮 | 本次 | G2（DeepSeek 重判） | 胜/负 | p |
|---|---|---:|---:|---|---:|
| memory | 1 | 49.0% | 50.0% | 19/21 | 0.875 |
| memory | 2 | 48.5% | 51.0% | 16/21 | 0.511 |
| graph | 1 | 55.0% | 50.5% | 25/16 | 0.211 |
| graph | 2 | 59.5% | 51.5% | 24/8 | 0.007 |

结论：memory 相对 G2 没有回退；graph 两轮都更高，但只有第 2 轮显著，只能算可能提升。详见 `locomo-200.md`、`lme-100.md`、`compare.md`。

## 缺陷清单

均来自 A 的人工复核，B 未发现缺陷。复现命令为 A 的命令加 `CS_SCENARIO_ONLY=<场景>`。

| # | 环节 | 缺陷 | 复现场景 | 影响 |
|---|---|---|---|---|
| 1 | answer | 分流成员没有长期记忆，结束时复述成员答复，把成员引用记忆得出的结论（蓝色、X200、没有相关记录）删掉 | premise、recall、forget | 6/90（premise-04/09、recall-03/10、forget-01/06），与 #5 并列最大单一来源：记忆召回正确，客户却看不到 |
| 2 | recall | 召回块与 `memory_search` 结果只带日期（`memory/recall.go` `time.DateOnly`），按得分排序，同日多条记录无法排先后 | temporal | 4/10 如实答「无法判断」；所有「最早/最近」类问题都受影响 |
| 3 | extract（不排除 consolidate） | 同一客户第 2 张工单的「充电器发烫」没有留下有效记录 | temporal | 4/10；抽取输出未留存，需开启抽取与整理决定日志才能区分是抽取为空还是整理误判 noop |
| 4 | tool | 成员按客户的错误前提调用 `memory_replace`，把正确的「蓝色」改成「红色…以用户最新说法为准」 | premise | 1/10，但会持久污染记忆；召回块的「与用户当前所说冲突时以用户为准」会诱导这一行为 |
| 5 | answer | 记录已召回或已确认为空，成员答复仍不用或含糊其辞（multi-04、update-04、forget-02/03/08/10） | multi、update、forget | 6/90 |

评测自身的问题（不是系统缺陷，修订评测时处理）：update 的子串状态检查误判 9/10；LLM 评审对同构答复判定不一致（update 1–3 与 5–10，forget 06/08 与 01–03/10）。

## 局限

- A 的被测模型与评审模型都是 deepseek-flash，评审可能对同源答复偏宽；人工复核只覆盖 45/90 项，未复核项沿用自动判定。
- C 的评审由 G2 的 gpt-4o 换成 DeepSeek；已用重判消除口径差，两者一致率 98.2%。
- A 每场景 10 次，95% 区间宽（例如 7/10 为 39.7%–89.2%），只能识别明显的问题，不能比较小幅差异。
- B 用离线脚本模型与哈希嵌入，验证的是存储、任务与索引层的不变量，不覆盖真实模型的抽取质量。
- LongMemEval 用的是清洗版 oracle，只跑 1 轮。
- A 交给评审的答复取发给客户 Q 的最后一条消息，而不是专门取提问工单的答复；若提问工单没有答复，会误取之前工单的答复。本次 90 次提问工单全部 delivered，不受影响。
- A 中 `driveTicket` 的错误（追问轮数过多、工单没有到达终态）记为 infra，而不是系统失败。本次 infra 为 0，没有影响。
- B 的取代检查只明确覆盖原钉住记录（及有效记录、取代者本身）；其他已失效记录上悬空的 `SupersededBy` 不在检查范围内。
