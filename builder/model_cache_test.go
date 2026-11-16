package builder

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/cache"
	shared "github.com/kart-io/goagent/constants"
	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/session"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/memory"
)

var testTagKey = bytes.Repeat([]byte("k"), 32)

func fixedScope(context.Context) (string, bool) { return "tenant", true }

// untaggedCache 只暴露 cache.Bytes，隐藏 SimpleCache 的 Tagged 能力。
type untaggedCache struct{ cache.Bytes }

func newModelCacheMemory(t *testing.T, cfg memory.Config) *memory.Memory {
	t.Helper()
	fs, err := durable.NewFileStore(t.TempDir())
	require.NoError(t, err)
	cfg.Durable = fs
	m, err := memory.New(cfg)
	require.NoError(t, err)
	return m
}

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestModelCacheValidation(t *testing.T) {
	sc := cache.NewSimpleCache(time.Minute)
	noErase := func(t *testing.T) *memory.Memory { return newModelCacheMemory(t, memory.Config{}) }
	withErase := func(within time.Duration) func(*testing.T) *memory.Memory {
		return func(t *testing.T) *memory.Memory {
			return newModelCacheMemory(t, memory.Config{
				OnErase:     func(context.Context, []string, string) error { return nil },
				EraseWithin: within, RetireGrace: within,
			})
		}
	}
	valid := ModelCache{Cache: sc, TTL: time.Minute, Scope: fixedScope, TagKey: testTagKey}
	with := func(f func(*ModelCache)) ModelCache { mc := valid; f(&mc); return mc }

	tests := []struct {
		name   string
		mc     ModelCache
		memory func(*testing.T) *memory.Memory
		// errContains 非空时期望构建失败，错误文案含该片段以确认触发的规则。
		errContains string
		warn        string
	}{
		{name: "合法配置", mc: valid},
		{name: "Cache 为空", mc: with(func(m *ModelCache) { m.Cache = nil }), errContains: "Cache 不能为空"},
		{name: "TTL 为 0", mc: with(func(m *ModelCache) { m.TTL = 0 }), errContains: "TTL 必须大于 0"},
		{name: "TTL 为负", mc: with(func(m *ModelCache) { m.TTL = -time.Second }), errContains: "TTL 必须大于 0"},
		{name: "没有 Scope 也没有长期记忆", mc: with(func(m *ModelCache) { m.Scope = nil }), errContains: "没有可信的隔离身份"},
		{name: "长期记忆下可省略 Scope", mc: with(func(m *ModelCache) { m.Scope = nil }), memory: noErase},
		{name: "EraseRequired 缓存不支持 Tagged", mc: ModelCache{Cache: untaggedCache{sc}, TTL: time.Minute, Scope: fixedScope, Erase: EraseRequired}, errContains: "EraseRequired 要求缓存实现 cache.Tagged"},
		{name: "EraseRequired 记忆未配置 OnErase", mc: with(func(m *ModelCache) { m.Erase = EraseRequired }), memory: noErase, errContains: "EraseRequired 要求长期记忆配置 OnErase"},
		{name: "EraseRequired 且 TTL 超过 EraseWithin", mc: with(func(m *ModelCache) { m.Erase = EraseRequired; m.TTL = 2 * time.Minute }), memory: withErase(time.Minute), errContains: "EraseRequired 要求 TTL 不超过"},
		{name: "EraseRequired 满足全部条件", mc: with(func(m *ModelCache) { m.Erase = EraseRequired }), memory: withErase(time.Minute)},
		{name: "TagKey 不足 32 字节", mc: with(func(m *ModelCache) { m.TagKey = []byte("short") }), errContains: "TagKey 至少 32 字节"},
		{name: "EraseOff 不要求 TagKey", mc: with(func(m *ModelCache) { m.TagKey = nil; m.Erase = EraseOff })},
		{name: "EraseAuto 缓存不支持 Tagged 只警告", mc: ModelCache{Cache: untaggedCache{sc}, TTL: time.Minute, Scope: fixedScope}, warn: "退回 TTL"},
		{name: "EraseAuto 且 TTL 超过 EraseWithin 只警告", mc: with(func(m *ModelCache) { m.TTL = 2 * time.Minute }), memory: withErase(time.Minute), warn: "残留到 TTL"},
		{name: "EraseOff 且 TTL 超过 EraseWithin 不警告", mc: with(func(m *ModelCache) { m.Erase = EraseOff; m.TTL = 2 * time.Minute }), memory: withErase(time.Minute)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := captureSlog(t)
			b := NewSimpleBuilder(newScriptModel()).WithModelCache(tt.mc)
			if tt.memory != nil {
				b = b.WithSessionLog(session.NewMemoryStore()).WithLongTermMemory(tt.memory(t))
			}
			_, err := b.Build()
			if tt.errContains != "" {
				require.Error(t, err)
				require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
				require.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			if tt.warn == "" {
				require.Empty(t, buf.String())
			} else {
				require.Contains(t, buf.String(), tt.warn)
			}
		})
	}
}

