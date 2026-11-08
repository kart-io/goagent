# 显式历史操作账本迁移

在线 `durable.Do/InspectOperation` 只接受 `goagent-operation-v2`。本包是宿主显式调用的一次性转换工具，不被执行器自动调用，不执行任何业务回调，不清空日志，也不改变 JSON 编码规则。

迁移步骤：

1. 停止并协调全部运行写入者，备份原日志。保留原 Store 的锁、原子写入和持久保证；有作用域的运行必须使用原来的 Store/Scope 视图。
2. 根据宿主持久的操作身份清单构造 `[]migrate.OperationRef{{Key: durable.ChildKey(parentKey, operationID), ID: operationID}}`。Key 是完整派生键，不是审批键或运行 ID；本接口不猜测、不自动枚举目标。
3. 调用 `OperationsV1(ctx, store, refs, OperationOptions{})` 预览。核对 `Pending`、`Current` 的目标身份；预览会持目标锁并加载校验，但不写快照。
4. 核对后显式传 `OperationOptions{Apply: true}`。所有目标按存储身份排序持锁，全部验证后经一次 `Store.SaveAll` 原子提交，只修改 v1 的版本字段。当前 v2 记录同样验证且不改写。
5. 再次预览，应全部出现在 `Current`。确认目标清单完整后再启用新的运行版本；已完成操作照常回放，不重新执行效果。

运行本包的 `ExampleOperationsV1` 可执行一个临时 FileStore 的预览及应用示例：

```sh
env -u GOROOT GOTOOLCHAIN=go1.27.3 GO_TEST_MODE=true go test ./core/durable/migrate -run '^ExampleOperationsV1$' -v
```

转换保留 Snapshot 其余字段，并将从 Store 加载的 Data 原字节原样交给 SaveAll，不重新编码 OperationRecord。身份、输入指纹、状态、尝试次数、上限、结果及错误分类由当前 `durable.DecodeOperationRecord` 统一验证；v1 不能带等待或效果核对语义。非法版本、损坏身份/载荷、重复目标或取消会在提交前拒绝。没有可接受的“删除旧日志重新跑”降级路径。

批次写入、取消和提交结果未知按 Store 契约处理：提交前失败不写入；提交结果未知可能已经全部写入，须重新预览判断，不能凭错误回滚或重复执行业务。重复迁移已成为 v2 的目标只会验证并跳过。调用方须提供满足原子 SaveAll 契约的后端；本包不会把缺少事务的后端伪装成可安全迁移的存储。
