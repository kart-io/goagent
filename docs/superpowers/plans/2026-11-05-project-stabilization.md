# 项目稳定化执行计划

> 执行方式：使用 superpowers:executing-plans 在当前会话逐项执行，新增行为遵循 superpowers:test-driven-development。用户已授权按上一轮建议的顺序执行。

**目标：** 使开发、独立评测、跨仓兼容和业务恢复验证具备可重复入口，并收紧覆盖率要求。

**架构：** 复用 Makefile 与 scripts 下的 Go 验证工具。临时依赖覆盖独立于关联仓库，公共契约沿用现有执行语义。

**技术栈：** Go 1.26.10、golangci-lint v2.15.0、Go test/race、现有 SQLite 退款案例。

**设计：** ../specs/2026-11-05-project-stabilization-design.md

## 全局约束

- 最低 Go 1.26.0，推荐 Go 1.26.10；保留用户原有未跟踪文件。
- 不改变公共签名、持久格式，不自动调用付费模型或真实支付。
- 关联仓库使用临时依赖覆盖，不修改其源码或依赖文件。
- 测试输出写入忽略目录或临时目录，最终报告区分通过、失败、跳过与未验证。

## 审查重点

- 继承陈旧 GOROOT 时 Makefile 是否仍能使用声明的工具链。
- 独立模块是否真正通过只读依赖测试。
- 跨仓覆盖是否包含嵌套模块，是否可能误用发布版依赖。
- 跨仓测试失败或取消是否保留证据，是否清理临时文件且不改下游仓库。
- 覆盖率临界值、缺失 multiagent 数据是否严格阻止放行。

## Task 1: 修复复现入口

**文件：** go.mod、evaluation/frameworkbench/go.mod/go.sum、Makefile、.github/workflows/ci.yml、docs/architecture/ARCHITECTURE.md、docs/development/DEVELOPMENT_GUIDE.md。

**输入/输出：** 从现有版本声明产生一致工具链与 make test-frameworkbench 入口。

- [x] 重现 frameworkbench 在 -mod=readonly 下失败。
- [x] 声明推荐工具链，更新独立模块依赖，统一 Makefile/CI 和文档。
- [x] 在陈旧 GOROOT 下运行 make build；运行 make test-frameworkbench、vet 和 v2 lint。
- [x] 独立提交本项。

## Task 2: 公共契约与跨仓验证

**文件：** scripts/compatcheck/main.go/main_test.go、Makefile、scripts/README.md、docs/guides/EXTENSION_CONTRACT.md、docs/development/DEVELOPMENT_GUIDE.md。

**接口：** go run ./scripts/compatcheck -root PATH -out NEW_DIRECTORY；发现 goagent-harness、goagent-examples、goagent-contrib 内全部模块；使用临时 modfile 和全部本地 replace，GOWORK=off；逐模块 go test -json -race -count=1，保存输出、依赖定位和状态摘要。

- [x] 用临时小模块测试缺失仓库、嵌套模块发现、路径含空格、本地 replace、失败传播和下游文件不变，先观察失败。
- [x] 实现命令与 Makefile 入口，补充公共行为和变更验证约定。
- [x] 执行命令覆盖真实本地关联仓库，阅读结构化报告和跳过项。
- [x] 独立提交本项。

## Task 3: 业务恢复闭环验收

**文件：** docs/development/BUSINESS_ACCEPTANCE.md、docs/evaluation/2026-11-05-project-stabilization/REPORT.md；复用 goagent-examples/workflow/refund-recovery 的现有测试。

**输入/输出：** 使用 Task 2 的当前源码依赖覆盖，输出可追溯的退款恢复验收证据。

- [ ] 核对并执行授权、重复请求、账本提交后崩溃、恢复、订单变更、过期、并发额度与旧版本拒绝测试。
- [ ] 整理真实业务适配所需认证、幂等键、对账和升级验收要求。
- [ ] 若用户提供实际测试环境，在授权范围内补验；否则明确记录仍未接入真实业务。
- [ ] 独立提交本地验收与边界说明。

## Task 4: 收紧覆盖率门禁

**文件：** scripts/coveragecheck/main.go/main_test.go、scripts/README.md、docs/development/TESTING_BEST_PRACTICES.md、.github/workflows/pr.yml。

**输入/输出：** 整体 ≥75%，新增 multiagent ≥85%，保留既有核心与 Provider 阈值。

- [ ] 用真实 profile 解析路径增加临界值和缺失 multiagent 回归，观察旧规则错误放行。
- [ ] 更新门禁及相关文档/PR 提示。
- [ ] 执行全量 race/覆盖率、门禁、build、vet、导入检查和 lint。
- [ ] 独立提交，完成整分支审查并处理实质问题。
