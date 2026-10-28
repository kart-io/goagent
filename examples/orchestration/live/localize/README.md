# 本地化：自定义上下文策略与同节点多 Agent

一个持久节点 `localize` 里先后运行两个 Agent：

| Agent | 作用域 | 工具 `lookup_term` 的实现 |
| --- | --- | --- |
| 翻译 | `translate` | 查商品编号的英文名（`SKU-7731 → Aurora Wireless Earbuds`）或术语译法 |
| 校对 | `proofread` | 返回英文风格规范 |

两个工具同名、实现不同，各自经独立 Dispatcher 记录日志。模型和工具的日志按 `WithDurableScope` 隔离；离线测试故意让两个 Agent 使用完全相同的调用 ID `c1`，验证它们不会互相命中日志。

`glossaryPolicy` 是自定义的 `contextpolicy.Policy`：

- 把术语表追加到首条用户消息末尾。只改非 system 消息的正文，不增删消息，满足策略运行时的协议检查；重复调用保持幂等。
- 描述信息包含 ID、Version，以及按术语内容和顺序计算的 `ConfigHash`，这些都进入模型请求意图。同版本只要改了一个译法，旧日志就不能复用，会在请求模型之前失败。
- 翻译和校对两个 Agent 都挂这个策略，各用一个独立实例。只给翻译挂时，MiniMax 的校对会把 "no-questions-asked refund" 润色成别的说法。

```sh
GOAGENT_LIVE_PROVIDER=minimax go run ./examples/orchestration/live/localize -dir /tmp/localize-1
```

`TestLiveLocalizeRecovery` 和 `TestScriptedLocalizeRecovery` 共用以下四步：

1. 翻译完成、校对开始前退出。
2. 换一版术语表恢复：报 intent 不一致，0 次请求，0 次工具执行。
3. 用原术语表恢复：翻译部分全部回放（翻译的工具执行次数为 0），校对正常执行；断言"崩溃前请求数 + 新增请求数 = 预算预留数"。
4. 回放：0 次请求，终稿不变。

终稿由宿主检查 5 个规定译法，不区分大小写。
