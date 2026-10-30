package preset

import (
	"github.com/kart-io/goagent/builder"
	"github.com/kart-io/goagent/core"
)

// Loop 是组件贡献的循环扩展点，按组件在列表中的顺序排列。
type Loop struct {
	PreStep        []builder.PreStepHook
	RequestError   []builder.RequestErrorHook
	TurnStopping   []builder.TurnStoppingHook
	PromptSections []builder.PromptSection
	PromptContexts []builder.PromptContext
}

func (l *Loop) add(o Loop) {
	l.PreStep = append(l.PreStep, o.PreStep...)
	l.RequestError = append(l.RequestError, o.RequestError...)
	l.TurnStopping = append(l.TurnStopping, o.TurnStopping...)
	l.PromptSections = append(l.PromptSections, o.PromptSections...)
	l.PromptContexts = append(l.PromptContexts, o.PromptContexts...)
}

// AddPreStep 追加 PreStep 钩子。
func (a *Assembler) AddPreStep(h builder.PreStepHook) {
	a.slot().loop.PreStep = append(a.slot().loop.PreStep, h)
}

// AddRequestError 追加 RequestError 钩子。
func (a *Assembler) AddRequestError(h builder.RequestErrorHook) {
	a.slot().loop.RequestError = append(a.slot().loop.RequestError, h)
}

// AddTurnStopping 追加 TurnStopping 钩子。
func (a *Assembler) AddTurnStopping(h builder.TurnStoppingHook) {
	a.slot().loop.TurnStopping = append(a.slot().loop.TurnStopping, h)
}

// AddPromptSection 追加进入 system 的提示词段落。
func (a *Assembler) AddPromptSection(s builder.PromptSection) {
	a.slot().loop.PromptSections = append(a.slot().loop.PromptSections, s)
}

// AddPromptContext 追加每步重算的提示词上下文。
func (a *Assembler) AddPromptContext(c builder.PromptContext) {
	a.slot().loop.PromptContexts = append(a.slot().loop.PromptContexts, c)
}

// Configure 把装配结果交给 Builder：系统提示词、工具与循环扩展点。
func Configure[C any, S core.State](b *builder.AgentBuilder[C, S], asm *Assembly) *builder.AgentBuilder[C, S] {
	l := asm.Loop
	b = b.WithSystemPrompt(asm.SystemPrompt).WithTools(asm.Tools...).
		WithPreStep(l.PreStep...).WithTurnStopping(l.TurnStopping...).
		WithPromptSections(l.PromptSections...).WithPromptContexts(l.PromptContexts...)
	if len(l.RequestError) > 0 {
		b = b.WithRequestError(0, l.RequestError...)
	}
	return b
}
