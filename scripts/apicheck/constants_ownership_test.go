package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedConstantOwnership(t *testing.T) {
	for _, tc := range []struct {
		file, source string
		bad          bool
	}{
		{"core/middleware/cache.go", `package middleware; const defaultNumShards=32`, true},
		{"tools/pipeline.go", `package tools; const defaultApprovalTimeout=10`, true},
		{"memory/new.go", `package memory; const DefaultFuturePolicy=20`, true},
		{"builder/hooks.go", `package builder; const modelResendAttempts=3`, true},
		{"core/session/event.go", `package session; const TypeTurnStart="turn/start"`, true},
		{"workflow/graph.go", `package workflow; const START="__start__"`, true},
		{"multiagent/route.go", `package multiagent; const Finish="FINISH"`, true},
		{"constants/runtime.go", `package constants; const DefaultFuturePolicy=20`, false},
		{"errors/codes.go", `package errors; const DefaultMessage="错误"`, false},
		{"codec/json/json.go", `package json; const defaultBufferLimit=32`, false},
		{"harness/env/executor.go", `package env; const defaultCreateTimeout=20`, false},
		{"tools/pipeline.go", `package tools; type PreKind int;const (PreAllow PreKind=iota;PreDeny)`, false},
		{"tools/options.go", `package tools;type Phase int;const(DefaultPhase Phase=iota;ReadyPhase)`, false},
		{"tools/options.go", `package tools;import policy "example.com/policy";const DefaultMode policy.Duration=1`, false},
		{"tools/options.go", `package tools;import t "time";const defaultWait t.Duration=1`, true},
		{"core/session/file.go", `package session;const fileVersion="goagent-session-v1"`, false},
		{"tools/sharded_cache.go", `package tools;const prime32=16777619`, false},
		{"examples/demo/main.go", `package main;const DefaultPort=8000`, false},
		{"core/session/sessiontest/test.go", `package sessiontest;const DefaultFixture="run"`, false},
	} {
		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, tc.file, tc.source, 0)
		require.NoError(t, err)
		require.Equal(t, tc.bad, len(inspect(fs, tc.file, f)) > 0, tc.file+": "+tc.source)
	}
}

