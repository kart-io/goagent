# 不可变 Agent 修订

`Seal` 校验并复制定义和所有文件，`Verify` 重算内容摘要；`Archive` 只暴露按 ID 读取。
`FileArchive` 提供 `Open`、`Put`、`Load`、`Close`，不提供删除或垃圾回收。
宿主按完整 `Runtime` 身份查找运行时加载器，本包不解析动态版本、不运行程序。
运行时仅允许 `native`、`executable`、`oci`：`native` 使用宿主注册的不可变程序身份，
不得设置 `Artifact` 或 `Image`；`executable` 的 `Artifact` 必须指向已归档文件，
文件 SHA-256 必须等于 `Digest`，不得设置 `Image`；`oci` 的 `Image` 必须以
`@sha256:<Digest>` 结尾且不得设置 `Artifact`。OCI 镜像层未归档，保留与拉取由宿主负责。
所有类型均要求非空 `Version`，`Runtime` 仍为可比较结构。

`Config` 必须是 JSON 对象，缺省 `Tools` 规范为 `[]`。JSON 对象键排序，数字保留原文，
因此 `1` 和 `1.0` 可以拥有不同摘要；大整数不会转为浮点数。空映射统一为 `{}`，空文件统一为空字节。
技能目录为归档根下的斜线相对目录（`.` 表示根），每项必须含名称匹配的 `SKILL.md`。
调用方不得在调用期间并发修改输入；返回值归调用方独立拥有，后续修改不影响磁盘归档。

归档目录必须由可信调用方拥有。单个 `<id>.json` 包含全部文件字节；读取要求严格模式和规范编码，
任何字节改写（包括空白）均视为损坏。写入先同步临时文件，再以硬链接原子创建目标，绝不覆盖已有对象，
最后同步目录。部署文件系统须支持硬链接和目录同步。取消发生在发布前会阻止写入；发布后的同步失败可能返回错误，
此时相同内容可安全重试。关闭等待正在进行的操作结束，重复关闭安全。

## 使用与限制

完整宿主装配、版本发布、会话绑定和实际程序加载见 [AgentRevision 指南](../docs/guides/AGENT_REVISIONS.md)。
公共 API 不依赖 management、Builder、模型或 Harness；只负责内容校验与归档。

限制位于 `constants/revisions.go`：归档 64 MiB（含 Base64）、定义 4 MiB、单文件 16 MiB、文件数 10000、路径 4096 字节、JSON 深度 128。
取消不抢占已进行的文件系统调用或锁等待，等待结束后重新检查上下文。

本地验证：`env -u GOROOT go test -race ./revision`。
