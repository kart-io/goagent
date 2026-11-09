package adapters

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kart-io/goagent/codec/json"
	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/revision"
	"github.com/stretchr/testify/require"
)

var _ mg.RevisionFactory = (*ArchivedFactory)(nil)

type archivedTestArchive func(context.Context, string) (revision.AgentRevision, error)

func (f archivedTestArchive) Load(ctx context.Context, id string) (revision.AgentRevision, error) {
	return f(ctx, id)
}

type archivedTestRuntime struct{ output string }

func (r *archivedTestRuntime) Run(context.Context, mg.RunSpec) (mg.RunResult, error) {
	return mg.RunResult{Outcome: "completed", Output: json.RawMessage(r.output)}, nil
}

func (r *archivedTestRuntime) Inspect(context.Context, mg.RunSpec) (mg.RunEvidence, error) {
	return mg.RunEvidence{State: "absent"}, nil
}

func archivedSnapshot(t *testing.T, version, digest string) revision.AgentRevision {
	t.Helper()
	snapshot, err := revision.Seal(revision.Definition{
		Config:  json.RawMessage(`{"b":2,"a":1}`),
		Prompts: map[string]string{"system": "归档提示"},
		Tools:   json.RawMessage(`[{"name":"read"}]`),
		Models:  map[string]revision.Reference{"main": {ID: "model", Version: "1"}},
		Policy:  revision.Reference{ID: "policy", Version: "1"},
		Skills:  map[string]string{"declared": "assets/chosen"},
		Runtime: revision.Runtime{Kind: "native", Version: version, Digest: strings.Repeat(digest, 64)},
	}, map[string][]byte{
		"assets/chosen/SKILL.md":            []byte("---\nname: declared\ndescription: 已声明技能\n---\n归档技能正文"),
		"assets/chosen/references/data.txt": []byte("归档引用"),
		// 故意放入无效同级技能；未声明的文件不得参与技能发现。
		"assets/undeclared/SKILL.md": []byte("无效技能正文"),
		"program.txt":                []byte(version),
	})
	require.NoError(t, err, "测试快照封存失败")
	return snapshot
}

func archivedVersion(snapshot revision.AgentRevision) mg.Version {
	return mg.Version{RevisionID: snapshot.ID, ImplementationVersion: snapshot.Definition.Runtime.Version, Config: json.RawMessage("{ \"a\": 1, \"b\": 2 }")}
}

func archivedFixed(snapshot revision.AgentRevision) archivedTestArchive {
	return func(context.Context, string) (revision.AgentRevision, error) { return snapshot, nil }
}

func archivedLoader(build func(context.Context, RevisionEnvironment, mg.Resources) (mg.Binding, error)) RevisionLoader {
	return RevisionLoader{
		Check: func(context.Context, revision.AgentRevision) (mg.Capabilities, error) {
			return mg.Capabilities{Input: true}, nil
		},
		Build: build,
	}
}

func archivedEmptyTemp(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	require.NoError(t, err, "读取临时目录失败")
	require.Empty(t, entries, "适配器泄漏临时目录")
}

func TestArchivedExactRegistryAndOldSnapshots(t *testing.T) {
	ctx := context.Background()
	old := archivedSnapshot(t, "v1", "1")
	current := archivedSnapshot(t, "v2", "2")
	loads := 0
	archive := archivedTestArchive(func(_ context.Context, id string) (revision.AgentRevision, error) {
		loads++
		switch id {
		case old.ID:
			return old, nil
		case current.ID:
			return current, nil
		default:
			return revision.AgentRevision{}, fs.ErrNotExist
		}
	})
	registry := map[revision.Runtime]RevisionLoader{}
	for _, snapshot := range []revision.AgentRevision{old, current} {
		registry[snapshot.Definition.Runtime] = archivedLoader(func(_ context.Context, env RevisionEnvironment, _ mg.Resources) (mg.Binding, error) {
			require.Equal(t, snapshot.ID, env.Revision.ID, "运行时加载器选择了错误修订")
			data, err := env.Root.ReadFile("program.txt")
			require.NoError(t, err, "读取归档程序失败")
			require.Equal(t, snapshot.Definition.Runtime.Version, string(data), "物化内容与所选版本不符")
			return mg.Binding{Runtime: &archivedTestRuntime{output: `"` + string(data) + `"`}}, nil
		})
	}
	factory, err := NewArchivedFactory(archive, registry)
	require.NoError(t, err, "构造适配器失败")
	clear(registry)
	for _, snapshot := range []revision.AgentRevision{current, old, current, old} {
		_, err := factory.Validate(ctx, archivedVersion(snapshot))
		require.NoError(t, err, "校验历史版本失败")
		binding, err := factory.Build(ctx, archivedVersion(snapshot), mg.Resources{})
		require.NoError(t, err, "构建历史版本失败")
		result, err := binding.Runtime.Run(ctx, mg.RunSpec{})
		require.NoError(t, err, "运行历史版本失败")
		require.Equal(t, `"`+snapshot.Definition.Runtime.Version+`"`, string(result.Output), "运行结果不属于精确版本")
		require.NoError(t, binding.Cleanup(ctx), "清理历史版本失败")
	}
	require.Equal(t, 8, loads, "Build 必须重新加载，不得复用 Validate 快照")
}

