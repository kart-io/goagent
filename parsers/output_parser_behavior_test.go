package parsers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shared2 "github.com/kart-io/goagent/constants"
	agentErrors "github.com/kart-io/goagent/errors"
)

type personOutput struct {
	Name string `json:"name,omitempty" description:"姓名"`
	Age  int    `description:"年龄"`
}

func TestBaseOutputParser_Defaults(t *testing.T) {
	base := NewBaseOutputParser[int]()
	assert.Equal(t, "int", base.GetType())
	assert.Empty(t, base.GetFormatInstructions())

	_, err := base.Parse(context.Background(), "1")
	require.Error(t, err)

	assert.Equal(t, "unknown", NewBaseOutputParser[any]().GetType(), "接口类型的零值没有反射类型")
}

func TestJSONOutputParser_FormatInstructions(t *testing.T) {
	got := NewJSONOutputParser[personOutput](false).GetFormatInstructions()
	assert.Contains(t, got, "- `name`: (string) 姓名", "应取 json 标签中逗号前的名称")
	assert.Contains(t, got, "- `Age`: (int) 年龄", "无 json 标签时回退到字段名")
	assert.Contains(t, got, "```json")

	nonStruct := NewJSONOutputParser[[]string](false).GetFormatInstructions()
	assert.NotContains(t, nonStruct, "following fields")
}

func TestJSONOutputParser_ParseFailures(t *testing.T) {
	ctx := context.Background()

	_, err := NewJSONOutputParser[map[string]any](false).Parse(ctx, "纯文本")
	assert.ErrorIs(t, err, agentErrors.ErrParseFailed)

	_, err = NewJSONOutputParser[personOutput](false).Parse(ctx, `{"name": 1}`)
	assert.Error(t, err, "字段类型不符应失败")

	_, err = NewJSONOutputParser[map[string]any](true).Parse(ctx, `{"a": 1`)
	assert.ErrorIs(t, err, agentErrors.ErrParseFailed, "严格模式不接受未闭合的 JSON")

	_, err = NewJSONOutputParser[map[string]any](false).Parse(ctx, `{"a": 1`)
	require.Error(t, err, "宽松模式返回残片，但残片本身仍无法解析")
	assert.NotErrorIs(t, err, agentErrors.ErrParseFailed)
}

