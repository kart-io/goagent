// Package spill 把超长的工具结果写入宿主提供的目录，交给模型的结果替换为预览与文件名，
// 模型需要时用 read_spill 分段读取。目录不在工作区内，不受工作区只读模式与改动记录影响。
package spill

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kart-io/goagent/harness/env"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// ToolReadSpill 是读取溢出文件的工具名。
const ToolReadSpill = "read_spill"

const (
	previewRunes     = 1500
	defaultReadLimit = 2000
	maxLineRunes     = 2000
)

// Store 是溢出目录。溢出文件按会话分目录，文件名由工具名与调用 ID 组成。
type Store struct {
	fs       *env.LocalFS
	maxBytes int
}

// New 以 dir 为溢出目录（不存在时创建），结果的文本形式超过 maxBytes 字节时溢出。
func New(dir string, maxBytes int) (*Store, error) {
	if maxBytes <= 0 {
		return nil, errors.New("spill max bytes must be positive")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	local, err := env.NewLocalFS(dir)
	if err != nil {
		return nil, err
	}
	return &Store{fs: local, maxBytes: maxBytes}, nil
}

// Dir 返回溢出目录。
func (s *Store) Dir() string { return s.fs.Root() }

// Hook 返回调用后钩子，适用于任何工具的成功结果。写入失败时仍返回预览并说明原因，不报错，
// 以免已经产生副作用的工具因为溢出失败被重新执行。
func (s *Store) Hook() tools.PostHook {
	return func(ctx context.Context, call *tools.Call, out *interfaces.ToolOutput) (tools.PostDecision, error) {
		if !out.Success {
			return tools.PostDecision{}, nil
		}
		data, err := spillText(out.Result)
		if err != nil || len(data) <= s.maxBytes {
			return tools.PostDecision{}, nil
		}
		preview := []rune(string(data))
		if len(preview) > previewRunes {
			preview = preview[:previewRunes]
		}
		result := map[string]any{"truncated": true, "bytes": len(data), "preview": string(preview)}
		name, err := s.write(ctx, call, data)
		if err != nil {
			result["note"] = "the full result could not be saved (" + err.Error() + "); only the preview is available"
		} else {
			result["spill_path"] = name
			result["note"] = "the full result was saved to spill_path; read it with " + ToolReadSpill + " using offset and limit"
		}
		replaced := *out
		replaced.Result = result
		return tools.PostDecision{Replace: &replaced}, nil
	}
}

func (s *Store) write(ctx context.Context, call *tools.Call, data []byte) (string, error) {
	id := call.CallID
	if id == "" {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			return "", err
		}
		id = hex.EncodeToString(b)
	}
	session := call.SessionID
	if session == "" {
		session = "default"
	}
	name := sanitize(session) + "/" + sanitize(call.Tool) + "-" + sanitize(id) + ".txt"
	if _, err := s.fs.WriteFile(ctx, name, data, env.WriteIntent{}); err != nil {
		return "", err
	}
	return name, nil
}

// Prune 删除修改时间早于 olderThan 之前的溢出文件。
func (s *Store) Prune(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	return filepath.WalkDir(s.Dir(), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			return os.Remove(p)
		}
		return nil
	})
}

// ReadTool 返回 read_spill 工具，只能读取本目录内的溢出文件。
func (s *Store) ReadTool() interfaces.Tool {
	return tools.NewBaseTool(ToolReadSpill,
		"读取被截断的工具结果的完整内容，path 为截断结果中的 spill_path。返回带行号的内容（格式为 行号<TAB>内容），用 offset 与 limit 分段读取。",
		`{
			"type": "object",
			"properties": {
				"path": {"type": "string", "description": "截断结果中的 spill_path"},
				"offset": {"type": "integer", "minimum": 1, "description": "起始行号，从 1 开始，默认 1"},
				"limit": {"type": "integer", "minimum": 1, "description": "最多读取的行数，默认 2000"}
			},
			"required": ["path"],
			"additionalProperties": false
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			name, _ := input.Args["path"].(string)
			offset, limit := intArg(input.Args["offset"], 1), intArg(input.Args["limit"], defaultReadLimit)
			if offset < 1 || limit < 1 {
				return tools.Rejected("offset and limit must be integers >= 1", nil), nil
			}
			data, _, err := s.fs.ReadFile(ctx, name)
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, env.ErrOutsideWorkspace) {
				return tools.Rejected("spill file "+name+" does not exist (it may have been cleaned up)", nil), nil
			}
			if err != nil {
				return nil, err
			}
			lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if offset > len(lines) {
				return tools.Rejected(fmt.Sprintf("offset %d exceeds total lines %d", offset, len(lines)), nil), nil
			}
			end := min(len(lines), offset-1+limit)
			var sb strings.Builder
			for i := offset - 1; i < end; i++ {
				line := lines[i]
				if r := []rune(line); len(r) > maxLineRunes {
					line = string(r[:maxLineRunes]) + "…"
				}
				fmt.Fprintf(&sb, "%6d\t%s\n", i+1, line)
			}
			return &interfaces.ToolOutput{Success: true, Result: map[string]any{
				"path": name, "content": sb.String(), "start_line": offset, "end_line": end, "total_lines": len(lines),
			}}, nil
		})
}

// intArg 接受 JSON 数字（float64、json.Number）与整数，缺省或无法解析时返回 def。
func intArg(v any, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		if i, err := strconv.Atoi(n.String()); err == nil {
			return i
		}
		return 0
	}
	return def
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
// 其余值写成缩进 JSON。原样写出避免长字符串被转义成一行而超出单行上限。
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
