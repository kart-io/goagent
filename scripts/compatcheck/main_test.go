package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
)

func writeFixture(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func compatibilityFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "包含空格的 workspace")
	writeFixture(t, root, "goagent/go.mod", "module github.com/kart-io/goagent\n\ngo 1.26.0\n")
	writeFixture(t, root, "goagent/value.go", "package goagent\nconst Value = 42\n")
	for _, repo := range []string{"goagent-harness", "goagent-examples", "goagent-contrib/retrieval", "goagent-contrib/retrieval/nested"} {
		writeFixture(t, root, repo+"/go.mod", "module github.com/kart-io/"+repo+"\n\ngo 1.26.0\n\nrequire github.com/kart-io/goagent v0.0.0\n")
		writeFixture(t, root, repo+"/value_test.go", `package consumer
import (
 "testing"
 current "github.com/kart-io/goagent"
)
func TestCurrentSource(t *testing.T) {
 if current.Value != 42 { t.Fatalf("没有使用当前源码: %d", current.Value) }
}
func TestUnavailableService(t *testing.T) { t.Skip("没有配置测试服务") }
`)
	}
	writeFixture(t, root, "stale-core/go.mod", "module github.com/kart-io/goagent\n\ngo 1.26.0\n")
	writeFixture(t, root, "stale-core/value.go", "package goagent\nconst Value = 7\n")
	writeFixture(t, root, "goagent-harness/cmd/helper/main.go", `package main
import (
 "fmt"
 current "github.com/kart-io/goagent"
)
func main() { fmt.Print(current.Value) }
`)
	writeFixture(t, root, "goagent-harness/child_test.go", `package consumer
import (
 "os/exec"
 "strings"
 "testing"
)
func TestNestedBuildUsesCurrentCore(t *testing.T) {
 output, err := exec.Command("go", "run", "./cmd/helper").CombinedOutput()
 if err != nil || strings.TrimSpace(string(output)) != "42" {
  t.Fatalf("子构建没有使用当前源码: %s, %v", output, err)
 }
}
`)
	// 覆盖时须删除带版本的替换，不能让旧核心优先于当前源码。
	f, err := os.OpenFile(filepath.Join(root, "goagent-harness/go.mod"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("\nreplace github.com/kart-io/goagent v0.0.0 => ../stale-core\n")
	if closeErr := f.Close(); err != nil || closeErr != nil {
		t.Fatalf("写入替换失败: %v, %v", err, closeErr)
	}
	return root
}

func TestDiscoverRequiresAllRepositoriesAndFindsNestedModules(t *testing.T) {
	root := compatibilityFixture(t)
	modules, err := discover(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 5 {
		t.Fatalf("必须包含核心和全部嵌套模块，实际: %+v", modules)
	}
	if err := os.RemoveAll(filepath.Join(root, "goagent-harness")); err != nil {
		t.Fatal(err)
	}
	if _, err := discover(context.Background(), root); err == nil {
		t.Fatal("缺少关联仓库时不能静默减少验证范围")
	}
}

func TestVerifyUsesCurrentSourceKeepsEvidenceAndDoesNotEditConsumers(t *testing.T) {
	root := compatibilityFixture(t)
	t.Setenv("GOROOT", filepath.Join(root, "旧标准库"))
	t.Setenv("GOWORK", filepath.Join(root, "不存在的工作区"))
	t.Setenv("GOFLAGS", "-不存在的选项")
	tmp := filepath.Join(t.TempDir(), "临时依赖 包含空格")
	if err := os.Mkdir(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", tmp)
	before := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			before[path] = string(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "report")
	if err := verify(context.Background(), root, out, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var report summary
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Modules) != 4 {
		t.Fatalf("需要执行四个下游模块: %s", data)
	}
	for _, result := range report.Modules {
		wantTests := 1
		if result.Path == "github.com/kart-io/goagent-harness" {
			wantTests = 2
		}
		if result.Status != "passed" || result.PassedTests != wantTests || len(result.SkippedTests) != 1 || result.CoreDir != filepath.Join(root, "goagent") {
			t.Fatalf("源码定位、通过与跳过证据不完整: %+v", result)
		}
		if _, err := os.Stat(filepath.Join(out, result.Log)); err != nil {
			t.Fatal(err)
		}
	}
	seen := 0
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		want, exists := before[path]
		if err == nil && (!exists || string(data) != want) {
			t.Errorf("下游文件被修改或新增: %s", path)
		}
		seen++
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if seen != len(before) {
		t.Fatal("下游文件被删除")
	}
	if err := verify(context.Background(), root, out, 30*time.Second); err == nil {
		t.Fatal("不能覆盖既有验收证据")
	}

	writeFixture(t, root, "goagent-harness/value_test.go", "package consumer\nimport \"testing\"\nfunc TestRegression(t *testing.T) { t.Fatal(\"契约回归\") }\n")
	failedOut := filepath.Join(t.TempDir(), "failed")
	if err := verify(context.Background(), root, failedOut, 30*time.Second); err == nil {
		t.Fatal("下游测试失败必须传播为命令失败")
	}
	data, err = os.ReadFile(filepath.Join(failedOut, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	var passed, failed int
	for _, result := range report.Modules {
		switch result.Status {
		case "passed":
			passed++
		case "failed":
			failed++
			log, err := os.ReadFile(filepath.Join(failedOut, result.Log))
			if err != nil || !strings.Contains(string(log), "契约回归") {
				t.Fatalf("失败证据丢失: %s, %v", log, err)
			}
		}
	}
	if passed != 3 || failed != 1 {
		t.Fatalf("失败模块不能掩盖其他模块的结果: %s", data)
	}
}

func TestVerifyRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verify(ctx, compatibilityFixture(t), filepath.Join(t.TempDir(), "report"), time.Second); err == nil {
		t.Fatal("取消后不能继续启动验证")
	}
}
