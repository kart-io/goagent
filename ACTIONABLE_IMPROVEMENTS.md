# 代码改进行动指南

本文档提供 goagent optimization 分支代码审查中发现的所有改进建议的具体实施指南。

---

## P0: 关键问题处理

### 任务 P0-1: 处理 TODO 注释

**文件**: `builder/reasoning_presets.go:262-264`

**当前代码**:
```go
	// TODO: Apply middlewares if configured
	// Middleware integration needs to be implemented based on
	// the actual middleware application pattern in GoAgent

	return agent
```

**问题分析**:
TODO 注释表示中间件集成功能尚未实现。当前代码已实现了回调处理(WithCallbacks),但中间件应用缺失。

**解决方案选项**:

**选项 1: 删除 TODO,保持当前实现**
```go
	// 中间件集成应与 WithCallbacks 保持一致,
	// 在需要时可在后续扩展
	return agent
```

**选项 2: 实现中间件应用** (推荐)
```go
	// 应用中间件(如果已配置)
	if len(b.middlewares) > 0 {
		// 假设 agent 实现了 WithMiddlewares 方法
		if withMiddleware, ok := agent.(interface{ WithMiddlewares(...middleware.Middleware) core.Agent }); ok {
			agent = withMiddleware.WithMiddlewares(b.middlewares...)
		}
	}
	
	return agent
```

**验证步骤**:
1. 检查 core.Agent 接口中的中间件支持
2. 验证推理 agent(CoT, ToT, ReAct)是否已实现中间件接口
3. 运行测试确保中间件正确应用

---

## P1: 重要改进

### 任务 P1-1: 补充验证器测试用例

**文件**: `tools/validator_test.go`

**当前状态**:
- 覆盖率: 64.1%
- 缺失的测试场景:
  1. 深层嵌套对象验证
  2. 大型数组性能测试  
  3. 并发验证
  4. 错误恢复

**实施方案**:

#### 测试 1: 深层嵌套对象

```go
// TestInputValidator_NestedObjectValidation 测试嵌套对象验证
func TestInputValidator_NestedObjectValidation(t *testing.T) {
	validator := NewInputValidator()
	
	// 3 层嵌套对象的 JSON Schema
	schema := `{
		"type": "object",
		"properties": {
			"user": {
				"type": "object",
				"properties": {
					"profile": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"age": {"type": "integer"}
						},
						"required": ["name"]
					}
				},
				"required": ["profile"]
			}
		},
		"required": ["user"]
	}`
	
	tool := NewBaseTool("test", "Test", schema, nil)
	
	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name: "valid nested structure",
			args: map[string]interface{}{
				"user": map[string]interface{}{
					"profile": map[string]interface{}{
						"name": "John",
						"age":  30,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing nested required field",
			args: map[string]interface{}{
				"user": map[string]interface{}{
					"profile": map[string]interface{}{
						"age": 30,
					},
				},
			},
			wantErr: true, // name is required
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{Args: tt.args}
			err := validator.Validate(context.Background(), tool, input)
			
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
```

#### 测试 2: 大型数组性能

```go
// TestInputValidator_LargeArrayPerformance 测试大型数组验证性能
func TestInputValidator_LargeArrayPerformance(t *testing.T) {
	validator := NewInputValidator()
	
	schema := `{
		"type": "object",
		"properties": {
			"items": {
				"type": "array",
				"items": {"type": "string"}
			}
		},
		"required": ["items"]
	}`
	
	tool := NewBaseTool("test", "Test", schema, nil)
	
	// 生成 10,000 元素的数组
	largeArray := make([]interface{}, 10000)
	for i := 0; i < 10000; i++ {
		largeArray[i] = fmt.Sprintf("item_%d", i)
	}
	
	input := &interfaces.ToolInput{
		Args: map[string]interface{}{
			"items": largeArray,
		},
	}
	
	start := time.Now()
	err := validator.Validate(context.Background(), tool, input)
	duration := time.Since(start)
	
	assert.NoError(t, err)
	// 验证性能不差于 100ms
	assert.Less(t, duration, 100*time.Millisecond,
		"large array validation should complete within 100ms")
	
	t.Logf("Large array validation took: %v", duration)
}
```

