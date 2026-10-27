package builder

import (
	"context"
	"fmt"
	"io"

	"github.com/kart-io/goagent/core"
)

// AsRunnable 将 Builder 接入标准泛型执行契约。
// Invoke 收集 ExecuteStream 的最终结果，因此工具、检索、中间件及用量路径相同。
// 需要 StreamClient；缓冲模型可声明实际流能力。原有 Execute 语义保持不变。
// 同一 Builder 的状态与记忆属于一个会话，适配器串行执行，等待可被取消。
func (a *ConfigurableAgent[C, S]) AsRunnable() *core.StreamRunnable[any, *AgentOutput] {
	mode := core.StreamModeTokens
	if capability, ok := a.llmClient.(core.StreamCapability); ok {
		mode = capability.StreamMode()
		if mode == core.StreamModeFinal {
			mode = core.StreamModeSteps
		}
	}
	return core.NewStreamRunnable(mode, func(ctx context.Context, input any, emit func(core.StreamChunk[*AgentOutput]) error) error {
		events, err := a.ExecuteStream(ctx, input)
		if err != nil {
			return err
		}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case event, ok := <-events:
				if !ok {
					return io.ErrUnexpectedEOF
				}
				if event.Error != nil {
					return event.Error
				}
				if event.Type == "error" {
					return fmt.Errorf("执行失败：%s", event.Message)
				}
				chunk := core.StreamChunk[*AgentOutput]{Data: event.Output, Done: event.Type == "done",
					Event: &core.StreamEvent{Type: event.Type, Delta: event.Delta, Step: event.Step, Tool: event.Tool, Sources: event.Sources, Usage: event.Usage}}
				if chunk.Done && chunk.Data == nil {
					return fmt.Errorf("最终事件缺少输出")
				}
				if err := emit(chunk); err != nil {
					return err
				}
				if chunk.Done {
					return nil
				}
			}
		}
	})
}
