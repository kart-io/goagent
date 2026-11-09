# Skills 公共加载能力验收

日期：2026-11-09。验收对象是两个工作区中的源码变更，基线与命令结果见 [checks.json](checks.json)。

最新[设计续审](design-review/SUMMARY.md)及其定向检查覆盖后续改动；本页全量测试和源码摘要保留为上一轮快照，不表示续审后重新运行了全量测试。

后续[多专业视角审查](multi-agent-review/SUMMARY.md)新增确认并修复 6 项缺陷，三个视角已完成复核；
独立安全视角因平台自动安全过滤未完成，Harness 独立发布依赖门禁仍未通过。下列源码摘要和回归记录已刷新到修复后版本。

## 实现范围

- GoAgent `skills`：Parse、Discover、Catalog.List/Load/ReadReference/Close，来源、作者版本及原文 SHA-256。
- `tools/skill`：可选 `load_skill`，保留原 Harness 的参数与结果形状，由宿主管理 Catalog 生命周期。
- `codec/yaml.DecodeMapping`：在物化前检查输入、别名展开、重复键、键类型及数值指数预算；通用 Unmarshal 行为不变。
- Harness Preset：复用公共包，保留来源默认值、优先级、授权、提示词与 Manifest 装配。版本 3 的解析差异及摘要变更已写入迁移说明。
- [FAQ 案例](../../../examples/skills/03-catalog/)展示元数据、正文和参考资料读取，不需要 Harness 或模型密钥。

## 最终验证

| 检查 | 结果 |
|---|---|
| GoAgent `GO_TEST_MODE=true go test -race -timeout 120s ./...` | 72 个测试包通过，见 [core-race.log](core-race.log) |
| Harness 实际源码，临时 workspace，`go test -race -count=1 -timeout=120s ./...` | 11 个测试包通过；无源码 overlay，见 [harness-race.log](harness-race.log) |
| 双方 `go build ./...` / `go vet ./...` | 通过 |
| GoAgent golangci-lint / 导入门禁 | 0 issues / 0 findings |
| 新加载包与工具 Windows amd64 / macOS arm64 交叉编译 | 通过；不是目标平台运行验证 |
| race 覆盖率 | skills 93.1%，tools/skill 94.6%，codec/yaml 88.4%；整体 79.4%，全部门禁通过 |
| Parse fuzz | 最终一轮 31036 次执行通过，见 [fuzz.txt](fuzz.txt) |
| 直接运行 FAQ 示例 | 通过，见 [example.log](example.log) |

Harness 命令与退出码见 [harness-checks.json](harness-checks.json)，文件边界回归包含在全量测试中。
两个仓库对应源码摘要分别见 `SOURCE_SHA256SUMS` 和 `HARNESS_SOURCE_SHA256SUMS`，可在各自仓库根目录用 `sha256sum -c` 校验。

## 独立审查关闭项

独立审查通过隔离复现发现以下问题，修复后复核关闭；没有修改生产代码来绕过验收。

| 问题 | 修复与回归证据 |
|---|---|
| YAML 别名将小文件展开为大量内存 | DecodeMapping 在物化前累计展开预算；保留小型别名，拒绝循环与超限 |
| 已列出的技能目录被替换为 FIFO 后阻塞 | 获取目录句柄保留最终 `/.`；永久测试模拟列举后替换 |
| Harness 解析兼容性变化超出摘要变更 | 文档列明 YAML 1.2 布尔、字符串名称/版本、null、字段长度和合并键差异 |
| null 值及非字符串键被转换后误接受 | 映射解码保留类型，技能字段显式验证类型与存在性 |
| 技能包的第三方依赖绕过导入门禁 | 在非模块导入的提前返回前执行限制，测试允许标准库、拒绝第三方实现依赖 |
| 重复键放大错误与内存 | 每个映射在共享解码前检查唯一性，固定长度错误；600 个重复键复现从约 30.9 MB 降至约 294 KB 分配 |
| 短指数触发巨大有理数计算 | 数值指数计入解码预算；极端数值复现从约 96.4 MB 降至约 48 KB 分配 |
| Harness 来源路径本身为 FIFO 时阻塞 | 工作区、绝对和 home 相对路径均按目录获取；真实 Build 回归无需写入者即可返回 |
| 别名键和普通键重复时静默覆盖 | 按解析后的键值检查唯一性，两种顺序及嵌套元数据均拒绝 |

分配数据是独立局部复现的观测值，不是性能承诺。永久回归位于 `skills`、`codec/yaml`、`tools/skill` 和 Harness `preset`。

## 边界与发布衔接

- Catalog 的正文、元数据和摘要是发现时快照；附属资源是实时受限读取，不声明整包不可变或签名验证。
- 本地文件访问不是执行沙箱；脚本执行、授权、安装更新与上下文保留仍归宿主。
- Harness 的版本依赖尚未包含新公共包，当前验证使用本地 workspace。独立发布前须更新可获取的 GoAgent 版本并通过 `GOWORK=off`；本次不声明该发布门禁已通过。
- Harness 根模块测试不含独立 Postgres 模块和需显式启用的 Docker 场景；本次验收没有调用真实模型。