func TestJSONOutputParser_ExtractionEdges(t *testing.T) {
	p := NewJSONOutputParser[map[string]any](false)
	padding := strings.Repeat("x", 2000)

	tests := []struct {
		name string
		text string
		want string
	}{
		{"未闭合的代码块回退到括号扫描", "```json\n{\"a\":1}", `{"a":1}`},
		{"大文本中的代码块", padding + "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"大文本中的对象", padding + `{"a":1}` + padding, `{"a":1}`},
		{"大文本末尾的未闭合残片", padding + `{"a":1`, `{"a":1`},
		{"数组优先于其后的对象", `[1] {"a":1}`, `[1]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, p.extractJSON(tt.text))
			assert.Equal(t, tt.want, p.extractJSONOptimized(tt.text))
		})
	}
}

func TestStructuredOutputParser(t *testing.T) {
	type output struct {
		Title string `json:"title"`
		Note  string `json:"note"`
	}
	schema := map[string]FieldSchema{
		"title": {Name: "title", Type: "string", Description: "标题", Required: true},
		"note":  {Name: "note", Type: "string", Description: "备注"},
	}
	p := NewStructuredOutputParser[output](schema)
	ctx := context.Background()

	got, err := p.Parse(ctx, "title: 你好\nnote:世界\n其他内容")
	require.NoError(t, err)
	assert.Equal(t, output{Title: "你好", Note: "世界"}, got)

	got, err = p.Parse(ctx, "**title**: 粗体")
	require.NoError(t, err)
	assert.Equal(t, output{Title: "粗体"}, got, "可选字段缺失不报错")

	_, err = p.Parse(ctx, "note: 缺少必需字段")
	assert.ErrorIs(t, err, agentErrors.ErrMissingField)

	instructions := p.GetFormatInstructions()
	assert.Contains(t, instructions, "**title** (REQUIRED): 标题")
	assert.Contains(t, instructions, "**note**: 备注")
	assert.Contains(t, instructions, "title: <value>")
}

func TestListOutputParser(t *testing.T) {
	ctx := context.Background()

	got, err := NewListOutputParser("").Parse(ctx, "1. 苹果\n\n- 香蕉\n* 橙子\n• 葡萄\n")
	require.NoError(t, err)
	assert.Equal(t, []string{"苹果", "香蕉", "橙子", "葡萄"}, got)

	got, err = NewListOutputParser(",").Parse(ctx, "苹果, 香蕉 ,橙子")
	require.NoError(t, err)
	assert.Equal(t, []string{"苹果", "香蕉", "橙子"}, got)

	got, err = NewListOutputParser(",").Parse(ctx, "苹果\n香蕉")
	require.NoError(t, err)
	assert.Equal(t, []string{"苹果", "香蕉"}, got, "已按行拆分时不再应用分隔符")

	got, err = NewListOutputParser("").Parse(ctx, "  ")
	require.NoError(t, err)
	assert.Empty(t, got)

	assert.Contains(t, NewListOutputParser("").GetFormatInstructions(), "one item per line")
	assert.Contains(t, NewListOutputParser(";").GetFormatInstructions(), "separated by ';'")
}

func TestEnumOutputParser(t *testing.T) {
	ctx := context.Background()
	allowed := []string{"Red", "Green"}

	got, err := NewEnumOutputParser(allowed, false).Parse(ctx, "  rED ")
	require.NoError(t, err)
	assert.Equal(t, "Red", got, "大小写不敏感时返回标准写法")

	got, err = NewEnumOutputParser(allowed, true).Parse(ctx, "Green")
	require.NoError(t, err)
	assert.Equal(t, "Green", got)

	_, err = NewEnumOutputParser(allowed, true).Parse(ctx, "green")
	assert.ErrorIs(t, err, agentErrors.ErrInvalidFormat)

	_, err = NewEnumOutputParser(allowed, false).Parse(ctx, "Blue")
	assert.ErrorIs(t, err, agentErrors.ErrInvalidFormat)

	assert.Contains(t, NewEnumOutputParser(allowed, true).GetFormatInstructions(), "Red, Green")
}

func TestBooleanOutputParser(t *testing.T) {
	p := NewBooleanOutputParser()
	ctx := context.Background()

	for text, want := range map[string]bool{
		"yes": true, " TRUE ": true, "是": true, "1": true,
		"no": false, "False": false, "否": false, "0": false,
	} {
		got, err := p.Parse(ctx, text)
		require.NoError(t, err, text)
		assert.Equal(t, want, got, text)
	}

	_, err := p.Parse(ctx, "abc")
	assert.ErrorIs(t, err, agentErrors.ErrParseFailed)

	assert.Contains(t, p.GetFormatInstructions(), "'yes' or 'no'")
}

func TestRegexOutputParser(t *testing.T) {
	ctx := context.Background()
	p := NewRegexOutputParser(map[string]string{
		"id":     `id=(\d+)`,
		"word":   `hello`,
		"absent": `nothing-(\d+)`,
	})

	got, err := p.Parse(ctx, "hello id=42")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"id": "42", "word": "hello"}, got, "未匹配的字段不写入结果")

	_, err = NewRegexOutputParser(map[string]string{"bad": `(`}).Parse(ctx, "x")
	assert.Error(t, err)

	assert.NotEmpty(t, p.GetFormatInstructions())
}

func TestChainOutputParser(t *testing.T) {
	ctx := context.Background()
	strict := NewEnumOutputParser([]string{"a"}, true)
	fallback := NewEnumOutputParser([]string{"b"}, true)
	chain := NewChainOutputParser[string](strict, fallback)

	got, err := chain.Parse(ctx, "a")
	require.NoError(t, err)
	assert.Equal(t, "a", got)

	got, err = chain.Parse(ctx, "b")
	require.NoError(t, err)
	assert.Equal(t, "b", got, "前一个解析器失败后尝试下一个")

	_, err = chain.Parse(ctx, "c")
	assert.ErrorIs(t, err, agentErrors.ErrInvalidFormat, "保留最后一个解析器的错误")

	assert.Equal(t, strict.GetFormatInstructions(), chain.GetFormatInstructions())
	assert.Equal(t, "string", chain.GetType())

	empty := NewChainOutputParser[string]()
	_, err = empty.Parse(ctx, "a")
	require.Error(t, err, "空链不能返回成功")
	assert.True(t, errors.Is(err, agentErrors.ErrNoParsers))
	assert.Empty(t, empty.GetFormatInstructions())
	assert.Equal(t, "unknown", empty.GetType())
}

func TestReActOutputParser_Parse(t *testing.T) {
	p := NewReActOutputParser()
	ctx := context.Background()

	got, err := p.Parse(ctx, "Thought: 需要查询\nAction: search\nAction Input: {\"q\":\"go\"}")
	require.NoError(t, err)
	assert.Equal(t, "需要查询", got.Thought)
	assert.Equal(t, "search", got.Action)
	assert.Equal(t, map[string]any{"q": "go"}, got.ActionInput)

	got, err = p.Parse(ctx, "Action: search\nAction Input: 纯文本")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"input": "纯文本"}, got.ActionInput, "非 JSON 输入按字符串包装")

	got, err = p.Parse(ctx, "Final Answer: 完成\n第二行")
	require.NoError(t, err)
	assert.Equal(t, "完成\n第二行", got.FinalAnswer)

	_, err = p.Parse(ctx, "Thought: 只有思考")
	assert.Error(t, err, "没有行动也没有答案")

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = p.Parse(canceled, "Final Answer: 完成")
	assert.ErrorIs(t, err, context.Canceled)

	assert.Contains(t, p.GetFormatInstructions(), shared2.MarkerFinalAnswer)
}

func TestReActOutputParser_ParseWithRetry(t *testing.T) {
	p := NewReActOutputParser()
	ctx := context.Background()

	got, err := p.ParseWithRetry(ctx, "Final Answer: 完成", 2)
	require.NoError(t, err)
	assert.Equal(t, "完成", got.FinalAnswer)

	_, err = p.ParseWithRetry(ctx, "无效内容", 2)
	assert.Error(t, err)

	_, err = p.ParseWithRetry(ctx, "Final Answer: 完成", 0)
	assert.Error(t, err, "未尝试解析时不能返回成功")
}

func TestReActOutputParser_Validate(t *testing.T) {
	p := NewReActOutputParser()
	input := map[string]any{"q": "go"}

	assert.Error(t, p.Validate(nil))
	assert.NoError(t, p.Validate(&ReActOutput{FinalAnswer: "完成"}))
	assert.Error(t, p.Validate(&ReActOutput{ActionInput: input}), "缺少 action")
	assert.Error(t, p.Validate(&ReActOutput{Action: "search"}), "缺少 action_input")
	assert.NoError(t, p.Validate(&ReActOutput{Action: "search", ActionInput: input}))
}
