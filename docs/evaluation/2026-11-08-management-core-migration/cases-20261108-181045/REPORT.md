# 当前主线案例重跑

执行时间：2026-11-08T18:10:45.248594+08:00。核心受检提交 `1940ddd`，扩展受检提交 `76b96d3`。本轮重新构建实际案例宿主并执行，没有沿用之前运行结果，也没有修改生产代码。

## 结果

| 验证 | 本轮实际结果 |
| --- | --- |
| 内置退款案例 | 通过：退款1次、10 Tokens、幂等复用Task；旧Session/Fork为v1、新Session为v2 |
| HTTP黑盒案例 | 8组全部通过，133次请求，宿主退出码0 |
| 恢复、并发与资源案例 | 29个通过测试事件，0失败、0测试跳过 |
| 独立数据库核对 | 通过：两租户合计7Task、5笔退款、250金额、5个不同执行键；无活动worker owner |

测试事件含父测试及子用例，不等于29个互不重叠的业务场景。

## HTTP业务验证

1. 未认证请求返回401。
2. 同键同内容复用Task，同键不同内容返回409，普通resume不能绕过审批。
3. 批准及重复批准只退款一次，迟到反向审批被拒绝，处理人与理由保留。
4. 拒绝审批不退款。
5. 等待审批时取消任务，迟到批准被拒绝且不退款。
6. 停用Agent阻塞续跑，重新启用后沿用已保存决定。
7. 旧Session和Fork绑定v1，新Session使用发布的v2；分支写入不改变源历史。
8. 重复观测不改变日志：事件数30→30且内容摘要不变；跨页汇总为6Task、4笔退款、200金额、55Tokens。

## 恢复与并发

实际运行了提交断点恢复矩阵（含效果已经写入后的崩溃与取消）、审批等待时终止并重建进程、Fork复制后的进程退出、Outbox接收后确认丢失重投、图批准/拒绝、取消等待真实退出、同Session串行、并发审批唯一胜出、权限撤销与停用后恢复、校验关闭期限、启动恢复中关闭及真实错误保留、已知/未知用量归属。

实际命令分两批运行，分别25和4个通过事件，避免将没有匹配到的测试算作已执行；完整名称见 [summary.json](summary.json)，原始事件见 [process.jsonl](process.jsonl) 与 [outbox-usage.jsonl](outbox-usage.jsonl)。

## SQL独立核对与执行边界

内置 `-verify` 案例使用随机 `demo-UUID` 租户，HTTP脚本使用固定 `demo` 租户。最初辅助统计错误地把筛选demo后的期望设为两组总数，查明租户隔离后分别核对，再校验全表汇总，结果一致。没有把这个统计口径错误当作业务缺陷，也没有通过修改业务代码让断言通过。

HTTP租户：6Task（5成功、1取消）、4笔退款、金额200。内置案例另有1个成功Task、1笔退款。合计6成功、1取消、5笔退款、金额250；5个执行键均唯一。宿主退出后管理表没有活动worker owner。详见 [database.json](database.json)、[内置结果](builtin.json)、[HTTP原始结果](http.json)。

使用新建的专用Postgres17.6和真实TCP HTTP、Agent执行、审批与持久日志。模型为离线脚本模型，退款是专用数据库中的业务记录；不请求外部模型或真实支付系统。全部测试结束后删除本轮数据库容器及宿主二进制，不操作用户其他容器。

## 复跑

在contrib根目录准备专用Postgres后运行；GOAGENT_CORE_ROOT指向当前核心主线。GOAGENT_CASE_DIR是本轮独立输出目录，GOAGENT_CASE_CONTAINER是对应专用数据库容器名。

```bash
export GOAGENT_CORE_ROOT=/path/to/goagent
export GOAGENT_PGSTORE_DSN='postgres://postgres:management-local@127.0.0.1:55442/postgres?sslmode=disable'
export GOAGENT_CASE_DIR="$PWD/.claude/management-case"
export GOAGENT_CASE_CONTAINER=goagent-management-demo
mkdir -p "$GOAGENT_CASE_DIR"
bash scripts/with-local-modules.sh go -C management build -o "$GOAGENT_CASE_DIR/host" ./examples/host
"$GOAGENT_CASE_DIR/host" -migrate -verify -listen 127.0.0.1:0
python3 management/examples/host/test_cases.py --host-binary "$GOAGENT_CASE_DIR/host" --postgres-container "$GOAGENT_CASE_CONTAINER" --output "$GOAGENT_CASE_DIR/http.json"
bash scripts/with-local-modules.sh go -C management test -race -count=1 -timeout 240s -run '^(TestRestartAtEveryCommitBoundary|TestApprovalSurvivesProcessKillAndRestart|TestForkCrashAfterLogCopy|TestOutboxCrashAfterRemoteAcceptanceKeepsStableDelivery)$' ./integration
```