// 核准清单独立取自常量归属审查，不从实现表生成，防止漏项后测试也跟着放行。
func TestApprovedConstantEntrypointsCannotReturn(t *testing.T) {
	approved := map[string]string{
		"builder/inboxWriteTimeout":              "constants.DefaultInboxWriteTimeout",
		"builder/maxRetryAfter":                  "constants.MaxModelRetryAfter",
		"builder/modelResendAttempts":            "constants.DefaultModelResendAttempts",
		"builder/contextpolicy/ReadToolName":     "constants.ToolReadContextResult",
		"core/budget/DefaultMaxAgentCalls":       "constants.DefaultMaxAgentCalls",
		"core/budget/Unlimited":                  "constants.BudgetUnlimited",
		"core/durable/ContinuationKindInterrupt": "constants.ContinuationKindInterrupt",
		"core/durable/ReasonInvalidJSON":         "constants.InterruptReasonInvalidJSON",
		"core/durable/ReasonRejected":            "constants.InterruptReasonRejected",
		"core/durable/ReasonValidatePanic":       "constants.InterruptReasonValidatePanic",
		"core/durable/inboxSeenLimit":            "constants.DurableInboxSeenLimit",
		"core/durable/interruptAttempts":         "constants.DefaultInterruptAttempts",
		"core/durable/maxReasonRunes":            "constants.InterruptMaxReasonRunes",
		"core/middleware/defaultNumShards":       "constants.DefaultMiddlewareCacheShards",
		"core/session/TypeAssistantMessage":      "constants.SessionEventAssistantMessage",
		"core/session/TypeBoundarySnapshot":      "constants.SessionEventBoundarySnapshot",
		"core/session/TypeHistoryRewrite":        "constants.SessionEventHistoryRewrite",
		"core/session/TypeRewind":                "constants.SessionEventRewind",
		"core/session/TypeSystemMessage":         "constants.SessionEventSystemMessage",
		"core/session/TypeToolDefinitions":       "constants.SessionEventToolDefinitions",
		"core/session/TypeToolResult":            "constants.SessionEventToolResult",
		"core/session/TypeTurnEnd":               "constants.SessionEventTurnEnd",
		"core/session/TypeTurnStart":             "constants.SessionEventTurnStart",
		"core/session/TypeUserMessage":           "constants.SessionEventUserMessage",
		"llm/providers/OllamaBaseURL":            "constants.OllamaBaseURL",
		"multiagent/Finish":                      "constants.MultiagentFinish",
		"tools/defaultApprovalTimeout":           "constants.DefaultApprovalTimeout",
		"tools/resumableAttempts":                "constants.DefaultResumableToolAttempts",
		"tools/spill/ToolReadSpill":              "constants.ToolReadSpill",
		"tools/spill/defaultReadLimit":           "constants.DefaultSpillReadLimitLines",
		"tools/spill/maxLineRunes":               "constants.SpillMaxLineRunes",
		"tools/spill/previewRunes":               "constants.SpillPreviewRunes",
		"workflow/END":                           "constants.WorkflowEnd",
		"workflow/RejectApprovalMismatch":        "constants.WorkflowRejectApprovalMismatch",
		"workflow/RejectBeforeApprove":           "constants.WorkflowRejectBeforeApprove",
		"workflow/RejectResumeInput":             "constants.WorkflowRejectResumeInput",
		"workflow/START":                         "constants.WorkflowStart",
	}
	require.Len(t, approved, 37)
	require.Equal(t, approved, migratedConstantOwners)
	for old, target := range approved {
		t.Run(old, func(t *testing.T) {
			owner, name := filepath.Dir(old), filepath.Base(old)
			for _, tc := range []struct {
				decl string
				want int
			}{
				{"const " + name + " = 1", 1},
				{"type Phase int; const " + name + " Phase = 1", 1},
				{"var " + name + " = 1", 1},
				{"import shared \"github.com/kart-io/goagent/constants\"; var " + name + " = shared." + strings.TrimPrefix(target, "constants."), 1},
				{"import shared \"github.com/kart-io/goagent/constants\"; const " + name + " = shared." + strings.TrimPrefix(target, "constants."), 2},
			} {
				fs := token.NewFileSet()
				file := filepath.Join(owner, "regression.go")
				node, err := parser.ParseFile(fs, file, "package review; "+tc.decl, 0)
				require.NoError(t, err)
				findings := inspect(fs, file, node)
				require.Len(t, findings, tc.want, tc.decl)
				require.Contains(t, findings[len(findings)-1].message, target)
			}
		})
	}
}

