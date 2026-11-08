# 真实模型验收可靠性修复

用户要求修复此前验收中存在的问题。本轮修复的是contrib真实模型验收工具的四类可靠性缺陷；核心管理生产API没有改动，没有增加兼容代码。旧报告中已修复的状态码、逐字匹配、Token上限和幂等断言，没有重复算作本轮修复。

核心基线`aaec092`，contrib基线`ca7138f`。最终源码校验值和交付提交见[summary.json](summary.json)。

## 修复项与证据

| 问题 | 根因与修复 | 验证 |
| --- | --- | --- |
| HTTP失败迟报 | ready/wait的Eventually条件协程仍调用Fatal；改为主测试协程同步轮询，503当场失败 | session、task两个子进程用例先失败后通过，不再等到10/90秒轮询截止 |
| 超时、断流与慢消费者丢终态 | ctx结束或EOF只关通道，缓冲满还可能阻塞或丢原上游错误；补明确失败终态，必要时舍弃未消费正文以保留终态；有原帧时原样保留 | 截止、取消、EOF、非Done错误、内部截止时停止消费、原成功/错误帧身份均验证；未知usage保持nil |
| 异常退出丢请求证据 | 最终案例JSON到cleanup才写；请求前先写started并Sync，终态/用量/模型元数据变化继续增量写入并Sync | 子进程在上游入口直接os.Exit(42)，不执行cleanup，仍能读取started；无法写入时模型调用数0；错误日志不保存原始敏感文本 |
| 部分配置和报告碰撞发现太晚 | 原来逐提供方检查Key、场景结束才检查JSON覆盖；现在先检查全部选中配置，再独占新目录并预留请求日志 | 缺另一提供方配置、旧版只含场景JSON目录均在数据库/模型之前失败，旧报告内容不变 |

请求日志是每提供方独立的JSONL，index稳定。按index取最后一行可重建当前记录。started只代表本地接纳且准备调用，不能证明远端收到请求。没有实际usage时保持未知，不填0；历史上一份未知请求预留仍按旧报告保持未知，无法追溯补算。

## 先复现，再修复

有效首次RED：[red-correct-fixture.jsonl](red-correct-fixture.jsonl)，2个控制通过、7个失败事件（含父测试），对应HTTP迟报、超时/EOF无终态和非统一前检。最早一次[red.jsonl](red.jsonl)中的429故障fixture缺少Err导致空指针，先修正该fixture，再取得上述有效RED；它不作为生产缺陷。

独立审阅额外发现的旧目录碰撞和内部截止退出见[review-red.jsonl](review-red.jsonl)。原上游终态丢失见[terminal-red.jsonl](terminal-red.jsonl)。每项均保留修复前失败；没有删除失败记录或用其他模型替换失败结果。

最终[故障集三轮race](final-fault-three-rounds.jsonl)：66个通过事件，0失败0跳过。事件含父测试和辅助子进程入口，不代表66个不同业务案例。故障源和429为本地确定性注入，不调用真实模型，不能算真实提供方事故验证。

## 真实模型复验

| 轮次 | DeepSeek | Gemini | 实际请求/已知Token |
| --- | --- | --- | --- |
| 初轮修复后完整业务矩阵 | 5组通过，11请求、5,508 Token | 5组通过，11请求、3,208 Token | 22 / 8,716 |
| 审阅收紧目录和内部截止后，批准续跑定向复验 | 通过，2请求、1,028 Token | 通过，2请求、594 Token | 4 / 1,622 |
| 本轮真实调用总计 | 全部已完成且基础usage完整 | 无额外失败调用 | 26 / 10,338 |

模型分别为deepseek-flash、gemini-2.5-flash。五组业务仍为批准与同进程重建续跑、拒绝、审批等待取消、多轮与精确Fork、新旧Session配置版本绑定；逐Task和跨Session用量一致，批准SQL效果1，其余0。没有真实支付操作。

[完整矩阵日志](live.jsonl)、[定向复验日志](live-reviewed.jsonl)。live-final与live-reviewed目录分别保留每个案例报告及提供方请求JSONL。对四份请求日志独立取最新投影，与全部场景Calls、逐Task实际Token核对，26个started与26个completed对应，无遗漏/重复。详见summary.json。

执行顺序如上：完整矩阵之后仅收紧失败/报告前检路径，并定向复跑批准案例；最后针对已收到的原终态保留分支只运行确定性故障回归，没有为每次工具修订重复付费，也没有声称每次修订后重新执行全10组。

## 完整回归与审阅

- 最终管理模块race：98个通过事件，0失败，1项显式付费入口跳过；数据库用例没有跳过。
- GOWORK=off独立模块完整管理race：同样98通过、0失败、1付费入口跳过。
- go vet通过，golangci-lint 0 issues；没有修改go.mod/go.sum。
- 独立审阅最初1 Important、1 Minor及后续1残余Minor均修复，最后复核无剩余问题，[审阅记录](review.md)。

原始完整回归见[management-final.jsonl](management-final.jsonl)、[standalone-final.jsonl](standalone-final.jsonl)、[静态检查](verified-vet-lint.log)。

复跑说明在contrib management/docs/live-model.md；GOAGENT_MANAGEMENT_LIVE_REPORT_DIR现在必须设置且目录必须不存在。新增live_observer_test.go负责观察和证据，live_reliability_test.go负责无需付费的故障回归。核心与contrib工作区均需在提交前扫描证据，确保没有各自配置凭据原值、认证头和思考内容。

本轮不覆盖真实提供方限流/超时事故、OS进程崩溃后的完整业务恢复或文件系统断电，也没有把未知历史费用补算为确定值。

作者评分：代码质量94、测试覆盖96、规范95；需求匹配95、架构一致95、风险控制93；综合95，通过。该评分与独立审阅意见分开记录。
