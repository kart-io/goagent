# 持久化人工介入（离线）

在 `review` 节点前暂停并保存检查点，由人工通过、拒绝或修改草稿，再恢复到对应分支。没有真实模型请求、消息发送或支付操作。

在仓库根目录执行：

```sh
HITL_DIR=$(mktemp -d)
go run ./examples/human-in-the-loop -dir "$HITL_DIR" -text '待审查的草稿'
```

输出 `Waiting: true`、`Done: false`，以及 `Key`。此时进程已经退出；用同一目录和运行 ID 查看已保存状态：

```sh
go run ./examples/human-in-the-loop -dir "$HITL_DIR" -action status
```

把以下 `Key` 占位文字替换为刚才输出的值：

```sh
go run ./examples/human-in-the-loop -dir "$HITL_DIR" -action resume \
  -key '输出中的 Key' -reviewer demo-reviewer \
  -decision approve -text '人工修订后的草稿'
```

返回 `Done: true`，业务状态为 `accepted`，文本为修改后的内容。使用 `-decision reject` 则走拒绝分支，状态为 `rejected`；不传 `-text` 时保留原草稿。错误决定或其他 reviewer 会报错，原检查点仍在等待，可以改正后重交。

默认运行 ID 为 `review-1`。处理下一份草稿用新的 `-run review-2`；完成后重复 `-action start` 只返回保存的结果，不创建新审批。原 Key 已被消费，再次 `resume` 会报错。

`demo-reviewer` 只是离线规则，不是身份认证。生产服务必须先认证，再将可信身份放入 context，并检查权限、业务事实与允许修改的字段。参见 [HITL 接入指南](../../docs/guides/HUMAN_IN_THE_LOOP.md)。
