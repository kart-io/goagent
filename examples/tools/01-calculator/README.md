# 01-calculator 计算器工具示例

演示 `tools/compute` 中的两种计算器工具，不调用 LLM。

## 运行

```bash
go run ./examples/tools/01-calculator
```

## 内容

1. **`compute.NewCalculatorTool()`**（工具名 `calculator`）：参数 `expression`，支持 `+ - * / ^` 和括号，例如 `2^3 + 4 * 5 = 28`。
2. **`compute.NewAdvancedCalculatorTool(compute.CalculatorOperations{...})`**（工具名 `advanced_calculator`）：参数 `operation` 与 `operands`。示例开启了 `Add/Subtract/Multiply/Divide/Power/Sqrt/Abs`，并调用 `add`、`subtract`、`multiply`、`divide`、`power`、`sqrt`、`abs`、`sin`、`cos`、`log`、`ln`。
3. **错误处理**：`10 / 0`、`(1 + 2`、`abc` 都会让 `Invoke` 返回错误，如 `division by zero`、`mismatched parentheses`。

```go
calculator := compute.NewCalculatorTool()
output, err := calculator.Invoke(ctx, &interfaces.ToolInput{
    Args:    map[string]interface{}{"expression": "(10 + 5) * 2"},
    Context: ctx,
})
// output.Success == true, output.Result == 30
```
