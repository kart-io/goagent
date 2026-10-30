package coding

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/kart-io/goagent/harness/env"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// SpillDir 是溢出结果在工作区内的存放目录，建议加入 .gitignore。
const SpillDir = ".goagent/spill"

const spillPreviewRunes = 1500

// SpillHook 返回调用后钩子：成功结果按 JSON 编码超过 maxBytes 时完整写入工作区的 SpillDir，
// 交给模型的结果替换为预览与文件路径，模型需要时用 read_file 分段读取。适用于任何工具的结果。
// 溢出文件不计入 Changes，也不受先读后改约束。
func SpillHook(w *Workspace, maxBytes int) tools.PostHook {
	return func(ctx context.Context, call *tools.Call, out *interfaces.ToolOutput) (tools.PostDecision, error) {
		if !out.Success || maxBytes <= 0 {
			return tools.PostDecision{}, nil
		}
		data, err := spillText(out.Result)
		if err != nil || len(data) <= maxBytes {
			return tools.PostDecision{}, nil
		}
		id := call.CallID
		if id == "" {
			b := make([]byte, 8)
			if _, err := rand.Read(b); err != nil {
				return tools.PostDecision{}, err
			}
			id = hex.EncodeToString(b)
		}
		name := fmt.Sprintf("%s/%s-%s.txt", SpillDir, sanitize(call.Tool), sanitize(id))
		if _, err := w.base.WriteFile(ctx, name, data, env.WriteIntent{}); err != nil {
			return tools.PostDecision{}, fmt.Errorf("spill %s result: %w", call.Tool, err)
		}
		preview := []rune(string(data))
		if len(preview) > spillPreviewRunes {
			preview = preview[:spillPreviewRunes]
		}
		replaced := *out
		replaced.Result = map[string]any{
			"truncated":  true,
			"bytes":      len(data),
			"preview":    string(preview),
			"spill_path": name,
			"note":       "the full result was saved to spill_path; read it with read_file using offset and limit",
		}
		return tools.PostDecision{Replace: &replaced}, nil
	}
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '.' || r < ' ' {
			return '_'
		}
		return r
	}, s)
}

// spillText 把结果写成可按行读取的文本：字符串原样写出，map 的字符串字段按 "== 键 ==" 分节原样写出，
// 其余值写成缩进 JSON。原样写出避免长字符串被转义成一行而超出 read_file 的单行上限。
func spillText(result any) ([]byte, error) {
	switch v := result.(type) {
	case string:
		return []byte(v), nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sb strings.Builder
		for _, k := range keys {
			fmt.Fprintf(&sb, "== %s ==\n", k)
			if s, ok := v[k].(string); ok {
				sb.WriteString(s)
			} else {
				data, err := json.MarshalIndent(v[k], "", "  ")
				if err != nil {
					return nil, err
				}
				sb.Write(data)
			}
			sb.WriteString("\n")
		}
		return []byte(sb.String()), nil
	}
	return json.MarshalIndent(result, "", "  ")
}
