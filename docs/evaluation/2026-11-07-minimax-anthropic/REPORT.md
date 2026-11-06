# MiniMax Anthropic 协议真实执行验证

日期：2026-11-07（Asia/Singapore）。源码 `bc23061`，包含 Anthropic 原生状态精度修复 `1caa01fc`。Go 1.27.3，darwin/arm64。

按用户要求，本轮通过 GoAgent 的 **`providers.NewAnthropicWithOptions`** 调用 MiniMax，基地址为 **`https://api.minimax.cn/anthropic`**，模型为 **`MiniMax-M3.1-Flash`**。实际记录的五次请求全部为 `POST /anthropic/v1/messages`、`stream: true`，响应也报告同一模型。该协议与端点参见 [MiniMax 国内官方说明](https://platform.minimax.cn/docs/api-reference/text-anthropic-api)。

结果：**5 次真实生成请求均返回 HTTP 200，已知用量 3245 Token，无自动重试。** 流式调用、完整原生消息续接和显式工具直返下的进程恢复通过；两项内容/精度契约失败保留，不能将整轮记为全通过。

## 实际场景

| 场景 | 请求数 | 已知 Token | 结果 |
|---|---:|---:|---|
| JSON 输出 → Strict 解码 → YAML 往返 → 类型克隆 → FileStore 重建 | 1 | 336 | 通过，文本中的数值原文保持精度 |
| 数值工具参数：大整数 ID 与高精度小数 | 1 | 756 | 失败；ID 保留，小数在上游原始响应中已缩短，授权层拒绝，零效果 |
| 字符串参数、工具续接、模型最终回答 | 2 | 1497 | 工具调用和原生历史回传通过；最终回答格式失败；后续零请求回放保留同一失败答案 |
| 字符串参数、显式工具直返、效果落盘后退出进程、恢复与重放 | 1 | 656 | 通过；退出码86，恢复和重放均零模型请求，效果恰好一次 |

后两项是不同配置。显式 `WithReturnDirectTools("verify_synthetic")` 把完整工具 JSON 作为最终答案，不要求模型再生成回执；其通过不能替代此前“模型最终只返回 receipt”的失败。

## 原生协议检查

- GoAgent 使用 Anthropic 官方 SDK 构造流式 Messages 请求，未改写请求体或替换上游响应。
- 字符串场景的首次终态包含 `thinking`、`tool_use` 两个内容块及一个思考签名。逐字段比较下一轮实际 HTTP 请求中的 assistant 内容，确认其与保存的 ProviderState 完整一致。
- 字符串工具参数在模型返回、授权、执行与上游续接历史中保持原值。
- 显式直返场景在工具效果和 Dispatcher 日志落盘后主动退出进程。新进程从同一存储恢复，工具效果仍为1条，新增模型/HTTP请求均为0；再次重放也为0。恢复阶段的请求上限设为0，意外请求会在发出前失败。
- 凭据读取自本机既有配置，仅在测试子进程映射为 `MINIMAX_CN_API_KEY`；清除子进程中其他供应商默认配置。未修改全局配置，认证头从未写入日志。

## 保留的失败

数值工具调用要求：

```json
{"id":9007199254740993,"rate":0.1234567890123456789,"label":"<测试&>"}
```

实际网络响应 `input_json_delta.partial_json` 拼接后为：

```json
{"id": 9007199254740993, "label": "<测试&>", "rate": 0.12345678901234568}
```

因此能确认：在 GoAgent 解析前，上游响应中的小数已经缩短。本轮不能进一步区分模型生成与服务端内部处理。授权检查拒绝错误参数，没有工具效果，也没有进入原计划的崩溃恢复阶段。

原字符串场景工具效果正确，模型最终返回完整 JSON（含 id、rate、receipt 等），没有遵守“只返回 receipt”的预设格式。此项保持失败；新进程的零请求回放返回同一答案，效果仍为1条。

这两项没有通过重试、放宽原断言或覆盖原日志转为成功。精确业务字段可采用字符串 Schema，由宿主校验原文；宿主若选择工具直返，应显式采用对应契约。

## 复现与证据

- [接入指南](../../guides/MINIMAX_ANTHROPIC.md)：显式国内端点、模型、认证及原生状态处理。示例已提取并使用 Go 1.27.3 编译通过。
- [summary.json](summary.json)、[usage.json](usage.json)：逐场景结论、协议审计与真实用量。
- [raw-evidence.json.gz](raw-evidence.json.gz)：85份原始 HTTP 请求、响应流、阶段结果、首次失败、恢复记录、探针及审计脚本。
- [manifest.json](manifest.json)：归档和逐份内容 SHA-256；所有内容通过凭据扫描，仅脱敏本机路径前缀。
- [probe.go.txt](probe.go.txt)、[run.py](run.py)、[recovery.py](recovery.py)：可复现源码。首次探针源码也保留在压缩包中。

```sh
# 使用新的独立工作目录；运行器 work 路径也要同步调整，已有证据目录不得复用。
env -u GOROOT GOTOOLCHAIN=go1.27.3 go build -o <新工作目录>/probe <新工作目录>/probe.go
python3 <新工作目录>/run.py
# 首轮保留两项失败，返回非零退出码；检查结果后再运行独立恢复场景。
env -u GOROOT GOTOOLCHAIN=go1.27.3 go build -o <新工作目录>/probe-direct <新工作目录>/probe.go
python3 <新工作目录>/recovery.py
```

每次请求最多4096输出Token（含thinking），每阶段125秒，总上限5次生成请求。本轮没有新增 MiniMax Provider 或修改其他仓库的旧适配器；实际执行直接复用通用 Anthropic Provider。本轮只新增接入文档和验证资料，生产 Go 源码未改动，没有接入真实订单、支付或身份服务。
