package middleware

import (
	"context"

	"github.com/kart-io/goagent/interfaces"
)

// ToolMiddleware 定义了工具中间件的接口。
//
// 中间件可以在工具执行前后添加横切关注点，如日志记录、缓存、限流等。
// 采用洋葱模型，多个中间件可以链式组合。
//
// 使用示例:
//
//	// 创建自定义中间件
//	type MyMiddleware struct {
//	    *BaseToolMiddleware
//	}
//
//	func (m *MyMiddleware) OnBeforeInvoke(ctx context.Context, tool interfaces.Tool, input *interfaces.ToolInput) (*interfaces.ToolInput, error) {
//	    // 前置处理
//	    return input, nil
//	}
//
//	// 应用中间件到工具
//	wrappedTool := tools.WithMiddleware(myTool, NewMyMiddleware())
type ToolMiddleware interface {
	// Name 返回中间件的名称，用于日志和调试
	Name() string

	// OnBeforeInvoke 在工具调用前执行
	//
	// 可以修改输入、验证参数、记录日志等。
	// 如果返回错误，则中止工具执行。
	OnBeforeInvoke(ctx context.Context, tool interfaces.Tool, input *interfaces.ToolInput) (*interfaces.ToolInput, error)

	// OnAfterInvoke 在工具调用后执行
	//
	// 可以修改输出、记录日志、更新缓存等。
	// 如果返回错误，则覆盖原始结果。
	OnAfterInvoke(ctx context.Context, tool interfaces.Tool, output *interfaces.ToolOutput) (*interfaces.ToolOutput, error)

	// OnError 在工具执行出错时调用
	//
	// 可以处理、包装或记录错误。
	// 返回的错误将作为最终错误返回给调用者。
	OnError(ctx context.Context, tool interfaces.Tool, err error) error
}

// ToolInvoker 是工具调用函数的类型定义。
//
// 它封装了工具的实际执行逻辑，中间件通过包装 ToolInvoker 来添加功能。
type ToolInvoker func(context.Context, *interfaces.ToolInput) (*interfaces.ToolOutput, error)

// ToolMiddlewareFunc 是中间件函数的类型定义。
//
// 它接收一个工具和下一个 invoker，返回包装后的 invoker。
// 这允许中间件链式组合，形成洋葱模型。
//
// 使用示例:
//
//	func MyMiddlewareFunc(tool interfaces.Tool, next ToolInvoker) ToolInvoker {
//	    return func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
//	        // 前置处理
//	        output, err := next(ctx, input)  // 调用下一层
//	        // 后置处理
//	        return output, err
//	    }
//	}
type ToolMiddlewareFunc func(interfaces.Tool, ToolInvoker) ToolInvoker

