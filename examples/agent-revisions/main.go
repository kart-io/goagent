// 本例编译并执行仓内受信任小程序，演示归档工件的精确重载，不调用模型。
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kart-io/goagent/codec/json"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/management/adapters"
	"github.com/kart-io/goagent/revision"
)

type result struct {
	OldOutput, NewOutput, ReloadedOldOutput string
	OldRevisionID, NewRevisionID            string
}

func main() {
	res, err := run(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(res.OldOutput)
	fmt.Println(res.NewOutput)
	fmt.Println(res.ReloadedOldOutput)
}

func run(ctx context.Context) (res result, err error) {
	if err := ctx.Err(); err != nil {
		return res, err
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		return res, fmt.Errorf("无法定位示例源码")
	}
	program := filepath.Join(filepath.Dir(source), "program", "main.go")
	dir, err := os.MkdirTemp("", "goagent-artifact-example-*")
	if err != nil {
		return res, err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	buildDir := filepath.Join(dir, "build")
	if err := os.Mkdir(buildDir, 0o700); err != nil {
		return res, err
	}
	archiveDir := filepath.Join(dir, "archive")
	archive, err := revision.Open(archiveDir)
	if err != nil {
		return res, err
	}
	defer func() { err = errors.Join(err, archive.Close()) }()

	buildCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	registry := make(map[revision.Runtime]adapters.RevisionLoader)
	var versions [2]mg.Version
	for i, prompt := range []string{"旧版提示词", "新版提示词"} {
		version := fmt.Sprintf("v%d", i+1)
		binary := filepath.Join(buildDir, version)
		command := exec.CommandContext(buildCtx, "go", "build", "-trimpath", "-ldflags", "-X main.version="+version, "-o", binary, program)
		command.Env = buildEnvironment()
		if output, buildErr := command.CombinedOutput(); buildErr != nil {
			return res, fmt.Errorf("编译 %s 失败：%w；编译器输出：%s", version, errors.Join(buildErr, buildCtx.Err()), output)
		}
		data, err := os.ReadFile(binary)
		if err != nil {
			return res, err
		}
		definition := revision.Definition{
			Config:  json.RawMessage(`{}`),
			Policy:  revision.Reference{ID: "trusted-example", Version: version},
			Prompts: map[string]string{"system": prompt},
			Runtime: revision.Runtime{
				Kind: "executable", Version: version, Artifact: "bin/agent",
				Digest: fmt.Sprintf("%x", sha256.Sum256(data)),
			},
		}
		sealed, err := archive.Put(ctx, definition, map[string][]byte{"bin/agent": data})
		if err != nil {
			return res, err
		}
		versions[i] = mg.Version{RevisionID: sealed.ID, ImplementationVersion: version, Config: json.RawMessage(`{}`)}
		registry[definition.Runtime] = executableLoader()
	}
	cancel()
	res.OldRevisionID, res.NewRevisionID = versions[0].RevisionID, versions[1].RevisionID
	// 删除自己创建的原编译目录，再关闭重开归档；后续只能执行归档重建的文件。
	if err := os.RemoveAll(buildDir); err != nil {
		return res, err
	}
	if err := archive.Close(); err != nil {
		return res, err
	}
	reopened, err := revision.Open(archiveDir)
	if err != nil {
		return res, err
	}
	defer func() { err = errors.Join(err, reopened.Close()) }()
	factory, err := adapters.NewArchivedFactory(reopened, registry)
	if err != nil {
		return res, err
	}
	for i, target := range []*string{&res.OldOutput, &res.NewOutput, &res.ReloadedOldOutput} {
		*target, err = execute(ctx, factory, versions[i%2])
		if err != nil {
			return res, err
		}
	}
	return res, nil
}

// 使用本机工具链和标准库，移除继承的旧 GOROOT、交叉编译与额外编译选项，禁止下载。
func buildEnvironment() []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "GOROOT", "GOOS", "GOARCH", "GOFLAGS", "GOENV", "GOWORK", "GOTOOLCHAIN", "GOPROXY", "GOSUMDB", "CGO_ENABLED":
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GOENV=off", "GOWORK=off", "GOFLAGS=", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "CGO_ENABLED=0")
}

func executableLoader() adapters.RevisionLoader {
	return adapters.RevisionLoader{
		Check: func(ctx context.Context, _ revision.AgentRevision) (mg.Capabilities, error) {
			return mg.Capabilities{}, ctx.Err()
		},
		Build: func(ctx context.Context, env adapters.RevisionEnvironment, _ mg.Resources) (mg.Binding, error) {
			if err := ctx.Err(); err != nil {
				return mg.Binding{}, err
			}
			artifact := env.Revision.Definition.Runtime.Artifact
			// 宿主仅为已注册的受信任程序显式添加执行权限，库不会自动执行归档文件。
			if err := env.Root.Chmod(artifact, 0o500); err != nil {
				return mg.Binding{}, err
			}
			return mg.Binding{Runtime: &executable{
				path:   filepath.Join(env.Root.Name(), filepath.FromSlash(artifact)),
				prompt: env.Revision.Definition.Prompts["system"],
			}}, nil
		},
	}
}

type executable struct{ path, prompt string }

func (e *executable) Run(ctx context.Context, _ mg.RunSpec) (mg.RunResult, error) {
	output, err := exec.CommandContext(ctx, e.path, e.prompt).CombinedOutput()
	if err != nil {
		return mg.RunResult{}, fmt.Errorf("归档程序执行失败：%w；输出：%s", errors.Join(err, ctx.Err()), output)
	}
	raw, err := json.Marshal(strings.TrimSuffix(string(output), "\n"))
	return mg.RunResult{Outcome: "completed", Output: raw}, err
}

// Inspect 仅声明无持久执行证据；本例不实现生产环境的副作用恢复协议。
func (*executable) Inspect(ctx context.Context, _ mg.RunSpec) (mg.RunEvidence, error) {
	return mg.RunEvidence{State: "absent"}, ctx.Err()
}

func execute(ctx context.Context, factory *adapters.ArchivedFactory, version mg.Version) (output string, err error) {
	binding, err := factory.Build(ctx, version, mg.Resources{})
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, binding.Cleanup(context.WithoutCancel(ctx))) }()
	runResult, err := binding.Runtime.Run(ctx, mg.RunSpec{})
	if err != nil {
		return "", err
	}
	err = json.Unmarshal(runResult.Output, &output)
	return output, err
}
