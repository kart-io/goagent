# 五项执行内核设计改进

基线fda8f34；工作分支feat/execution-contract-hardening。使用独立工作树，原主工作区保持master且干净。本轮无真实模型请求、无Gemini计费操作、无全局配置改动，不推送或合并。

## 交付

| 建议 | 实现 | 核心验收 |
|---|---|---|
| 最终验收不受纠正额度影响 | WithOutputValidator、StreamRunnable.WithFinalValidator | 纯文本、工具直返、PreStep Halt、停止钩子跳过、中间件与内外完成回调改写、取消、panic、持久重放 |
| 标准治理装配 | NewGovernedBuilder、GovernanceReport、WithDispatcher | 显式预算/工具额度/期限/授权/有限准入与排队；正确Scope.Bind；禁止两种顺序混入裸工具；重建重放 |
| 执行模式一致性 | BuildContext能力检查 | 六类原生工具流配置提前拒绝；纯文本重试和最终验收保持支持 |
| 版本升级检查 | StateVersion/Components、Manifest、CheckCompatibility | 固定旧摘要、声明漂移、副本隔离、只读且不解码业务状态；缺失运行不兼容 |
| 规模性能验证 | 三类Go benchmark与离线示例 | 32组、每组3次；原始测量及完整工作量断言；当前工作树跨仓验证 |

完整用法与迁移见[最终验收、治理装配与升级检查](../../guides/EXECUTION_ACCEPTANCE.md)。实现复用已有执行源、Scope、Dispatcher、Diagnose，无新运行时或依赖。校验器只读可重入，可能多次调用；不能回滚已发生的模型、工具、会话、检查点与回调效果。Pipe后续主动变换自行验收。治理报告是静态配置快照，兼容匹配也不代表可以安全恢复未知效果。

## 验证与审查

最终版本60个测试包的全仓race通过，库覆盖率80.0%，所有覆盖率门槛通过；build、vet、lint（0问题）、apicheck（0违规）、独立frameworkbench race均通过。核心实现最终提交332cecc，跨仓编译选择6cf997d；二者仅差期限测试夹具，生产代码一致。

各行为按RED→GREEN实现；历史错误块Done可以为true，因此成功判定始终是Done且Error为空。新增治理示例两次重建累计模型请求2次、工具效果1次。

独立整分支审查读取全部变更与设计，定向四包测试通过，发现Critical0、Important1、Minor0：外层OnError panic会覆盖原验收错误。用通用Runnable与Builder两条回归先复现，再以errors.Join保留原始错误和PanicError，10轮定向race通过。最终全量验证后交付，不另行调度二次审查。

主要重复命令：

```sh
GO_TEST_MODE=true go test -race -count=1 -timeout=180s -coverprofile=.claude/design-contracts-20261107/coverage-final.out -covermode=atomic ./...
bash scripts/check_coverage.sh .claude/design-contracts-20261107/coverage-final.out
make build vet lint test-frameworkbench
go run ./scripts/apicheck
GO_TEST_MODE=true go run ./examples/execution-governed
go run ./scripts/compatcheck -root /path/to/repos -core-dir /path/to/current/goagent -out /new/report
python3 docs/evaluation/2026-11-07-execution-design-hardening/verify.py
```

Go1.27.3，macOS arm64、Apple M4 Pro。lint使用Go1.27.3构建的golangci-lint2.15.0。本轮兼容检查新加-core-dir并用真实子构建验证显式核心优先：主树常量7、工作树42，只有消费实际工作树才通过。未指定核心的首轮旧主树检查中断，原证据保留但不计入当前验证。

## 性能结果

以下为三次采样中位数，固定夹具，无网络模型延迟。历史消息每条包含32次“证据”及序号，变更场景在计时外重建历史。

| 场景 | 中位时间 | 分配字节/次 |
|---|---:|---:|
| 内存读取100条 | 2.47微秒 | 32464 |
| 内存读取10000条 | 0.369毫秒 | 4382416 |
| 文件读取100条 | 0.106毫秒 | 182980 |
| 文件读取10000条 | 8.87毫秒 | 17036811 |
| 内存不变同步10000条 | 14.77毫秒 | 21542353 |
| 文件不变同步10000条 | 15.43毫秒 | 21539899 |
| 首次卸载1MiB结果 | 1.71毫秒 | 3153086 |
| 还原1MiB结果 | 0.456毫秒 | 2106232 |
| 4CPU共享Agent | 6.897微秒/操作 | 4241 |
| 4CPU独立会话 | 2.057微秒/操作 | 4233 |

追加与重写的全部24组历史场景、大结果4组及会话4组见benchmarks.json。历史/大结果使用固定5次迭代、重复3次，样本小，仅用于定位开销；并发结论采用单独300ms、1/4CPU、各3次的复验，不采用初步5次迭代结果。

测量显示长历史同步分配随历史显著增长，多会话应使用独立Agent实例；没有真实生产延迟目标或负载，本轮保留现有日志结构，没有加入未经验证的缓存、索引或分段机制。这是框架离线开销，不代表线上模型耗时或普遍性能提升。

## 关联仓库已有失败

明确选择本次工作树后，15个模块7个通过、8个构建失败：

- 通过：Contrib的acp、durable/pgstore、httpagent；Examples主模块和orchestration/httpagent；Harness主模块和integration/postgres。
- 未通过：Contrib的document、document/pdf、retrieval、retrieval/cohere、retrieval/qdrant、retrieval/vertexai、tools/database、tools/practical。

8项均在进入测试前因旧`github.com/kart-io/goagent/utils/json`导入失败；基线该包已经不存在，旧主树验证也已复现该错误。其后续迁移应改用codec/json并验证调用语义，本轮未修改这些独立仓库，也没有恢复旧兼容别名。服务或真实模型条件不足而跳过的集成测试保留在JSON报告，不能视为已执行。不能宣称全项目生态验证通过。

## 证据

[摘要](summary.json)、[基准数据](benchmarks.json)、[校验清单](manifest.json)、[完整证据](evidence.json.gz)与[离线核对程序](verify.py)组成可离线核对记录；包含108份脱敏文件，包括全部关键RED/GREEN、完整检查、当前工作树跨仓日志、原始性能数据、独立审查与源码快照。路径脱敏，进程凭据扫描，逐文件SHA-256和长度校验。

### 执行中的取舍

- 验收错误链修复限定到配置最终验收的Runnable，保留无验收路径既有的回调异常优先契约；相关旧断言保留，新回归保证验收sentinel与PanicError同时可识别。
- 受治理装配增加Scope.Bind身份核对，避免只有配置报告却没有实际根预算。
- 兼容检查新增显式工作树参数；独立Contrib仓库旧编解码导入迁移未混入本轮核心提交，失败如实保留。
- 性能部分交付基准与测量，不在缺少生产目标时重写存储；若业务要求长期超大历史，应据真实负载再评估投影缓存或日志分段。

最后全量压力检查发现既有客户追问用例要求300ms内必须先进入等待，实际期限可能在首次抽取前就到期。新增“运行前已经到期”确定性RED后修正夹具：提前到期不得完成、不得调用补充确认，运行前过期必须零模型请求；正常进入等待时仍保留原审批与过期恢复断言，没有延长期限或修改预算实现。10轮race通过，原失败日志保留。