func TestDefaultPoliciesResolveTransparentAliases(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         int
	}{
		{"基本类型别名", `package tools;type Count = int;const DefaultCount Count=3`, 1},
		{"时间别名", `package tools;import t "time";type Wait=t.Duration;const DefaultWait Wait=t.Second`, 1},
		{"链式别名", `package tools;import t "time";type A=t.Duration;type B=A;type C=B;const DefaultWait C=t.Second`, 1},
		{"时间点导入", `package tools;import . "time";const DefaultWait Duration=Second`, 1},
		{"时间点导入别名", `package tools;import . "time";type Wait=Duration;const DefaultWait Wait=Second`, 1},
		{"定义的领域枚举", `package tools;type Phase int;const(DefaultInitial Phase=iota;DefaultReady)`, 0},
		{"领域枚举转换", `package tools;type Phase int;const DefaultPhase=Phase(0)`, 0},
		{"具名领域常量引用", `package tools;type Phase int;const ReadyPhase Phase=1;const DefaultPhase=ReadyPhase`, 0},
		{"具名领域常量链式引用", `package tools;type Phase int;const ReadyPhase Phase=1;const NextPhase=ReadyPhase;const DefaultPhase=(NextPhase)`, 0},
		{"转换表达式的iota继承", `package tools;type Phase int;const(DefaultInitial=Phase(iota);DefaultReady)`, 0},
		{"具名常量表达式继承", `package tools;type Phase int;const ReadyPhase Phase=1;const(DefaultInitial=ReadyPhase;DefaultReady)`, 0},
		{"领域常量算术", `package tools;type Phase int;const ReadyPhase Phase=1;const DefaultNext=ReadyPhase+1;const DefaultNeg=-ReadyPhase`, 0},
		{"比较不携带领域类型", `package tools;type Phase int;const ReadyPhase Phase=1;const DefaultFlag=ReadyPhase==1`, 1},
		{"移位只保留左操作数类型", `package tools;type Phase int;const DefaultCount=1<<Phase(2);const DefaultPhase=Phase(1)<<2`, 1},
		{"基本类型转换不豁免", `package tools;type Count=int;const DefaultCount=Count(3)`, 1},
		{"时间类型转换不豁免", `package tools;import t "time";const DefaultWait=t.Duration(1)`, 1},
		{"时间具名常量引用不豁免", `package tools;import t "time";const Wait t.Duration=t.Second;const DefaultWait=Wait`, 1},
		{"显式基本类型优先于值", `package tools;type Phase int;const ReadyPhase Phase=1;const Count int=int(ReadyPhase);const DefaultCount=Count`, 1},
		{"常量引用循环终止", `package tools;const A=B;const B=A;const DefaultCount=A`, 1},
		{"未知内置函数不当类型转换", `package tools;const DefaultSize=len("abc")`, 1},
		{"定义的泛型领域枚举", `package tools;type Phase[T any] int;const DefaultPhase Phase[int]=1;const DefaultNext=Phase[int](0)`, 0},
		{"泛型领域透明别名", `package tools;type Phase[T any] int;type Alias[T any]=Phase[T];const DefaultPhase Alias[int]=1`, 0},
		{"泛型基本类型透明别名", `package tools;type Plain[T any]=int;const DefaultCount Plain[string]=1;const DefaultNext=Plain[string](1)`, 2},
		{"领域枚举别名", `package tools;type Phase int;type Alias=Phase;const DefaultPhase Alias=1`, 0},
		{"同名领域时间", `package tools;type Duration int;const DefaultWait Duration=1`, 0},
		{"时间底层的定义类型", `package tools;import t "time";type Wait t.Duration;const DefaultWait Wait=1`, 0},
		{"遮蔽基本类型的领域定义", `package tools;type int uint;const DefaultPhase int=1`, 0},
		{"点导入与本地类型区分", `package tools;import . "time";type Duration int;const DefaultWait Duration=1`, 0},
		{"不同常量重置继承类型", `package tools;type Phase int;const(DefaultPhase Phase=1;DefaultCount=2;DefaultOther)`, 2},
		{"循环不获得领域豁免", `package tools;type A=B;type B=A;const DefaultWait A=1`, 1},
		{"局部常量不受包级规则影响", `package tools;func f(){const DefaultCount=3}`, 0},
		{"普通默认变量不扩展为常量规则", `package tools;var DefaultFuturePolicy=3`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := token.NewFileSet()
			node, err := parser.ParseFile(fs, "tools/options.go", tc.source, 0)
			require.NoError(t, err)
			require.Len(t, inspect(fs, "tools/options.go", node), tc.want)
		})
	}
}

