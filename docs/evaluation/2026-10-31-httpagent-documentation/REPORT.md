# HTTP Agent 正式文档补充验收

2026-10-31。用户要求补齐新增功能的使用和设计文档。此次仅新增/更新文档、索引与文档校验脚本，未修改生产Go代码、业务行为或依赖。

## 正式入口

| 仓库 | 交付 |
| --- | --- |
| goagent-contrib，024f7e6 | httpagent/docs/USER_GUIDE.md、DESIGN.md、API_REFERENCE.md、VERIFICATION.md；模块和仓库README索引；verify_docs.py |
| goagent-examples，1eee65e | HTTP案例README和SCENARIOS链接正式文档 |
| goagent，本记录所属提交 | docs/guides/HTTP_AGENT.md、docs/design/HTTP_AGENT_DESIGN.md；中英文README、文档首页、API审查记录及默认验证清单 |

API基线为已推送的cfb3e85，核心v0.32.2。功能仍在feat/httpagent-webhooks-20261031分支，未合并主分支或打tag。正文明确区分可获取的提交版本与正式发布tag，常规使用无需本地workspace。

使用指南含离线案例、可复制的完整Plugin/工具/Hook/HTTP服务、inline与queued装配、独立worker、通知、模型和框架接入、停服及错误排查。API参考列出所有Config字段、默认值、公开绑定/连接/接纳接口、报文及实际错误映射。设计包含架构、owner生命周期、WS升级与queued接纳时序图，描述资源释放和保证边界。

## 核对与验证

| 检查 | 结果 |
| --- | --- |
| 扩展README及4份正式文档 | 5份本地链接检查通过，2个完整Go包构建与vet成功；[module-docs.log](module-docs.log) |
| 主仓当前入口文档 | 21份本地链接检查通过，4个完整Go程序编译成功；[core-docs.log](core-docs.log) |
| 案例README/SCENARIOS | 本地文件链接检查通过 |
| 使用指南完整服务真实运行 | JSON、SSE、inline Webhook均200，SSE含实际echo_input tool_start和唯一done，原生Hook调用6次，SIGTERM exit0；[结果](smoke-result.json)、[原始SSE](smoke-sse.txt)、[日志](smoke-server.log) |

Go使用1.25.0，仅在验证子进程清除错误GOROOT。扩展脚本使用临时独立模块，GOWORK=off，从远端cfb3e85导入依赖，不使用本机replace，不改宿主go.mod。普通文档脚本仅编译/vet，不调用模型；完整服务实测另外显式使用已经运行的本地Ollama llama3.1:8b，未下载模型，服务进程已停止。

源码逐项核对handler/binding/types/run/transport/webhook/websocket及传输接口/默认适配文件。明确了容易误读的行为：

- DeliveryBudget按每个生命周期事件发布创建，started与终态分别计时，Observer先于Delivery。
- queued先接纳，不自动启动worker；Handler.Run不持久缓存完成结果，稳定RunID不自动保证只执行一次。
- 标准WS Origin默认按允许的scheme与请求Host匹配；自行升级由宿主负责握手策略。
- SSE提交前失败可为JSON；ready、必要Cleanup完成和全局容量回收不是同一时刻。
- Deadline只是合作式context边界，无法强停不配合的宿主Go代码。

全量文档检查还发现旧LLM_API示例漏providers导入，并将真实OpenAI请求描述为离线模拟；已修正导入及说明，重新编译通过。

校验不覆盖远端URL内容、Markdown锚点、Mermaid渲染或所有自然语言断言。无package声明的接口片段由源码人工核对，不宣称自动编译全部片段。此次不重复全仓race：生产代码未变，新增完整程序的编译、实际入口调用及文档链接是直接对应检查。
