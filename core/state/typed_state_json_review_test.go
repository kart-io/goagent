package state

import (
	"math"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
	"github.com/stretchr/testify/require"
)

func reviewJSONSchema() *StateSchema {
	nested := NewStateSchema("内层", "1").AddField(&FieldSchema{Name: "字节", Type: FieldTypeBytes}).AddField(&FieldSchema{Name: "时间", Type: FieldTypeTime}).AddField(&FieldSchema{Name: "整数", Type: FieldTypeInt})
	return NewStateSchema("往返", "1").AddField(&FieldSchema{Name: "字节", Type: FieldTypeBytes}).AddField(&FieldSchema{Name: "时间", Type: FieldTypeTime}).AddField(&FieldSchema{Name: "整数", Type: FieldTypeInt}).AddField(&FieldSchema{Name: "嵌套", Type: FieldTypeObject, ObjectSchema: nested}).AddField(&FieldSchema{Name: "字节数组", Type: FieldTypeArray, ItemType: FieldTypeBytes}).AddField(&FieldSchema{Name: "时间数组", Type: FieldTypeArray, ItemType: FieldTypeTime}).AddField(&FieldSchema{Name: "整数数组", Type: FieldTypeArray, ItemType: FieldTypeInt})
}

func TestReviewRound1TypedStateJSONRoundTrip(t *testing.T) {
	schema := reviewJSONSchema()
	original := NewTypedState(schema)
	instant := time.Date(2026, 11, 8, 10, 11, 12, 123456789, time.FixedZone("", 8*60*60))
	require.NoError(t, original.FromMap(map[string]any{"字节": []byte{0, 1, 255}, "时间": instant, "整数": int64(math.MaxInt64), "嵌套": map[string]any{"字节": []byte{3}, "时间": instant, "整数": int64(math.MinInt64)}, "字节数组": [][]byte{{1}, {2}}, "时间数组": []time.Time{instant}, "整数数组": []int64{9007199254740993, math.MaxInt64, math.MinInt64}}))
	wire, err := json.Marshal(original)
	require.NoError(t, err)
	restored := NewTypedState(schema)
	require.NoError(t, json.Unmarshal(wire, restored))
	require.Equal(t, original.ToMap()["字节"], restored.ToMap()["字节"])
	require.Equal(t, instant.Format(time.RFC3339Nano), restored.ToMap()["时间"].(time.Time).Format(time.RFC3339Nano))
	n, err := restored.GetInt("整数")
	require.NoError(t, err)
	require.Equal(t, int64(math.MaxInt64), n)
	nested := restored.ToMap()["嵌套"].(map[string]any)
	require.Equal(t, []byte{3}, nested["字节"])
	require.Equal(t, instant.Format(time.RFC3339Nano), nested["时间"].(time.Time).Format(time.RFC3339Nano))
	require.Equal(t, int64(math.MinInt64), nested["整数"])
	require.Equal(t, []any{[]byte{1}, []byte{2}}, restored.ToMap()["字节数组"])
	require.Equal(t, instant.Format(time.RFC3339Nano), restored.ToMap()["时间数组"].([]any)[0].(time.Time).Format(time.RFC3339Nano))
	require.Equal(t, []any{int64(9007199254740993), int64(math.MaxInt64), int64(math.MinInt64)}, restored.ToMap()["整数数组"])
}

func TestReviewRound1TypedStateJSONExactInteger(t *testing.T) {
	for text, want := range map[string]int64{"9007199254740993": 9007199254740993, "9223372036854775807": math.MaxInt64, "-9223372036854775808": math.MinInt64, "9007199254740993.0": 9007199254740993, "1e3": 1000} {
		t.Run(text, func(t *testing.T) {
			s := NewTypedState(reviewJSONSchema())
			require.NoError(t, json.Unmarshal([]byte(`{"data":{"整数":`+text+`}}`), s))
			got, err := s.GetInt("整数")
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

func TestReviewRound1TypedStateJSONInvalidAtomic(t *testing.T) {
	for _, data := range []string{
		`{"字节":"!!!"}`, `{"时间":"今天"}`, `{"嵌套":{"字节":"!!!"}}`, `{"嵌套":{"时间":"今天"}}`,
		`{"字节数组":["AQ==","!!!"]}`, `{"时间数组":["2026-11-08T00:00:00Z","今天"]}`,
		`{"整数":9223372036854775808}`, `{"整数":-9223372036854775809}`, `{"整数":1.00000000000000000000000001}`, `{"整数数组":[1,9007199254740993.1]}`, `{"整数":"42"}`,
	} {
		t.Run(data, func(t *testing.T) {
			s := NewTypedState(reviewJSONSchema())
			require.NoError(t, s.Set("整数", int64(7)))
			s.MarkClean()
			err := json.Unmarshal([]byte(`{"data":`+data+`}`), s)
			require.Error(t, err)
			require.Equal(t, map[string]any{"整数": int64(7)}, s.ToMap())
			require.False(t, s.IsDirty())
		})
	}
}

func TestReviewRound1FromMapRemainsStrict(t *testing.T) {
	s := NewTypedState(reviewJSONSchema())
	require.Error(t, s.FromMap(map[string]any{"字节": "AQ=="}))
	require.Error(t, s.FromMap(map[string]any{"时间": "2026-11-08T00:00:00Z"}))
	require.Error(t, s.FromMap(map[string]any{"整数": json.Number("42")}))
}

func TestReviewRound1TypedStateJSONNullContainers(t *testing.T) {
	schema := NewStateSchema("空容器", "1").AddField(&FieldSchema{Name: "字节", Type: FieldTypeBytes, Required: true}).AddField(&FieldSchema{Name: "数组", Type: FieldTypeArray, ItemType: FieldTypeInt, Required: true}).AddField(&FieldSchema{Name: "对象", Type: FieldTypeObject, Required: true})
	original := NewTypedState(schema)
	require.NoError(t, original.FromMap(map[string]any{"字节": []byte(nil), "数组": []int(nil), "对象": map[string]any(nil)}))
	wire, err := json.Marshal(original)
	require.NoError(t, err)
	restored := NewTypedState(schema)
	require.NoError(t, json.Unmarshal(wire, restored))
	require.NoError(t, schema.Validate(restored.ToMap()))
	require.Equal(t, []byte(nil), restored.ToMap()["字节"])
}

func TestReviewRound1TypedStateJSONOptionalNullObject(t *testing.T) {
	nested := NewStateSchema("内层", "1").AddField(&FieldSchema{Name: "必需", Type: FieldTypeInt, Required: true})
	schema := NewStateSchema("外层", "1").AddField(&FieldSchema{Name: "对象", Type: FieldTypeObject, ObjectSchema: nested})
	original := NewTypedState(schema)
	require.NoError(t, original.Set("对象", nil))
	wire, err := json.Marshal(original)
	require.NoError(t, err)
	restored := NewTypedState(schema)
	require.NoError(t, json.Unmarshal(wire, restored))
	value, ok := restored.Get("对象")
	require.True(t, ok)
	require.Nil(t, value)
}
