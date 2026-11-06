package json

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"io"
	"testing"
)

var benchEvent = map[string]any{"type": "tool_end", "run_id": "r-1", "data": map[string]any{"name": "get_order", "ok": true, "n": 3}}

// 直接调用标准库作为基线，衡量 codec 封装与编码器池的开销。
func BenchmarkMarshalDirect(b *testing.B) {
	for b.Loop() {
		_, _ = jsonv2.Marshal(benchEvent, stable)
	}
}
func BenchmarkMarshalCodec(b *testing.B) {
	for b.Loop() {
		_, _ = Marshal(benchEvent)
	}
}
func BenchmarkEncodeReused(b *testing.B) {
	enc := jsontext.NewEncoder(io.Discard, stable)
	for b.Loop() {
		_ = jsonv2.MarshalEncode(enc, benchEvent)
	}
}
func BenchmarkEncodeJSON(b *testing.B) {
	for b.Loop() {
		_ = Encode(io.Discard, benchEvent)
	}
}
