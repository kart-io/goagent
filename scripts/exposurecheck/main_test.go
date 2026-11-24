package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 合成模块：st 为稳定包，ex 为实验包。
var baseFiles = map[string]string{
	"go.mod": "module " + module + "\n\ngo 1.27\n",
	"ex/ex.go": `package ex

import (
	"context"
	"encoding/json"
)

type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
}

type EraseFunc func(ctx context.Context, ns []string) error

type Raw = json.RawMessage

type Handle struct{ n int }

func (h *Handle) Size() int { return h.n }

type Config struct{ A int }

func (c *Config) Name() string { return "" }

func (c Config) Kind() string { return "" }

type Gone struct{}
`,
	"st/st.go": `package st

import (
	"io"

	"` + module + `/ex"
)

type Options struct {
	Cache  ex.Cache
	Config ex.Config
}

func Use(h *ex.Handle) ex.EraseFunc { return nil }

func Payload(r ex.Raw) {}

func Read(r io.Reader) {}
`,
}

const baseRegistry = `# 注释
ex.Cache     full
ex.EraseFunc full
ex.Raw       alias
ex.Handle    identity
ex.Config    full
`

type edit struct{ file, old, new string }

func writeTree(t *testing.T, edits []edit) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range baseFiles {
		for _, e := range edits {
			if e.file == name {
				if !strings.Contains(content, e.old) {
					t.Fatalf("%s 中找不到待替换片段 %q", name, e.old)
				}
				content = strings.Replace(content, e.old, e.new, 1)
			}
		}
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func writeText(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRun(t *testing.T) {
	base := writeTree(t, nil)
	cases := []struct {
		name        string
		edits       []edit
		registry    string
		wantMissing []string // 每项须是某条缺失事实的前缀；nil 表示不应有缺失
		wantErr     string
	}{
		{name: "无变化"},
		{
			name:        "实验接口新增方法",
			edits:       []edit{{"ex/ex.go", "Get(ctx context.Context, key string) ([]byte, error)\n", "Get(ctx context.Context, key string) ([]byte, error)\n\tPut(key string)\n"}},
			wantMissing: []string{"type ex.Cache interface{Get("},
		},
		{
			name:        "实验函数类型改签名",
			edits:       []edit{{"ex/ex.go", "ns []string) error", "ns []string, scope string) error"}},
			wantMissing: []string{"type ex.EraseFunc func(context.Context, []string) error"},
		},
		{
			name:        "别名改指向",
			edits:       []edit{{"ex/ex.go", "type Raw = json.RawMessage", "type Raw = []byte\n\nvar _ json.RawMessage"}},
			wantMissing: []string{"func st.Payload func(encoding/json/jsontext.Value)", "type ex.Raw = encoding/json/jsontext.Value"},
		},
		{
			name:        "标准库类型互换",
			edits:       []edit{{"st/st.go", "func Read(r io.Reader) {}", "func Read(r io.Writer) {}"}},
			wantMissing: []string{"func st.Read func(io.Reader)"},
		},
		{
			name:        "值接收者改为指针接收者",
			edits:       []edit{{"ex/ex.go", "func (c Config) Kind()", "func (c *Config) Kind()"}},
			wantMissing: []string{"method ex.Config.Kind() string on T"},
		},
		{
			name:  "不透明句柄的方法集变化不报",
			edits: []edit{{"ex/ex.go", "func (h *Handle) Size() int { return h.n }", "func (h *Handle) Len() int { return h.n }"}},
		},
		{
			name: "兼容的新增不报",
			edits: []edit{
				{"ex/ex.go", "type Config struct{ A int }", "type Config struct{ A, B int }"},
				{"ex/ex.go", "func (c *Config) Name()", "func (c Config) Name()"},
				{"st/st.go", "func Read(r io.Reader) {}", "func Read(r io.Reader) {}\n\nfunc Write(w io.Writer) {}"},
			},
		},
		{
			name:    "未登记的新暴露",
			edits:   []edit{{"st/st.go", "func Read(r io.Reader) {}", "func Read(r io.Reader) {}\n\nfunc Done() ex.Gone { return ex.Gone{} }"}},
			wantErr: "未登记的传递暴露: ex.Gone（经 st.Done 暴露）",
		},
		{name: "登记已过期", registry: baseRegistry + "ex.Gone full\n", wantErr: "登记已过期: ex.Gone"},
		{name: "别名登记为 full", registry: strings.Replace(baseRegistry, "ex.Raw       alias", "ex.Raw full", 1), wantErr: "登记模式不符: ex.Raw"},
		{name: "非别名登记为 alias", registry: strings.Replace(baseRegistry, "ex.Cache     full", "ex.Cache alias", 1), wantErr: "登记模式不符: ex.Cache"},
		{name: "条目缺少模式", registry: "ex.Cache\n", wantErr: "登记条目须为"},
		{name: "模式无效", registry: "ex.Cache frozen\n", wantErr: "模式无效"},
		{name: "条目重复", registry: baseRegistry + "ex.Cache full\n", wantErr: "重复"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			registry := c.registry
			if registry == "" {
				registry = baseRegistry
			}
			missing, err := run(options{
				head:     writeTree(t, c.edits),
				base:     base,
				stable:   writeText(t, "stable.txt", "# 注释\nst\n"),
				registry: writeText(t, "registry.txt", registry),
			})
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("错误 = %v，期望包含 %q", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.wantMissing == nil && len(missing) > 0 {
				t.Fatalf("不应报出缺失事实，实际:\n%s", strings.Join(missing, "\n"))
			}
			for _, want := range c.wantMissing {
				found := false
				for _, m := range missing {
					found = found || strings.HasPrefix(m, want)
				}
				if !found {
					t.Fatalf("缺失事实中没有 %q，实际:\n%s", want, strings.Join(missing, "\n"))
				}
			}
		})
	}
}

// 基线中尚不存在的稳定包不影响比较；HEAD 中的加载错误必须失败。
func TestLoadMissingPackages(t *testing.T) {
	dir := writeTree(t, nil)
	stable := writeText(t, "stable.txt", "st\nnewpkg\n")
	registry := writeText(t, "registry.txt", baseRegistry)
	if _, err := run(options{head: dir, stable: stable, registry: registry}); err == nil {
		t.Fatal("HEAD 缺少稳定包时应失败")
	}
	head := writeTree(t, nil)
	if err := os.MkdirAll(filepath.Join(head, "newpkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(head, "newpkg", "n.go"), []byte("package newpkg\n\nfunc New() int { return 0 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	missing, err := run(options{head: head, base: dir, stable: stable, registry: registry})
	if err != nil || len(missing) > 0 {
		t.Fatalf("基线缺少新稳定包时应只比较已有部分: missing=%v err=%v", missing, err)
	}
}

// 仓库登记表须与当前源码一致。
func TestRepositoryRegistry(t *testing.T) {
	if testing.Short() {
		t.Skip("加载整个主模块，-short 时跳过")
	}
	if _, err := run(options{head: "../..", stable: "../stable-packages.txt", registry: "../api-exposure.txt"}); err != nil {
		t.Fatal(err)
	}
}
