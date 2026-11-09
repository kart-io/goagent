package adapters

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	agentErrors "github.com/kart-io/goagent/errors"
	mg "github.com/kart-io/goagent/management"
	"github.com/kart-io/goagent/revision"
	"github.com/kart-io/goagent/skills"
)

// RevisionLoader 由宿主注册精确程序身份。Check 不得分配需要清理的资源；
// Build 自行执行宿主允许的程序，不会由适配器自动执行归档中的代码。
// Build 若在返回 Binding 前发生 panic，须自行释放尚未交出的资源。
type RevisionLoader struct {
	Check func(context.Context, revision.AgentRevision) (mg.Capabilities, error)
	Build func(context.Context, RevisionEnvironment, mg.Resources) (mg.Binding, error)
}

// RevisionEnvironment 持有本次构建专用的快照、目录句柄和技能目录。
// Root 与 Skills 借用至 Binding.Cleanup；技能来源路径属于宿主临时环境。
type RevisionEnvironment struct {
	Revision revision.AgentRevision
	Root     *os.Root
	Skills   *skills.Catalog
}

// ArchivedFactory 只按归档身份与完整运行时键选择加载器，不解析 latest。
type ArchivedFactory struct {
	archive revision.Archive
	loaders map[revision.Runtime]RevisionLoader
}

// NewArchivedFactory 复制注册表，后续修改调用方的映射不会改变加载器选择。
func NewArchivedFactory(archive revision.Archive, loaders map[revision.Runtime]RevisionLoader) (*ArchivedFactory, error) {
	if archivedNil(archive) {
		return nil, agentErrors.ErrManagementInvalid
	}
	registry := make(map[revision.Runtime]RevisionLoader, len(loaders))
	for runtime, loader := range loaders {
		_, err := hex.DecodeString(runtime.Digest)
		if strings.TrimSpace(runtime.Kind) == "" || strings.TrimSpace(runtime.Version) == "" ||
			len(runtime.Digest) != 64 || strings.ToLower(runtime.Digest) != runtime.Digest || err != nil || loader.Check == nil || loader.Build == nil {
			return nil, agentErrors.ErrManagementInvalid
		}
		registry[runtime] = loader
	}
	return &ArchivedFactory{archive: archive, loaders: registry}, nil
}

// ValidateRevision 只校验归档与宿主能力，不创建目录、句柄或运行时。
func (f *ArchivedFactory) ValidateRevision(ctx context.Context, v mg.Version) (snapshot revision.AgentRevision, capabilities mg.Capabilities, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = archivedPanic(p)
		}
		if err != nil {
			snapshot, capabilities = revision.AgentRevision{}, mg.Capabilities{}
		}
	}()
	return f.validateRevision(ctx, v)
}

// validateRevision 将 panic 交给调用边界处理，避免 Build 将校验 panic 当作普通失败重试。
func (f *ArchivedFactory) validateRevision(ctx context.Context, v mg.Version) (snapshot revision.AgentRevision, capabilities mg.Capabilities, err error) {
	if err = ctx.Err(); err != nil {
		return
	}
	if f == nil || archivedNil(f.archive) || v.RevisionID == "" {
		err = agentErrors.ErrManagementInvalid
		return
	}
	snapshot, err = f.archive.Load(ctx, v.RevisionID)
	if err != nil {
		return
	}
	if err = ctx.Err(); err != nil {
		return
	}
	if err = revision.Verify(snapshot); err != nil {
		err = errors.Join(agentErrors.ErrManagementInvalid, err)
		return
	}
	if snapshot.ID != v.RevisionID || snapshot.Definition.Runtime.Version != v.ImplementationVersion {
		err = agentErrors.ErrManagementInvalid
		return
	}
	// 不信任归档实现的所有权约定；校验后取得独立且规范化的快照。
	snapshot, err = revision.Seal(snapshot.Definition, snapshot.Files)
	if err != nil {
		err = errors.Join(agentErrors.ErrManagementInvalid, err)
		return
	}
	if snapshot.ID != v.RevisionID || snapshot.Definition.Runtime.Version != v.ImplementationVersion {
		err = agentErrors.ErrManagementInvalid
		return
	}
	var config []byte
	config, err = mg.CanonicalJSON(v.Config)
	if err != nil {
		return
	}
	if !bytes.Equal(config, snapshot.Definition.Config) {
		err = agentErrors.ErrManagementInvalid
		return
	}
	loader, ok := f.loaders[snapshot.Definition.Runtime]
	if !ok {
		err = agentErrors.ErrManagementUnsupported
		return
	}
	if err = ctx.Err(); err != nil {
		return
	}
	capabilities, err = loader.Check(ctx, cloneArchivedRevision(snapshot))
	if err == nil {
		err = ctx.Err()
	}
	return
}

func (f *ArchivedFactory) Validate(ctx context.Context, v mg.Version) (mg.Capabilities, error) {
	_, capabilities, err := f.ValidateRevision(ctx, v)
	return capabilities, err
}

