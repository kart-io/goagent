package preset

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kart-io/goagent/harness/env"
	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools/coding"
	"github.com/kart-io/goagent/tools/policy"
	"github.com/kart-io/goagent/tools/spill"
)

// 内置组件名。
const (
	ComponentPersona      = "persona"
	ComponentInstructions = "instructions"
	ComponentWorkspace    = "workspace"
	ComponentCodingTools  = "coding-tools"
	ComponentPermissions  = "permissions"
	ComponentSpill        = "spill"
)

func init() {
	Register(personaSpec)
	Register(instructionsSpec)
	Register(workspaceSpec)
	Register(codingToolsSpec)
	Register(permissionsSpec)
	Register(spillSpec)
}

// PersonaConfig 是 persona 组件的配置：一段系统提示词。
type PersonaConfig struct {
	Prompt string `yaml:"prompt"`
}

var personaSpec = Spec[PersonaConfig]{
	Name: ComponentPersona, Version: "1",
	Validate: func(c *PersonaConfig) error {
		if strings.TrimSpace(c.Prompt) == "" {
			return errors.New("prompt 不能为空")
		}
		return nil
	},
	Build: func(_ context.Context, a *Assembler, c *PersonaConfig) error {
		a.AddPrompt(c.Prompt)
		return nil
	},
}

// InstructionsConfig 是 instructions 组件的配置：把工作区内的项目说明文件拼进系统提示词。
type InstructionsConfig struct {
	// Files 默认 [AGENTS.md, CLAUDE.md]。
	Files []string `yaml:"files"`
	// MaxBytes 是拼接结果的上限，默认 32768，超出时截断。
	MaxBytes int `yaml:"maxBytes"`
}

var instructionsSpec = Spec[InstructionsConfig]{
	Name: ComponentInstructions, Version: "1", Singleton: true, Requires: []string{ComponentWorkspace},
	Validate: func(c *InstructionsConfig) error {
		if len(c.Files) == 0 {
			c.Files = []string{"AGENTS.md", "CLAUDE.md"}
		}
		if c.MaxBytes == 0 {
			c.MaxBytes = 32768
		}
		if c.MaxBytes < 0 {
			return errors.New("maxBytes 不能为负")
		}
		return nil
	},
	Build: func(ctx context.Context, a *Assembler, c *InstructionsConfig) error {
		w, _, err := a.Workspace()
		if err != nil {
			return err
		}
		text, err := w.ProjectInstructionsFrom(ctx, c.Files...)
		if err != nil || text == "" {
			return err
		}
		if len(text) > c.MaxBytes {
			text = strings.ToValidUTF8(text[:c.MaxBytes], "") + "\n（项目说明超过上限，已截断）"
		}
		a.AddPrompt("项目说明：\n" + text)
		return nil
	},
}

// WorkspaceConfig 是 workspace 组件的配置。沙箱模式是对执行环境的要求，执行器做不到时 Build 失败：
// read-only 下文件系统只读，命令只能在以只读方式挂载工作区的容器中执行，否则不提供命令工具；
// workspace-write 下命令只能在容器中执行；danger-full-access 允许在宿主机执行。
type WorkspaceConfig struct {
	// Sandbox 默认 workspace-write。
	Sandbox Sandbox `yaml:"sandbox"`
	// Executor 为 docker（默认）或 local。
	Executor string `yaml:"executor"`
	// Image 是 docker 执行器的镜像。
	Image string `yaml:"image"`
	// Network 表示命令可以联网；执行器的实际能力须与之一致。
	Network        bool          `yaml:"network"`
	CommandTimeout time.Duration `yaml:"commandTimeout"`
	MaxOutputBytes int           `yaml:"maxOutputBytes"`
	// Linters 在写入后检查文件，失败时撤销写入；文件路径作为最后一个参数追加到命令后。
	Linters []LinterConfig `yaml:"linters"`
}

// LinterConfig 按扩展名选择检查命令。
type LinterConfig struct {
	Ext     string   `yaml:"ext"`
	Command []string `yaml:"command"`
}

var workspaceSpec = Spec[WorkspaceConfig]{
	Name: ComponentWorkspace, Version: "1", Singleton: true,
	Validate: func(c *WorkspaceConfig) error {
		if c.Sandbox == "" {
			c.Sandbox = WorkspaceWrite
		}
		if !c.Sandbox.valid() {
			return fmt.Errorf("sandbox %q 无效", c.Sandbox)
		}
		if c.Executor == "" {
			c.Executor = "docker"
		}
		switch c.Executor {
		case "docker":
			if c.Image == "" {
				return errors.New("docker 执行器需要 image")
			}
		case "local":
			if c.Image != "" {
				return errors.New("local 执行器不使用 image")
			}
		default:
			return fmt.Errorf("executor %q 无效，可选 docker、local", c.Executor)
		}
		if c.CommandTimeout < 0 || c.MaxOutputBytes < 0 {
			return errors.New("commandTimeout 与 maxOutputBytes 不能为负")
		}
		for _, l := range c.Linters {
			if !strings.HasPrefix(l.Ext, ".") || len(l.Command) == 0 {
				return errors.New("linter 需要以 . 开头的 ext 与非空 command")
			}
		}
		return nil
	},
	Needs: func(c *WorkspaceConfig) Needs {
		return Needs{Sandbox: c.Sandbox, Executor: c.Executor, Image: c.Image, Network: c.Network}
	},
	Build: buildWorkspace,
}

