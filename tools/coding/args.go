package coding

import (
	"encoding/json"
	"fmt"
	"math"
)

// 模型参数可能来自 JSON 解码（数字为 float64）或 Dispatcher 日志回放（数字为 json.Number）。

func stringArg(args map[string]any, key string) string {
	s, _ := args[key].(string)
	return s
}

func boolArg(args map[string]any, key string) bool {
	b, _ := args[key].(bool)
	return b
}

func intArg(args map[string]any, key string, def int) (int, error) {
	switch v := args[key].(type) {
	case nil:
		return def, nil
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		if v == math.Trunc(v) {
			return int(v), nil
		}
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n), nil
		}
	}
	return 0, fmt.Errorf("%s must be an integer", key)
}

func stringsArg(args map[string]any, key string) ([]string, error) {
	switch v := args[key].(type) {
	case nil:
		return nil, nil
	case []string:
		return v, nil
	case []any:
		out := make([]string, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be an array of strings", key)
			}
			out[i] = s
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s must be an array of strings", key)
}
