// Package env 定义 Agent 执行环境的能力接口：文件系统与命令执行。工具只依赖这些接口，
// 替换实现（本地、容器、远程沙箱）即可整体迁移执行位置而不改工具。
//
// 路径一律是相对工作区根目录、以 / 分隔的路径；实现负责把它限定在工作区内。
package env

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"time"
)

// 错误。工具据此把状态性失败交给模型而不是中止运行。
var (
	ErrOutsideWorkspace = errors.New("path is outside the workspace")
	// ErrExists 表示 CreateIfAbsent 写入时文件已存在。
	ErrExists = errors.New("file already exists")
	// ErrStale 表示 IfVersion 写入时文件已不是该版本。
	ErrStale = errors.New("file changed since it was read")
	// ErrNotObserved 表示修改已存在的文件前没有读取过它。
	ErrNotObserved = errors.New("file has not been read")
	// ErrReadOnly 表示经 ReadOnly 包装的文件系统拒绝写入与删除。
	ErrReadOnly = errors.New("file system is read-only")
)

// Version 标识文件内容，内容相同则版本相同。空串表示没有版本。
type Version string

// Info 描述工作区内的一个条目。
type Info struct {
	Path    string // 以 / 分隔的相对路径，根目录为 "."
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
}

// IsDir 报告条目是否为目录。
func (i Info) IsDir() bool { return i.Mode.IsDir() }

// WriteIntent 约束一次写入。零值表示无条件写入。
type WriteIntent struct {
	// CreateIfAbsent 要求文件不存在，否则返回 ErrExists。
	CreateIfAbsent bool
	// IfVersion 非空时要求文件当前版本与之相同，否则返回 ErrStale。
	IfVersion Version
	// Perm 是写入后的权限位；0 表示保留原权限，新文件为 0644。
	Perm fs.FileMode
}

// FileSystem 是工作区文件系统。实现负责限定路径（含符号链接），并声明路径解析的并发限制。
// 条件检查与写入须相对于参与实现所声明协调协议的写者原子执行；各实现须说明协调范围，
// 不应据此推断对绕过接口的外部写者也具有 CAS 保证。LocalFS 的具体范围见其文档。
type FileSystem interface {
	// Resolve 把相对路径或工作区内的绝对路径规范为相对路径。
	Resolve(path string) (string, error)
	Stat(ctx context.Context, path string) (Info, error)
	ReadFile(ctx context.Context, path string) ([]byte, Version, error)
	// WriteFile 写入文件并按需创建父目录，返回新版本。提交后的持久化确认也可能出错，
	// 此时文件可能已经更新，调用方应重新读取确认，而不能把任意 error 都视为未写入。
	WriteFile(ctx context.Context, path string, data []byte, intent WriteIntent) (Version, error)
	Remove(ctx context.Context, path string) error
	// Walk 按词法顺序遍历 path 下的条目（含 path 自身），不跟随符号链接；fn 返回 fs.SkipDir、fs.SkipAll 的语义同 filepath.WalkDir。
	Walk(ctx context.Context, path string, fn func(Info) error) error
}

// Capabilities 声明执行环境的特性，审批与工具可据此调整行为。
type Capabilities struct {
	// Remote 表示命令不在宿主机上执行。
	Remote bool
	// Container 表示命令已在容器中隔离执行。
	Container bool
	// Network 表示命令可以访问网络。
	Network bool
	// ReadOnlyRoot 表示命令看到的工作区是只读的，命令无法改动工作区文件。
	ReadOnlyRoot bool
}

// Command 是一次在工作区内执行的命令。
type Command struct {
	// Argv 是可执行文件及其参数，不经 shell 解析；需要 shell 语法时传 {"sh", "-c", script}。
	Argv []string
	// Dir 是工作区内的相对目录，空串表示根目录。
	Dir string
}

// Result 是命令执行结果，Output 合并了 stdout 与 stderr。
type Result struct {
	Output   []byte
	ExitCode int
}

// Executor 在工作区内执行命令。命令以非零码退出时返回 Result 与 nil error；
// 只有命令未能启动或 ctx 结束时返回 error，此时 Result 可带已产生的输出。
type Executor interface {
	Run(ctx context.Context, cmd Command) (Result, error)
	Capabilities() Capabilities
}

// entryPath 只解析 p 的父目录、保留最后一段，使符号链接本身而不是其目标成为删除对象。
func entryPath(f FileSystem, p string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(p))
	dir, err := f.Resolve(filepath.Dir(clean))
	if err != nil {
		return "", err
	}
	if dir == "." {
		return filepath.Base(clean), nil
	}
	return dir + "/" + filepath.Base(clean), nil
}
