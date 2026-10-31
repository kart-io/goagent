# 显式零温修复与真实验证

日期：2026-10-31。主库基线 `2dfad53`，分支 `fix/explicit-zero-temperature-20261031`；使用本地未提交修复验证，版本与二进制摘要见 [versions.json](versions.json)。

## 问题与修复

此前 ACP 真实联调发现设置 `temperature=0` 仍发送 `0.7`。根因共有三层：`ConfigToOptions` 和 `GetTemperature` 将零当作缺省；Builder 请求没有区分显式零；OpenAI SDK 的 `omitempty` 在最终 JSON 中省略零。只修改其中一处无法修好整条链路。

- 为请求与客户端配置增加 `TemperatureSet`，选项和 JSON 解码自动维护。Go 字面量显式零需同时设置标记；缺省继续回落，JSON `null` 清除设置。
- Builder 三条请求路径传递显式温度；内置九个 Provider 使用统一解析逻辑，优先级仍是请求、客户端、默认 `0.7`。未设置的旧请求和非零请求保持原有 JSON 字段顺序，避免改变持久意图指纹。
- 原生 HTTP Provider 发送零值；OpenAI 和 DeepSeek 原生流在 SDK HTTP 边界补入零，保留其他字段的原始 JSON 数字精度，并维护重定向所需的 `GetBody`。Embedding 请求不受影响。
- 未修改 SDK、依赖版本或业务默认温度。自定义 Provider 应迁移到 `BaseProvider.ResolveTemperature(req)` 才能识别新标记。

## 自动验证

先写回归测试并复现失败，再实现修复。[失败记录](red.log)保留了配置、JSON、Builder 和最终 HTTP 请求丢失零值的证据。

| 检查 | 结果 |
| --- | --- |
| 九个 Provider 实际 HTTP 请求矩阵 | 通过：客户端零温、请求零覆盖、默认与正数回归 |
| OpenAI 六个同步/流式/工具入口、DeepSeek 原生流 | 通过 |
| JSON 标准库与项目编解码器、配置复制 | 通过 |
| SDK HTTP 307 重定向、大整数保真、Embedding 隔离 | 通过 |
| `go test -race ./...` | 全量通过，见 [日志](full-race.log) |
| contrib ACP 使用当前主库 `go test -race ./...` | 通过，见 [日志](acp-race.log) |
| examples ACP 使用当前主库竞态回归 | 通过，见 [日志](example-race.log) |
| golangci-lint / 导入层级 | 0 issues / 0 findings |

Go 命令使用 `env -u GOROOT GOTOOLCHAIN=go1.25.0`。跨仓库测试通过临时 go.work 使用本地模块，未向产品 go.mod 写入机器路径。

## 真实客户端与模型

使用真实 Neovim 0.12.6 headless、未修改的 CodeCompanion v20.0.1、独立 Agent 进程、Ollama `llama3.1:8b`、真实文件与 OS 子进程。两层温度均为 `0`，最大输出 512；已移除此前 `0.01` 绕过配置。模型请求通过本地透明代理转发至 `127.0.0.1:11434/v1`。

[最终 HTTP 请求](state-last-model-request.json)明确包含 `"temperature":0`。[Neovim 结构化报告](nvim-report.json)中九项检查全部通过：握手、模型回答、文件读取、允许写入、命令执行、拒绝无副作用、取消真实 `sleep 30` 子进程、同连接重载、跨进程恢复后继续对话。取消检查确认 PID 已消失，恢复回放 21 条更新。审批由测试回调选择，未声称验证人工 GUI 操作。

该客户端文件读取使用磁盘，未保存缓冲区不支持；客户端也未声明 terminal 能力，命令使用真实 LocalExecutor。这些客户端能力边界保持如实记录。温度为零并不等于任意模型回答质量有保证。

复现材料：[真实 Agent 源码](agent-main.go.txt)、[临时模块配置](agent-go.mod)、[Neovim 驱动](nvim-live.lua)、[ACP 原始通信](nvim-agent-stdout.jsonl)。机器路径需按实际检出位置调整；源码通过 HTTP 调用现有模型，无模拟模型响应。此前带 `0.01` 的历史验证仍保留在 contrib 报告中。

## 官方 ACP TCK 回归

使用同一个零温二进制运行固定版本 TCK：**57 passed、17 skipped、0 failed，52.78 秒，CONFORMANT**。31 项强制要求全部通过。跳过项属于未声明的可选能力或未提供认证测试参数，不等于已验证这些功能。见 [原始日志](tck-zero-run.log)及 [结构化报告](tck-zero-report.json)。

```sh
# 在主库根目录运行离线回归
env -u GOROOT GOTOOLCHAIN=go1.25.0 go test -race ./...
env -u GOROOT GOTOOLCHAIN=go1.25.0 bash scripts/verify_imports.sh
# 在含真实 Agent 二进制、workspace、nvim-live.lua 的临时验证目录运行
GOAGENT_LIVE_ROOT="$PWD" NVIM_APPNAME=goagent-acp-validation nvim --headless -u NONE -l nvim-live.lua
venv/bin/acp-tck --agent-cwd "$PWD/workspace" --timeout 45 --startup-timeout 60 --test-timeout 120 --cancel-prompt '请调用 run_command，argv 为 ["sleep","30"]，执行结束后只回复完成。' --report-json tck-zero-report.json -- "$PWD/goagent-acp-live"
```
