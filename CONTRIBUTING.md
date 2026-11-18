# Contributing to GoAgent

[English](#english) | [中文](#中文)

## English

Thanks for your interest in GoAgent. Bug reports, questions, documentation fixes and pull requests are all welcome.

### Before you start

- For a bug, open an [issue](https://github.com/kart-io/goagent/issues/new/choose) with a minimal reproduction.
- For a new feature or an API change, open an issue first so the design can be discussed before you write code.
- GoAgent is pre-1.0; breaking changes are allowed but must be listed with migration notes in [CHANGELOG.md](CHANGELOG.md) under `Unreleased`.

### Development setup

Requires the Go version declared in [go.mod](go.mod).

```bash
git clone https://github.com/kart-io/goagent.git
cd goagent
go test ./...
```

Most tests and examples run offline with mock models; examples that call a real LLM read the provider's API key from the environment.

### Checks to run before opening a PR

These match what the PR workflow runs:

```bash
make fmt                          # gofmt
bash scripts/verify_imports.sh    # import layering rules
go vet ./...
go test -race ./...
golangci-lint run
```

New behavior needs tests. Prefer table-driven tests that call the public API and assert what a caller can observe.

### Commits and pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org/): `feat(scope): ...`, `fix(scope): ...`, `docs: ...`, `test: ...`, `chore: ...`; mark breaking changes with `!` (e.g. `feat(builder)!: ...`).
- Commit types drive automatic releases after merge (`feat` → minor, `fix`/`perf`/`refactor` → patch), so choose them carefully.
- Keep one logical change per PR and explain *why* in the description.

### License

By contributing you agree that your contributions are licensed under the [Apache License 2.0](LICENSE).

## 中文

欢迎提交问题、提问、文档修正与 PR。

- 报告 Bug 请附最小复现；新功能或 API 变更请先开 Issue 讨论设计，再写代码。
- 项目尚未发布 1.0，允许破坏性变更，但必须在 [CHANGELOG.md](CHANGELOG.md) 的 `Unreleased` 下写明迁移说明。
- 提 PR 前运行上面 “Checks to run before opening a PR” 中的命令，与 PR 流水线一致；新行为需要测试，优先表驱动、通过公开 API 断言调用方可见的结果。
- 提交信息遵循 Conventional Commits；合并后会按类型自动发版（`feat` 升次版本，`fix`/`perf`/`refactor` 升修订版本），`!` 表示破坏性变更。
- 提交即表示同意以 [Apache License 2.0](LICENSE) 授权你的贡献。
