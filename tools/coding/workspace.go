// Package coding 提供 AI 编码 Agent 的工作区工具：读写与精确编辑文件、glob/grep 检索、
// 在工作区内执行命令、git 操作与待办清单。文件与命令分别经 env.FileSystem 与 env.Executor 访问，
// 替换实现（如 env.DockerExecutor）即可整体迁移执行位置。权限与审批由 tools.Pipeline 承担。
package coding

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"time"

	"github.com/kart-io/goagent/harness/env"
	"github.com/kart-io/goagent/interfaces"
)

const (
	defaultCommandTimeout = 2 * time.Minute
	defaultMaxOutputBytes = 30000
	// maxFileBytes 限制单个文件读入内存的大小，读写编辑与 grep 共用。
	maxFileBytes = 10 << 20
)

// Workspace 是编码 Agent 的工作区。文件经 env.Observed 实施先读后改，经 env.Checkpointed 记录改动以便回滚。
type Workspace struct {
	base           env.FileSystem
	cp             *env.Checkpointed
	fs             *env.Observed
	exec           env.Executor
	root           string
	commandTimeout time.Duration
	maxOutputBytes int
	linter         Linter

	mu    sync.Mutex
	todos []Todo
	// editMu 让编辑的"读取当前内容、计算、写回"串行，同一轮并发编辑同一文件不会因版本冲突被拒。
	editMu sync.Mutex
}

// Linter 返回检查 path（相对路径）的命令；返回 nil 表示该文件不检查。
// 命令以非零码退出时编辑被撤销，输出交给模型。
type Linter func(path string) []string

// Option 配置 Workspace。
type Option func(*Workspace)

// WithExecutor 设置命令执行器，默认在宿主机工作区目录执行。
func WithExecutor(e env.Executor) Option {
	return func(w *Workspace) { w.exec = e }
}

// WithCommandTimeout 设置 run_command 与 git 工具的单次超时，默认 2 分钟。
func WithCommandTimeout(d time.Duration) Option {
	return func(w *Workspace) { w.commandTimeout = d }
}

// WithMaxOutputBytes 设置命令输出交给模型的最大字节数，超出时保留首尾，默认 30000。
func WithMaxOutputBytes(n int) Option {
	return func(w *Workspace) { w.maxOutputBytes = n }
}

// WithLinter 在 write_file、edit_file、multi_edit 写入后运行检查；检查失败时撤销本次写入，
// 把检查输出交给模型（SWE-agent 的消融显示带 lint 的编辑对成功率影响最大）。
func WithLinter(l Linter) Option {
	return func(w *Workspace) { w.linter = l }
}

// NewWorkspace 以宿主机目录 root 为工作区，命令默认在该目录执行。
func NewWorkspace(root string, opts ...Option) (*Workspace, error) {
	local, err := env.NewLocalFS(root)
	if err != nil {
		return nil, err
	}
	opts = append([]Option{WithExecutor(env.LocalExecutor{Root: local.Root()})}, opts...)
	w, err := New(local, nil, opts...)
	if err != nil {
		return nil, err
	}
	w.root = local.Root()
	return w, nil
}

// New 以任意 FileSystem 与 Executor 构造工作区，两者须指向同一工作区。executor 可由 WithExecutor 提供。
func New(fsys env.FileSystem, executor env.Executor, opts ...Option) (*Workspace, error) {
	if fsys == nil {
		return nil, errors.New("workspace file system must not be nil")
	}
	cp := env.NewCheckpointed(fsys)
	w := &Workspace{
		base:           fsys,
		cp:             cp,
		fs:             env.NewObserved(cp),
		exec:           executor,
		commandTimeout: defaultCommandTimeout,
		maxOutputBytes: defaultMaxOutputBytes,
	}
	for _, opt := range opts {
		opt(w)
	}
	if w.exec == nil {
		return nil, errors.New("workspace executor must not be nil")
	}
	if w.commandTimeout <= 0 || w.maxOutputBytes <= 0 {
		return nil, errors.New("workspace command timeout and max output must be positive")
	}
	return w, nil
}

