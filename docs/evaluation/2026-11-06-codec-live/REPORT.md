# 编解码与工具参数的真实模型验证

日期：2026-11-06。基线 `0e9e54d8`（`v0.49.1`）；Go 1.27.3，darwin/arm64。

本轮向真实上游发出 **16 次生成请求**，另有 **2 次只读模型列表请求**。生成请求中 HTTP 200 共 13 次，Gemini HTTP 402 共 3 次；已知总用量 **6654 Token**，3 次失败没有返回用量，不记为零用量。没有 SDK 或探针自动重试。

真实调用发现并修复 Anthropic `ProviderState` 回放仍会舍入工具参数的问题；修复后另发一次真实续接，线上请求字段及模型回答均通过。DeepSeek Flash 的一次答案格式失败、Gemini 的计费阻断和修复前的精度失败全部保留，不将整轮判定为全部通过。

## 场景与结果

所有输入和工具效果均为合成数据，不连接真实订单、身份或支付系统。数值为 ID `9007199254740993`、小数 `0.1234567890123456789`，文本为 `<测试&>`。每次输出最多 2048 Token，探针阶段上限 125 秒。

| 路由与模型 | 场景 | 生成请求 | 已知 Token | 修复前判定 |
|---|---|---:|---:|---|
| DeepSeek 原生 / deepseek-flash | JSON → Strict → YAML → 类型克隆 → FileStore 重建 | 1 | 269 | 通过 |
| DeepSeek 原生 / deepseek-flash | 数值参数、效果落盘后退出进程、恢复与回放 | 2 | 1021 | 最终答案格式失败；数值、恢复、一次效果及零请求回放通过 |
| DeepSeek 原生 / deepseek-flash | 字符串参数工具循环与回放 | 2 | 1159 | 通过 |
| Gemini 原生 / gemini-2.5-flash | JSON 往返 | 1 | 未知 | HTTP 402 阻断 |
| Gemini 原生 / gemini-2.5-flash | 数值参数与恢复 | 1 | 未知 | HTTP 402 阻断 |
| Gemini 原生 / gemini-2.5-flash | 字符串参数与回放 | 1 | 未知 | HTTP 402 阻断 |
| Anthropic 协议 / deepseek-v4-pro | JSON 往返 | 1 | 392 | 通过 |
| Anthropic 协议 / deepseek-v4-pro | 数值参数、效果落盘后退出进程、恢复与回放 | 2 | 1296 | 应用层通过，线上历史 input 精度失败 |
| Anthropic 协议 / deepseek-v4-pro | 字符串参数工具循环与回放 | 2 | 1030 | 通过 |
| DeepSeek 原生 / deepseek-v4-pro | 同模型、不同协议的数值恢复对照 | 2 | 1067 | 通过 |
| 修复后的 Anthropic 协议 / deepseek-v4-pro | 复用首次真实消息和 ProviderState 续接 | 1 | 420 | 请求参数精确、原生字段保留、正确回执；不重新执行工具 |

前 10 个独立场景经线上字段核对后的结果为：**5 通过、1 答案契约失败、1 框架精度失败、3 计费阻断**。最后一行是修复后的追加验证，不能覆盖或删除首次精度失败。

当前 Anthropic 配置指向 `https://api.deepseek.com/anthropic`，模型实际报告为 `deepseek-v4-pro`。这是真实 Anthropic 协议兼容测试，**不代表 Claude 模型已测试**。原生 DeepSeek V4 Pro 对照用于减少模型差异对协议判断的影响。

## 真实请求揭示的问题

首次 Anthropic 数值调用中，模型返回的逻辑 `ToolCalls`、持久化 `ProviderState`、授权参数和实际工具参数都保留精度；但是第二轮真正发往上游的 assistant `tool_use.input` 已变成：

```json
{"id":9007199254740992,"rate":0.12345678901234568,"label":"<测试&>"}
```

正确值应为：

```json
{"id":9007199254740993,"rate":0.1234567890123456789,"label":"<测试&>"}
```

根因位于 `llm/providers/anthropic.go` 的原生状态分支：SDK 的 `MessageParam` 自定义解码把 `Input any` 变为含 float64 的对象。已有无原生状态分支虽然发送参数原文，实际 Agent 续接优先走 ProviderState，因此仍会舍入。

修复提交 **`1caa01fc`** 从同一原生状态另外读取 `content[].input` 的 JSON 原文，覆盖 SDK 对应工具字段；其余消息、思考块签名和既有工具 ID 校验保持原路径。

新回归覆盖 SSE → 原生状态 → 消息序列化/恢复 → 重建 Provider → SDK 输出请求，定点检查 assistant 工具 input，包含嵌套大数、小数、数组、字符串、null 和思考签名。旧实现先失败；修复后相关回归 20 轮 race 通过。只检查请求体是否包含原数字并不足够，因为用户消息和工具结果本身也包含原数字。