func buildWorkspace(_ context.Context, a *Assembler, c *WorkspaceConfig) error {
	if a.Root() == "" {
		return errors.New("Host.Root 未设置")
	}
	local, err := env.NewLocalFS(a.Root())
	if err != nil {
		return err
	}
	var executor env.Executor
	switch c.Executor {
	case "local":
		executor = env.LocalExecutor{Root: local.Root()}
	case "docker":
		network := ""
		if c.Network {
			network = "bridge"
		}
		executor = env.DockerExecutor{Root: local.Root(), Image: c.Image, Network: network, ReadOnly: c.Sandbox == ReadOnly}
	}
	caps := executor.Capabilities()
	var fsys env.FileSystem = local
	commands := true
	switch c.Sandbox {
	case ReadOnly:
		fsys = env.ReadOnly(local)
		commands = caps.ReadOnlyRoot
	case WorkspaceWrite:
		if !caps.Container {
			return fmt.Errorf("沙箱模式 %s 要求在容器中执行命令，%s 执行器无法限制命令写入工作区之外；改用 docker 执行器或 %s", WorkspaceWrite, c.Executor, DangerFullAccess)
		}
	}
	if commands && caps.Network != c.Network {
		return fmt.Errorf("%s 执行器的网络能力（%v）与配置 network: %v 不符", c.Executor, caps.Network, c.Network)
	}
	opts := []coding.Option{coding.WithExecutor(executor)}
	if c.CommandTimeout > 0 {
		opts = append(opts, coding.WithCommandTimeout(c.CommandTimeout))
	}
	if c.MaxOutputBytes > 0 {
		opts = append(opts, coding.WithMaxOutputBytes(c.MaxOutputBytes))
	}
	if len(c.Linters) > 0 {
		linters := c.Linters
		opts = append(opts, coding.WithLinter(func(path string) []string {
			for _, l := range linters {
				if filepath.Ext(path) == l.Ext {
					return append(slices.Clone(l.Command), path)
				}
			}
			return nil
		}))
	}
	w, err := coding.New(fsys, nil, opts...)
	if err != nil {
		return err
	}
	return a.SetWorkspace(w, WorkspaceInfo{Root: local.Root(), Sandbox: c.Sandbox, Executor: c.Executor, Image: c.Image, Network: c.Network, CommandsEnabled: commands})
}

var (
	codingWriteTools   = []string{coding.ToolWriteFile, coding.ToolEditFile, coding.ToolMultiEdit, coding.ToolGitCommit}
	codingCommandTools = []string{coding.ToolRunCommand, coding.ToolGitStatus, coding.ToolGitDiff, coding.ToolGitLog, coding.ToolGitCommit}
	codingAllTools     = []string{
		coding.ToolReadFile, coding.ToolWriteFile, coding.ToolEditFile, coding.ToolMultiEdit, coding.ToolGlob, coding.ToolGrep,
		coding.ToolRunCommand, coding.ToolGitStatus, coding.ToolGitDiff, coding.ToolGitLog, coding.ToolGitCommit, coding.ToolTodoWrite,
	}
)

// CodingToolsConfig 是 coding-tools 组件的配置。
type CodingToolsConfig struct {
	// Tools 列出要提供的工具，缺省为沙箱模式下可用的全部工具；列出不可用的工具时 Build 失败。
	Tools []string `yaml:"tools"`
}

var codingToolsSpec = Spec[CodingToolsConfig]{
	Name: ComponentCodingTools, Version: "1", Singleton: true, Requires: []string{ComponentWorkspace},
	Validate: func(c *CodingToolsConfig) error {
		for _, name := range c.Tools {
			if !slices.Contains(codingAllTools, name) {
				return fmt.Errorf("未知的编码工具 %q", name)
			}
		}
		return nil
	},
	Build: func(_ context.Context, a *Assembler, c *CodingToolsConfig) error {
		w, info, err := a.Workspace()
		if err != nil {
			return err
		}
		unavailable := func(name string) string {
			if info.Sandbox == ReadOnly && slices.Contains(codingWriteTools, name) {
				return "只读模式不提供写入类工具"
			}
			if !info.CommandsEnabled && slices.Contains(codingCommandTools, name) {
				return "执行器无法以只读方式挂载工作区，不提供执行命令的工具"
			}
			return ""
		}
		for _, name := range c.Tools {
			if reason := unavailable(name); reason != "" {
				return fmt.Errorf("工具 %s 不可用：%s", name, reason)
			}
		}
		var out []interfaces.Tool
		for _, t := range w.Tools() {
			if (len(c.Tools) == 0 && unavailable(t.Name()) == "") || slices.Contains(c.Tools, t.Name()) {
				out = append(out, t)
			}
		}
		a.AddTools(out...)
		return nil
	},
}