#### 测试 3: 并发验证

```go
// TestInputValidator_Concurrent 测试并发验证的线程安全性
func TestInputValidator_Concurrent(t *testing.T) {
	validator := NewInputValidator()
	
	schema := `{
		"type": "object",
		"properties": {
			"value": {"type": "string"}
		},
		"required": ["value"]
	}`
	
	tool := NewBaseTool("test", "Test", schema, nil)
	
	numGoroutines := 100
	var wg sync.WaitGroup
	errors := make([]error, numGoroutines)
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			
			input := &interfaces.ToolInput{
				Args: map[string]interface{}{
					"value": fmt.Sprintf("value_%d", idx),
				},
			}
			
			errors[idx] = validator.Validate(context.Background(), tool, input)
		}(i)
	}
	
	wg.Wait()
	
	// 所有验证应该成功
	for i, err := range errors {
		assert.NoError(t, err, "validation failed for goroutine %d", i)
	}
}
```

#### 测试 4: 错误恢复

```go
// TestInputValidator_ErrorRecovery 测试验证器错误恢复能力
func TestInputValidator_ErrorRecovery(t *testing.T) {
	validator := NewInputValidator()
	
	schema := `{
		"type": "object",
		"properties": {
			"value": {"type": "integer"}
		}
	}`
	
	tool := NewBaseTool("test", "Test", schema, nil)
	
	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name:    "first validation succeeds",
			args:    map[string]interface{}{"value": 10},
			wantErr: false,
		},
		{
			name:    "second validation fails",
			args:    map[string]interface{}{"value": "invalid"},
			wantErr: true,
		},
		{
			name:    "third validation succeeds after failure",
			args:    map[string]interface{}{"value": 20},
			wantErr: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := &interfaces.ToolInput{Args: tt.args}
			err := validator.Validate(context.Background(), tool, input)
			
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
```

**执行步骤**:
1. 将上述测试函数添加到 `tools/validator_test.go`
2. 运行测试: `go test ./tools -v -run "NestedObject|LargeArray|Concurrent|ErrorRecovery"`
3. 验证所有测试通过
4. 检查覆盖率: `go test ./tools -cover`

---

### 任务 P1-2: 统一错误处理

**文件**: `tools/validator.go`

**问题分析**:
当前存在两种错误处理方式混用的情况:
1. 公开方法: 使用 agentErrors.New() 包装错误
2. 内部方法: 使用 fmt.Errorf() 返回错误

这导致错误链路的一致性问题。

**解决方案**:

将所有内部方法的错误处理改为统一使用 agentErrors:

```go
// 旧的 validateRequired 方法
func (v *InputValidator) validateRequired(s *schema, args map[string]interface{}) error {
	for _, required := range s.Required {
		if _, exists := args[required]; !exists {
			return fmt.Errorf("required parameter '%s' is missing", required)
		}
	}
	return nil
}

// 新的 validateRequired 方法 (使用 agentErrors)
func (v *InputValidator) validateRequired(s *schema, args map[string]interface{}) error {
	for _, required := range s.Required {
		if _, exists := args[required]; !exists {
			return agentErrors.New(
				agentErrors.CodeToolValidation,
				fmt.Sprintf("required parameter '%s' is missing", required),
			).WithComponent("input_validator")
		}
	}
	return nil
}
```

类似地更新:
- `validateTypes()`
- `validateType()`
- `validateNoExtraArgs()`
- `validateStringWithSchema()`
- `validateNumberWithSchema()`
- `validateBooleanWithSchema()`
- `validateArrayWithSchema()`
- `validateObjectWithSchema()`
- `validateFormat()`