追加的第 16 次真实生成请求复用了首次真实返回的完整历史。独立解析 HTTP 请求体确认：仅工具 input 中的两个数字恢复精度，其他 JSON 字段与首次请求一致；上游返回 200 和正确回执。该次没有重新执行工具，也没有重新运行整个三阶段付费场景。前后差异见 [wire-audit.json](wire-audit.json)。

## 恢复、内容与环境边界

- 三个数值执行场景均在工具效果写入且 Dispatcher 日志落盘后以退出码 86 主动退出进程。新进程只追加一次模型请求，不重复工具效果；最终回放新增模型和 HTTP 请求均为 0，效果记录始终为 1 条。
- DeepSeek Flash 的数值场景最终输出了包含 id、rate、receipt 的完整 JSON，没有遵守“最终只输出 receipt”的预设契约，保留失败。后续零请求回放返回完全相同的错误格式答案，没有再次请求模型。
- Gemini 模型列表返回 200，但 3 次生成分别返回 402 `RESOURCE_EXHAUSTED`，提示预付余额耗尽。模型可列出不等于生成额度可用。本轮没有验证到 Gemini 的实际参数精度、工具回放或恢复，也没有追加重试。
- 本轮没有 MiniMax 或 Claude 的有效生成配置；没有将 DeepSeek 兼容端点结果归为 Claude 结果。
- 每个模型样本很小，不据此报告供应商成功率或通用能力排名。跨仓完整矩阵、生产业务和长时间模糊测试本轮未重跑。

## 本地修复验收

| 检查 | 结果 |
|---|---|
| Provider 原生回放相关回归 | 20 轮 race 通过 |
| 主模块完整 race 与覆盖率 | 通过；整体 79.9%，全部门禁通过 |
| build、vet、golangci-lint v2.15（Go 1.27.3 构建） | 通过，lint 0 issues |
| 导入分层检查 | 0 findings |
| 独立 frameworkbench race | 通过 |
| 独立只读代码复审 | Critical / Important / Minor 均为 0 |

复审者没有自行调用真实模型或重跑全量测试；执行者另核对了上述命令退出码及线上请求。SDK 未来版本与未知字段的全面保真不在当前用例保证范围内。

完整本地套件保留既有辅助子进程入口、flaky 工具超时、已删除缓存能力和未启用真实 Docker 等跳过，未将这些项目计为通过；完整名单保留在全量日志中。

## 证据与复现

- [summary.json](summary.json)：经线上字段检查修正后的判定、组件验证、源码及限制。
- [usage.json](usage.json)：每次调用的模型报告、用量及错误；失败用量保留未知。
- [raw-evidence.json.gz](raw-evidence.json.gz)：221 份原始请求、响应流、阶段结果、首次运行清单、失败、成功、源码和本地测试日志。解压为“相对路径 → 原始内容”的 JSON。
- [manifest.json](manifest.json)：归档及逐份内容 SHA-256；凭据扫描通过，认证头从未写入日志；本机路径前缀替换为 `<repo>`、`<home>`，其余内容保留。
- [probe.go.txt](probe.go.txt)、[run.py](run.py)、[supplement.py](supplement.py)：探针和运行器。压缩包另保留修复前的探针源码及本轮审计脚本。

复现应使用新的独立工作目录，并相应修改运行器的 `work` 路径；既有 `runs` 不得复用或删除。密钥仅由环境提供，不写入命令行或文件。探针缺少 `-live` 时退出 2，不发送请求。

```sh
# 将 probe.go.txt 复制到独立目录中的 probe.go，再按该目录编译、运行。
env -u GOROOT GOTOOLCHAIN=go1.27.3 go build -o <新工作目录>/probe <新工作目录>/probe.go
# run.py 会记录失败并返回非零退出码；先检查真实结果，再决定是否运行补充场景。
python3 <新工作目录>/run.py
python3 <新工作目录>/supplement.py

# 修复后的真实续接只有一次请求，source 是第一次运行保存的完整请求文件。
<新工作目录>/probe -live -route anthropic-deepseek -model deepseek-v4-pro \
  -case continuation -phase fixed -dir <新的续接输出目录> \
  -source <原始数值场景目录>/resume-call-01-request.json

make coverage
make build vet lint test-frameworkbench GOLINT=<Go1.27构建的golangci-lint>
env -u GOROOT GOTOOLCHAIN=go1.27.3 go run ./scripts/apicheck
```

初始生成请求上限为 15；发现原生状态缺陷后明确追加一次验证，总上限调整为 16，实际发送 16 次。首次运行器只做应用层检查，因此其原始 passed 标记不能替代本报告的线上字段判定；原始标记与错误均保留，未伪装成同一轮全部通过。
