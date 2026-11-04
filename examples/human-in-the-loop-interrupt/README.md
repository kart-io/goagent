# 节点执行中途向人要输入

无需模型或 API 密钥。

```sh
DIR=$(mktemp -d)
go run ./examples/human-in-the-loop-interrupt -dir "$DIR" -order A1
```

输出 `Waiting: true`，`Interrupts[0]` 给出 `Key` 与问题。在另一个进程中回应：

```sh
go run ./examples/human-in-the-loop-interrupt -dir "$DIR" -order A1 -key '输出中的 Key' -input 120
```

返回 `Done: true`，`status` 为 `refunded`。输入 `-1` 会被拒绝，仍在等待，改正后可以重交。
