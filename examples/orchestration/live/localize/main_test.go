package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/builder/contextpolicy"
	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/kart-io/goagent/examples/orchestration/internal/demomodel"
	"github.com/kart-io/goagent/examples/orchestration/internal/livecase"
	"github.com/kart-io/goagent/examples/orchestration/internal/livecase/livecasetest"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func TestMissingPhrases(t *testing.T) {
	require.Empty(t, MissingPhrases("Aurora Wireless Earbuds: Delivery Confirmation, No-Questions-Asked Refund, original payment method, 3 business days"))
	require.Equal(t, []string{"business days"}, MissingPhrases("aurora wireless earbuds delivery confirmation no-questions-asked refund original payment method 3 working days"))
	require.Equal(t, RequiredPhrases, MissingPhrases(""))
}

func TestGlossaryPolicy(t *testing.T) {
	a, err := newGlossaryPolicy(DefaultGlossary)
	require.NoError(t, err)
	b, err := newGlossaryPolicy(Glossary{Version: "v1", Terms: append([][2]string(nil), DefaultGlossary.Terms...)})
	require.NoError(t, err)
	require.Equal(t, a.Descriptor(), b.Descriptor(), "相同配置得到相同描述")
	require.True(t, a.Descriptor().Durable)

	changed := Glossary{Version: "v1", Terms: append([][2]string{{"签收", "receipt"}}, DefaultGlossary.Terms[1:]...)}
	c, err := newGlossaryPolicy(changed)
	require.NoError(t, err)
	require.Equal(t, a.Descriptor().Version, c.Descriptor().Version)
	require.NotEqual(t, a.Descriptor().ConfigHash, c.Descriptor().ConfigHash, "同版本改术语也必须改变哈希")
	reordered := Glossary{Version: "v1", Terms: [][2]string{DefaultGlossary.Terms[1], DefaultGlossary.Terms[0], DefaultGlossary.Terms[2], DefaultGlossary.Terms[3]}}
	d, err := newGlossaryPolicy(reordered)
	require.NoError(t, err)
	require.NotEqual(t, a.Descriptor().ConfigHash, d.Descriptor().ConfigHash, "顺序属于配置")

	_, err = newGlossaryPolicy(Glossary{Version: "v1"})
	require.Error(t, err)
	_, err = newGlossaryPolicy(Glossary{Terms: DefaultGlossary.Terms})
	require.Error(t, err)

	turn := contextpolicy.Turn{Round: 2, RemainingCalls: 3, Messages: []llm.Message{
		{Role: "system", Content: "sys"}, {Role: "user", Content: "原文"}, {Role: "assistant", Content: "a"}, {Role: "user", Content: "第二条"}}}
	out, err := a.BeforeTurn(context.Background(), turn)
	require.NoError(t, err)
	require.Len(t, out.Messages, 4, "不增删消息")
	require.Equal(t, "sys", out.Messages[0].Content, "不改 system")
	require.Contains(t, out.Messages[1].Content, "签收 → delivery confirmation")
	require.Equal(t, "第二条", out.Messages[3].Content, "只改首条用户消息")
	require.Equal(t, 2, out.Round)
	again, err := a.BeforeTurn(context.Background(), out)
	require.NoError(t, err)
	require.Equal(t, out.Messages[1].Content, again.Messages[1].Content, "幂等")
	require.Equal(t, int64(1), a.Stats()["injected"])
}

func call(id, term string) llm.ToolCall {
	c := llm.ToolCall{ID: id, Type: "function"}
	c.Function.Name = "lookup_term"
	b, _ := json.Marshal(map[string]string{"term": term})
	c.Function.Arguments = string(b)
	return c
}

// scriptedClient 让翻译与校对 Agent 使用完全相同的工具调用 ID "c1"，
// 用来证明同节点内的两个 Agent 只靠作用域隔离日志。
func scriptedClient() *livemodel.Client {
	var calls atomic.Int64
	return livemodel.Wrap(&demomodel.Model{Calls: &calls, Respond: func(r *llm.CompletionRequest) llm.StreamChunk {
		translator := strings.Contains(r.Messages[0].Content, "中译英")
		results := 0
		for _, m := range r.Messages {
			if m.Role == "tool" {
				results++
			}
		}
		switch {
		case translator && results == 0:
			return llm.StreamChunk{ToolCalls: []llm.ToolCall{call("c1", "SKU-7731")}}
		case translator:
			text := "Aurora Wireless Earbuds: within 7 days of delivery confirmation, unopened items get a no-questions-asked refund, refunded to the original payment method in 3 business days."
			return llm.StreamChunk{Content: text, Delta: text}
		case results == 0:
			return llm.StreamChunk{ToolCalls: []llm.ToolCall{call("c1", "style")}}
		default:
			var last string
			for _, m := range r.Messages {
				if m.Role == "user" {
					last = strings.SplitN(strings.TrimPrefix(m.Content, "待校对译文："), marker, 2)[0]
				}
			}
			return llm.StreamChunk{Content: last, Delta: last}
		}
	}}, livemodel.Config{Provider: "scripted", Model: "v1"})
}

