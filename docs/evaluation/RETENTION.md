# 评测资料保留约定

报告、输入数据、原始结果、成功和失败日志、运行清单，以及用于生成和校验结果的脚本均是评测依据，保留在版本库中。不要使用全局 `*.log` 忽略或批量清理来删除这些资料。

- 新一轮验证使用独立日期目录或运行 ID，保留旧结论和失败记录；不要覆盖历史原始数据。
- 更正结论时新增说明，标明原始基线、修复提交、运行命令、环境和适用范围。
- 保留报告中的相对链接和脚本依赖。重新测试不能替代已经发生的真实模型调用记录。
- 临时缓存、字节码和未归档输出可忽略；正式归档前检查凭据及个人信息，必要脱敏须明确记录，不能编造原始结果。
- 本机路径脱敏只替换路径前缀，其余内容保持原样：仓库根目录写作 `<repo>`，评测用 Python 虚拟环境写作 `<venv>`，Python 安装目录写作 `<python>`。

2026-10-28：在 `codex/composable-orchestration-r2` 反向撤销清理提交 `92bf21b`，恢复其删除的 196 份日志和 6 个 Python 脚本，并恢复相关报告和复现说明。历史文件取自该清理提交的父提交，恢复不表示重新运行评测。另保留本次整合前的独立本地备份。

2026-10-29：业务验证案例迁出到 [goagent-examples](https://github.com/kart-io/goagent-examples)，来源提交为 `d429c47`。迁出内容为 `examples/workflow/business-cases`、`examples/workflow/refund-recovery`、`examples/orchestration`、`examples/internal/livemodel`，以及 `scripts/business_cases_live.sh`、`scripts/business_tool_loop_live.sh`；在新仓库中去掉 `examples/` 前缀，脚本仍在 `scripts/`。历史报告与日志不改写：其中指向上述路径的相对链接和命令以 goagent `d429c47` 为准，新的评测在 goagent-examples 中运行。
