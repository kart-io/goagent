# 宿主接入稳定化实施计划

> 按writing-plans组织，独立任务使用dispatching-parallel-agents，新增行为采用TDD，最终独立代码审阅。

**目标：**完成可取消构建、标准执行装配和本地可重复测量，为真实宿主接入建立可验证基础。
**架构：**BuildContext复用Initialize，execution.Scope只组合公开接口；测量不改变执行引擎或存储格式。
**技术栈：**Go1.25、现有预算/FileStore/Dispatcher/Workflow，无新依赖。
**设计：**docs/superpowers/specs/2026-10-31-host-stabilization-design.md。

## 全局约束

不推送或部署；scope不拥有宿主资源生命周期；不会强杀忽略ctx的第三方；测量结果不能代表生产效果；无实际宿主不得宣称生产试运行完成。

## 审阅重点

构建取消后仍返回Agent；跨请求context误继承；Scope与预算身份/持久性不匹配；恢复key被根身份覆盖；文件与内存测量条件不同或计数不完整。

## 任务

- [x] T1 BuildContext：builder/builder.go、configurable_agent.go及新测试。先测试已取消、nil、检查点阻塞可取消、回调ctx传播、回调期间取消不成功；确认失败，再委派实现。Build兼容测试、builder race。
- [x] T2 execution.Scope：新execution/scope.go、scope_test.go、integration_test.go；scripts/apicheck及测试。先以无API/冲突测试失败，再实现。公开接入审批/恢复与根预算，反向导入规则先失败再实现，execution包race。
- [x] T3 离线测量：evaluation/executionbench/{probe.go,probe_test.go,cmd/run/main.go}，core/budget/ledger_benchmark_test.go。先测试配置非法、取消、三mode操作计数/预算/清理/JSON，再实现；CLI10/100/1000完整矩阵，微基准固定规模，记录环境与测量限制。
- [x] T4 文档/验收：新HOST_INTEGRATION指南、README/CHANGELOG、版本能力状态；保存实际测量报告。全仓race、导入检查、Harness跨仓，独立最终审阅后本地提交。

## 依赖与实施

T1/T2彼此独立；T3只消费既有公共能力并可独立实现，最终示例使用Scope。主代理负责边界、文档与完整测量，工作者按文件所有权实施，不回滚其他编辑，不并发git提交。
实际宿主试运行等待用户提供服务仓库和任务；现有离线/集成工具先完成，不虚构业务数据。

计划独立审阅完成，采纳设计末节五项最小约束；仓内实施无剩余阻塞。真实宿主试运行仍等待服务与任务。

## 证据驱动的最小优化

首次9格矩阵：1000操作内存预算累计分配701,362,848字节，文件模式约31.8秒；固定1000条RecordUsage为515,962ns/op、268,576B/op、2034allocs/op。由此追加T3a：以类型化深复制替代内存ledger的JSON往返；持久读取前不再复制随后丢弃的内存镜像。没有修改持久格式、锁或同步保证。新增128条固定账本分配预算测试在旧实现观察218次分配超过192上限，优化后通过，并保留所有可变证据深复制测试。按同一矩阵与基准重测，而非直接重写增量账本。

## 实施记录

- T1/T2/T3 已完成，新增行为先观察失败再实现；builder、execution、observe、budget、探针及导入规则定向测试通过，相关包 race 通过。离线 Scope 示例成功运行。
- T3a 已完成同条件前后矩阵与固定账本微基准，原始数据及测量范围见 [实测报告](../../evaluation/2026-10-31-execution-overhead/REPORT.md)。不改变文件存储格式、锁或同步保存。
- 独立实现审阅发现探针继承宿主 Journal 的 P2，补充九种组合的拒绝与无写入回归后修复；实现及文档复审无剩余 P0/P1/P2。
- 全仓 race、导入检查、Harness 发布基线与跨仓测试/build/vet 验收通过；实际服务仓库与任务尚未提供，真实宿主试运行待办，不计入仓内实施完成声明。


### 本地验收命令

```bash
env -u GOROOT GO_TEST_MODE=true go test -race -timeout 120s ./...
env -u GOROOT bash scripts/verify_imports.sh
env -u GOROOT go run ./examples/execution-scope
# 在相邻 goagent-harness 仓库运行，参数指向本次 GoAgent 工作树
env -u GOROOT GO_TEST_MODE=true ./scripts/verify-goagent.sh /absolute/path/to/goagent
git diff --check
```

上述命令均退出0；GOROOT处理仅用于本机环境不匹配，不修改用户全局配置。完整测量命令及原始结果见实测报告。真实宿主试运行继续待办，取得服务仓库与任务后再实施。
