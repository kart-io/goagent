# 继续测试与边界修复（2026-11-05）

## 结论与范围

从上轮稳定化基线 `b220340` 继续扩大测试，发现并修复模型收流取消、空流和 Shell 后代清理问题；同时修复两处跨仓测试夹具，并补强进程清理测试。修复后执行主模块全量竞态与覆盖率、15 个关联模块的实服务矩阵，以及 Linux 运行时补测。

本轮没有调用付费模型，没有接入真实身份、订单或支付服务。测试事件包含父测试及子测试，不是独立业务样本数；不同运行不合并成成功率。

## 复现的问题与处理

| 问题 | 失败证据 | 处理与回归 |
|---|---|---|
| `llm.Complete` 在已结束的上下文中仍请求提供方；取消与流关闭或终态同时就绪时，随机返回 EOF 或成功 | 新回归在旧实现上失败，记录提供方被调用及错误分类偏差 | 请求前、取流后和读块后检查上下文；`Complete` 与 `Chat` 共用修复。取消边界 20 轮 race 通过 |
| 提供方返回 nil 流且无错误时，一直等待调用方期限 | 将 `b220340:llm/complete.go` 用 Go overlay 覆盖到新测试，250ms 后得到 DeadlineExceeded，回归失败 | nil 流立即失败；已取消时优先保留上下文错误。完整 llm 包族 race 通过 |
| Shell 只终止父进程，后代持有输出管道，50ms 超时实际约 2s 后返回，还产生迟到写入；上下文错误链丢失 | 超时及正常父进程退出回归在旧实现失败；保留旧源码 overlay 运行证据 | 复用 Unix 进程组清理，限制输出等待，保留部分输出、退出码、ExitError 和上下文原因；shell/env/compatcheck 三轮 race 通过 |
| macOS 上构建的 SSH runner 被送入 Linux 容器，Exec format error | 首轮实服务矩阵 Harness 失败，19 个失败测试事件；独立 ELF 回归同样失败 | Harness 显式 `GOOS=linux`，增加 ELF 断言；真实 SSH 完整契约通过 |
| Docker 执行器夹具使用 Colima 未共享的系统临时目录 | 主模块首次扩大覆盖率运行失败；独立挂载探针证实系统临时目录文件不可见，用户目录可读写 | 仿照 Harness 已有夹具在用户目录创建临时工作区；真实读写、只读、超时清理通过 |
| 示例登记重试测试的 50ms 墙钟期限可能在提交钩子前到期，未消费的钩子使下一次重试继续失败 | SSH 修复后的矩阵中示例仓库失败，2 个失败事件，错误位于第二次登记 | 在提交点确定性注入 DeadlineExceeded，并断言钩子已消费；该用例 20 轮 race 通过。此项验证错误分类与重试，不声称模拟了真实期限触发 |

进程清理测试经独立审查后增加后代就绪信号，再触发取消并观察迟到效果，避免把“后代未启动”误判为清理成功。定向三轮 race 通过。审查未发现 Critical 或 Important；唯一 Minor 已处理。

## 验证结果

最终计数、运行环境和源码版本见 [verification.json](verification.json)；关联模块结果及完整跳过清单见 [compatibility.json](compatibility.json)。

| 验证 | 结果 |
|---|---|
| budget/durable/workflow 故障、恢复、取消、并发关键路径 | 10 轮 race 通过 |
| builder/tools/providers/multiagent 协议关键路径 | shuffle、5 轮 race 通过 |
| compatcheck 完整验证器测试 | shuffle、3 轮 race 通过 |
| 主模块完整竞态与覆盖率（含真实 Docker 执行器） | 通过；3887 个通过事件、9 个跳过；整体 79.5%，multiagent 93.3%，全部覆盖率门禁通过 |
| 构建、vet、golangci-lint v2.15、导入分层、独立 frameworkbench | 全部通过；lint 0 issues，导入分层 0 findings |
| Windows amd64 全模块编译 | 通过；没有 Windows 运行时验证 |
| Linux Go 1.26.6、CGO=0，Harness sandbox 包 | 82 个通过事件、4 个跳过；Supervisor、实际 Landlock 及 native 回退通过 |
| 同一 Linux 容器内 durable/budget/workflow/llm/shell/env | 351 个通过事件、1 个跳过，全部包命令通过；此轮没有 race，宿主机另跑 race |
| Harness Docker 工具工作区与容器环境探测补测 | 2 个用例 race 通过；TMPDIR 指向用户目录下专用临时目录 |
| 修复后的 15 模块实服务完整竞态矩阵 | 15/15 模块通过；1813 个通过测试事件、50 个跳过 |

