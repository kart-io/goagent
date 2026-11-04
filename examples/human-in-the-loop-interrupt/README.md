# 节点执行中途向人要输入

无需模型或 API 密钥。

```sh
DIR=$(mktemp -d)
go run ./examples/human-in-the-loop-interrupt -dir "$DIR" -order A1
```

输出 `Waiting: true`，`Interrupts[0]` 的 `key` 字段是中断键，`info` 是问题。在另一个进程中回应：

```sh
go run ./examples/human-in-the-loop-interrupt -dir "$DIR" -order A1 -key '输出中的 key' -input 120
```

返回 `Done: true`，`status` 为 `refunded`。输入 `-1` 会被拒绝，进程以非零状态退出，图仍在等待，改正后可以重交。