func TestArchivedValidationIsIsolatedAndResourceFree(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	snapshot := archivedSnapshot(t, "v1", "1")
	loader := archivedLoader(func(_ context.Context, env RevisionEnvironment, _ mg.Resources) (mg.Binding, error) {
		require.NoError(t, revision.Verify(env.Revision), "Check 修改影响了构建快照")
		data, err := env.Root.ReadFile("program.txt")
		require.NoError(t, err, "读取物化文件失败")
		require.Equal(t, "v1", string(data), "Check 修改影响了物化文件")
		return mg.Binding{Runtime: &archivedTestRuntime{}}, nil
	})
	loader.Check = func(_ context.Context, copy revision.AgentRevision) (mg.Capabilities, error) {
		copy.Definition.Config[0] = '!'
		copy.Definition.Tools[0] = '!'
		copy.Definition.Prompts["system"] = "已修改"
		copy.Definition.Models["main"] = revision.Reference{}
		copy.Definition.Skills["declared"] = "其他目录"
		copy.Files["program.txt"][0] = '!'
		delete(copy.Files, "assets/chosen/SKILL.md")
		return mg.Capabilities{Input: true}, nil
	}
	factory, err := NewArchivedFactory(archivedFixed(snapshot), map[revision.Runtime]RevisionLoader{snapshot.Definition.Runtime: loader})
	require.NoError(t, err, "构造适配器失败")
	validated, caps, err := factory.ValidateRevision(context.Background(), archivedVersion(snapshot))
	require.NoError(t, err, "校验失败")
	require.True(t, caps.Input, "未返回加载器能力")
	require.NoError(t, revision.Verify(validated), "Check 污染了返回快照")
	validated.Files["program.txt"][0] = '?'
	validated.Definition.Config[0] = '?'
	require.NoError(t, revision.Verify(snapshot), "返回快照借用了归档内存")
	archivedEmptyTemp(t, temporary)
	binding, err := factory.Build(context.Background(), archivedVersion(snapshot), mg.Resources{})
	require.NoError(t, err, "构建失败")
	require.NoError(t, binding.Cleanup(context.Background()), "清理失败")
	archivedEmptyTemp(t, temporary)
}

func TestArchivedMissingLoaderFailsClosed(t *testing.T) {
	snapshot := archivedSnapshot(t, "v1", "1")
	for _, field := range []string{"kind", "version", "digest", "artifact", "image"} {
		t.Run(field, func(t *testing.T) {
			key := snapshot.Definition.Runtime
			switch field {
			case "kind":
				key.Kind = "other"
			case "version":
				key.Version = "latest"
			case "digest":
				key.Digest = strings.Repeat("2", 64)
			case "artifact":
				key.Artifact = "other/program"
			case "image":
				key.Image = "other/image"
			}
			loader := archivedLoader(func(context.Context, RevisionEnvironment, mg.Resources) (mg.Binding, error) {
				t.Fatal("不得回退到其他程序身份")
				return mg.Binding{}, nil
			})
			loader.Check = func(context.Context, revision.AgentRevision) (mg.Capabilities, error) {
				t.Fatal("缺少精确加载器时不得调用其他 Check")
				return mg.Capabilities{}, nil
			}
			factory, err := NewArchivedFactory(archivedFixed(snapshot), map[revision.Runtime]RevisionLoader{key: loader})
			require.NoError(t, err, "构造适配器失败")
			_, err = factory.Validate(context.Background(), archivedVersion(snapshot))
			require.ErrorIs(t, err, agentErrors.ErrManagementUnsupported, "缺少加载器必须拒绝校验")
			_, err = factory.Build(context.Background(), archivedVersion(snapshot), mg.Resources{})
			require.ErrorIs(t, err, agentErrors.ErrManagementUnsupported, "缺少加载器必须拒绝构建")
		})
	}
}

