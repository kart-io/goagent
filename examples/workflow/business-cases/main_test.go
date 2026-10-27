package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type cliResult struct {
	Case, Status string
	Simulated    bool
	Snapshot     struct {
		State         map[string]any
		Key           string
		Waiting, Done bool
	}
}

func TestBusinessCaseChild(t *testing.T) {
	if os.Getenv("GOAGENT_BUSINESS_CASE_CHILD") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg != "--" {
			continue
		}
		if err := runCLI(context.Background(), os.Args[i+1:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(2)
}

func TestCasesRecoverAcrossProcesses(t *testing.T) {
	for _, tc := range []struct{ scenario, actor, kind, businessID string }{
		{"devops", "demo-operator", "restart_service", "incident-001"},
		{"article-review", "demo-reviewer", "publish_article", "article-001"},
		{"auto-reply", "", "send_reply", "message-001"},
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			dir := t.TempDir()
			base := []string{"-case", tc.scenario, "-dir", dir, "-run", "r"}
			if tc.scenario == "article-review" {
				input := filepath.Join(dir, "input.json")
				require.NoError(t, os.WriteFile(input, []byte(`{"article_id":"article-001","title":"需要复核的文章","body":"DEMO_REVIEW 请人工确认本次发布内容。"}`), 0600))
				base = append(base, "-input", input)
			}
			invoke := func(code int, extra ...string) []byte {
				t.Helper()
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				args := append([]string{"-test.run=^TestBusinessCaseChild$", "--"}, base...)
				cmd := exec.CommandContext(ctx, os.Args[0], append(args, extra...)...)
				cmd.Env = append(os.Environ(), "GOAGENT_BUSINESS_CASE_CHILD=1")
				output, err := cmd.CombinedOutput()
				if code == 0 {
					require.NoError(t, err, string(output))
				} else {
					var exit *exec.ExitError
					require.ErrorAs(t, err, &exit, string(output))
					require.Equal(t, code, exit.ExitCode(), string(output))
				}
				return output
			}
			b, err := openBackend(dir)
			require.NoError(t, err)
			crashArgs := []string{"-crash-after-effect"}
			var waiting cliResult
			if tc.actor != "" {
				require.NoError(t, json.Unmarshal(invoke(0), &waiting))
				require.Equal(t, "approval_required", waiting.Status)
				require.True(t, waiting.Snapshot.Waiting)
				require.False(t, waiting.Snapshot.Done)
				noEffect(t, b, tc.kind, tc.businessID)
				require.Contains(t, string(invoke(1, "-approve", waiting.Snapshot.Key, "-actor", "untrusted")), "not authorized")
				noEffect(t, b, tc.kind, tc.businessID)
				crashArgs = append(crashArgs, "-approve", waiting.Snapshot.Key, "-actor", tc.actor)
			}
			invoke(86, crashArgs...)
			first := readEffect(t, b, tc.kind, tc.businessID)
			if tc.actor != "" {
				require.Equal(t, waiting.Snapshot.Key, first.ExecutionKey)
			}
			var completed cliResult
			require.NoError(t, json.Unmarshal(invoke(0), &completed))
			require.Equal(t, "completed", completed.Status)
			require.True(t, completed.Simulated && completed.Snapshot.Done)
			require.Equal(t, first.Receipt, completed.Snapshot.State["receipt"])
			var repeated cliResult
			require.NoError(t, json.Unmarshal(invoke(0), &repeated))
			require.Equal(t, completed, repeated)
			// A fresh workflow receiving the same business request also deduplicates.
			var duplicate cliResult
			require.NoError(t, json.Unmarshal(invoke(0, "-run", "duplicate"), &duplicate))
			if duplicate.Snapshot.Waiting {
				require.NoError(t, json.Unmarshal(invoke(0, "-run", "duplicate", "-approve", duplicate.Snapshot.Key, "-actor", tc.actor), &duplicate))
			}
			require.True(t, duplicate.Snapshot.Done)
			require.Equal(t, first.Receipt, duplicate.Snapshot.State["receipt"])
			require.Equal(t, first, readEffect(t, b, tc.kind, tc.businessID))
			files, err := filepath.Glob(filepath.Join(dir, "effects", "*.json"))
			require.NoError(t, err)
			require.Len(t, files, 1)
		})
	}
}

func TestCLIInputValidation(t *testing.T) {
	for _, input := range []string{"null", "{}", "{", `{"unknown":true}`, `{} {}`} {
		t.Run(input, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "input.json")
			require.NoError(t, os.WriteFile(path, []byte(input), 0600))
			var output bytes.Buffer
			err := runCLI(context.Background(), []string{"-case", "auto-reply", "-dir", dir, "-input", path}, &output)
			require.Error(t, err)
			files, err := filepath.Glob(filepath.Join(dir, "effects", "*.json"))
			require.NoError(t, err)
			require.Empty(t, files)
		})
	}
}

func TestEmptyArticleAndInvalidReplyFailClosed(t *testing.T) {
	for _, tc := range []struct{ scenario, input string }{
		{"article-review", `{"article_id":"a","title":"标题","body":"  "}`},
		{"auto-reply", `{"message_id":"m","recipient":"real-user","message":"营业时间是什么？"}`},
		{"auto-reply", `{"message_id":"m","recipient":"demo-user","message":"  "}`},
	} {
		dir := t.TempDir()
		path := filepath.Join(dir, "input.json")
		require.NoError(t, os.WriteFile(path, []byte(tc.input), 0600))
		err := runCLI(context.Background(), []string{"-case", tc.scenario, "-dir", dir, "-input", path}, &bytes.Buffer{})
		require.Error(t, err)
		files, err := filepath.Glob(filepath.Join(dir, "effects", "*.json"))
		require.NoError(t, err)
		require.Empty(t, files)
	}
}

func TestCLIRejectsInvalidOptions(t *testing.T) {
	for _, args := range [][]string{{}, {"-dir", t.TempDir(), "-case", "unknown"}, {"-dir", t.TempDir(), "-run", " "}, {"-dir", t.TempDir(), "extra"}} {
		require.Error(t, runCLI(context.Background(), args, &bytes.Buffer{}), strings.Join(args, " "))
	}
}
