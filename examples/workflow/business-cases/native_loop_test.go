package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/stretchr/testify/require"
)

func appendAudit(path, value string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(value + "\n")
	return errors.Join(err, f.Sync(), f.Close())
}
func auditLines(path string) []string {
	b, _ := os.ReadFile(path)
	if strings.TrimSpace(string(b)) == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

type auditedNativeClient struct {
	llm.ToolStreamClient
	dir string
}

func (m *auditedNativeClient) StreamMode() core.StreamMode {
	if c, ok := m.ToolStreamClient.(core.StreamCapability); ok {
		return c.StreamMode()
	}
	return core.StreamModeTokens
}
func (m *auditedNativeClient) CompleteToolsStream(ctx context.Context, req *llm.CompletionRequest, tools []interfaces.Tool) (<-chan *llm.StreamChunk, error) {
	path := filepath.Join(m.dir, "requests")
	if len(auditLines(path)) >= 4 {
		return nil, fmt.Errorf("total model request budget exhausted")
	}
	if err := appendAudit(path, "request"); err != nil {
		return nil, err
	}
	return m.ToolStreamClient.CompleteToolsStream(ctx, req, tools)
}

func runNativeChild(t *testing.T) {
	t.Helper()
	dir := os.Getenv("GOAGENT_NATIVE_CHILD_DIR")
	provider, model := os.Getenv("GOAGENT_BUSINESS_PROVIDER"), os.Getenv("GOAGENT_NATIVE_MODEL")
	var client llm.ToolStreamClient
	if url := os.Getenv("GOAGENT_NATIVE_MOCK_URL"); url != "" {
		client = &minimaxCompletion{key: "offline-key", model: "test-model", endpoint: url, client: &http.Client{Timeout: 10 * time.Second}}
	} else {
		judge, err := newModelJudge(provider, model)
		require.NoError(t, err)
		var ok bool
		client, ok = judge.client.(llm.ToolStreamClient)
		require.True(t, ok)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	store, err := durable.NewFileStore(filepath.Join(dir, "checkpoints"))
	require.NoError(t, err)
	backend, err := openBackend(dir)
	require.NoError(t, err)
	stage := ""
	backend.after = func() error {
		if stage == "B" {
			os.Exit(86)
		}
		return nil
	}
	audited := &auditedNativeClient{ToolStreamClient: client, dir: dir}
	graph, err := nativeLoopGraph(ctx, store, backend, audited, provider+"/"+model, func(s string) error { stage = s; return appendAudit(filepath.Join(dir, "tools"), s) })
	require.NoError(t, err)
	snapshot, err := graph.Run(ctx, "native", "Record stage A and stage B, then report both receipts.", "")
	require.NoError(t, err)
	require.True(t, snapshot.Done)
	require.NotEmpty(t, snapshot.State)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "answer"), []byte(snapshot.State), 0600))
}

type nativeLoopReport struct {
	Provider          string            `json:"provider"`
	Model             string            `json:"model"`
	Passed            bool              `json:"passed"`
	ModelCallsByPhase []int             `json:"model_calls_by_phase"`
	ToolAttempts      []string          `json:"tool_attempts"`
	Effects           int               `json:"effects"`
	DistinctKeys      bool              `json:"distinct_keys"`
	KnownTokens       int               `json:"known_tokens"`
	UsageReports      int               `json:"usage_reports"`
	OperationStatuses map[string]string `json:"operation_statuses"`
	Error             string            `json:"error,omitempty"`
	Limitations       string            `json:"limitations"`
}

