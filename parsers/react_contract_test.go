package parsers

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReActFinalAnswerFormats(t *testing.T) {
	for _, tt := range []struct{ text, want string }{
		{"Thought: ready\nFinal Answer: First paragraph.\n\nSecond paragraph.", "First paragraph.\n\nSecond paragraph."},
		{"ANSWER: 9", "9"},
		{"**Final Answer:**\n答案第一行\n答案第二行", "答案第一行\n答案第二行"},
		{"### 最终答案：9", "9"},
		{"Thought: ready\nFinal Answer: ANSWER: 9", "ANSWER: 9"},
	} {
		got, err := NewReActOutputParser().Parse(context.Background(), tt.text)
		require.NoError(t, err)
		require.Equal(t, tt.want, got.FinalAnswer)
	}
}
func TestReActDoesNotTreatQuotedMarkerAsAnswer(t *testing.T) {
	p := NewReActOutputParser()
	out, err := p.Parse(context.Background(), `Thought: search for Final Answer: example
Action: search
Action Input: {"query":"Final Answer: example"}`)
	require.NoError(t, err)
	require.Empty(t, out.FinalAnswer)
	require.Equal(t, "search", out.Action)
	for _, text := range []string{"Final Answer: ", "Thought: maybe ANSWER: 9", "unstructured prose"} {
		_, err = p.Parse(context.Background(), text)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = p.Parse(ctx, "ANSWER: 9")
	require.ErrorIs(t, err, context.Canceled)
}
