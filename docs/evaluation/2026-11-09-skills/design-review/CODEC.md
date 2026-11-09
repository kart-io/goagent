# 编解码与格式契约续审

日期：2026-11-09。主审负责 `codec/yaml/mapping.go`、`mapping_test.go`，结合既有 `yaml.go`、`codec/json/json.go`、`skills/parse.go` 和 `parse_test.go` 核对转换语义。

## 确认发现 DC-01：二进制元数据被静默转换（P2，已修复）

原始输入为合法 UTF-8 文档：

```yaml
name: sample
description: !!binary /w==
```

`mappingGuard` 原先允许该标量进入共享转换。YAML 把二进制标签解为包含任意字节的 Go string；共享 JSON 编码遵循 v1 的替换规则，得到 U+FFFD。`DecodeMapping` 与 `skills.Parse` 均返回成功，说明内容已经改变。即使 `!!binary dXNlZnVs` 的字节是合法文本，也被误当成字符串字段接受。

修复在新映射入口的标量检查中拒绝 `!!binary`，包括嵌套映射、序列和别名所指的二进制值。字段继续使用文本类型；二进制素材走附属文件读取。没有改变旧 `Unmarshal`，没有向 Skills 添加编解码库或额外执行能力。

回归测试先在旧实现上失败，再在修复后通过；包括普通 UTF-8 二进制、无效 UTF-8 二进制、嵌套与别名，以及旧 `Unmarshal` 仍可解码正常二进制的兼容断言。证据见 `binary-before.log` 和 `targeted-race.log`。旧失败是预期红灯，不是当前未解决项。

## 设计判断

- 独立的 `DecodeMapping` 合理：Skills 需要有类型和容量约束的映射，现有 `Unmarshal` 有其他调用者的兼容契约。直接收紧全局接口会扩大迁移范围。
- 先检查 Node 再复用共享转换会解析两次。这是目前可接受的维护取舍，避免重写时间戳、数字精度和 JSON 选项规则。预算是保守估计，不是精确堆内存上限；没有把它表述为通用执行时限。
- 当前未发现需要新增 Provider、Resolver、Executor 或修改 core 执行循环的依据。新增修复仅是私有标量分支，不新增公共 API。
- [官方接入指南](https://agentskills.io/integrate-skills)明确允许发现时缓存正文；因此缓存正文并不违反面向模型的分层披露。资源实时读取和整包不可变是不同契约，现有指南已说明。
- [官方格式规范](https://agentskills.io/specification)对名称描述包含 Unicode 范围；实现保留 ASCII 名称子集，并已在公开指南明确说明。它是已披露的兼容范围，不应宣称支持所有标准名称。
- 官方接入指南建议容错加载和冲突诊断；当前严格失败、先源优先的策略已有文档。本轮不把建议误写成协议 MUST，也不自动引入可配置策略体系。

## 范围限制

这是格式正确性和维护性审查；不是重新执行先前被平台过滤拦截的独立安全审查。本轮没有调用真实模型，也没有修改通用 JSON 的兼容行为。
