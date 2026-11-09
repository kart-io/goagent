# Skills 多专业视角审查汇总

日期：2026-11-09。审查对象为 GoAgent Skills 公共加载包、可选工具及 Harness 迁移的未提交源码。

**结论：三个专业视角已完成，新增的六项缺陷均修复并经原审查 agent 复核关闭；独立安全视角未完成，Harness 独立发布门禁仍未通过。**

## 分工与结果

| 视角 | 专业 agent | 结果 | 报告 |
|---|---|---|---|
| 架构与公共 API | gsd-code-reviewer / Darwin | 2 项 P2 已关闭；1 项门禁强化建议为可选项 | [ARCHITECTURE.md](ARCHITECTURE.md) |
| Harness 集成与兼容性 | gsd-integration-checker / Hubble | 1 项 P2 已关闭；未复现生命周期缺陷 | [INTEGRATION.md](INTEGRATION.md) |
| 测试证据与开发体验 | gsd-verifier / Boyle | 2 项 P2、1 项 P3 已关闭，外部 Builder/消息探针通过 | [VERIFICATION.md](VERIFICATION.md) |
| 文件与解析安全 | gsd-security-auditor / Newton | 被平台自动安全过滤拦截，未完成 | [SECURITY.md](SECURITY.md) |

每个 agent 独立读取源码并运行定向探针；主审负责合并判断、修改源码与最终整体检查。
初始源码摘要见 [BASELINE.json](BASELINE.json)，最终源码摘要见 [FINAL_SOURCES.json](FINAL_SOURCES.json)。

## 确认缺陷与修复

| ID | 等级 | 发现 | 修复与验收 |
|---|---|---|---|
| AR-01 | P2 | 描述符耗尽等读取故障被当成“文件不存在”的完成结果 | 未分类故障返回执行错误，保留 errors.Is；真实子进程 EMFILE 回归及原始探针通过 |
| AR-02 | P2 | 参数错误与 EIO 合并时，业务码掩盖 I/O 故障 | 逐个检查错误树中的全部原因，任何未知分支保留为执行失败；两种顺序、外层包装和未知技能路径均覆盖 |
| INT-01 | P2 | 原有 `./references/a.md`、内部 `.`、重复斜线路径失效 | 在拒绝父目录、绝对路径、反斜线等输入后归一化良性相对路径；实际 Harness 调用及边界回归通过 |
| V-01 | P2 | 二进制内容返回成功，在模型请求中发生 UTF-8 替换 | 工具明确拒绝非 UTF-8 内容；Catalog 原始字节读取不变；外部 Builder 消息验证通过 |
| V-02 | P2 | 超限文件被错误报告为不存在 | 已知限制、权限和路径问题返回对应拒绝原因；超限提示与存在性贯穿工具验证 |
| V-03 | P3 | 示例读取被拒绝仍退出 0 | 示例检查工具结果，正常资源退出 0，缺失资源退出 1；永久正反例通过 |

修复没有依赖未公开的 Go 错误文字。无法分类的 os.Root 错误保留错误链；Dispatcher 可以返回 nil 输出和执行错误，
Harness 回归据此同时验证错误来源与没有文件内容返回。v3 工具结果变化已补入用户指南及 Harness 文档。

## 可选建议与发布条件

- Builder 禁止未来导入 Skills 的额外门禁是可选加固，当前没有违规依赖，本轮未实施。
- 空资源列表从 null 变为 [] 已补充文档，不计作未解决缺陷。
- Harness 的版本依赖仍不包含新公共包。`GOWORK=off` 实测编译失败，必须先提供可获取的 GoAgent 提交/版本，再更新 Harness 依赖并通过独立门禁。详见 [RELEASE.md](RELEASE.md)。

## 验证证据

最终命令与结果以 [final-checks.json](final-checks.json) 为准。原审查探针、运行边界、历史失败和关闭证据保留在三个分报告中。
本轮没有发布提交；没有运行独立 Postgres 或需显式启用的 Docker 场景，也没有用本地 workspace 代替独立发布门禁。
