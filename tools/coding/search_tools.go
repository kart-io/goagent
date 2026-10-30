package coding

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/kart-io/goagent/harness/env"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

const (
	defaultGlobLimit = 200
	defaultGrepLimit = 100
	maxMatchRunes    = 500
)

// walkFiles 遍历 base 下的普通文件并跳过 .git 目录，fn 返回 fs.SkipAll 时提前结束。
// 遍历中途 ctx 结束时返回 ctx 的错误。
func (w *Workspace) walkFiles(ctx context.Context, base string, fn func(env.Info) error) error {
	return w.fs.Walk(ctx, base, func(info env.Info) error {
		if info.IsDir() {
			if path.Base(info.Path) == ".git" && info.Path != base {
				return fs.SkipDir
			}
			return nil
		}
		if !info.Mode.IsRegular() {
			return nil
		}
		return fn(info)
	})
}

// relTo 返回 p 相对 base 的路径，二者都是相对工作区根目录的路径。
func relTo(base, p string) string {
	if base == "." {
		return p
	}
	if p == base {
		return path.Base(p)
	}
	return strings.TrimPrefix(p, base+"/")
}

// matchGlob 按 / 分段匹配，"**" 匹配零个或多个路径段，其余段使用 path.Match 语法。
// 按 (模式位置, 路径位置) 记忆结果，多个 ** 也是多项式时间。
func matchGlob(pattern, name []string) bool {
	memo := map[[2]int]bool{}
	var match func(pi, ni int) bool
	match = func(pi, ni int) bool {
		key := [2]int{pi, ni}
		if v, ok := memo[key]; ok {
			return v
		}
		var r bool
		switch {
		case pi == len(pattern):
			r = ni == len(name)
		case pattern[pi] == "**":
			r = match(pi+1, ni) || (ni < len(name) && match(pi, ni+1))
		case ni == len(name):
			r = false
		default:
			ok, _ := path.Match(pattern[pi], name[ni])
			r = ok && match(pi+1, ni+1)
		}
		memo[key] = r
		return r
	}
	return match(0, 0)
}

// normalizeGlob 去掉模型常写的 ./ 前缀和结尾的 /。
func normalizeGlob(p string) string {
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	return strings.TrimSuffix(p, "/")
}

func validGlob(pattern string) bool {
	for _, seg := range strings.Split(pattern, "/") {
		if _, err := path.Match(seg, ""); err != nil {
			return false
		}
	}
	return pattern != ""
}

// resolveBase 解析可选的搜索起点参数，空串表示工作区根目录，起点必须存在。
func (w *Workspace) resolveBase(ctx context.Context, p string) (string, env.Info, error) {
	if p == "" {
		p = "."
	}
	rel, err := w.fs.Resolve(p)
	if err != nil {
		return "", env.Info{}, err
	}
	info, err := w.fs.Stat(ctx, rel)
	if err != nil {
		return "", env.Info{}, errors.New(rel + " does not exist")
	}
	return rel, info, nil
}