// PermissionsConfig 是 permissions 组件的配置。一个 preset 可以有多行，全部规则合并为一个策略（Deny > Ask > Allow），
// 因此不可信层可以新增一行 deny/ask 规则收紧权限，而无需替换可信层的行。
type PermissionsConfig struct {
	// Approval 为 ask（默认，交给宿主的 Approver）或 never（Ask 自动拒绝）。
	Approval string `yaml:"approval"`
	// Base 为 coding 时包含 coding.DefaultPolicy 的规则：放行读取、检索、待办与工作区内的编辑。
	Base string `yaml:"base"`
	// Rules 是权限规则。
	Rules []RuleConfig `yaml:"rules"`
	// SessionTools 列出审批时可以整工具"本会话允许"的工具。
	SessionTools []string `yaml:"sessionTools"`
}

// RuleConfig 是一条权限规则，Decision 为 allow、ask 或 deny。
type RuleConfig struct {
	Tool     string `yaml:"tool"`
	Command  string `yaml:"command"`
	Decision string `yaml:"decision"`
}

var decisions = map[string]policy.Decision{"allow": policy.Allow, "ask": policy.Ask, "deny": policy.Deny}

var permissionsSpec = Spec[PermissionsConfig]{
	Name: ComponentPermissions, Version: "1",
	Validate: func(c *PermissionsConfig) error {
		if c.Approval == "" {
			c.Approval = "ask"
		}
		if c.Approval != "ask" && c.Approval != "never" {
			return fmt.Errorf("approval %q 无效，可选 ask、never", c.Approval)
		}
		if c.Base != "" && c.Base != "coding" {
			return fmt.Errorf("base %q 无效，可选 coding", c.Base)
		}
		for _, r := range c.Rules {
			if r.Tool == "" {
				return errors.New("规则缺少 tool")
			}
			if _, ok := decisions[r.Decision]; !ok {
				return fmt.Errorf("规则 decision %q 无效，可选 allow、ask、deny", r.Decision)
			}
		}
		return nil
	},
	Needs: func(c *PermissionsConfig) Needs {
		var n Needs
		if c.Base == "coding" {
			n.Grants = append(n.Grants, "采用 coding 默认放行规则")
		}
		for _, r := range c.Rules {
			if r.Decision == "allow" {
				n.Grants = append(n.Grants, "放行 "+strings.TrimSpace(r.Tool+" "+r.Command))
			}
		}
		for _, t := range c.SessionTools {
			n.Grants = append(n.Grants, "允许整工具会话批准 "+t)
		}
		return n
	},
	Build: func(_ context.Context, a *Assembler, c *PermissionsConfig) error {
		if c.Base == "coding" {
			base := coding.DefaultPolicy()
			a.AddRules(base.Rules, base.SessionTools, false)
		}
		rules := make([]policy.Rule, len(c.Rules))
		for i, r := range c.Rules {
			rules[i] = policy.Rule{Tool: r.Tool, Command: r.Command, Decision: decisions[r.Decision]}
		}
		a.AddRules(rules, c.SessionTools, true)
		if c.Approval == "never" {
			a.DenyApprovals()
		}
		return nil
	},
}

// SpillConfig 是 spill 组件的配置：超长结果写入宿主产物目录，并提供 read_spill 读取。
type SpillConfig struct {
	// MaxBytes 默认 100000。
	MaxBytes int `yaml:"maxBytes"`
	// Retention 是溢出文件的保留期，默认 168h；Build 时清理过期文件。
	Retention time.Duration `yaml:"retention"`
}

var spillSpec = Spec[SpillConfig]{
	Name: ComponentSpill, Version: "1", Singleton: true,
	Validate: func(c *SpillConfig) error {
		if c.MaxBytes == 0 {
			c.MaxBytes = 100000
		}
		if c.Retention == 0 {
			c.Retention = 168 * time.Hour
		}
		if c.MaxBytes < 0 || c.Retention < 0 {
			return errors.New("maxBytes 与 retention 不能为负")
		}
		return nil
	},
	Build: func(_ context.Context, a *Assembler, c *SpillConfig) error {
		store, err := spill.New(filepath.Join(a.ArtifactDir(), "spill"), c.MaxBytes)
		if err != nil {
			return err
		}
		// 清理是尽力而为，失败不影响本次运行。
		_ = store.Prune(c.Retention)
		a.AddPostHook(store.Hook())
		a.AddTools(store.ReadTool())
		a.AddRules([]policy.Rule{{Tool: spill.ToolReadSpill, Decision: policy.Allow}}, nil, false)
		return nil
	},
}
