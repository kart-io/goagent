package adapters

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/revision"
	"github.com/stretchr/testify/require"
)

func TestArchivedRetryValidationBoundary(t *testing.T) {
	for _, scenario := range []string{"load-error", "check-error", "load-panic", "check-panic"} {
		t.Run(scenario, func(t *testing.T) {
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)
			snapshot := archivedSnapshot(t, "v1", "1")
			archive := archivedTestArchive(func(context.Context, string) (revision.AgentRevision, error) {
				switch scenario {
				case "load-error":
					return revision.AgentRevision{}, fs.ErrPermission
				case "load-panic":
					panic(fs.ErrPermission)
				}
				return snapshot, nil
			})
			loader := archivedLoader(func(context.Context, RevisionEnvironment, mg.Resources) (mg.Binding, error) {
				t.Fatal("校验失败不得调用宿主 Build")
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
			// 公开校验仍捕获 panic，并且不对独立校验授予构建重试承诺。
			validated, caps, err := factory.ValidateRevision(context.Background(), archivedVersion(snapshot))
			require.ErrorIs(t, err, fs.ErrPermission, "公开校验应保留失败原因")
			require.Empty(t, validated, "失败校验不得返回快照")
			require.Empty(t, caps, "失败校验不得返回能力")
			var marker *mg.RetryableBuildError
			require.False(t, errors.As(err, &marker), "独立校验不得自动标记构建重试")

			binding, err := factory.Build(context.Background(), archivedVersion(snapshot), mg.Resources{})
			require.ErrorIs(t, err, fs.ErrPermission, "构建应保留校验失败原因")
			require.Equal(t, strings.HasSuffix(scenario, "-error"), errors.As(err, &marker), "普通校验失败可重试，校验 panic 不得自动标记")
			require.Nil(t, binding.Runtime, "失败构建不得返回运行时")
			require.NotNil(t, binding.Cleanup, "失败构建仍应提供幂等清理")
			require.NoError(t, binding.Cleanup(context.Background()), "校验失败后的清理失败")
			require.NoError(t, binding.Cleanup(context.Background()), "重复清理失败")
			archivedEmptyTemp(t, temporary)
		})
	}
}

func TestArchivedRetryMaterializationFailure(t *testing.T) {
	for _, scenario := range []string{"mkdir", "partial-tree"} {
		t.Run(scenario, func(t *testing.T) {
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)
			snapshot := archivedSnapshot(t, "v1", "1")
			if scenario == "mkdir" {
				t.Setenv("TMPDIR", filepath.Join(temporary, "missing"))
			} else {
				// 合法归档路径超出本机文件名限制，验证部分物化后的资源释放。
				snapshot.Files["partial/"+strings.Repeat("x", 300)] = []byte("不能物化")
				var err error
				snapshot, err = revision.Seal(snapshot.Definition, snapshot.Files)
				require.NoError(t, err, "封存测试快照失败")
			}
			loader := archivedLoader(func(context.Context, RevisionEnvironment, mg.Resources) (mg.Binding, error) {
				t.Fatal("物化失败不得调用宿主 Build")
				return mg.Binding{}, nil
			})
			factory, err := NewArchivedFactory(archivedFixed(snapshot), map[revision.Runtime]RevisionLoader{snapshot.Definition.Runtime: loader})
			require.NoError(t, err, "构造适配器失败")
			_, err = factory.Validate(context.Background(), archivedVersion(snapshot))
			require.NoError(t, err, "测试快照应通过校验")
			binding, err := factory.Build(context.Background(), archivedVersion(snapshot), mg.Resources{})
			var marker *mg.RetryableBuildError
			require.ErrorAs(t, err, &marker, "物化失败且清理成功应标记可重试")
			var pathErr *os.PathError
			require.ErrorAs(t, err, &pathErr, "重试标记应保留文件系统失败原因")
			require.Nil(t, binding.Runtime, "物化失败不得返回运行时")
			require.NotNil(t, binding.Cleanup, "物化失败仍应提供清理")
			require.NoError(t, binding.Cleanup(context.Background()), "物化失败后的重复清理失败")
			archivedEmptyTemp(t, temporary)
		})
	}
}

