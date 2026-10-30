# Harness 独立项目设计

用户已批准按前一轮建议执行。项目名与落盘路径由实施者确定。

## 目标与边界

建立同级独立 Git 仓库 `goagent-harness`，模块 `github.com/kart-io/goagent-harness`。
迁出 `harness/preset` → `preset`、`tools/coding` → `tools/coding`，以及编码与计划模式两个示例。
GoAgent 保留 Builder、Graph、durable、budget、observe、Dispatcher/Pipeline、Handle、循环扩展点、`harness/env`、`tools/policy`、`tools/spill`。
独立项目单向依赖 GoAgent 的公开接口，不增加新的执行循环或兼容转发包。

## 方案

采用独立仓库与独立 Go module，明确版本边界。嵌套模块不满足项目独立的目标；整体迁出 harness/env 会使通用 spill 反向依赖上层，因此暂保留环境包路径。
新模块依赖已发布的 GoAgent v0.23.0（包含本次迁出的循环扩展点）；本地 workspace 覆盖到拆分后的 GoAgent，另外关闭 workspace 验证发布依赖。
保留全部迁移测试与 Apache-2.0 许可证，记录来源提交。历史设计文档保留并注明迁移；有效入口文档改为新项目路径。

## 发布与执行约束

本次创建本地仓库并提交可验证结果，不创建远程仓库、不 push、不发布 tag。
先验证独立模块，再从 GoAgent 删除已迁出的包；用脚本禁止 GoAgent 生产代码反向依赖 Harness。
本地跨仓脚本使用临时 go.work，不在 go.mod 写入本地 replace，不依赖固定用户绝对路径。

## 验收

独立模块 GOWORK=off 的全量测试与构建通过；本地 workspace 中两个模块全量测试通过。
Preset 装配、审批、子 Agent、运行中交互及恢复由迁入测试和公开 API 集成测试验证。
GoAgent 导入层级与禁止反向依赖检查通过；迁移指南及两个仓库的 README/CHANGELOG 更新。