// Root 返回本地工作区解析符号链接后的根目录；由 New 构造时为空串。
func (w *Workspace) Root() string { return w.root }

// Tools 返回全部编码工具。权限与审批经 tools.Pipeline 配置，可用 DefaultPolicy。
func (w *Workspace) Tools() []interfaces.Tool {
	return []interfaces.Tool{
		newReadFileTool(w),
		newWriteFileTool(w),
		newEditFileTool(w),
		newMultiEditTool(w),
		newGlobTool(w),
		newGrepTool(w),
		newRunCommandTool(w),
		newGitStatusTool(w),
		newGitDiffTool(w),
		newGitLogTool(w),
		newGitCommitTool(w),
		newTodoWriteTool(w),
	}
}

// Changes 返回经写入类工具改动过的文件。run_command 等命令对文件的改动不在其中。
func (w *Workspace) Changes() []env.Change { return w.cp.Changes() }

// Revert 把文件恢复到首次修改前的状态，不传 paths 时回滚全部；回滚后的文件须重新读取才能修改。
func (w *Workspace) Revert(ctx context.Context, paths ...string) error {
	w.editMu.Lock()
	defer w.editMu.Unlock()
	before := w.cp.Changes()
	err := w.cp.Revert(ctx, paths...)
	remaining := map[string]bool{}
	for _, c := range w.cp.Changes() {
		remaining[c.Path] = true
	}
	for _, c := range before {
		if !remaining[c.Path] {
			w.fs.Forget(c.Path)
		}
	}
	return err
}

// projectInstructionFiles 是约定的项目说明文件，按此顺序拼接。
var projectInstructionFiles = []string{"AGENTS.md", "CLAUDE.md"}

// ProjectInstructions 读取工作区根目录的 AGENTS.md 与 CLAUDE.md，供宿主拼进系统提示词；都不存在时返回空串。
// 指向工作区外、不是文本文件的说明文件被跳过，以免工作区外的内容进入提示词发给模型提供方。
func (w *Workspace) ProjectInstructions(ctx context.Context) (string, error) {
	var sb strings.Builder
	for _, name := range projectInstructionFiles {
		data, err := w.readText(ctx, name, false)
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errNotText) || errors.Is(err, env.ErrOutsideWorkspace) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		fmt.Fprintf(&sb, "<%s>\n%s\n</%s>\n", name, strings.TrimSpace(string(data)), name)
	}
	return sb.String(), nil
}

// errNotText 表示文件存在但不能作为文本处理，交给模型而非中止运行。
var errNotText = errors.New("not a text file")

// inGitDir 判断路径是否位于 .git 目录内。改写 .git/config 或 hooks 能让无需审批的 git 工具执行任意命令。
func inGitDir(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".git" {
			return true
		}
	}
	return false
}

// readText 读取文本文件，拒绝目录、过大或含 NUL 字节的文件。observe 为真时记录读取，允许之后修改。
func (w *Workspace) readText(ctx context.Context, path string, observe bool) ([]byte, error) {
	info, err := w.fs.Stat(ctx, path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%w: path is a directory", errNotText)
	}
	if info.Size > maxFileBytes {
		return nil, fmt.Errorf("%w: file is larger than %d bytes", errNotText, maxFileBytes)
	}
	var data []byte
	if observe {
		data, _, err = w.fs.ReadFile(ctx, path)
	} else {
		data, _, err = w.fs.Peek(ctx, path)
	}
	if err != nil {
		return nil, err
	}
	if isBinary(data) {
		if observe {
			w.fs.Forget(path)
		}
		return nil, fmt.Errorf("%w: file looks binary", errNotText)
	}
	return data, nil
}

func isBinary(data []byte) bool {
	n := min(len(data), 8000)
	for _, b := range data[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}