**验证步骤**:
1. 编辑 `tools/validator.go` 中的所有内部验证函数
2. 运行所有测试: `go test ./tools -v`
3. 确认所有测试仍然通过

---

## P2: 可选优化

### 任务 P2-1: 提取配置合并逻辑

**文件**: `builder/reasoning_presets.go`

**问题分析**:
多个 WithXxx 方法中有大量重复的配置合并代码。建议提取为公共工具函数。

**重复代码示例** (WithChainOfThought 和 WithTreeOfThought 都有):
```go
if len(config) > 0 {
    provided := config[0]
    if provided.Name != "" {
        cfg.Name = provided.Name
    }
    if provided.Description != "" {
        cfg.Description = provided.Description
    }
    // ... 更多字段合并
}
```

**改进方案**:

创建公共的配置合并函数:

```go
// mergeConfig 用于合并配置字段
// 这个函数使用反射来减少重复代码
func mergeConfig(target interface{}, source interface{}, fieldsToMerge ...string) {
	targetVal := reflect.ValueOf(target).Elem()
	sourceVal := reflect.ValueOf(source)
	
	for _, fieldName := range fieldsToMerge {
		sourceField := sourceVal.FieldByName(fieldName)
		if !sourceField.IsValid() {
			continue
		}
		
		targetField := targetVal.FieldByName(fieldName)
		if !targetField.IsValid() {
			continue
		}
		
		// 根据字段类型决定是否合并
		switch sourceField.Kind() {
		case reflect.String:
			if sourceField.String() != "" {
				targetField.SetString(sourceField.String())
			}
		case reflect.Int, reflect.Int64:
			if sourceField.Int() > 0 {
				targetField.SetInt(sourceField.Int())
			}
		case reflect.Bool:
			// 对于 bool 字段,如果 source 为 true 就设置
			if sourceField.Bool() {
				targetField.SetBool(sourceField.Bool())
			}
		case reflect.Slice:
			if sourceField.Len() > 0 {
				targetField.Set(sourceField)
			}
		case reflect.Float64:
			if sourceField.Float() > 0 {
				targetField.SetFloat(sourceField.Float())
			}
		}
	}
}

// 使用示例
func (b *AgentBuilder[C, S]) WithChainOfThought(config ...cot.CoTConfig) *AgentBuilder[C, S] {
	cfg := cot.CoTConfig{
		Name:        "chain-of-thought",
		Description: "Agent that uses step-by-step reasoning",
		LLM:         b.llmClient,
		Tools:       b.tools,
		MaxSteps:    10,
		ZeroShot:    true,
	}

	// 使用 mergeConfig 简化配置合并
	if len(config) > 0 {
		mergeConfig(&cfg, config[0], 
			"Name", "Description", "MaxSteps", 
			"ShowStepNumbers", "RequireJustification",
			"FinalAnswerFormat", "FewShot", "FewShotExamples",
		)
	}

	b.metadata["reasoning_pattern"] = "cot"
	b.metadata["cot_config"] = cfg

	return b
}
```

**替代方案** (简单版本,不使用反射):

```go
// mergeCoTConfig 合并 CoT 配置
func mergeCoTConfig(target *cot.CoTConfig, source cot.CoTConfig) {
	if source.Name != "" {
		target.Name = source.Name
	}
	if source.Description != "" {
		target.Description = source.Description
	}
	if source.MaxSteps > 0 {
		target.MaxSteps = source.MaxSteps
	}
	if source.ShowStepNumbers {
		target.ShowStepNumbers = source.ShowStepNumbers
	}
	if source.RequireJustification {
		target.RequireJustification = source.RequireJustification
	}
	if source.FinalAnswerFormat != "" {
		target.FinalAnswerFormat = source.FinalAnswerFormat
	}
	if source.FewShot {
		target.FewShot = source.FewShot
		target.ZeroShot = false
	}
	if len(source.FewShotExamples) > 0 {
		target.FewShotExamples = source.FewShotExamples
	}
}

// 使用示例
func (b *AgentBuilder[C, S]) WithChainOfThought(config ...cot.CoTConfig) *AgentBuilder[C, S] {
	cfg := cot.CoTConfig{/* 默认值 */}
	
	if len(config) > 0 {
		mergeCoTConfig(&cfg, config[0])
	}
	
	return b
}
```

