# 多技能协作示例

演示一个自行实现的 Skill（技能）协作层：注册与发现、按上下文路由、串行/并行执行、结果聚合与错误处理。

`Skill`、`SkillRegistry`、`SkillRouter`、`SkillExecutor`、`SkillResultAggregator` 等类型都定义在本目录的 `main.go` 中，并非 goagent 提供的包；示例只用到 goagent 的 `interfaces.Tool` 与 `tools.NewFunctionTool`（Math 技能内部的 `calculator` 工具）。不调用模型，可直接运行。

## 运行

```bash
go run ./examples/skills/01-demo
```

## 核心组件（main.go）

| 类型 | 职责 |
| --- | --- |
| `Skill` / `BaseSkill` | 技能接口与基础实现：名称、描述、类别、关键词、工具列表、`RegisterAction` 注册的动作；`CanHandle` 对路由上下文打分 |
| `SkillRegistry` | `Register`（重复注册报错）、`Unregister`、`Get`、`List`、`ListByCategory`、`FindByKeyword`、`Size` |
| `SkillRouter` | 默认带 `keyword`、`category`、`score` 三种 `RoutingStrategy`；`Route` 返回按分数排序的 `RoutingResult`，`RouteTop` 取前 n 个 |
| `SkillExecutor` | `ExecuteByName`、`ExecuteByContext`、`ExecuteSequential`（前一步结果放入下一步输入的 Context）、`ExecuteParallel`；`SetMaxConcurrency`、`SetTimeout` |
| `SkillResultAggregator` | 聚合模式 `AggregateMerge`、`AggregateBest`、`AggregateConsensus`、`AggregateWeighted`、`AggregateChain`；`AggregatedResult` 含成功/失败计数、耗时、错误列表等 |

## 演示场景

1. **注册与发现**：注册 math、text、data、utility 四个技能，按类别/关键词查找、查看详情、注销 `utility`、演示重复注册被拒绝。
2. **上下文路由**：用不同的 `RoutingContext`（查询、类别、关键词）路由到最合适的技能。
3. **串行执行**：多个技能依次执行，结果沿链传递。
4. **并行执行**：多个技能并发执行。
5. **聚合与错误处理**：加入 `error_prone` 技能（`success`、`fail`、`timeout`、`random_fail` 动作），演示部分失败时的聚合结果。

## 内置技能与动作

| 技能 | 动作 |
| --- | --- |
| `math` | `calculate`（`operation`: add/subtract/multiply/divide/power/mod，参数 `a`、`b`）、`statistics`（`data` 数组） |
| `text` | `transform`（uppercase/lowercase/reverse/trim/title）、`analyze`（长度、词数、行数等）、`split` |
| `data` | `filter`（`condition` + `threshold`）、`transform`（`operation` + `factor`）、`aggregate`（可选 `group_by`） |
| `utility` | `datetime`（`format`: date/time/full/iso/unix）、`generate_id`（`prefix`）、`format_number` |

调用方式：

```go
output, err := executor.ExecuteByName(ctx, "math", &SkillInput{
    Action: "calculate",
    Args:   map[string]interface{}{"operation": "add", "a": 10.0, "b": 20.0},
})
```