func TestArchivedCorruptionAndVersionMismatchRejected(t *testing.T) {
	for _, scenario := range []string{"bytes", "path", "id", "config", "implementation", "missing-id"} {
		t.Run(scenario, func(t *testing.T) {
			snapshot := archivedSnapshot(t, "v1", "1")
			version := archivedVersion(snapshot)
			switch scenario {
			case "bytes":
				snapshot.Files["program.txt"][0] = '!'
			case "path":
				snapshot.Files["../escape"] = []byte("越界")
			case "id":
				version.RevisionID = strings.Repeat("f", 64)
			case "config":
				version.Config = json.RawMessage(`{"a":2}`)
			case "implementation":
				version.ImplementationVersion = "v2"
			case "missing-id":
				version.RevisionID = ""
			}
			loader := archivedLoader(func(context.Context, RevisionEnvironment, mg.Resources) (mg.Binding, error) {
				t.Fatal("无效修订不得调用 Build")
				return mg.Binding{}, nil
			})
			loader.Check = func(context.Context, revision.AgentRevision) (mg.Capabilities, error) {
				t.Fatal("无效修订不得调用 Check")
				return mg.Capabilities{}, nil
			}
			factory, err := NewArchivedFactory(archivedFixed(snapshot), map[revision.Runtime]RevisionLoader{snapshot.Definition.Runtime: loader})
			require.NoError(t, err, "构造适配器失败")
			_, _, err = factory.ValidateRevision(context.Background(), version)
			require.ErrorIs(t, err, agentErrors.ErrManagementInvalid, "无效修订必须拒绝")
			_, err = factory.Build(context.Background(), version, mg.Resources{})
			require.ErrorIs(t, err, agentErrors.ErrManagementInvalid, "无效修订必须拒绝构建")
		})
	}
}

func TestArchivedMaterializedSkillsAndRootLifetime(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	snapshot := archivedSnapshot(t, "v1", "1")
	var environment RevisionEnvironment
	var cleanupCalls atomic.Int32
	loader := archivedLoader(func(_ context.Context, env RevisionEnvironment, resources mg.Resources) (mg.Binding, error) {
		environment = env
		require.Equal(t, "session-log", resources.Session.LogID, "宿主资源未传递")
		return mg.Binding{Runtime: &archivedTestRuntime{}, Cleanup: func(ctx context.Context) error {
			cleanupCalls.Add(1)
			_, err := env.Root.ReadFile("program.txt")
			require.NoError(t, err, "加载器清理之前根目录已失效")
			_, err = env.Skills.ReadReference(ctx, "declared", "references/data.txt")
			require.NoError(t, err, "加载器清理之前技能目录已失效")
			return nil
		}}, nil
	})
	factory, err := NewArchivedFactory(archivedFixed(snapshot), map[revision.Runtime]RevisionLoader{snapshot.Definition.Runtime: loader})
	require.NoError(t, err, "构造适配器失败")
	binding, err := factory.Build(context.Background(), archivedVersion(snapshot), mg.Resources{Session: mg.Session{LogID: "session-log"}})
	require.NoError(t, err, "构建失败")
	metadata := environment.Skills.List()
	require.Len(t, metadata, 1, "不应发现未声明的同级技能")
	require.Equal(t, "declared", metadata[0].Name, "发现了错误技能")
	require.Equal(t, filepath.Join(filepath.Dir(environment.Root.Name()), "catalog", "declared", "SKILL.md"), metadata[0].Source, "技能来源应为私有宿主目录")
	for name := range snapshot.Files {
		info, err := environment.Root.Stat(filepath.FromSlash(name))
		require.NoError(t, err, "读取文件属性失败")
		require.True(t, info.Mode().IsRegular(), "物化文件必须为普通文件")
		require.Equal(t, fs.FileMode(0o400), info.Mode().Perm(), "物化文件应只读")
	}
	for _, dir := range []string{environment.Root.Name(), filepath.Dir(environment.Root.Name()), filepath.Dir(metadata[0].Source)} {
		info, err := os.Stat(dir)
		require.NoError(t, err, "读取目录属性失败")
		require.Equal(t, fs.FileMode(0o700), info.Mode().Perm(), "物化目录必须私有")
	}
	// 清空归档侧字节后，本次运行仍完全依靠自己的文件和技能句柄。
	clear(snapshot.Files)
	data, err := environment.Skills.ReadReference(context.Background(), "declared", "references/data.txt")
	require.NoError(t, err, "归档存储不可用后技能读取失败")
	require.Equal(t, "归档引用", string(data), "技能引用内容不符")
	_, err = environment.Root.ReadFile("../catalog/declared/SKILL.md")
	require.Error(t, err, "根目录句柄不应允许访问同级目录")
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if err := binding.Cleanup(context.Background()); err != nil {
				t.Errorf("并发清理失败：%v", err)
			}
		})
	}
	wg.Wait()
	require.Equal(t, int32(1), cleanupCalls.Load(), "加载器清理必须幂等")
	_, err = environment.Root.ReadFile("program.txt")
	require.Error(t, err, "清理后根目录应已关闭")
	_, err = environment.Skills.Load(context.Background(), "declared")
	require.ErrorIs(t, err, fs.ErrClosed, "清理后技能目录应已关闭")
	archivedEmptyTemp(t, temporary)
}