**执行步骤**:
1. 选择一个合并方案(推荐简单版本)
2. 创建新的合并函数
3. 更新所有 WithXxx 方法
4. 运行测试确保功能不变: `go test ./builder -v`

---

### 任务 P2-2: 增强验证器的粒度控制

**文件**: `tools/validator.go`

**现状**:
当前 InputValidator 只有 3 个开关:
- ValidateTypes (全有或全无)
- ValidateRequired (全有或全无)
- StrictMode (全有或全无)

**改进方案**:

添加更细粒度的验证选项:

```go
// InputValidator 提供工具输入验证功能
type InputValidator struct {
	// 基础验证开关
	StrictMode           bool
	ValidateTypes        bool
	ValidateRequired     bool
	
	// 新增: 细粒度控制
	ValidateConstraints  bool  // 验证 min/max/length 等约束
	ValidateEnums        bool  // 验证枚举值
	ValidatePatterns     bool  // 验证正则表达式
	ValidateFormats      bool  // 验证特殊格式(email/uri/uuid)
	AllowNullValues      bool  // 是否允许 null 值
	AllowAdditionalProps bool // 是否允许额外的属性
	
	// 性能相关
	MaxNestingDepth      int   // 最大嵌套深度(0 表示无限制)
	MaxArraySize         int   // 最大数组元素数(0 表示无限制)
	MaxStringLength      int   // 最大字符串长度(0 表示无限制)
}

// NewInputValidator 创建默认配置的验证器
func NewInputValidator() *InputValidator {
	return &InputValidator{
		StrictMode:           false,
		ValidateTypes:        true,
		ValidateRequired:     true,
		ValidateConstraints:  true,
		ValidateEnums:        true,
		ValidatePatterns:     true,
		ValidateFormats:      true,
		AllowNullValues:      true,
		AllowAdditionalProps: true,
		MaxNestingDepth:      10,
		MaxArraySize:         10000,
		MaxStringLength:      1000000,
	}
}

// NewLaxValidator 创建宽松模式的验证器
func NewLaxValidator() *InputValidator {
	return &InputValidator{
		StrictMode:           false,
		ValidateTypes:        true,
		ValidateRequired:     false,
		ValidateConstraints:  false,
		ValidateEnums:        false,
		ValidatePatterns:     false,
		ValidateFormats:      false,
		AllowNullValues:      true,
		AllowAdditionalProps: true,
	}
}

// 在 Validate 方法中使用这些开关
func (v *InputValidator) validateStringWithSchema(fieldName string, value interface{}, schema *core.PropertySchema) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("expected string")
	}

	// 仅在启用时验证约束
	if v.ValidateConstraints {
		if schema.MinLength != nil && len(str) < *schema.MinLength {
			return fmt.Errorf("length must be at least %d", *schema.MinLength)
		}
		if schema.MaxLength != nil && len(str) > *schema.MaxLength {
			return fmt.Errorf("length must not exceed %d", *schema.MaxLength)
		}
	}

	// 仅在启用时验证枚举
	if v.ValidateEnums && len(schema.Enum) > 0 {
		found := false
		for _, enum := range schema.Enum {
			if str == enum {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("must be one of %v", schema.Enum)
		}
	}

	// 仅在启用时验证模式
	if v.ValidatePatterns && schema.Pattern != "" {
		matched, err := regexp.MatchString(schema.Pattern, str)
		if err != nil || !matched {
			return fmt.Errorf("does not match pattern %s", schema.Pattern)
		}
	}

	// 仅在启用时验证格式
	if v.ValidateFormats && schema.Format != "" {
		if err := validateFormat(str, schema.Format); err != nil {
			return err
		}
	}

	return nil
}
```

