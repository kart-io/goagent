package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/kart-io/goagent/examples/internal/livemodel"
	"github.com/stretchr/testify/require"
)

// 不调用模型：错误行必须落在卸载阈值和第一页之外，模型才需要翻页。
func TestPaymentLogsRequirePaging(t *testing.T) {
	logs := PaymentLogs()
	require.Equal(t, logs, PaymentLogs(), "日志必须确定，恢复时意图哈希才一致")
	require.Equal(t, 1, strings.Count(logs, "ERROR"))
	at := utf8.RuneCountInString(logs[:strings.Index(logs, "ERROR")])
	require.Greater(t, at, 3000, "错误行不在第一页")
	require.Greater(t, utf8.RuneCountInString(logs), 400, "超过卸载阈值")
}

const childDir, childMode = "GOAGENT_LOG_CHILD_DIR", "GOAGENT_LOG_CHILD_MODE"

var crashMarker = regexp.MustCompile(`CRASHED_AFTER_CALLS=(\d+)`)

// 子进程入口：crash 模式在第一次分页读取时直接退出，模拟进程被杀。
func runChild(t *testing.T) {
	client, err := livemodel.FromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var hook readHook
	if os.Getenv(childMode) == "crash" {
		hook = func(int64) {
			fmt.Printf("\nCRASHED_AFTER_CALLS=%d\n", client.Calls())
			os.Exit(86)
		}
	}
	out, err := Run(context.Background(), client, os.Getenv(childDir), hook)
	if err != nil {
		fmt.Fprintln(os.Stderr, livemodel.Redact(err.Error()))
		os.Exit(2)
	}
	data, _ := json.Marshal(out)
	fmt.Printf("\nRESULT=%s\n", data)
	os.Exit(86)
}

func TestLiveLogTriageCrashRecovery(t *testing.T) {
	if os.Getenv(childDir) != "" {
		runChild(t)
	}
	client := livemodel.Require(t)
	dir := t.TempDir()
	child := func(mode string) string {
		cmd := exec.Command(os.Args[0], "-test.run=^TestLiveLogTriageCrashRecovery$")
		cmd.Env = append(os.Environ(), childDir+"="+dir, childMode+"="+mode)
		b, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit, string(b))
		require.Equal(t, 86, exit.ExitCode(), string(b))
		return string(b)
	}
	result := func(out string) Result {
		i := strings.LastIndex(out, "RESULT=")
		require.GreaterOrEqual(t, i, 0, out)
		var r Result
		require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(strings.SplitN(out[i+len("RESULT="):], "\n", 2)[0])), &r))
		return r
	}

	crashed := crashMarker.FindStringSubmatch(child("crash"))
	require.NotNil(t, crashed, "第一次运行应在分页读取时退出")
	crashCalls, _ := strconv.Atoi(crashed[1])
	require.GreaterOrEqual(t, crashCalls, 2, "退出前已完成 fetch 与读取两个模型回合")

	resumed := result(child("resume"))
	t.Logf("%s/%s 恢复：%+v", client.Live.Provider, client.Live.Model, resumed)
	require.Contains(t, resumed.Answer, ErrorCode)
	require.Contains(t, resumed.Answer, ErrorUpstream)
	require.Zero(t, resumed.FetchRuns, "崩溃前已完成的 fetch_logs 不再执行")
	require.GreaterOrEqual(t, resumed.PageReads, int64(1), "未完成的读取在恢复时重试")
	require.Equal(t, int64(1), resumed.Offloaded, "长日志被卸载为引用")
	require.Equal(t, crashCalls+int(resumed.NewModelCalls), resumed.Budget.ReservedRequests, "崩溃前的请求被回放而非重发")
	require.LessOrEqual(t, resumed.Budget.ReservedRequests, 10)

	replayed := result(child("replay"))
	require.Zero(t, replayed.NewModelCalls)
	require.Zero(t, replayed.FetchRuns)
	require.Zero(t, replayed.PageReads)
	require.Equal(t, resumed.Answer, replayed.Answer)
	require.Equal(t, resumed.Budget, replayed.Budget)
}