func TestArchivedBuildFailureCleanup(t *testing.T) {
	for _, scenario := range []string{"error", "nil", "typed-nil", "panic", "canceled", "cleanup-panic"} {
		t.Run(scenario, func(t *testing.T) {
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)
			snapshot := archivedSnapshot(t, "v1", "1")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var environment RevisionEnvironment
			cleanupCalls := 0
			loader := archivedLoader(func(_ context.Context, env RevisionEnvironment, _ mg.Resources) (mg.Binding, error) {
				environment = env
				binding := mg.Binding{Runtime: &archivedTestRuntime{}, Cleanup: func(ctx context.Context) error {
					cleanupCalls++
					require.NoError(t, ctx.Err(), "失败清理不应继承取消状态")
					if scenario == "cleanup-panic" {
						panic(fs.ErrPermission)
					}
					return fs.ErrPermission
				}}
				switch scenario {
				case "error", "cleanup-panic":
					return binding, fs.ErrNotExist
				case "nil":
					binding.Runtime = nil
				case "typed-nil":
					binding.Runtime = (*archivedTestRuntime)(nil)
				case "panic":
					panic(fs.ErrInvalid)
				case "canceled":
					cancel()
				}
				return binding, nil
			})
			factory, err := NewArchivedFactory(archivedFixed(snapshot), map[revision.Runtime]RevisionLoader{snapshot.Definition.Runtime: loader})
			require.NoError(t, err, "构造适配器失败")
			binding, err := factory.Build(ctx, archivedVersion(snapshot), mg.Resources{})
			require.Error(t, err, "失败构建应返回错误")
			if scenario == "error" || scenario == "canceled" || scenario == "cleanup-panic" {
				require.NotNil(t, binding.Runtime, "必须保留宿主曾返回 Runtime 的事实，禁止误判为安全重试")
			} else {
				require.Nil(t, binding.Runtime, "未返回运行时的失败不得凭空创建运行时")
			}
			switch scenario {
			case "error", "cleanup-panic":
				require.ErrorIs(t, err, fs.ErrNotExist, "应保留构建原因")
			case "nil", "typed-nil":
				require.ErrorIs(t, err, agentErrors.ErrManagementInvalid, "空运行时必须拒绝")
			case "panic":
				require.ErrorIs(t, err, fs.ErrInvalid, "应保留 panic 错误原因")
			case "canceled":
				require.ErrorIs(t, err, context.Canceled, "应保留取消原因")
			}
			if scenario != "panic" {
				require.ErrorIs(t, err, fs.ErrPermission, "应合并加载器清理错误")
				require.ErrorIs(t, binding.Cleanup(context.Background()), fs.ErrPermission, "重复清理应返回相同错误")
				require.Equal(t, 1, cleanupCalls, "失败清理必须仅执行一次")
			}
			_, readErr := environment.Root.ReadFile("program.txt")
			require.Error(t, readErr, "失败构建应关闭根目录")
			_, readErr = environment.Skills.Load(context.Background(), "declared")
			require.ErrorIs(t, readErr, fs.ErrClosed, "失败构建应关闭技能目录")
			archivedEmptyTemp(t, temporary)
		})
	}
}