**执行步骤**:
1. 更新 InputValidator 结构体添加新字段
2. 更新工厂函数
3. 在验证逻辑中添加开关检查
4. 添加测试覆盖这些新选项

---

### 任务 P2-3: 添加性能监测

**文件**: `tools/validator.go`

**改进方案**:

```go
// ValidationMetrics 包含验证性能指标
type ValidationMetrics struct {
	TotalDurationMs   int64  // 总耗时
	SchemaParseDurationMs int64  // Schema 解析耗时
	ValidationDurationMs  int64  // 验证逻辑耗时
	ValidationsPerformed  int    // 执行的验证数
	ValidationsPassed     int    // 通过的验证数
	ValidationsFailed     int    // 失败的验证数
}

// ValidateWithMetrics 执行验证并返回性能指标
func (v *InputValidator) ValidateWithMetrics(
	ctx context.Context,
	tool interfaces.Tool,
	input *interfaces.ToolInput,
) (*ValidationMetrics, error) {
	metrics := &ValidationMetrics{}
	
	start := time.Now()
	
	// 解析 Schema
	parseStart := time.Now()
	schema, err := v.parseSchema(tool.ArgsSchema())
	metrics.SchemaParseDurationMs = time.Since(parseStart).Milliseconds()
	
	if err != nil {
		return metrics, err
	}
	
	// 执行验证
	validationStart := time.Now()
	if err := v.Validate(ctx, tool, input); err != nil {
		metrics.ValidationDurationMs = time.Since(validationStart).Milliseconds()
		metrics.TotalDurationMs = time.Since(start).Milliseconds()
		metrics.ValidationsFailed = 1
		return metrics, err
	}
	
	metrics.ValidationDurationMs = time.Since(validationStart).Milliseconds()
	metrics.TotalDurationMs = time.Since(start).Milliseconds()
	metrics.ValidationsPassed = 1
	
	return metrics, nil
}

// 使用示例
func ExampleMetrics() {
	validator := NewInputValidator()
	tool := &MyTool{}
	input := &interfaces.ToolInput{Args: map[string]interface{}{}}
	
	metrics, err := validator.ValidateWithMetrics(context.Background(), tool, input)
	if err != nil {
		log.Printf("Validation failed: %v", err)
		return
	}
	
	log.Printf("Validation metrics: %+v", metrics)
	if metrics.TotalDurationMs > 100 {
		log.Warn("Validation is slow, consider optimization")
	}
}
```

**执行步骤**:
1. 定义 ValidationMetrics 结构体
2. 创建 ValidateWithMetrics 方法
3. 在关键位置添加时间测量
4. 添加测试验证指标

---

## 实施优先级建议

### 第 1 周 (优先级 P0-P1)
- [ ] 处理 TODO 注释 (1 小时)
- [ ] 补充验证器测试 (3-4 小时)
- [ ] 统一错误处理 (2-3 小时)

### 第 2-3 周 (优先级 P2)
- [ ] 提取配置合并逻辑 (2-3 小时)
- [ ] 增强验证器粒度控制 (2-3 小时)
- [ ] 添加性能监测 (1-2 小时)

---

## 验证清单

完成每项改进后,执行以下验证:

```bash
# 1. 编译
go build ./...

# 2. 格式化
go fmt ./builder ./tools

# 3. 静态分析
go vet ./builder ./tools

# 4. 单元测试
go test ./builder ./tools -v

# 5. 覆盖率检查
go test ./builder ./tools -cover

# 6. 竞态检测 (P1-3 完成后)
go test ./builder ./tools -race

# 7. 基准测试 (P2-3 完成后)
go test -bench=. ./tools
```

---

**更新时间**: 2025-11-30  
**维护人**: Code Review Team  