type stubTool struct{ name, desc, schema string }

func (s stubTool) Name() string        { return s.name }
func (s stubTool) Description() string { return s.desc }
func (s stubTool) ArgsSchema() string  { return s.schema }
func (s stubTool) Invoke(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
	return nil, nil
}

func baseKeyInput() cacheKeyInput {
	return cacheKeyInput{
		Identity: "u1", SessionID: "s1", Client: "openai\x00gpt",
		Request: &llm.CompletionRequest{Model: "gpt", Messages: []llm.Message{{Role: "user", Content: "hi"}}},
		Tools:   []interfaces.Tool{stubTool{"t1", "d", `{"a":1,"b":2}`}},
		Memory:  []memory.NamespaceVersion{{Namespace: []string{"t", "u"}, Scope: "sc", LastForgetSeq: 1, LastRewindSeq: 2}},
	}
}

func TestModelCacheKey(t *testing.T) {
	baseKey, ok := modelCacheKey(baseKeyInput())
	require.True(t, ok)
	require.True(t, strings.HasPrefix(baseKey, "mc1:"))

	differs := []struct {
		name   string
		mutate func(*cacheKeyInput)
	}{
		{"身份", func(in *cacheKeyInput) { in.Identity = "u2" }},
		{"会话 ID", func(in *cacheKeyInput) { in.SessionID = "s2" }},
		{"客户端身份", func(in *cacheKeyInput) { in.Client = "openai\x00other" }},
		{"模型名", func(in *cacheKeyInput) { in.Request.Model = "other" }},
		{"消息内容", func(in *cacheKeyInput) { in.Request.Messages[0].Content = "hello" }},
		{"显式 Temperature=0", func(in *cacheKeyInput) { in.Request.TemperatureSet = true }},
		{"增加工具", func(in *cacheKeyInput) { in.Tools = append(in.Tools, stubTool{"t2", "d", `{}`}) }},
		{"工具描述", func(in *cacheKeyInput) { in.Tools = []interfaces.Tool{stubTool{"t1", "d2", `{"a":1,"b":2}`}} }},
		{"工具 schema 内容", func(in *cacheKeyInput) { in.Tools = []interfaces.Tool{stubTool{"t1", "d", `{"a":1,"b":3}`}} }},
		{"记忆命名空间", func(in *cacheKeyInput) { in.Memory[0].Namespace = []string{"t", "v"} }},
		{"记忆作用域", func(in *cacheKeyInput) { in.Memory[0].Scope = "sc2" }},
		{"LastForgetSeq", func(in *cacheKeyInput) { in.Memory[0].LastForgetSeq = 9 }},
		{"LastRewindSeq", func(in *cacheKeyInput) { in.Memory[0].LastRewindSeq = 9 }},
		{"长度前缀：身份与会话 ID 边界", func(in *cacheKeyInput) { in.Identity, in.SessionID = "u", "1s1" }},
	}
	for _, tt := range differs {
		t.Run("改动后键不同/"+tt.name, func(t *testing.T) {
			in := baseKeyInput()
			tt.mutate(&in)
			key, ok := modelCacheKey(in)
			require.True(t, ok)
			require.NotEqual(t, baseKey, key)
		})
	}

	t.Run("长度前缀区分 (ab,c) 与 (a,bc)", func(t *testing.T) {
		a, b := baseKeyInput(), baseKeyInput()
		a.Identity, a.SessionID = "ab", "c"
		b.Identity, b.SessionID = "a", "bc"
		ka, _ := modelCacheKey(a)
		kb, _ := modelCacheKey(b)
		require.NotEqual(t, ka, kb)
	})
	t.Run("schema 键顺序不同键相同", func(t *testing.T) {
		in := baseKeyInput()
		in.Tools = []interfaces.Tool{stubTool{"t1", "d", `{"b":2,"a":1}`}}
		key, ok := modelCacheKey(in)
		require.True(t, ok)
		require.Equal(t, baseKey, key)
	})
	t.Run("schema 仅在超过 2^53 的整数上不同键不同", func(t *testing.T) {
		a, b := baseKeyInput(), baseKeyInput()
		a.Tools = []interfaces.Tool{stubTool{"t1", "d", `{"maximum":9007199254740993}`}}
		b.Tools = []interfaces.Tool{stubTool{"t1", "d", `{"maximum":9007199254740992}`}}
		ka, ok := modelCacheKey(a)
		require.True(t, ok)
		kb, ok := modelCacheKey(b)
		require.True(t, ok)
		require.NotEqual(t, ka, kb)
	})
	t.Run("schema 非法 JSON 不缓存", func(t *testing.T) {
		in := baseKeyInput()
		in.Tools = []interfaces.Tool{stubTool{"t1", "d", `{bad`}}
		_, ok := modelCacheKey(in)
		require.False(t, ok)
	})
}

