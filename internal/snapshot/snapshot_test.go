package snapshot

import (
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/require"
)

func TestCloneErrorClassification(t *testing.T) {
	for _, value := range []interface{}{sync.Mutex{}, invalidCloner{}, opaqueValue{data: []byte{1}}, make(chan int)} {
		_, err := Clone(value)
		require.Equal(t, agentErrors.CodeInvalidInput, agentErrors.GetCode(err), "%T 必须使用明确的输入错误分类", value)
		var classified *agentErrors.AgentError
		require.ErrorAs(t, err, &classified)
		require.Equal(t, "snapshot", classified.Component)
		require.Equal(t, "clone", classified.Operation)
	}
}

type namedBytes []byte
type namedMap map[string]namedBytes
type cycleNode struct {
	Value  namedMap
	Next   *cycleNode
	Shared *cycleNode
}
type opaqueValue struct{ data []byte }
type namedMutex sync.Mutex
type cloningValue struct{ data []byte }

func (v *cloningValue) CloneSnapshot() (interface{}, error) {
	return &cloningValue{data: append([]byte(nil), v.data...)}, nil
}

type invalidCloner struct{}

func (invalidCloner) CloneSnapshot() (interface{}, error) { return "错误类型", nil }

type failingCloner struct{}

func (failingCloner) CloneSnapshot() (interface{}, error) { return nil, errors.New("拒绝复制") }

func TestClonePreservesTypesCyclesAndAliases(t *testing.T) {
	original := &cycleNode{Value: namedMap{"v": namedBytes{1}}}
	original.Next = original
	original.Shared = original
	cloned, err := Clone(original)
	require.NoError(t, err)
	copy := cloned.(*cycleNode)
	require.NotSame(t, original, copy)
	require.Same(t, copy, copy.Next)
	require.Same(t, copy, copy.Shared)
	copy.Value["v"][0] = 9
	require.Equal(t, byte(1), original.Value["v"][0])

	m := map[string]interface{}{}
	m["self"] = m
	mCopy, err := Clone(m)
	require.NoError(t, err)
	require.Equal(t, reflect.ValueOf(mCopy).Pointer(), reflect.ValueOf(mCopy.(map[string]interface{})["self"]).Pointer())
	require.NotEqual(t, reflect.ValueOf(m).Pointer(), reflect.ValueOf(mCopy).Pointer())

	s := make([]interface{}, 1)
	s[0] = s
	sCopy, err := Clone(s)
	require.NoError(t, err)
	require.Equal(t, reflect.ValueOf(sCopy).Pointer(), reflect.ValueOf(sCopy.([]interface{})[0]).Pointer())
	require.NotEqual(t, reflect.ValueOf(s).Pointer(), reflect.ValueOf(sCopy).Pointer())
}

func TestCloneNilTimeAndArray(t *testing.T) {
	for _, original := range []interface{}{nil, (*cycleNode)(nil), namedMap(nil), namedBytes(nil), (func())(nil), (chan int)(nil)} {
		copy, err := Clone(original)
		require.NoError(t, err)
		require.Equal(t, reflect.TypeOf(original), reflect.TypeOf(copy))
		require.True(t, reflect.DeepEqual(original, copy))
	}
	type record struct {
		At      time.Time
		Items   [1]namedBytes
		private string
	}
	original := record{At: time.Now(), Items: [1]namedBytes{{1}}, private: "保留"}
	copy, err := Clone(original)
	require.NoError(t, err)
	require.Equal(t, original, copy)
	copy.(record).Items[0][0] = 9
	require.Equal(t, byte(1), original.Items[0][0])
}

func TestCloneRejectsOpaqueResources(t *testing.T) {
	for _, value := range []interface{}{
		func() {}, make(chan int), opaqueValue{data: []byte{1}}, sync.Mutex{}, namedMutex{}, sync.RWMutex{}, sync.Once{}, sync.WaitGroup{}, sync.Map{}, atomic.Int64{},
		struct{ Lock sync.Mutex }{}, struct{ private sync.Once }{}, invalidCloner{}, failingCloner{},
	} {
		_, err := Clone(value)
		require.Error(t, err, "%T 必须显式拒绝", value)
	}
	original := &cloningValue{data: []byte{1}}
	copy, err := Clone(original)
	require.NoError(t, err)
	copy.(*cloningValue).data[0] = 9
	require.Equal(t, byte(1), original.data[0])
}

func TestClonePreservesInteriorAliases(t *testing.T) {
	type nested struct{ Values [2]int }
	type record struct {
		Alias       *int
		ArrayAlias  *int
		NestedAlias *nested
		Value       int
		Nested      nested
	}
	original := &record{Value: 1, Nested: nested{Values: [2]int{2, 3}}}
	original.Alias = &original.Value
	original.ArrayAlias = &original.Nested.Values[1]
	original.NestedAlias = &original.Nested
	value, err := Clone(original)
	require.NoError(t, err)
	copy := value.(*record)
	require.Same(t, &copy.Value, copy.Alias, "指针字段必须指向副本的内部字段")
	require.Same(t, &copy.Nested.Values[1], copy.ArrayAlias)
	require.Same(t, &copy.Nested, copy.NestedAlias)
	copy.Value = 9
	copy.Nested.Values[1] = 8
	require.Equal(t, 9, *copy.Alias)
	require.Equal(t, 8, *copy.ArrayAlias)
	require.Equal(t, 1, original.Value)
	require.Equal(t, 3, original.Nested.Values[1])
}

func TestClonePreservesNamedPointerAliases(t *testing.T) {
	type intPointer *int
	t.Run("指针字段在值前", func(t *testing.T) {
		type record struct {
			Alias intPointer
			Plain *int
			Value int
		}
		original := &record{Value: 1}
		original.Alias, original.Plain = &original.Value, &original.Value
		value, err := Clone(original)
		require.NoError(t, err)
		copy := value.(*record)
		require.Same(t, &copy.Value, (*int)(copy.Alias))
		require.Same(t, copy.Plain, (*int)(copy.Alias))
		copy.Value = 9
		require.Equal(t, 9, *copy.Alias)
		require.Equal(t, 1, original.Value)
	})
	t.Run("指针字段在值后", func(t *testing.T) {
		type record struct {
			Value int
			Plain *int
			Alias intPointer
		}
		original := &record{Value: 1}
		original.Alias, original.Plain = &original.Value, &original.Value
		value, err := Clone(original)
		require.NoError(t, err)
		copy := value.(*record)
		require.Same(t, &copy.Value, (*int)(copy.Alias))
		require.Same(t, copy.Plain, (*int)(copy.Alias))
		*copy.Alias = 9
		require.Equal(t, 9, copy.Value)
		require.Equal(t, 1, original.Value)
	})
	t.Run("命名指针先分配与普通视图同址", func(t *testing.T) {
		type record struct {
			Alias intPointer
			Plain *int
		}
		integer := 1
		original := record{Alias: &integer, Plain: &integer}
		value, err := Clone(original)
		require.NoError(t, err)
		copy := value.(record)
		require.Same(t, copy.Plain, (*int)(copy.Alias))
		*copy.Alias = 9
		require.Equal(t, 9, *copy.Plain)
		require.Equal(t, 1, integer)
	})
}
