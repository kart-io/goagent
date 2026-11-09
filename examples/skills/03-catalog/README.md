# 本地 Skills Catalog

从 GoAgent 仓库根目录运行：

```sh
env -u GOROOT go run ./examples/skills/03-catalog
```

案例依次展示元数据目录、技能正文与资源列表，再用 `load_skill` 读取参考资料。
它使用合成的 FAQ 技能，不调用模型、不依赖 Harness。`-dir` 可指定自己的技能目录；
演示读取的技能名固定为 `faq`。

实际应用可以把 `skilltool.New(catalog)` 交给 `Builder.WithTools`，自行选择要展示的
元数据并组装提示词。停止使用工具后关闭 Catalog。完整边界见
[Skills 加载指南](../../../docs/guides/SKILLS.md)。
