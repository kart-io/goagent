# 统一管理层迁入核心实施计划

> 执行方式：使用 superpowers:executing-plans 按任务连续完成；用户已确认分层方案，无须重复选择执行方式。

**Goal:** 通用管理能力通过 github.com/kart-io/goagent/management 使用，数据库和 Harness 保持扩展实现。
**Architecture:** 原样迁移实体、服务、Worker 和适配，沿用 execution.Scope。按现有核心约束统一 JSON 与错误入口，contrib 通过接口提供 SQL 存储和验收宿主。
**Tech Stack:** Go 1.27.3、当前 codec/json、OTel、Postgres 17.6、pgx/v5、标准库 HTTP。
**Spec:** ../specs/2026-11-08-management-core-migration.md

## 全局约束

- 不保留旧根包、转发或版本兼容分支；不新增核心数据库或 Harness 依赖。
- 保留原始持久记录和 HTTP 业务契约，集中错误入口为 errors.ErrManagement*。
- 仅本地验证；原工作区改动保持不变；跨仓未发布接口显式使用临时 workspace，不伪造已发布版本。

## 审阅重点

- 大整数、嵌套重复键与未知字段在 JSON 入口切换后继续正确。
- 错误身份切换后 HTTP 400/401/403/404/409/422/429/503 及重试分类不变。
- 核心消费者无需 pgx/contrib/Harness，底层不得反向导入 management。
- 旧根包确实删除，保留的 pgstore/集成测试没有漏改 import 或 OpenAPI 路径。
- 真实进程恢复、关闭、审批退款一次和 Fork 在迁移后继续成立。

### Task 1: 核心包与分层迁移

**Files:** 新增 management/*.go、management/adapters/*、management/httpapi/*、errors/management.go；修改 scripts/apicheck/main.go 和 main_test.go。
**Interfaces:** 消费原 Store/Runtime/Factory/Config 合约；产出 management.New、NewWorker、adapters.NativeFactory/GraphFactory、httpapi.New，公共领域错误改为 errors.ErrManagement*。

- [x] 运行未修改核心的全套 race 基线；保留日志。
- [x] 先移入现有管理测试并补实际外部消费者新路径编译检查；观察新包尚不存在时失败。为分层规则添加禁止下层导入 management、禁止核心导入 contrib/pgx 的失败用例。
- [x] 移入通用生产文件和 OpenAPI；统一 codec/json、精确数值/重复键/未知字段解码；集中错误定义并修改直接引用；删除 contrib 旧通用实现。
- [x] 运行核心管理用例、分层检查、独立消费者构建；期望通过且依赖图不含 pgx/contrib/Harness。
- [x] 运行核心全套 race 与覆盖率、vet、lint、独立 frameworkbench、现有 make build/run；期望按当前门禁通过，记录任何现有跳过或基线问题。
- [x] 提交核心包迁移和核心迁移指南。

### Task 2: 扩展与案例接入新核心

**Files:** 修改 contrib management/pgstore、integration、examples/host 的 imports、management/README.md、当前操作/验收文档和必要的本地验证入口；保留 management/harness 独立模块。
**Interfaces:** 消费 Task 1 的新 management 和 errors.ErrManagement*；pgstore 继续实现 management.Store/HistoryReader，案例通过新 httpapi.New 服务。

- [x] 将数据库集成测试切换到新核心入口，在无对应核心包时观察失败；更新实际 SQL 实现和案例，禁止旧根包别名。
- [x] 使用 GOAGENT_CORE_ROOT 临时 workspace 跑 contrib 13 模块 vet/race，专用 Postgres 执行重启/同步提交测试；无新失败，管理模块不得跳过。
- [x] 编译新入口的案例宿主，实际执行内置退款案例及8组 HTTP 黑盒案例，检查审批退款一次、拒绝/取消、版本/Fork和只读汇总。
- [x] 跑 contrib lint，核对全部原测试归属/数量、依赖边界和文档链接；提交扩展改动。
- [x] 进行一次独立完整差异审阅；重要问题先失败后修复并回归；输出迁移报告、复跑命令、实际结果及真实发布前提。