const childDir, childStep, childScripted = "GOAGENT_LOCALIZE_DIR", "GOAGENT_LOCALIZE_STEP", "GOAGENT_LOCALIZE_SCRIPTED"

type childResult struct {
	Result
	Calls int64
	Err   string
}

func childMain() {
	client := scriptedClient()
	if os.Getenv(childScripted) != "1" {
		var err error
		if client, err = livemodel.FromEnv(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	}
	glossary, hooks := DefaultGlossary, Hooks{}
	switch os.Getenv(childStep) {
	case "crash":
		hooks.AfterTranslate = func() { livecase.ExitWithResult(childResult{Calls: client.Calls()}) }
	case "changed-glossary":
		glossary = Glossary{Version: "v1", Terms: append([][2]string{{"签收", "receipt of goods"}}, DefaultGlossary.Terms[1:]...)}
	}
	out, err := Run(context.Background(), client, os.Getenv(childDir), glossary, hooks)
	r := childResult{Result: out, Calls: client.Calls()}
	if err != nil {
		r.Err = livemodel.Redact(err.Error())
	}
	livecase.ExitWithResult(r)
}

func exercise(t *testing.T, testName string, scripted bool) {
	dir := t.TempDir()
	env := func(step string) []string {
		e := []string{childDir + "=" + dir, childStep + "=" + step}
		if scripted {
			e = append(e, childScripted+"=1")
		}
		return e
	}
	var crashed childResult
	livecasetest.Result(t, livecasetest.Child(t, testName, env("crash")...), &crashed)
	require.GreaterOrEqual(t, crashed.Calls, int64(2), "商品名只能查工具：翻译至少一次工具回合加一次作答")

	var changed childResult
	livecasetest.Result(t, livecasetest.Child(t, testName, env("changed-glossary")...), &changed)
	require.Contains(t, changed.Err, "intent", "术语表改变后不能复用旧翻译日志")
	require.Zero(t, changed.NewModelCalls, "在请求模型前拒绝")
	require.Zero(t, changed.TranslatorLookups+changed.StyleLookups)

	var resumed childResult
	livecasetest.Result(t, livecasetest.Child(t, testName, env("resume")...), &resumed)
	t.Logf("恢复：%+v", resumed)
	require.Empty(t, resumed.Err)
	require.Empty(t, MissingPhrases(resumed.English), resumed.English)
	require.Zero(t, resumed.TranslatorLookups, "翻译 Agent 的工具调用全部回放")
	require.Equal(t, int64(1), resumed.StyleLookups, "校对 Agent 用同名工具、同一调用 ID 也不会命中翻译的日志")
	require.Equal(t, int(crashed.Calls+resumed.NewModelCalls), resumed.Budget.ReservedRequests, "崩溃前的翻译请求不重发")

	var replayed childResult
	livecasetest.Result(t, livecasetest.Child(t, testName, env("resume")...), &replayed)
	require.Empty(t, replayed.Err)
	require.Zero(t, replayed.NewModelCalls)
	require.Zero(t, replayed.TranslatorLookups+replayed.StyleLookups)
	require.Equal(t, resumed.English, replayed.English)
	require.Equal(t, resumed.Budget, replayed.Budget)
}

func TestScriptedLocalizeRecovery(t *testing.T) {
	if os.Getenv(childDir) != "" {
		childMain()
	}
	exercise(t, "TestScriptedLocalizeRecovery", true)
}

func TestLiveLocalizeRecovery(t *testing.T) {
	if os.Getenv(childDir) != "" {
		childMain()
	}
	client := livemodel.Require(t)
	t.Logf("模型：%s/%s", client.Live.Provider, client.Live.Model)
	exercise(t, "TestLiveLocalizeRecovery", false)
}