type identityClient struct{ id string }

func (identityClient) CompleteStream(context.Context, *llm.CompletionRequest) (<-chan *llm.StreamChunk, error) {
	return nil, nil
}
func (identityClient) Provider() shared.Provider { return "p" }
func (c identityClient) CacheIdentity() string   { return c.id }

type namedClient struct {
	identityClient
	name string
}

func (c namedClient) ModelName() string { return c.name }

type plainClient struct{ identityClient }

func TestClientIdentity(t *testing.T) {
	tests := []struct {
		name   string
		client llm.Client
		model  string
		want   string
		wantOK bool
	}{
		{"CacheIdentity 优先", namedClient{identityClient{"dep-1"}, "m"}, "req-model", "dep-1", true},
		{"请求模型名优先于 ModelName", namedClient{name: "m"}, "req-model", "p\x00req-model", true},
		{"回退到 ModelName", namedClient{name: "m"}, "", "p\x00m", true},
		{"都没有时不缓存", plainClient{}, "", "", false},
		{"只有请求模型名", plainClient{}, "req-model", "p\x00req-model", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := clientIdentity(tt.client, &llm.CompletionRequest{Model: tt.model})
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestModelCacheTags(t *testing.T) {
	k1, k2 := bytes.Repeat([]byte("a"), 32), bytes.Repeat([]byte("b"), 32)
	require.NotContains(t, sessionTag(k1, "session-secret"), "session-secret")
	require.NotContains(t, memoryTag(k1, []string{"tenant-x", "alice"}, "scope-x"), "alice")
	require.NotEqual(t, sessionTag(k1, "s"), sessionTag(k2, "s"))
	require.NotEqual(t, memoryTag(k1, []string{"a"}, "s"), memoryTag(k2, []string{"a"}, "s"))
	require.NotEqual(t, memoryTag(k1, []string{"a:b", "c"}, "s"), memoryTag(k1, []string{"a", "b:c"}, "s"))
	require.NotEqual(t, memoryTag(k1, []string{"a", "b"}, ""), memoryTag(k1, []string{"a"}, "b"))
	require.NotEqual(t, sessionTag(k1, "x"), memoryTag(k1, []string{"x"}, ""))
}

func TestModelCacheScope(t *testing.T) {
	nsCtx := memory.WithNamespace(context.Background(), "t1", "alice")
	tests := []struct {
		name   string
		scope  func(context.Context) (string, bool)
		ltm    bool
		ctx    context.Context
		wantOK bool
	}{
		{"Scope 返回空身份", func(context.Context) (string, bool) { return "", true }, false, nsCtx, false},
		{"Scope 返回 false", func(context.Context) (string, bool) { return "x", false }, true, nsCtx, false},
		{"Scope 有效", fixedScope, false, context.Background(), true},
		{"无 Scope 有长期记忆取命名空间", nil, true, nsCtx, true},
		{"无 Scope 命名空间为空", nil, true, context.Background(), false},
		{"无 Scope 无长期记忆", nil, false, nsCtx, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := ModelCache{Scope: tt.scope}.identity(tt.ctx, tt.ltm)
			require.Equal(t, tt.wantOK, ok)
			require.Equal(t, tt.wantOK, id != "")
		})
	}

	a, _ := ModelCache{}.identity(memory.WithNamespace(context.Background(), "a:b", "c"), true)
	b, _ := ModelCache{}.identity(memory.WithNamespace(context.Background(), "a", "b:c"), true)
	require.NotEqual(t, a, b)
}

func TestCachedTurnCodec(t *testing.T) {
	in := &llm.StreamChunk{Content: "你好", FinishReason: "stop", ToolCalls: []llm.ToolCall{{ID: "c1", Type: "function"}}}
	raw, err := encodeCachedTurn(in)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(raw), "mc1\n"))
	out, err := decodeCachedTurn(raw)
	require.NoError(t, err)
	require.Equal(t, in, out)

	for name, bad := range map[string][]byte{
		"前缀错误":    []byte(`mc2` + "\n{}"),
		"JSON 非法": []byte("mc1\n{bad"),
		"空":       nil,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeCachedTurn(bad)
			require.Error(t, err)
		})
	}
}

