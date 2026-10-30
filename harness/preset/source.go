// Package preset 以声明式配置组合自定义 Agent：一个 preset 由若干组件行组成，组件列表即该 Agent 的全部能力。
// 配置分层叠加（内置 → 用户 → 项目 → 本地 → 宿主），但权限上限只由可信层决定：项目层与本地层只能收紧，
// 扩权项须经宿主授权才生效。设计见 docs/design/HARNESS_CORE_DESIGN.md 第 14 节。
package preset

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// 约定的层名。
const (
	LayerBuiltin = "builtin"
	LayerUser    = "user"
	LayerProject = "project"
	LayerLocal   = "local"
	LayerHost    = "host"
)

// Sandbox 是工作区的沙箱模式，按宽松程度递增。
type Sandbox string

const (
	ReadOnly         Sandbox = "read-only"
	WorkspaceWrite   Sandbox = "workspace-write"
	DangerFullAccess Sandbox = "danger-full-access"
)

func (s Sandbox) rank() int {
	switch s {
	case ReadOnly:
		return 0
	case WorkspaceWrite:
		return 1
	case DangerFullAccess:
		return 2
	}
	return -1
}

func (s Sandbox) valid() bool { return s.rank() >= 0 }

// Source 是一层配置。Load 按传入顺序叠加，后者覆盖前者。
type Source struct {
	// Layer 是层名，用于 Dump 与错误信息。
	Layer string
	// Path 是配置来源，用于 Dump 与错误信息；代码内的配置可填描述性文字。
	Path string
	// Trusted 表示该层可信：可声明 limits，组件不受上限约束，可锁定组件。
	Trusted bool
	Data    []byte
}

// Origin 是一行配置的来源。
type Origin struct {
	Layer, Path string
	Trusted     bool
}

func (o Origin) String() string { return o.Layer + " (" + o.Path + ")" }

// FileSources 读取约定位置的配置文件，不存在的文件跳过：
// userDir 非空时读取 userDir/.goagent/agents.yml（可信），再读取 root/.goagent/agents.yml 与
// root/.goagent/agents.local.yml（不可信：位于仓库目录内，可被仓库中的脚本写入）。
// 内置层放在返回值之前、宿主层放在之后传给 Load。
func FileSources(root, userDir string) ([]Source, error) {
	type candidate struct {
		layer, path string
		trusted     bool
	}
	var candidates []candidate
	if userDir != "" {
		candidates = append(candidates, candidate{LayerUser, filepath.Join(userDir, ".goagent", "agents.yml"), true})
	}
	candidates = append(candidates,
		candidate{LayerProject, filepath.Join(root, ".goagent", "agents.yml"), false},
		candidate{LayerLocal, filepath.Join(root, ".goagent", "agents.local.yml"), false})
	var out []Source
	for _, c := range candidates {
		data, err := os.ReadFile(c.path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, Source{Layer: c.layer, Path: c.path, Trusted: c.trusted, Data: data})
	}
	return out, nil
}

// Limits 是可信层声明的权限上限，约束来自不可信层的组件；超出上限的部分属于扩权项。
type Limits struct {
	// Sandbox 是沙箱模式上限，默认 workspace-write。
	Sandbox Sandbox
	// Executors 是允许的执行器种类，nil 表示不限。
	Executors []string
	// Images 是允许的容器镜像，nil 表示不限。
	Images []string
	// Network 表示允许命令联网，默认禁止。
	Network bool
	// HostProcesses 是允许在宿主机启动的进程（stdio MCP、命令钩子），默认不允许。
	HostProcesses []string
	// Secrets 是允许引用的秘密名，默认不允许。
	Secrets []string
}

func defaultLimits() Limits { return Limits{Sandbox: WorkspaceWrite} }

// exceeded 返回 needs 超出上限的各项原因。
func (l Limits) exceeded(n Needs, secrets []string) []string {
	var reasons []string
	if n.Sandbox != "" && n.Sandbox.rank() > l.Sandbox.rank() {
		reasons = append(reasons, fmt.Sprintf("沙箱模式 %s 超过上限 %s", n.Sandbox, l.Sandbox))
	}
	if n.Executor != "" && l.Executors != nil && !slices.Contains(l.Executors, n.Executor) {
		reasons = append(reasons, fmt.Sprintf("执行器 %s 不在允许列表 %v 中", n.Executor, l.Executors))
	}
	if n.Image != "" && l.Images != nil && !slices.Contains(l.Images, n.Image) {
		reasons = append(reasons, fmt.Sprintf("镜像 %s 不在允许列表 %v 中", n.Image, l.Images))
	}
	if n.Network && !l.Network {
		reasons = append(reasons, "联网超出上限")
	}
	for _, p := range n.HostProcesses {
		if !slices.Contains(l.HostProcesses, p) {
			reasons = append(reasons, fmt.Sprintf("在宿主机启动进程 %s（不受沙箱约束）", p))
		}
	}
	for _, s := range secrets {
		if !slices.Contains(l.Secrets, s) {
			reasons = append(reasons, fmt.Sprintf("引用秘密 %s", s))
		}
	}
	return append(reasons, n.Grants...)
}

type limitsFile struct {
	Sandbox       *Sandbox  `yaml:"sandbox"`
	Executors     *[]string `yaml:"executors"`
	Images        *[]string `yaml:"images"`
	Network       *bool     `yaml:"network"`
	HostProcesses *[]string `yaml:"hostProcesses"`
	Secrets       *[]string `yaml:"secrets"`
}

// apply 用文件中出现的字段替换当前上限。
func (f *limitsFile) apply(l *Limits) error {
	if f.Sandbox != nil {
		if !f.Sandbox.valid() {
			return fmt.Errorf("limits.sandbox %q 无效", *f.Sandbox)
		}
		l.Sandbox = *f.Sandbox
	}
	if f.Executors != nil {
		l.Executors = *f.Executors
	}
	if f.Images != nil {
		l.Images = *f.Images
	}
	if f.Network != nil {
		l.Network = *f.Network
	}
	if f.HostProcesses != nil {
		l.HostProcesses = *f.HostProcesses
	}
	if f.Secrets != nil {
		l.Secrets = *f.Secrets
	}
	return nil
}
