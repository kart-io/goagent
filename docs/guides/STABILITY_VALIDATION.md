# 稳定化验证与发布顺序

## 本地门槛

使用 go.mod 声明的 Go 工具链，在当前源码执行全仓 `go test -race -count=1 ./...`、覆盖率门槛、`go build`、`go vet`、Lint、导入分层和独立 frameworkbench 契约测试。跳过项单独检查，不用自动重试后的绿灯代替首次结果。

## 离线长稳

```sh
go run ./scripts/stabilitysoak -duration 24h -workers 4 -interval 1s -sample 1m
```

每个周期通过公开接口完成治理工具循环、文件会话日志、重建后的零模型请求/零工具效果重放、持久图审批与完成重放，以及已取消工具调用的零效果验证。模型为无网络的合成流；每个周期应恰好两次模型请求、一次工具效果和一次图节点效果。

这是同一进程反复创建、执行、重建、清理的生命周期负载。每周期回收临时状态，以保持存储有界；不验证长会话历史持续累积、真实模型质量、Postgres、远端服务或生产业务容量。长会话规模另用已有会话基准验证，生产容量需使用实际业务与存储后端单独测试。

默认每周期期限5秒，每分钟采样；堆内存256MiB、goroutine128个、临时存储16MiB为采样门槛，可以通过 `-max-heap`、`-max-goroutines`、`-max-disk` 调整。它们是采样峰值，不是连续测量或操作系统硬配额。P95/P99是累计混合周期耗时的直方图上界，包含清理，末桶使用实际最大值。一次执行错误、取消、门槛超限或采样长时间中断均不能算通过。

运行时临时状态自动删除，不生成报告文件。需跨会话检查时，先在干净、已提交源码上构建二进制，再启动：

```sh
go build -o /tmp/goagent-stabilitysoak ./scripts/stabilitysoak
/tmp/goagent-stabilitysoak -duration 24h -status-address 127.0.0.1:18763
curl --fail http://127.0.0.1:18763/status
```

接口仅监听回环地址，结果留在进程内存。`Phase=passed`、`Result.Elapsed >= Target`、有效周期非零且 `Revision` 与待发布提交完全一致，才表示该源码完成声明时长；`running`、`failed`、`unknown` 或 `-dirty` 均不能作为稳定发布证据。检查结束后用 `curl -X POST http://127.0.0.1:18763/stop` 关闭进程；提前停止按取消失败处理。执行期间应防止电脑休眠，不能把挂起时间当持续负载。

## MiniMax 回归

使用[MiniMax Anthropic 指南](MINIMAX_ANTHROPIC.md#有界定期回归)中的专用凭据和入口。每次最多三个请求，无网络错误重试；不要把每周的少量真实回归扩大为24小时持续付费调用。GitHub 工作流只有配置环境 Secret 并显式启用后才运行；权限不足时可以由宿主本机调度同一入口，凭据仍从本机读取。

## 版本对齐

合并主分支不等于已经发布。仅在目标提交本地门槛与长稳通过后创建核心正式 tag；本轮包含构建期拒绝过去静默忽略的配置，应按0.x阶段的 minor升级，而不是仅补patch。发布前重新核对远端版本，不能覆盖既有tag。

Contrib采用独立模块tag，按真实依赖顺序发布：

1. 核心正式tag可下载后，在Contrib各迁移模块中更新核心版本，执行单模块 `GOWORK=off go test -mod=readonly -race ./...`。
2. 发布retrieval的新版本。
3. 将document、cohere、qdrant、vertexai锁定到已经发布的retrieval版本，分别执行独立测试后发布；database、practical没有同仓上游，可独立发布。
4. 将PDF锁定到已经发布的document、retrieval版本，独立验证后发布。
5. 用不含任何本地replace的全新临时消费模块下载整套正式版本并构建；临时模块完成后删除。

不要先提交指向尚未发布tag的依赖，不以workspace联调结果代替发布后的独立消费验证。发布时若长稳尚在运行，保留未发布状态，不把短时预检标记成24小时通过。

本轮候选版本如下，只有对应上游tag真实发布后才修改下游依赖；发布时须再次检查远端是否已有新版本。

| 模块 | 候选tag | 同仓依赖 |
|---|---|---|
| goagent | `v0.50.0` | 无 |
| retrieval | `retrieval/v0.4.1` | 无 |
| document | `document/v0.3.1` | retrieval `v0.4.1` |
| retrieval/cohere | `retrieval/cohere/v0.4.1` | retrieval `v0.4.1` |
| retrieval/qdrant | `retrieval/qdrant/v0.4.1` | retrieval `v0.4.1` |
| retrieval/vertexai | `retrieval/vertexai/v0.4.1` | retrieval `v0.4.1` |
| tools/database | `tools/database/v0.3.1` | 无 |
| tools/practical | `tools/practical/v0.3.1` | 无 |
| document/pdf | `document/pdf/v0.3.1` | document `v0.3.1`、retrieval `v0.4.1` |

本轮迁移模块的核心依赖随正式发布对齐到已存在的核心新tag。ACP、HTTP Agent、pgstore没有此次编解码迁移，不为版本号一致而抬高最低依赖；消费端仍须显式锁定所验证的核心版本。
