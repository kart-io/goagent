package interfaces

import "testing"

func TestTokenUsageAdd(t *testing.T) {
	u := TokenUsage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3, CachedTokens: 4, ReasoningTokens: 5, ToolUsePromptTokens: 6}
	u.Add(&TokenUsage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30, CachedTokens: 40, ReasoningTokens: 50, ToolUsePromptTokens: 60})
	want := TokenUsage{PromptTokens: 11, CompletionTokens: 22, TotalTokens: 33, CachedTokens: 44, ReasoningTokens: 55, ToolUsePromptTokens: 66}
	if u != want {
		t.Fatalf("Add 后 = %+v，期望 %+v", u, want)
	}

	u.Add(nil)
	if u != want {
		t.Fatalf("Add(nil) 不应改变用量，实际 %+v", u)
	}
}

func TestTokenUsageIsEmpty(t *testing.T) {
	cases := []struct {
		name  string
		usage TokenUsage
		want  bool
	}{
		{"零值", TokenUsage{}, true},
		{"仅 PromptTokens", TokenUsage{PromptTokens: 1}, false},
		{"仅 CompletionTokens", TokenUsage{CompletionTokens: 1}, false},
		{"仅 TotalTokens", TokenUsage{TotalTokens: 1}, false},
		{"仅 CachedTokens", TokenUsage{CachedTokens: 1}, false},
		{"仅 ReasoningTokens", TokenUsage{ReasoningTokens: 1}, false},
		{"仅 ToolUsePromptTokens", TokenUsage{ToolUsePromptTokens: 1}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.usage.IsEmpty(); got != c.want {
				t.Fatalf("IsEmpty() = %v，期望 %v", got, c.want)
			}
		})
	}
}