func (f *ArchivedFactory) Build(ctx context.Context, v mg.Version, resources mg.Resources) (binding mg.Binding, err error) {
	var directory string
	var root, catalogRoot *os.Root
	var catalog *skills.Catalog
	var loaderCleanup func(context.Context) error
	var loaderStarted bool
	var once sync.Once
	var cleanupErr error
	cleanup := func(ctx context.Context) error {
		once.Do(func() {
			// 先让加载器停止使用资源；即使其清理失败或 panic 也继续释放目录。
			if loaderCleanup != nil {
				cleanupErr = archivedCleanup(ctx, loaderCleanup)
			}
			if catalog != nil {
				cleanupErr = errors.Join(cleanupErr, catalog.Close())
			}
			if catalogRoot != nil {
				cleanupErr = errors.Join(cleanupErr, catalogRoot.Close())
			}
			if root != nil {
				cleanupErr = errors.Join(cleanupErr, root.Close())
			}
			if directory != "" {
				cleanupErr = errors.Join(cleanupErr, os.RemoveAll(directory))
			}
		})
		return cleanupErr
	}
	defer func() {
		panicked := false
		if p := recover(); p != nil {
			panicked = true
			err = errors.Join(err, archivedPanic(p))
		}
		if err != nil {
			cleanupErr := cleanup(context.WithoutCancel(ctx))
			err = errors.Join(err, cleanupErr)
			// 仅在宿主 Build 尚未开始、且本次资源已全部释放时承诺可安全重试。
			// panic 一律保守处理；宿主显式标记仍保留，但清理失败须由调用方否决重试。
			if !loaderStarted && !panicked && cleanupErr == nil {
				err = &mg.RetryableBuildError{Err: err}
			}
			// 失败时保留宿主是否交出 Runtime 的事实，供 worker 否决安全重试。
			// 本次资源已清理；调用方收到错误后只能判断 Runtime 是否存在，不得执行。
			binding.Cleanup = cleanup
		}
	}()
	snapshot, _, err := f.validateRevision(ctx, v)
	if err != nil {
		return binding, err
	}
	directory, err = os.MkdirTemp("", "goagent-revision-*")
	if err != nil {
		return binding, err
	}
	for _, name := range []string{"revision", "catalog"} {
		if err = os.Mkdir(filepath.Join(directory, name), 0o700); err != nil {
			return binding, err
		}
	}
	root, err = os.OpenRoot(filepath.Join(directory, "revision"))
	if err != nil {
		return binding, err
	}
	catalogRoot, err = os.OpenRoot(filepath.Join(directory, "catalog"))
	if err != nil {
		return binding, err
	}
	for name, data := range snapshot.Files {
		if err = writeArchivedFile(ctx, root, name, data); err != nil {
			return binding, err
		}
	}
	// 单独复制显式技能，避免 Discover 将同级未声明技能纳入目录。
	for skillName, source := range snapshot.Definition.Skills {
		for name, data := range snapshot.Files {
			if err = ctx.Err(); err != nil {
				return binding, err
			}
			relative := name
			if source != "." {
				var found bool
				relative, found = strings.CutPrefix(name, source+"/")
				if !found {
					continue
				}
			}
			if err = writeArchivedFile(ctx, catalogRoot, path.Join(skillName, relative), data); err != nil {
				return binding, err
			}
		}
	}
	catalog, err = skills.Discover(ctx, catalogRoot)
	if err != nil {
		return binding, err
	}
	// Catalog 已持有独立子目录句柄，不再借用发现用的根目录。
	err = catalogRoot.Close()
	catalogRoot = nil
	if err != nil {
		return binding, err
	}
	loaderStarted = true
	binding, err = f.loaders[snapshot.Definition.Runtime].Build(ctx, RevisionEnvironment{Revision: snapshot, Root: root, Skills: catalog}, resources)
	loaderCleanup = binding.Cleanup
	binding.Cleanup = cleanup
	if err == nil && archivedNil(binding.Runtime) {
		err = agentErrors.ErrManagementInvalid
	}
	if err == nil {
		err = ctx.Err()
	}
	return binding, err
}

func writeArchivedFile(ctx context.Context, root *os.Root, name string, data []byte) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = root.MkdirAll(filepath.FromSlash(path.Dir(name)), 0o700); err != nil {
		return err
	}
	file, err := root.OpenFile(filepath.FromSlash(name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	for len(data) > 0 {
		if err = ctx.Err(); err != nil {
			return err
		}
		n, writeErr := file.Write(data[:min(len(data), 32*1024)])
		if writeErr != nil {
			return writeErr
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return ctx.Err()
}

func cloneArchivedRevision(snapshot revision.AgentRevision) revision.AgentRevision {
	snapshot.Definition.Config = bytes.Clone(snapshot.Definition.Config)
	snapshot.Definition.Tools = bytes.Clone(snapshot.Definition.Tools)
	snapshot.Definition.Prompts = maps.Clone(snapshot.Definition.Prompts)
	snapshot.Definition.Models = maps.Clone(snapshot.Definition.Models)
	snapshot.Definition.Skills = maps.Clone(snapshot.Definition.Skills)
	snapshot.Files = maps.Clone(snapshot.Files)
	for name, data := range snapshot.Files {
		snapshot.Files[name] = bytes.Clone(data)
	}
	return snapshot
}

func archivedNil(value any) bool {
	if value == nil {
		return true
	}
	switch v := reflect.ValueOf(value); v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func archivedPanic(value any) error {
	err := agentErrors.New(agentErrors.CodeInternal, "归档加载器回调发生 panic").WithComponent("management")
	if cause, ok := value.(error); ok {
		return errors.Join(err, cause)
	}
	return err
}

func archivedCleanup(ctx context.Context, cleanup func(context.Context) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = archivedPanic(p)
		}
	}()
	return cleanup(ctx)
}
