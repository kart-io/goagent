package state

import (
	"fmt"
	"math"
	"testing"

	"github.com/kart-io/goagent/codec/json"
	"github.com/stretchr/testify/require"
)

func TestReviewRound2SchemaArrayByteEncoding(t *testing.T) {
	type octets []uint8
	for _, itemType := range []FieldType{"", FieldTypeInt} {
		for _, value := range []any{[]byte{1, 2}, octets{1, 2}, [2]uint8{1, 2}} {
			t.Run(string(itemType)+"/"+fmt.Sprintf("%T", value), func(t *testing.T) {
				schema := NewStateSchema("字节数组", "1").AddField(&FieldSchema{Name: "值", Type: FieldTypeArray, ItemType: itemType})
				original := NewTypedState(schema)
				require.NoError(t, original.Set("值", value))
				wire, err := json.Marshal(original)
				require.NoError(t, err)
				var envelope struct {
					Data map[string]json.RawMessage `json:"data"`
				}
				require.NoError(t, json.Unmarshal(wire, &envelope))
				require.JSONEq(t, `[1,2]`, string(envelope.Data["值"]))
				restored := NewTypedState(schema)
				require.NoError(t, json.Unmarshal(wire, restored))
				require.NoError(t, schema.Validate(restored.ToMap()))
			})
		}
	}
}

func TestReviewRound2NestedArrayAndBytesCoexist(t *testing.T) {
	type octets []uint8
	inner := NewStateSchema("对象", "1").AddField(&FieldSchema{Name: "数组", Type: FieldTypeArray, ItemType: FieldTypeInt}).AddField(&FieldSchema{Name: "字节", Type: FieldTypeBytes})
	schema := NewStateSchema("混合", "1").AddField(&FieldSchema{Name: "对象", Type: FieldTypeObject, ObjectSchema: inner}).AddField(&FieldSchema{Name: "嵌套数组", Type: FieldTypeArray, ItemType: FieldTypeArray}).AddField(&FieldSchema{Name: "字节数组", Type: FieldTypeArray, ItemType: FieldTypeBytes}).AddField(&FieldSchema{Name: "字节", Type: FieldTypeBytes}).AddField(&FieldSchema{Name: "大整数", Type: FieldTypeArray, ItemType: FieldTypeInt})
	original := NewTypedState(schema)
	require.NoError(t, original.FromMap(map[string]any{"对象": map[string]any{"数组": octets{1, 2}, "字节": []byte{1, 2}}, "嵌套数组": [][]byte{{1, 2}, {3}}, "字节数组": [][]byte{{1, 2}, {3}}, "字节": []byte{1, 2}, "大整数": []int64{9007199254740993, math.MaxInt64}}))
	wire, err := json.Marshal(original)
	require.NoError(t, err)
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(wire, &envelope))
	require.JSONEq(t, `{"数组":[1,2],"字节":"AQI="}`, string(envelope.Data["对象"]))
	require.JSONEq(t, `[[1,2],[3]]`, string(envelope.Data["嵌套数组"]))
	require.JSONEq(t, `["AQI=","Aw=="]`, string(envelope.Data["字节数组"]))
	require.Equal(t, `"AQI="`, string(envelope.Data["字节"]))
	require.Equal(t, `[9007199254740993,9223372036854775807]`, string(envelope.Data["大整数"]))
	restored := NewTypedState(schema)
	require.NoError(t, json.Unmarshal(wire, restored))
	require.NoError(t, schema.Validate(restored.ToMap()))
	require.Equal(t, []any{int64(9007199254740993), int64(math.MaxInt64)}, restored.ToMap()["大整数"])
	require.Equal(t, []byte{1, 2}, restored.ToMap()["字节"])
	require.Equal(t, []any{[]byte{1, 2}, []byte{3}}, restored.ToMap()["字节数组"])
	// 编码投影不得改写宿主传入的具体容器，严格 Set 规则保持原状。
	require.IsType(t, octets{}, original.ToMap()["对象"].(map[string]any)["数组"])
	require.Error(t, original.Set("大整数", []string{"1"}))
}