func TestModelCacheEraserAndDeleteSession(t *testing.T) {
	ctx := context.Background()
	sc := cache.NewSimpleCache(time.Minute)
	mc := ModelCache{Cache: sc, TTL: time.Minute, Scope: fixedScope, TagKey: testTagKey}
	nsA, nsB := []string{"t", "a"}, []string{"t", "b"}

	set := func(key, session string, ns []string) {
		require.NoError(t, sc.SetTagged(ctx, key, []byte("v"), time.Minute, sessionTag(testTagKey, session), memoryTag(testTagKey, ns, "sc")))
	}
	has := func(key string) bool {
		ok, err := sc.Has(ctx, key)
		require.NoError(t, err)
		return ok
	}
	set("k1", "s1", nsA)
	set("k2", "s2", nsA)
	set("k3", "s1", nsB)

	require.NoError(t, mc.Eraser()(ctx, nsA, "sc"))
	require.False(t, has("k1"))
	require.False(t, has("k2"))
	require.True(t, has("k3"), "只删除这个命名空间的条目")

	set("k1", "s1", nsA)
	set("k2", "s2", nsA)
	require.NoError(t, mc.DeleteSession(ctx, "s1"))
	require.False(t, has("k1"))
	require.False(t, has("k3"))
	require.True(t, has("k2"), "只删除这个会话的条目")

	off := mc
	off.Erase = EraseOff
	require.Nil(t, off.Eraser())
	require.Nil(t, ModelCache{Cache: untaggedCache{sc}}.Eraser())
}

// Worker 不经过 Builder 校验：会真正做标签删除时 TagKey 不足必须报错，不能静默落空；不做标签删除时保持空操作。
func TestModelCacheEraseTagKeyCheck(t *testing.T) {
	ctx := context.Background()
	sc := cache.NewSimpleCache(time.Minute)
	for _, tt := range []struct {
		name      string
		mc        ModelCache
		nilEraser bool
		wantErr   bool
	}{
		{name: "TagKey 为空", mc: ModelCache{Cache: sc}, wantErr: true},
		{name: "TagKey 不足 32 字节", mc: ModelCache{Cache: sc, TagKey: []byte("short")}, wantErr: true},
		{name: "EraseRequired 且 TagKey 不足", mc: ModelCache{Cache: sc, Erase: EraseRequired}, wantErr: true},
		{name: "TagKey 合法", mc: ModelCache{Cache: sc, TagKey: testTagKey}},
		{name: "EraseOff 不校验", mc: ModelCache{Cache: sc, Erase: EraseOff}, nilEraser: true},
		{name: "缓存不支持 Tagged 不校验", mc: ModelCache{Cache: untaggedCache{sc}}, nilEraser: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			check := func(err error) {
				if tt.wantErr {
					require.Error(t, err)
					require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
					return
				}
				require.NoError(t, err)
			}
			check(tt.mc.DeleteSession(ctx, "s1"))
			eraser := tt.mc.Eraser()
			if tt.nilEraser {
				require.Nil(t, eraser)
				return
			}
			check(eraser(ctx, []string{"t", "a"}, "sc"))
		})
	}
}

