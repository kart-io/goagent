# 真实模型 HITL 验证

在仓库根目录运行，凭据从 `GEMINI_API_KEY`、`DEEPSEEK_API_KEY` 环境变量读取：

```sh
python3 scripts/hitl_live.py --live --out docs/evaluation/NEW-HITL-RUN
```

输出目录必须不存在。默认测试 `gemini-2.5-flash` 与 `deepseek-flash`，可通过 `GEMINI_MODEL` / `DEEPSEEK_MODEL` 指定模型，或用 `--providers deepseek` 仅测试一家。每家最多 3 次调用，每次最多 512 输出 Token，不自动重试；真实请求可能计费。

脚本编译 [Go 探针](cmd/run/main.go)，每阶段启动独立进程。使用合成订单和脚本提交的人工决定，验证：起草后暂停、等待重放、无效键/身份/金额拒绝、人工修改 88→66、修改保存后 `os.Exit(86)`、新进程恢复生成、完成重放、过期响应、拒绝分支。每个场景配置持久预算，审批场景最多两次模型调用，拒绝场景一次。

结果包含原始 stdout/stderr、检查点、用量、`results.json`、`review.json` 和 SHA256 清单。成功要求执行断言与内容检查都通过。内容检查是此中文固定样例的字面规则，并不是通用事实核验器；支付、登录认证与真实人工审批均未接入。业务中的支付结果通知应绑定实际执行结果。

只复核已保存结果，不调用模型：

```sh
python3 scripts/hitl_live.py --review docs/evaluation/2026-11-04-hitl-live
```

该历史样本应返回退出码 1：Gemini 虽采用正确的 66 元，仍错误声称退款已完成。原始运行只检查流程与金额，其 `results.json.passed=true` 不包含后补的内容检查；综合结论见 `review.json`。[完整报告](../../docs/evaluation/2026-11-04-hitl-live/REPORT.md)保留了这一评测遗漏及原始驱动，不重新请求模型修饰结果。
