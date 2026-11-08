package state

import (
	"context"
	"errors"
	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/stretchr/testify/require"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestRuntimeErrorGateStateClassification(t *testing.T) {
	for _, kind := range []FieldType{FieldTypeBytes, FieldTypeTime, FieldType("未知")} {
		require.True(t, agentErrors.IsCode(validateType(kind, 123), agentErrors.CodeStateValidation))
	}
	for _, number := range []json.Number{"1e9999", "9223372036854775809", "1e-9999", "1.1"} {
		_, err := decodeStateJSONInteger(number)
		require.True(t, agentErrors.IsCode(err, agentErrors.CodeStateValidation))
	}
	_, err := decodeStateJSONInteger("1e9999")
	var cause *strconv.NumError
	require.True(t, errors.As(err, &cause), "保留数字解析原因链")
}

func TestReviewRT02StringWriterLiveness(t *testing.T) {
	s := NewAgentState()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				_ = s.String()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ctx.Err() == nil {
			s.Set("值", 1)
		}
	}()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("String与写者死锁")
	}
}

func TestReviewRT07SchemaTypes(t *testing.T) {
	nested := NewStateSchema("内层", "1").AddField(&FieldSchema{Name: "数量", Type: FieldTypeInt, Required: true})
	cases := []struct {
		name  string
		field FieldSchema
		value any
		valid bool
	}{
		{"数组非法", FieldSchema{Type: FieldTypeArray, ItemType: FieldTypeInt}, []string{"错"}, false},
		{"数组合法", FieldSchema{Type: FieldTypeArray, ItemType: FieldTypeInt}, []int{1}, true},
		{"字节非法", FieldSchema{Type: FieldTypeBytes}, 123, false}, {"字节合法", FieldSchema{Type: FieldTypeBytes}, []byte{1}, true},
		{"时间非法", FieldSchema{Type: FieldTypeTime}, "今天", false}, {"时间合法", FieldSchema{Type: FieldTypeTime}, time.Now(), true},
		{"小数", FieldSchema{Type: FieldTypeInt}, 1.5, false}, {"无穷", FieldSchema{Type: FieldTypeInt}, math.Inf(1), false},
		{"溢出", FieldSchema{Type: FieldTypeInt}, float64(math.MaxInt64), false}, {"小整数", FieldSchema{Type: FieldTypeInt}, int8(2), true},
		{"未知", FieldSchema{Type: FieldType("未知")}, 1, false},
		{"嵌套别名", FieldSchema{Type: FieldTypeObject, ObjectSchema: nested}, map[string]string{"数量": "错"}, false},
		{"非字符串键", FieldSchema{Type: FieldTypeObject}, map[int]int{1: 1}, false},
		{"必需非空", FieldSchema{Type: FieldTypeInt, Required: true}, nil, false},
		{"空值未知类型", FieldSchema{Type: FieldType("未知")}, nil, false},
		{"空数组未知元素", FieldSchema{Type: FieldTypeArray, ItemType: FieldType("未知")}, []any{}, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			tt.field.Name = "字段"
			s := NewStateSchema("测试", "1").AddField(&tt.field)
			err := s.Validate(map[string]any{"字段": tt.value})
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestReviewRT07TypedGetters(t *testing.T) {
	type text string
	type flag bool
	s := NewTypedState(NewStateSchema("读取", "1").AddField(&FieldSchema{Name: "文本", Type: FieldTypeString}).AddField(&FieldSchema{Name: "标记", Type: FieldTypeBool}).AddField(&FieldSchema{Name: "浮点", Type: FieldTypeFloat}).AddField(&FieldSchema{Name: "整数", Type: FieldTypeInt}))
	require.NoError(t, s.Set("文本", text("值")))
	require.NoError(t, s.Set("标记", flag(true)))
	require.NoError(t, s.Set("浮点", float32(1.5)))
	require.NoError(t, s.Set("整数", int16(7)))
	v, err := s.GetString("文本")
	require.NoError(t, err)
	require.Equal(t, "值", v)
	b, err := s.GetBool("标记")
	require.NoError(t, err)
	require.True(t, b)
	f, err := s.GetFloat("浮点")
	require.NoError(t, err)
	require.Equal(t, 1.5, f)
	i, err := s.GetInt("整数")
	require.NoError(t, err)
	require.Equal(t, int64(7), i)
}
