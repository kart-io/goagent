# 统一管理层迁入核心

通用管理代码从 contrib 迁入 GoAgent 主模块，按用户确认直接切换，不保留旧包转发或别名。业务数据表、任务状态、配置版本绑定与 HTTP 路由不因源码归属迁移而改变。

## 包与测试归属

| 原入口 | 当前入口 |
| --- | --- |
| `github.com/kart-io/goagent-contrib/management` | `github.com/kart-io/goagent/management` |
| `.../goagent-contrib/management/adapters` | `.../goagent/management/adapters` |
| `.../goagent-contrib/management/httpapi` | `.../goagent/management/httpapi` |
| `.../goagent-contrib/management/pgstore` | 保留，提供数据库实现 |
| `.../goagent-contrib/management/harness` | 保留为可选独立模块 |

原通用包单测与 OpenAPI 跟随实现迁入核心。数据库集成测试和退款宿主保留在 contrib，消费新管理入口。核心主模块没有新增 pgx、contrib 或 Harness 依赖；导入分层检查禁止执行基础层反向导入 management。

## 错误与 JSON 入口

旧 `management.ErrX` 直接改用 `errors.ErrManagementX`，其中 X 为 NotFound、Conflict、Invalid、Unauthenticated、Forbidden、Unsupported、Unavailable、ApprovalRequired、Busy、Owner。不保留旧错误入口。使用标准库 errors.Is 判定固定哨兵身份，结构化分类由核心 errors 包统一提供。

管理实体的 RawMessage 使用 `codec/json.RawMessage`。编解码使用 `codec/json`，不再直接依赖 encoding/json；幂等比较保留精确数值和重复键拒绝。HTTP 请求采用核心 Strict 解码规则：字段名必须与协议匹配，未知字段和非法 UTF-8 被拒绝。响应的业务 input/result/config 字段仍完整保留。

## 未发布代码的跨仓验证

GoAgent v0.50.0 不包含本次新增管理包。正式对外使用须先发布包含迁移代码的真实 GoAgent 版本，再更新 contrib 的依赖锁定；仅修改导入路径不代表旧发布版本已支持新包。

本地联合开发使用 contrib 已有临时 workspace 入口，不修改用户 go.work，不在核心 go.mod 写入本机 replace：

```bash
# 在 contrib 根目录；GOAGENT_CORE_ROOT 指向包含新 management 包的 GoAgent 工作树。
export GOAGENT_CORE_ROOT=/path/to/goagent
export GOAGENT_PGSTORE_DSN='postgres://postgres:management-local@127.0.0.1:55441/postgres?sslmode=disable'
bash scripts/with-local-modules.sh go -C management test -race -count=1 ./...
bash scripts/with-local-modules.sh go -C management run ./examples/host -migrate -verify -listen 127.0.0.1:0
```

数据库必须是专用验证实例；显式迁移表和故障注入不要指向业务数据库。案例使用离线模型并将退款记录写入数据库，不发起真实支付请求。
