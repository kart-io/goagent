# 发布门禁复核

结论：本地组合可以验证，但当前 Harness 不能独立构建或发布。这是已声明但尚未完成的依赖迁移门禁，不能计作已通过。

## 实测证据

在 Harness 根目录执行：

```sh
env -u GOROOT GOWORK=off go test -mod=readonly ./preset -run '^$'
```

退出码为 1，缺少 `github.com/kart-io/goagent/skills` 与 `github.com/kart-io/goagent/tools/skill`。
当前 `go.mod` 的 GoAgent 依赖尚不包含新包。命令及结果见 [release-check.json](release-check.json)。
`make test/race/build` 显式禁用 workspace；Harness CI 也不能使用本机的临时 workspace。

## 发布前需要完成

1. 将 GoAgent 新公共包置于可获取的提交或版本中。
2. 更新 Harness 对 GoAgent 的版本依赖，整理模块依赖并核对 Go 1.27 工具链要求。
3. 用 `GOWORK=off` 重新运行 Harness 的测试、构建和静态检查；正常发布 CI 中的 Docker/Postgres 场景仍需按原项目流程执行。

本轮审查没有发布提交、修改依赖版本或绕过发布门禁。临时 workspace 验证仅证明当前两个 checkout 可以组合使用。
