// Package plugin 提供 Agent 能力包：在 Go 中静态组合工具、提示词片段、循环钩子、中间件、回调与进程级资源，
// 由 Host 管理启停，经 Install 装进 builder.AgentBuilder。
package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
	"github.com/kart-io/goagent/core/middleware"
	"github.com/kart-io/goagent/interfaces"
)

// Plugin 是一个 Agent 能力包。
type Plugin interface {
	// Name 在一个 Host 内唯一：非空、无首尾空白、不含 "/" 与 "@"。
	Name() string
	// Version 进入持久运行中 PreStep 的身份：非空、无首尾空白、不含 "/"。
	Version() string
	// Contribute 在每次 Install 时调用，返回要装进本次构建的 Agent 的能力。
	Contribute(ctx context.Context, env *Env) (Contribution, error)
}

// Starter 由持有进程级资源的插件实现：Host.Start 时按依赖顺序调用 Start，Host.Stop 时逆序调用 Stop。
// Start 失败的插件不会再被调用 Stop，须自行释放已打开的部分。
type Starter interface {
	Start(ctx context.Context, env *Env) error
	Stop(ctx context.Context) error
}

// Dependent 声明必须先于本插件启动的插件名。
type Dependent interface {
	Requires() []string
}

// PreStep 是带身份的 PreStep 钩子；持久运行中以 "plugin/<名称>@<版本>/<Identity>" 进入日志，
// 插件升级版本或改变 Identity 后，恢复旧运行以 operation intent or attempt policy changed 失败。
type PreStep struct {
	Identity string
	Hook     builder.PreStepHook
}

// Contribution 是插件向 Agent 贡献的能力，各字段对应 Builder 的同名扩展点。
type Contribution struct {
	Tools          []interfaces.Tool
	PromptSections []builder.PromptSection
	PromptContexts []builder.PromptContext
	PreSteps       []PreStep
	RequestHooks   []builder.RequestHook
	RequestErrors  []builder.RequestErrorHook
	// MaxRecoveries 只对 RequestErrors 生效；所选插件取最大值，全为 0 时用 Builder 的默认值。
	MaxRecoveries int
	TurnStopping  []builder.TurnStoppingHook
	Middleware    []middleware.Middleware
	Callbacks     []core.Callback
}

func validateName(name string) error {
	if err := validateIdent("插件名", name); err != nil {
		return err
	}
	if strings.Contains(name, "@") {
		return fmt.Errorf("插件名 %q 不能包含 @", name)
	}
	return nil
}

func validateVersion(version string) error { return validateIdent("插件版本", version) }

func validateIdent(kind, v string) error {
	switch {
	case v == "":
		return fmt.Errorf("%s为空", kind)
	case strings.TrimSpace(v) != v:
		return fmt.Errorf("%s %q 含首尾空白", kind, v)
	case strings.Contains(v, "/"):
		return fmt.Errorf("%s %q 不能包含 /", kind, v)
	}
	return nil
}