func TestArchivedValidationFailuresLeaveNoResources(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	snapshot := archivedSnapshot(t, "v1", "1")
	for _, scenario := range []string{"load-error", "load-panic", "check-error", "check-panic", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			archive := archivedTestArchive(func(context.Context, string) (revision.AgentRevision, error) {
				if scenario == "load-error" {
					return revision.AgentRevision{}, fs.ErrPermission
				}
				if scenario == "load-panic" {
					panic(fs.ErrPermission)
				}
				return snapshot, nil
			})
			loader := archivedLoader(func(context.Context, RevisionEnvironment, mg.Resources) (mg.Binding, error) {
				t.Fatal("校验失败不得构建运行时")
				return mg.Binding{}, nil
			})
			loader.Check = func(context.Context, revision.AgentRevision) (mg.Capabilities, error) {
				if scenario == "check-panic" {
					panic(fs.ErrPermission)
				}
				return mg.Capabilities{}, fs.ErrPermission
			}
			factory, err := NewArchivedFactory(archive, map[revision.Runtime]RevisionLoader{snapshot.Definition.Runtime: loader})
			require.NoError(t, err, "构造适配器失败")
			if scenario == "canceled" {
				cancel()
			}
			_, err = factory.Build(ctx, archivedVersion(snapshot), mg.Resources{})
			if scenario == "canceled" {
				require.ErrorIs(t, err, context.Canceled, "应保留取消原因")
			} else {
				require.ErrorIs(t, err, fs.ErrPermission, "应保留归档或校验错误")
			}
			archivedEmptyTemp(t, temporary)
		})
	}
}

func TestArchivedConstructorRejectsInvalidInputs(t *testing.T) {
	snapshot := archivedSnapshot(t, "v1", "1")
	var nilArchive archivedTestArchive
	for _, archive := range []revision.Archive{nil, nilArchive} {
		_, err := NewArchivedFactory(archive, nil)
		require.ErrorIs(t, err, agentErrors.ErrManagementInvalid, "必须拒绝空归档")
	}
	_, err := NewArchivedFactory(archivedFixed(snapshot), map[revision.Runtime]RevisionLoader{snapshot.Definition.Runtime: {}})
	require.ErrorIs(t, err, agentErrors.ErrManagementInvalid, "必须拒绝缺失回调")
	factory, err := NewArchivedFactory(archivedFixed(snapshot), nil)
	require.NoError(t, err, "允许空注册表用于明确拒绝支持")
	_, err = factory.Validate(context.Background(), archivedVersion(snapshot))
	require.True(t, errors.Is(err, agentErrors.ErrManagementUnsupported), "空注册表不得隐式启用加载器")
}

func TestArchivedMaterializationFailureCleansPartialTree(t *testing.T) {
	temporary := t.TempDir()
	t.Setenv("TMPDIR", temporary)
	snapshot := archivedSnapshot(t, "v1", "1")
	// 归档允许的相对路径可能超出本机文件系统的单段名称限制。
	snapshot.Files["partial/"+strings.Repeat("x", 300)] = []byte("不能物化")
	snapshot, err := revision.Seal(snapshot.Definition, snapshot.Files)
	require.NoError(t, err, "平台不可物化的路径仍应满足归档格式")
	loader := archivedLoader(func(context.Context, RevisionEnvironment, mg.Resources) (mg.Binding, error) {
		t.Fatal("物化失败不得调用加载器")
		return mg.Binding{}, nil
	})
	factory, err := NewArchivedFactory(archivedFixed(snapshot), map[revision.Runtime]RevisionLoader{snapshot.Definition.Runtime: loader})
	require.NoError(t, err, "构造适配器失败")
	_, err = factory.Validate(context.Background(), archivedVersion(snapshot))
	require.NoError(t, err, "校验不得分配本机文件")
	archivedEmptyTemp(t, temporary)
	binding, err := factory.Build(context.Background(), archivedVersion(snapshot), mg.Resources{})
	require.Error(t, err, "名称超限时构建应失败")
	require.NoError(t, binding.Cleanup(context.Background()), "部分物化后的重复清理失败")
	archivedEmptyTemp(t, temporary)
}

func TestArchivedFileCopyCancellationAndExclusiveCreation(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err, "打开测试根目录失败")
	defer root.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = writeArchivedFile(ctx, root, "canceled/file", []byte("不能写入"))
	require.ErrorIs(t, err, context.Canceled, "复制前应检查取消状态")
	_, err = root.Stat("canceled")
	require.ErrorIs(t, err, fs.ErrNotExist, "已取消的复制不应创建目录")
	require.NoError(t, writeArchivedFile(context.Background(), root, "file", []byte("原内容")), "首次写入失败")
	err = writeArchivedFile(context.Background(), root, "file", []byte("替换内容"))
	require.ErrorIs(t, err, fs.ErrExist, "归档文件必须排他创建")
	data, err := root.ReadFile("file")
	require.NoError(t, err, "读取原内容失败")
	require.Equal(t, "原内容", string(data), "排他创建失败不得覆盖既有内容")
}