func TestArchivedRetryHostBuildBoundary(t *testing.T) {
	for _, scenario := range []string{
		"error", "panic", "nil", "typed-nil", "canceled",
		"explicit", "explicit-wrapped", "explicit-runtime", "cleanup-error", "cleanup-panic",
		"explicit-cleanup-error", "explicit-cleanup-panic",
	} {
		t.Run(scenario, func(t *testing.T) {
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)
			snapshot := archivedSnapshot(t, "v1", "1")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			explicit := strings.HasPrefix(scenario, "explicit")
			cleanupFails := strings.Contains(scenario, "cleanup-")
			hostMarker := &mg.RetryableBuildError{Err: fs.ErrNotExist}
			var environment RevisionEnvironment
			var cleanupCalls atomic.Int32
			loader := archivedLoader(func(_ context.Context, env RevisionEnvironment, _ mg.Resources) (mg.Binding, error) {
				environment = env
				binding := mg.Binding{Runtime: &archivedTestRuntime{}, Cleanup: func(cleanupCtx context.Context) error {
					cleanupCalls.Add(1)
					require.NoError(t, cleanupCtx.Err(), "失败清理不得继承取消状态")
					if strings.HasSuffix(scenario, "cleanup-panic") {
						panic(fs.ErrPermission)
					}
					if cleanupFails {
						return fs.ErrPermission
					}
					return nil
				}}
				if explicit && scenario != "explicit-runtime" {
					binding.Runtime = nil
				}
				switch scenario {
				case "panic":
					panic(fs.ErrNotExist)
				case "nil":
					binding.Runtime = nil
					return binding, nil
				case "typed-nil":
					binding.Runtime = (*archivedTestRuntime)(nil)
					return binding, nil
				case "canceled":
					cancel()
					return binding, nil
				case "explicit-wrapped":
					return binding, fmt.Errorf("宿主构建失败：%w", hostMarker)
				}
				if explicit {
					return binding, hostMarker
				}
				return binding, fs.ErrNotExist
			})
			factory, err := NewArchivedFactory(archivedFixed(snapshot), map[revision.Runtime]RevisionLoader{snapshot.Definition.Runtime: loader})
			require.NoError(t, err, "构造适配器失败")
			binding, err := factory.Build(ctx, archivedVersion(snapshot), mg.Resources{})
			require.Error(t, err, "失败构建应返回错误")
			if scenario == "panic" || scenario == "nil" || scenario == "typed-nil" || (explicit && scenario != "explicit-runtime") {
				require.Nil(t, binding.Runtime, "宿主未交出运行时")
			} else {
				require.NotNil(t, binding.Runtime, "必须保留宿主已交出 Runtime 的事实，禁止 worker 误判重试")
			}
			var marker *mg.RetryableBuildError
			require.Equal(t, explicit, errors.As(err, &marker), "进入宿主 Build 后只能保留显式标记")
			if explicit {
				require.Same(t, hostMarker, marker, "应保留宿主原始重试标记")
			}
			switch scenario {
			case "nil", "typed-nil":
				require.ErrorIs(t, err, agentErrors.ErrManagementInvalid, "空运行时应保留无效错误")
			case "canceled":
				require.ErrorIs(t, err, context.Canceled, "应保留宿主构建后的取消错误")
			default:
				require.ErrorIs(t, err, fs.ErrNotExist, "应保留宿主失败原因")
			}
			if cleanupFails {
				require.ErrorIs(t, err, fs.ErrPermission, "构建错误应包含清理失败原因")
			}
			require.NotNil(t, binding.Cleanup, "失败构建仍应提供幂等清理")
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					cleanupErr := binding.Cleanup(context.Background())
					if cleanupFails {
						if !errors.Is(cleanupErr, fs.ErrPermission) {
							t.Errorf("重复清理必须保留失败原因供 worker 否决安全重试：%v", cleanupErr)
						}
					} else if cleanupErr != nil {
						t.Errorf("重复清理失败：%v", cleanupErr)
					}
				})
			}
			wg.Wait()
			if scenario == "panic" {
				require.Zero(t, cleanupCalls.Load(), "panic 前未交出的宿主清理由宿主负责")
			} else {
				require.Equal(t, int32(1), cleanupCalls.Load(), "宿主清理必须仅执行一次")
			}
			_, readErr := environment.Root.ReadFile("program.txt")
			require.Error(t, readErr, "失败构建应关闭根目录")
			_, readErr = environment.Skills.Load(context.Background(), "declared")
			require.ErrorIs(t, readErr, fs.ErrClosed, "失败构建应关闭技能目录")
			archivedEmptyTemp(t, temporary)
		})
	}
}