// Validate 供 Worker 启动时自检：只检查与长期记忆无关的规则，文案与 Build 一致。
func TestModelCacheValidate(t *testing.T) {
	sc := cache.NewSimpleCache(time.Minute)
	valid := ModelCache{Cache: sc, TTL: time.Minute, TagKey: testTagKey}
	with := func(f func(*ModelCache)) ModelCache { mc := valid; f(&mc); return mc }
	for _, tt := range []struct {
		name    string
		mc      ModelCache
		wantErr string
	}{
		{name: "合法配置，Worker 不需要 Scope", mc: valid},
		{name: "Cache 为空", mc: with(func(m *ModelCache) { m.Cache = nil }), wantErr: "TTL 必须大于 0"},
		{name: "TTL 为 0", mc: with(func(m *ModelCache) { m.TTL = 0 }), wantErr: "TTL 必须大于 0"},
		{name: "TagKey 为空", mc: with(func(m *ModelCache) { m.TagKey = nil }), wantErr: "TagKey 至少 32 字节"},
		{name: "TagKey 不足 32 字节", mc: with(func(m *ModelCache) { m.TagKey = []byte("short") }), wantErr: "TagKey 至少 32 字节"},
		{name: "EraseOff 不要求 TagKey", mc: with(func(m *ModelCache) { m.TagKey = nil; m.Erase = EraseOff })},
		{name: "缓存不支持 Tagged 不要求 TagKey", mc: ModelCache{Cache: untaggedCache{sc}, TTL: time.Minute}},
		{name: "EraseRequired 缓存不支持 Tagged", mc: ModelCache{Cache: untaggedCache{sc}, TTL: time.Minute, Erase: EraseRequired}, wantErr: "cache.Tagged"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.mc.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
			require.Contains(t, err.Error(), tt.wantErr)
			// Build 对同一配置报同一条错误。
			_, berr := NewSimpleBuilder(newScriptModel()).WithModelCache(with(func(m *ModelCache) { *m = tt.mc; m.Scope = fixedScope })).Build()
			require.Error(t, berr)
			require.Equal(t, err.Error(), berr.Error())
		})
	}
}

// failingWriteCache 的写入总是失败，并记录 Delete 的键与收到的 ctx 是否已取消。
type failingWriteCache struct {
	cache.Bytes
	deleted   []string
	ctxErrors []error
}

func (f *failingWriteCache) SetBytes(context.Context, string, []byte, time.Duration) error {
	return errors.New("写入失败")
}

func (f *failingWriteCache) SetTagged(context.Context, string, []byte, time.Duration, ...string) error {
	return errors.New("写入失败")
}

func (f *failingWriteCache) DeleteTag(context.Context, string) error { return nil }

func (f *failingWriteCache) Delete(ctx context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	f.ctxErrors = append(f.ctxErrors, ctx.Err())
	return nil
}

func TestModelCacheStoreWriteErrorDeletes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		erase     EraseMode
		cancelled bool
	}{
		{"EraseOff", EraseOff, false},
		{"EraseAuto", EraseAuto, false},
		{"ctx 已取消仍删除", EraseOff, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fc := &failingWriteCache{Bytes: cache.NewSimpleCache(time.Hour, cache.WithoutBackgroundCleanup())}
			cfg := ModelCache{Cache: fc, TTL: time.Minute, TagKey: testTagKey, Erase: tc.erase}
			e := &modelCacheEntry{scope: &modelCacheScope{cfg: cfg, sessionID: "s1"}, key: "k1"}
			ctx, cancel := context.WithCancel(context.Background())
			if tc.cancelled {
				cancel()
			}
			defer cancel()
			e.store(ctx, &llm.StreamChunk{FinishReason: "stop", Content: "ok"}, func(*llm.StreamChunk) error { return nil })
			require.Equal(t, []string{"k1"}, fc.deleted)
			require.Equal(t, []error{nil}, fc.ctxErrors, "Delete 收到的 ctx 不应已取消")
		})
	}
}