func newGlobTool(w *Workspace) interfaces.Tool {
	return tools.NewBaseTool(ToolGlob,
		"按 glob 模式查找文件，返回相对工作区根目录的路径，按修改时间从新到旧排序。模式相对 path 匹配，用 / 分隔，** 匹配任意层目录，例如 **/*.go、cmd/*/main.go。跳过 .git 目录。",
		`{
			"type": "object",
			"properties": {
				"pattern": {"type": "string", "description": "glob 模式"},
				"path": {"type": "string", "description": "搜索的目录，默认工作区根目录"},
				"limit": {"type": "integer", "minimum": 1, "description": "最多返回的文件数，默认 200"}
			},
			"required": ["pattern"]
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			resp := tools.NewToolErrorResponse(ToolGlob).WithOperation("glob")
			pattern := normalizeGlob(stringArg(input.Args, "pattern"))
			if !validGlob(pattern) {
				return resp.ValidationError("pattern must be a valid glob pattern", "field", "pattern")
			}
			base, _, err := w.resolveBase(ctx, stringArg(input.Args, "path"))
			if err != nil {
				return resp.ValidationError(err.Error(), "field", "path")
			}
			limit, err := intArg(input.Args, "limit", defaultGlobLimit)
			if err != nil || limit < 1 {
				return resp.ValidationError("limit must be an integer >= 1", "field", "limit")
			}

			type hit struct {
				rel string
				mod int64
			}
			var hits []hit
			segs := strings.Split(pattern, "/")
			err = w.walkFiles(ctx, base, func(info env.Info) error {
				if matchGlob(segs, strings.Split(relTo(base, info.Path), "/")) {
					hits = append(hits, hit{rel: info.Path, mod: info.ModTime.UnixNano()})
				}
				return nil
			})
			if err != nil {
				return resp.ExecutionError("glob failed", nil, err)
			}
			sort.SliceStable(hits, func(i, j int) bool {
				if hits[i].mod != hits[j].mod {
					return hits[i].mod > hits[j].mod
				}
				return hits[i].rel < hits[j].rel
			})
			truncated := len(hits) > limit
			if truncated {
				hits = hits[:limit]
			}
			files := make([]string, len(hits))
			for i, h := range hits {
				files[i] = h.rel
			}
			return resp.Success(map[string]any{"files": files, "count": len(files), "truncated": truncated}, nil)
		})
}

type grepMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

func newGrepTool(w *Workspace) interfaces.Tool {
	return tools.NewBaseTool(ToolGrep,
		"用正则表达式（RE2 语法）搜索文件内容，返回匹配的文件路径、行号和行文本。path 可以是文件或目录；include 按 glob 过滤文件：不含 / 时只匹配文件名（如 *.go），含 / 时相对 path 匹配（如 internal/**/*.go）。跳过 .git 目录和二进制文件。搜索不算读取，修改文件前仍需 read_file。",
		`{
			"type": "object",
			"properties": {
				"pattern": {"type": "string", "description": "正则表达式"},
				"path": {"type": "string", "description": "搜索的文件或目录，默认工作区根目录"},
				"include": {"type": "string", "description": "文件过滤 glob，如 *.go 或 **/*_test.go"},
				"ignore_case": {"type": "boolean", "description": "是否忽略大小写，默认 false"},
				"limit": {"type": "integer", "minimum": 1, "description": "最多返回的匹配行数，默认 100"}
			},
			"required": ["pattern"]
		}`,
		func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			resp := tools.NewToolErrorResponse(ToolGrep).WithOperation("grep")
			expr := stringArg(input.Args, "pattern")
			if expr == "" {
				return resp.ValidationError("pattern is required", "field", "pattern")
			}
			if boolArg(input.Args, "ignore_case") {
				expr = "(?i)" + expr
			}
			re, err := regexp.Compile(expr)
			if err != nil {
				return resp.ValidationError("invalid regular expression: "+err.Error(), "field", "pattern")
			}
			include := normalizeGlob(stringArg(input.Args, "include"))
			if include != "" && !validGlob(include) {
				return resp.ValidationError("include must be a valid glob pattern", "field", "include")
			}
			base, baseInfo, err := w.resolveBase(ctx, stringArg(input.Args, "path"))
			if err != nil {
				return resp.ValidationError(err.Error(), "field", "path")
			}
			limit, err := intArg(input.Args, "limit", defaultGrepLimit)
			if err != nil || limit < 1 {
				return resp.ValidationError("limit must be an integer >= 1", "field", "limit")
			}

			// include 相对搜索目录匹配；path 是文件时以其所在目录为准。
			includeBase := base
			if !baseInfo.IsDir() {
				includeBase = path.Dir(base)
			}
			includeSegs := strings.Split(include, "/")
			matches := make([]grepMatch, 0)
			truncated := false
			err = w.walkFiles(ctx, base, func(info env.Info) error {
				if include != "" {
					name := []string{path.Base(info.Path)}
					if len(includeSegs) > 1 {
						name = strings.Split(relTo(includeBase, info.Path), "/")
					}
					if !matchGlob(includeSegs, name) {
						return nil
					}
				}
				data, err := w.readText(ctx, info.Path, false)
				if err != nil {
					return nil
				}
				sc := bufio.NewScanner(bytes.NewReader(data))
				sc.Buffer(make([]byte, 0, 64*1024), maxFileBytes)
				for n := 1; sc.Scan(); n++ {
					line := sc.Text()
					if !re.MatchString(line) {
						continue
					}
					if len(matches) == limit {
						truncated = true
						return fs.SkipAll
					}
					if r := []rune(line); len(r) > maxMatchRunes {
						line = string(r[:maxMatchRunes]) + "…"
					}
					matches = append(matches, grepMatch{Path: info.Path, Line: n, Text: line})
				}
				return nil
			})
			if err != nil {
				return resp.ExecutionError("grep failed", nil, err)
			}
			return resp.Success(map[string]any{"matches": matches, "count": len(matches), "truncated": truncated}, nil)
		})
}
