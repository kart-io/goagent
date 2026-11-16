package memorytest

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/memory"
)

// RunIndex 对 newIndex 返回的索引运行全部用例；每个用例调用一次 newIndex，返回的索引须独立且初始为空。
// 分数比较的容差为 1e-5，用例不构造分差小于 1e-4 的近似并列。
func RunIndex(t *testing.T, newIndex func(t *testing.T) memory.Index) {
	cases := []struct {
		name string
		run  func(t *testing.T, ctx context.Context, x memory.Index)
	}{
		{"归一化", testIndexNormalize},
		{"零向量与非有限值", testIndexInvalidVectors},
		{"排序与并列", testIndexOrderAndTies},
		{"并列按字节序", testIndexTieByteOrder},
		{"k截断与k非正", testIndexLimit},
		{"覆盖写入", testIndexOverwrite},
		{"不同维度共存", testIndexMixedDimensions},
		{"同ID换维度覆盖", testIndexOverwriteDimension},
		{"精确命名空间", testIndexExactNamespace},
		{"非法命名空间", testIndexInvalidNamespace},
		{"删除", testIndexDelete},
		{"空命名空间", testIndexEmptyNamespace},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.run(t, context.Background(), newIndex(t))
		})
	}
}

var indexNS = []string{"t", "alice"}

func upsertAll(t *testing.T, ctx context.Context, x memory.Index, vectors map[string][]float32) {
	t.Helper()
	for id, v := range vectors {
		require.NoError(t, x.Upsert(ctx, indexNS, id, v), id)
	}
}

func hitIDs(hits []memory.Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.ID
	}
	return out
}

func testIndexNormalize(t *testing.T, ctx context.Context, x memory.Index) {
	require.NoError(t, x.Upsert(ctx, indexNS, "a", []float32{3, 4}))
	hits, err := x.Search(ctx, indexNS, []float32{3, 4}, 1)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.InDelta(t, 1, hits[0].Score, 1e-5)
}

func testIndexInvalidVectors(t *testing.T, ctx context.Context, x memory.Index) {
	require.NoError(t, x.Upsert(ctx, indexNS, "ok", []float32{1, 0}))
	for name, v := range map[string][]float32{
		"零向量":  {0, 0},
		"NaN":  {float32(math.NaN()), 1},
		"+Inf": {float32(math.Inf(1)), 1},
	} {
		require.Error(t, x.Upsert(ctx, indexNS, "bad", v), name)
		_, err := x.Search(ctx, indexNS, v, 1)
		require.Error(t, err, name)
	}
	ids, err := x.IDs(ctx, indexNS)
	require.NoError(t, err)
	require.Equal(t, []string{"ok"}, ids, "非法写入不留下键")
}

func testIndexOrderAndTies(t *testing.T, ctx context.Context, x memory.Index) {
	upsertAll(t, ctx, x, map[string][]float32{"a": {1, 0}, "b": {1, 0}, "c": {0, 1}})
	hits, err := x.Search(ctx, indexNS, []float32{1, 0}, 3)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", "c"}, hitIDs(hits), "并列按 ID 升序")
	require.InDelta(t, 1, hits[0].Score, 1e-5)
	require.InDelta(t, 0, hits[2].Score, 1e-5)
}

// 并列的 ID 按字节序升序（与 strings.Compare 一致），不随数据库的排序规则变化。
func testIndexTieByteOrder(t *testing.T, ctx context.Context, x memory.Index) {
	upsertAll(t, ctx, x, map[string][]float32{"a": {1, 0}, "B": {1, 0}, "a-b": {1, 0}, "ab": {1, 0}, "_z": {1, 0}})
	hits, err := x.Search(ctx, indexNS, []float32{1, 0}, 5)
	require.NoError(t, err)
	require.Equal(t, []string{"B", "_z", "a", "a-b", "ab"}, hitIDs(hits))
}

func testIndexLimit(t *testing.T, ctx context.Context, x memory.Index) {
	upsertAll(t, ctx, x, map[string][]float32{"a": {1, 0}, "b": {1, 0}, "c": {0, 1}})
	for _, tt := range []struct {
		k    int
		want []string
	}{
		{1, []string{"a"}},
		{0, nil},
		{-1, nil},
	} {
		hits, err := x.Search(ctx, indexNS, []float32{1, 0}, tt.k)
		require.NoError(t, err, "k=%d", tt.k)
		require.Equal(t, len(tt.want), len(hits), "k=%d", tt.k)
		if len(tt.want) > 0 {
			require.Equal(t, tt.want, hitIDs(hits), "k=%d", tt.k)
		}
	}
}