func TestScanResolvesAliasesWithinTheirDeclaringPackage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  int
	}{
		{"跨文件时间别名", map[string]string{
			"tools/a_defaults.go": `package tools;const DefaultWait Wait=1`,
			"tools/z_types.go":    `package tools;import t "time";type Wait=t.Duration`,
		}, 1},
		{"跨文件链式基本别名", map[string]string{
			"tools/a_defaults.go": `package tools;const DefaultCount First=3`,
			"tools/b_first.go":    `package tools;type First=Second`,
			"tools/z_second.go":   `package tools;type Second=int`,
		}, 1},
		{"跨文件链式时间别名", map[string]string{
			"tools/a_defaults.go": `package tools;const DefaultWait First=1`,
			"tools/b_first.go":    `package tools;type First=Second`,
			"tools/z_second.go":   `package tools;import t "time";type Second=t.Duration`,
		}, 1},
		{"跨文件点导入时间别名", map[string]string{
			"tools/a_defaults.go": `package tools;const DefaultWait Wait=1`,
			"tools/z_types.go":    `package tools;import . "time";type Wait=Duration`,
		}, 1},
		{"别名声明的导入优先", map[string]string{
			"tools/a_defaults.go": `package tools;import t "example.com/policy";const DefaultWait Wait=1;const DefaultMode t.Duration=1`,
			"tools/z_types.go":    `package tools;import t "time";type Wait=t.Duration`,
		}, 1},
		{"外部领域别名不能误用消费文件导入", map[string]string{
			"tools/a_defaults.go": `package tools;import t "time";const DefaultMode Mode=1;const DefaultWait t.Duration=1`,
			"tools/z_types.go":    `package tools;import t "example.com/policy";type Mode=t.Duration`,
		}, 1},
		{"跨文件领域定义", map[string]string{
			"tools/a_defaults.go": `package tools;const(DefaultInitial Phase=iota;DefaultReady)`,
			"tools/z_types.go":    `package tools;type Phase int`,
		}, 0},
		{"跨文件具名常量引用与继承", map[string]string{
			"tools/a_defaults.go": `package tools;const(DefaultInitial=NextPhase;DefaultReady)`,
			"tools/b_values.go":   `package tools;const ReadyPhase Phase=1;const NextPhase=ReadyPhase`,
			"tools/z_types.go":    `package tools;type Phase int`,
		}, 0},
		{"跨文件转换与声明导入", map[string]string{
			"tools/a_defaults.go": `package tools;import t "time";const DefaultPhase=ReadyPhase;const DefaultWait t.Duration=1`,
			"tools/b_values.go":   `package tools;import t "example.com/policy";const ReadyPhase=t.Duration(1)`,
		}, 1},
		{"跨文件时间转换引用不豁免", map[string]string{
			"tools/a_defaults.go": `package tools;import t "example.com/policy";const DefaultWait=Wait;const DefaultPhase t.Duration=1`,
			"tools/b_values.go":   `package tools;import t "time";const Wait=t.Duration(1)`,
		}, 1},
		{"跨文件常量引用循环终止", map[string]string{
			"tools/a_defaults.go": `package tools;const DefaultWait=A`,
			"tools/b_values.go":   `package tools;const A=B`,
			"tools/c_values.go":   `package tools;const B=A`,
		}, 1},
		{"跨目录不得泄露类型", map[string]string{
			"core/defaults.go": `package core;type Wait=int;const DefaultWait Wait=1`,
			"tools/types.go":   `package tools;type Wait int`,
		}, 1},
		{"同目录不同包不得泄露类型", map[string]string{
			"tools/a_defaults.go": `package tools;type Wait=int;const DefaultWait Wait=1`,
			"tools/z_other.go":    `package other;type Wait int`,
		}, 1},
		{"测试声明不能改变生产判定", map[string]string{
			"tools/a_defaults.go": `package tools;type Wait=int;const DefaultWait Wait=1`,
			"tools/types_test.go": `package tools;type Wait int;const DefaultFixture=1`,
		}, 1},
		{"跨文件循环终止", map[string]string{
			"tools/a_defaults.go": `package tools;const DefaultWait A=1`,
			"tools/b_types.go":    `package tools;type A=B`,
			"tools/c_types.go":    `package tools;type B=A`,
		}, 1},
		{"测试辅助与近似目录区分", map[string]string{
			"harness/env/options.go":                    `package env;const DefaultFixture=1`,
			"harness/env_extra/options.go":              `package env;const DefaultWait=1`,
			"core/durable/storetest/options.go":         `package storetest;const DefaultFixture=1`,
			"core/durable/storetest_extra/options.go":   `package storetest;const DefaultWait=1`,
			"core/session/sessiontest/options.go":       `package sessiontest;const DefaultFixture=1`,
			"core/session/sessiontest_extra/options.go": `package sessiontest;const DefaultWait=1`,
		}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for file, source := range tc.files {
				path := filepath.Join(root, filepath.FromSlash(file))
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				require.NoError(t, os.WriteFile(path, []byte(source), 0o644))
			}
			count, err := scan(root)
			require.NoError(t, err)
			require.Equal(t, tc.want, count, fmt.Sprint(tc.files))
		})
	}
}