服务为本轮自建的回环地址 PostgreSQL 16 与 Qdrant；PDF 使用依赖缓存内的公开测试样例。Docker 使用本地 Alpine 镜像，SSH 使用测试自建容器。矩阵通过临时 modfile 和受控 GOFLAGS 绑定当前核心及本地关联模块，包含测试内的 Go 子构建，不改写下游依赖文件。

Linux 首次完整 sandbox 运行因缺 curl 而失败；补 curl/python3 后 sandbox 通过，但核心包的 bash 夹具因缺 bash 失败；补齐三项依赖后独立重跑通过。两轮失败均归档，没有将缺工具的失败改成跳过。Linux 使用 Docker `--init` 回收孤儿进程，未挂载宿主 Docker socket。

## 剩余验证边界

- 上轮关联矩阵的 99 个跳过中，50 项在本轮矩阵实际通过，20 项由 Linux 和 Docker 专项补测通过，剩余 29 项未覆盖。当前 Examples 比上轮报告多一项真实模型跳过，因此最终矩阵仍有 50 项跳过；不能直接用总数差值代表补测数量。逐项对应关系见 verification.json。
- 主模块 9 个跳过包括 3 个辅助子进程入口、1 个既有 flaky 工具执行器超时用例、5 个已删除缓存能力的旧测试。本轮没有将这些跳过当成通过；Shell 的新进程取消回归不替代通用工具执行器的全部超时语义。
- Linux 的 4 个 sandbox 跳过涉及未配置容器内 Docker、当前内核不能模拟低 Landlock ABI、以及没有 Bubblewrap。Linux core 唯一跳过为未配置容器内真实 Docker，宿主机已独立验证 Docker 执行器。
- 非 Unix 平台仍只有父进程取消，不承诺后代树清理；SSH 夹具固定目标为 Linux，但没有验证宿主与目标架构不一致的场景。
- 未运行生产升级、真实业务身份授权、真实支付和新一轮真实模型调用；这些结论不能由离线脚本和临时数据库替代。

## 源码与复现

- 主仓分支 `chore/stabilize-execution-contracts`：运行时修复 `632bd9c`，测试补强 `42ef451`。
- Harness 分支 `fix/ssh-test-linux-runner`：`78a18bc`。
- Examples 分支 `test/deterministic-deadline-retry`：`f69bbb7`。
- Contrib 未修改，基线 `e0c7533`。所有改动仅本地提交，未推送、合并或发布。

```sh
# Makefile 使用 go.mod 中的工具链并清除冲突的 GOROOT。
GOAGENT_ENV_DOCKER_IMAGE=alpine:3.20 make coverage
make build vet lint test-frameworkbench GOLINT=/path/to/golangci-lint-v2
env -u GOROOT GOTOOLCHAIN=go1.26.10 go run ./scripts/apicheck

# 用已有验证器复现关联矩阵。PG DSN、固定端口及容器名由运行者配置，不能写入版本库。
# 需预设 GOAGENT_PGSTORE_DSN、GOAGENT_PGSTORE_CONTAINER、GOAGENT_TEST_QDRANT_ADDR、TEST_PDF_PATH。
GOAGENT_DOCKER_TESTS=1 HARNESS_SANDBOX_DOCKER_IMAGE=alpine:3.20 \
  env -u GOROOT GOTOOLCHAIN=go1.26.10 go run ./scripts/compatcheck \
  -root .. -out /tmp/goagent-compat-new-run -timeout 15m
```

Linux 运行方式及专项命令见 verification.json。首次失败、旧实现回归、最终成功日志均保留在 [raw-logs.json.gz](raw-logs.json.gz) 中，可用 `gzip -dc` 解压为“日志路径 → 内容”的 JSON。机器路径前缀和凭据按 [manifest.json](manifest.json) 的脱敏规则处理；清单记录原文件和脱敏内容的 SHA-256。历史报告未覆盖，原有未跟踪沙箱研究资料未修改。

本轮专用 PostgreSQL 和 Qdrant 容器在测试结束后清理，见 [cleanup.json](cleanup.json)。测试构建的 SSH 镜像保留为本机缓存；没有清理其他容器或镜像。