func testIndexOverwrite(t *testing.T, ctx context.Context, x memory.Index) {
	require.NoError(t, x.Upsert(ctx, indexNS, "a", []float32{1, 0}))
	require.NoError(t, x.Upsert(ctx, indexNS, "a", []float32{0, 1}))
	hits, err := x.Search(ctx, indexNS, []float32{1, 0}, 5)
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, hitIDs(hits), "同 ID 只出现一次")
	require.InDelta(t, 0, hits[0].Score, 1e-5, "分数按新向量")
}

func testIndexMixedDimensions(t *testing.T, ctx context.Context, x memory.Index) {
	require.NoError(t, x.Upsert(ctx, indexNS, "x", []float32{1, 0}))
	require.NoError(t, x.Upsert(ctx, indexNS, "y", []float32{1, 0, 0}))

	hits, err := x.Search(ctx, indexNS, []float32{1, 0}, 5)
	require.NoError(t, err)
	require.Equal(t, []string{"x"}, hitIDs(hits))
	hits, err = x.Search(ctx, indexNS, []float32{1, 0, 0}, 5)
	require.NoError(t, err)
	require.Equal(t, []string{"y"}, hitIDs(hits))

	ids, err := x.IDs(ctx, indexNS)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"x", "y"}, ids)
}

func testIndexOverwriteDimension(t *testing.T, ctx context.Context, x memory.Index) {
	require.NoError(t, x.Upsert(ctx, indexNS, "a", []float32{1, 0}))
	require.NoError(t, x.Upsert(ctx, indexNS, "a", []float32{1, 0, 0}))
	ids, err := x.IDs(ctx, indexNS)
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, ids, "同 ID 只出现一次")
	hits, err := x.Search(ctx, indexNS, []float32{1, 0}, 5)
	require.NoError(t, err)
	require.Empty(t, hits, "旧维度的向量已被覆盖")
	hits, err = x.Search(ctx, indexNS, []float32{1, 0, 0}, 5)
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, hitIDs(hits))
}

func testIndexExactNamespace(t *testing.T, ctx context.Context, x memory.Index) {
	alice, sub, alice2 := []string{"t", "alice"}, []string{"t", "alice", "sub"}, []string{"t", "alice2"}
	for ns, id := range map[*[]string]string{&alice: "a", &sub: "b", &alice2: "c"} {
		require.NoError(t, x.Upsert(ctx, *ns, id, []float32{1, 0}))
	}
	hits, err := x.Search(ctx, alice, []float32{1, 0}, 5)
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, hitIDs(hits))
	ids, err := x.IDs(ctx, alice)
	require.NoError(t, err)
	require.Equal(t, []string{"a"}, ids)

	require.NoError(t, x.Clear(ctx, alice))
	for ns, want := range map[*[]string][]string{&alice: {}, &sub: {"b"}, &alice2: {"c"}} {
		ids, err := x.IDs(ctx, *ns)
		require.NoError(t, err)
		require.ElementsMatch(t, want, ids, "%v", *ns)
	}
}

func testIndexInvalidNamespace(t *testing.T, ctx context.Context, x memory.Index) {
	for name, ns := range map[string][]string{
		"nil":    nil,
		"空白组件":   {"t", " "},
		"含NUL组件": {"t", "a\x00b"},
	} {
		err := x.Upsert(ctx, ns, "a", []float32{1, 0})
		require.Error(t, err, name)
		require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err), name)
	}
}

func testIndexDelete(t *testing.T, ctx context.Context, x memory.Index) {
	upsertAll(t, ctx, x, map[string][]float32{"a": {1, 0}, "b": {1, 0}})
	require.NoError(t, x.Delete(ctx, indexNS, "a"))
	require.NoError(t, x.Delete(ctx, indexNS, "missing"))
	hits, err := x.Search(ctx, indexNS, []float32{1, 0}, 5)
	require.NoError(t, err)
	require.Equal(t, []string{"b"}, hitIDs(hits))
	ids, err := x.IDs(ctx, indexNS)
	require.NoError(t, err)
	require.Equal(t, []string{"b"}, ids)
}

func testIndexEmptyNamespace(t *testing.T, ctx context.Context, x memory.Index) {
	ids, err := x.IDs(ctx, indexNS)
	require.NoError(t, err)
	require.Empty(t, ids)
	hits, err := x.Search(ctx, indexNS, []float32{1, 0}, 5)
	require.NoError(t, err)
	require.Empty(t, hits)
}