func exerciseNativeProcesses(t *testing.T, env []string, report *nativeLoopReport) {
	t.Helper()
	dir := t.TempDir()
	defer func() {
		report.ToolAttempts = auditLines(filepath.Join(dir, "tools"))
		paths, _ := filepath.Glob(filepath.Join(dir, "checkpoints", "*.json"))
		report.OperationStatuses = map[string]string{}
		for _, path := range paths {
			b, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var s durable.Snapshot
			if json.Unmarshal(b, &s) != nil || s.Version != "goagent-operation-v1" {
				continue
			}
			var op durable.OperationRecord
			if json.Unmarshal(s.Data, &op) != nil {
				continue
			}
			report.OperationStatuses[op.ID] = op.Status
			if strings.HasPrefix(op.ID, "model/") {
				var turn llm.StreamChunk
				if json.Unmarshal(op.Value, &turn) == nil && turn.Usage != nil {
					report.KnownTokens += turn.Usage.TotalTokens
					report.UsageReports++
				}
			}
		}
		report.Passed = !t.Failed()
	}()
	before := 0
	for _, want := range []int{86, 0, 0} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeToolLoopChild$")
		cmd.Env = append(append(os.Environ(), env...), "GOAGENT_NATIVE_CHILD_DIR="+dir)
		cmd.WaitDelay = time.Second
		out, err := cmd.CombinedOutput()
		cancel()
		after := len(auditLines(filepath.Join(dir, "requests")))
		report.ModelCallsByPhase = append(report.ModelCallsByPhase, after-before)
		before = after
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				code = exit.ExitCode()
			} else {
				code = -1
			}
		}
		if code != want {
			report.Error = redactModelError(string(out))
			t.Fatalf("phase exit %d, expected %d: %s", code, want, report.Error)
		}
	}
	require.Equal(t, 1, report.ModelCallsByPhase[1], "only final model answer should be requested on resume")
	require.Zero(t, report.ModelCallsByPhase[2], "completed replay must not call model")
	require.Equal(t, []string{"A", "B", "B"}, auditLines(filepath.Join(dir, "tools")))
	paths, err := filepath.Glob(filepath.Join(dir, "effects", "*.json"))
	require.NoError(t, err)
	report.Effects = len(paths)
	require.Equal(t, 2, len(paths))
	keys := map[string]bool{}
	answer, err := os.ReadFile(filepath.Join(dir, "answer"))
	require.NoError(t, err)
	for _, path := range paths {
		b, err := os.ReadFile(path)
		require.NoError(t, err)
		var s durable.Snapshot
		require.NoError(t, json.Unmarshal(b, &s))
		var effect effectRecord
		require.NoError(t, json.Unmarshal(s.Data, &effect))
		require.NotEmpty(t, effect.ExecutionKey)
		keys[effect.ExecutionKey] = true
		require.Contains(t, string(answer), effect.Receipt)
	}
	report.DistinctKeys = len(keys) == 2
	require.True(t, report.DistinctKeys)
}

func TestNativeToolLoopChild(t *testing.T) {
	if os.Getenv("GOAGENT_NATIVE_CHILD_DIR") == "" {
		t.Skip("subprocess helper")
	}
	runNativeChild(t)
}

func TestNativeToolLoopProcessRecovery(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []llm.Message     `json:"messages"`
			Tools    []json.RawMessage `json:"tools"`
		}
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "bad request", 400)
			return
		}
		if calls.Add(1) == 1 {
			if len(req.Tools) != 2 {
				t.Error("missing native tool definitions")
			}
			fmt.Fprint(w, `{"choices":[{"message":{"content":"","reasoning_content":"opaque-continuation","tool_calls":[{"id":"a","type":"function","function":{"name":"record_A","arguments":"{\"stage\":\"A\"}"}},{"id":"b","type":"function","function":{"name":"record_B","arguments":"{\"stage\":\"B\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"total_tokens":11}}`)
			return
		}
		receipts := []string{}
		reasoning := ""
		for _, msg := range req.Messages {
			if msg.Role == "tool" {
				receipts = append(receipts, msg.Content)
			}
			if msg.Role == "assistant" {
				reasoning = msg.ReasoningContent
			}
		}
		if len(receipts) != 2 || reasoning != "opaque-continuation" {
			t.Error("lost tool results or provider continuation")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": strings.Join(receipts, " ")}, "finish_reason": "stop"}}, "usage": map[string]int{"total_tokens": 7}})
	}))
	defer server.Close()
	report := &nativeLoopReport{}
	exerciseNativeProcesses(t, []string{"GOAGENT_NATIVE_MOCK_URL=" + server.URL, "GOAGENT_BUSINESS_PROVIDER=minimax", "GOAGENT_NATIVE_MODEL=test-model"}, report)
	require.EqualValues(t, 2, calls.Load())
	require.Equal(t, 18, report.KnownTokens)
}

func TestLiveNativeToolLoop(t *testing.T) {
	if os.Getenv("GOAGENT_NATIVE_LIVE") != "1" {
		t.Skip("opt in with GOAGENT_NATIVE_LIVE=1")
	}
	provider := os.Getenv("GOAGENT_BUSINESS_PROVIDER")
	model := os.Getenv(strings.ToUpper(provider) + "_MODEL")
	require.NotEmpty(t, model)
	file, err := os.OpenFile(os.Getenv("GOAGENT_BUSINESS_REPORT"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	require.NoError(t, err)
	report := &nativeLoopReport{Provider: provider, Model: model, Limitations: "One synthetic native function-calling case. Real process exit after second local effect, before tool result checkpoint. External business effects are simulated. MiniMax is buffered final-only. Missing provider usage cannot be reconstructed."}
	defer func() {
		report.Passed = !t.Failed()
		data, err := json.MarshalIndent(report, "", "  ")
		require.NoError(t, err)
		_, err = file.Write(append(data, '\n'))
		require.NoError(t, err)
		require.NoError(t, file.Close())
	}()
	exerciseNativeProcesses(t, []string{"GOAGENT_NATIVE_MODEL=" + model, "GOAGENT_NATIVE_MOCK_URL="}, report)
}