// Chain 将多个中间件链式组合成单个 ToolInvoker。
//
// 中间件按照传入顺序执行 OnBeforeInvoke，按逆序执行 OnAfterInvoke，
// 形成洋葱模型（Onion Model）。
//
// 执行顺序示例:
//
//	middlewares := []ToolMiddleware{logging, caching, rateLimit}
//	Chain(middlewares, baseInvoker)
//
//	执行流程:
//	  logging.OnBeforeInvoke
//	    -> caching.OnBeforeInvoke
//	      -> rateLimit.OnBeforeInvoke
//	        -> baseInvoker (实际工具执行)
//	      <- rateLimit.OnAfterInvoke
//	    <- caching.OnAfterInvoke
//	  <- logging.OnAfterInvoke
//
// 参数:
//   - tool: 被包装的工具实例
//   - invoker: 最内层的调用函数（通常是工具的原始 Invoke 方法）
//   - middlewares: 中间件列表，按顺序应用
//
// 返回:
//   - 包装后的 ToolInvoker，包含所有中间件逻辑
func Chain(tool interfaces.Tool, invoker ToolInvoker, middlewares ...ToolMiddleware) ToolInvoker {
	// 没有中间件时直接返回原始 invoker
	if len(middlewares) == 0 {
		return invoker
	}

	// 从最后一个中间件开始，逆序包装（洋葱模型）
	wrapped := invoker
	for i := len(middlewares) - 1; i >= 0; i-- {
		middleware := middlewares[i]
		nextInvoker := wrapped

		// 为每个中间件创建包装层
		wrapped = func(ctx context.Context, input *interfaces.ToolInput) (*interfaces.ToolOutput, error) {
			// OnBeforeInvoke: 前置处理
			modifiedInput, err := middleware.OnBeforeInvoke(ctx, tool, input)
			if err != nil {
				// 前置错误处理
				return nil, middleware.OnError(ctx, tool, err)
			}

			// 调用下一层（可能是另一个中间件或实际工具）
			output, err := nextInvoker(ctx, modifiedInput)

			// 如果执行出错，调用错误处理
			if err != nil {
				return nil, middleware.OnError(ctx, tool, err)
			}

			// 将 input 信息传递给 output（通过 Metadata）
			// 这样 OnAfterInvoke 可以访问原始输入
			if output != nil {
				if output.Metadata == nil {
					output.Metadata = make(map[string]interface{})
				}
				// 传递关键的 input 信息
				if modifiedInput != nil && modifiedInput.Args != nil {
					// 复制特殊的元数据键（如__logging_start_time）
					for k, v := range modifiedInput.Args {
						if len(k) > 2 && k[:2] == "__" {
							output.Metadata[k] = v
						}
					}
				}
			}

			// OnAfterInvoke: 后置处理
			modifiedOutput, err := middleware.OnAfterInvoke(ctx, tool, output)
			if err != nil {
				return nil, middleware.OnError(ctx, tool, err)
			}

			return modifiedOutput, nil
		}
	}

	return wrapped
}

// BaseToolMiddleware 提供中间件接口的默认实现。
//
// 所有方法都是空操作（no-op），子类可以选择性重写需要的方法。
// 这遵循了 Go 的组合模式，避免强制实现所有接口方法。
//
// 使用示例:
//
//	type LoggingMiddleware struct {
//	    *BaseToolMiddleware
//	    logger Logger
//	}
//
//	func NewLoggingMiddleware(logger Logger) *LoggingMiddleware {
//	    return &LoggingMiddleware{
//	        BaseToolMiddleware: NewBaseToolMiddleware("logging"),
//	        logger: logger,
//	    }
//	}
//
//	// 只需重写需要的方法
//	func (m *LoggingMiddleware) OnBeforeInvoke(ctx context.Context, tool interfaces.Tool, input *interfaces.ToolInput) (*interfaces.ToolInput, error) {
//	    m.logger.Info("Tool invoked", "tool", tool.Name())
//	    return input, nil
//	}
type BaseToolMiddleware struct {
	name string
}

// NewBaseToolMiddleware 创建一个基础中间件实例。
//
// 参数:
//   - name: 中间件名称，用于日志和调试
func NewBaseToolMiddleware(name string) *BaseToolMiddleware {
	return &BaseToolMiddleware{
		name: name,
	}
}

// Name 返回中间件名称
func (m *BaseToolMiddleware) Name() string {
	return m.name
}

// OnBeforeInvoke 默认实现，不做任何修改
func (m *BaseToolMiddleware) OnBeforeInvoke(ctx context.Context, tool interfaces.Tool, input *interfaces.ToolInput) (*interfaces.ToolInput, error) {
	return input, nil
}

// OnAfterInvoke 默认实现，不做任何修改
func (m *BaseToolMiddleware) OnAfterInvoke(ctx context.Context, tool interfaces.Tool, output *interfaces.ToolOutput) (*interfaces.ToolOutput, error) {
	return output, nil
}

// OnError 默认实现，直接返回原始错误
func (m *BaseToolMiddleware) OnError(ctx context.Context, tool interfaces.Tool, err error) error {
	return err
}
