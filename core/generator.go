package core

import (
	"context"
	"iter"
)

// Generator 定义生成器类型（基于 Go 1.25 iter.Seq2）
//
// Generator 提供惰性求值的流式处理能力，相比 Channel 有以下优势：
//   - 零内存分配（无 channel、goroutine 开销）
//   - 支持早期终止（通过 yield 返回值）
//   - 统一的迭代接口（for-range 循环）
//
// 示例：
//
//	gen := agent.RunGenerator(ctx, input)
//	for output, err := range gen {
//	    if err != nil {
//	        return err
//	    }
//	    fmt.Println(output)
//	}
type Generator[T any] iter.Seq2[T, error]

// GeneratorFunc 将函数转换为 Generator
//
// 参数：
//   - fn - 生成器函数，接受 yield 函数作为参数
//
// 返回：
//   - Generator 实例
//
// 示例：
//
//	gen := GeneratorFunc(func(yield func(T, error) bool) {
//	    for i := 0; i < 10; i++ {
//	        if !yield(data, nil) {
//	            return  // 早期终止
//	        }
//	    }
//	})
func GeneratorFunc[T any](fn func(yield func(T, error) bool)) Generator[T] {
	return Generator[T](fn)
}

// ToChannel 将 Generator 转换为 Channel
//
// 参数：
//   - ctx - 上下文（用于取消）
//   - gen - Generator 实例
//   - bufferSize - channel 缓冲区大小（0 表示无缓冲）
//
// 返回：
//   - 只读 channel，包含 StreamChunk
//
// 注意：
//   - 此函数会启动 goroutine 消费 Generator
//   - 当 ctx 取消或 Generator 结束时，channel 自动关闭
//
// 示例：
//
//	ch := ToChannel(ctx, gen, 10)
//	for item := range ch {
//	    if item.Error != nil {
//	        return item.Error
//	    }
//	    fmt.Println(item.Data)
//	}
func ToChannel[T any](ctx context.Context, gen Generator[T], bufferSize int) <-chan StreamChunk[T] {
	ch := make(chan StreamChunk[T], bufferSize)

	go func() {
		defer close(ch)

		gen(func(data T, err error) bool {
			select {
			case <-ctx.Done():
				ch <- StreamChunk[T]{Error: ctx.Err()}
				return false
			case ch <- StreamChunk[T]{Data: data, Error: err}:
				if err != nil {
					return false
				}
				return true
			}
		})
	}()

	return ch
}

// FromChannel 将 Channel 转换为 Generator
//
// 参数：
//   - ch - 输入 channel
//
// 返回：
//   - Generator 实例
//
// 示例：
//
//	gen := FromChannel(ch)
//	for data, err := range gen {
//	    // 处理数据
//	}
func FromChannel[T any](ch <-chan StreamChunk[T]) Generator[T] {
	return func(yield func(T, error) bool) {
		for event := range ch {
			if !yield(event.Data, event.Error) {
				return
			}
			if event.Error != nil {
				return
			}
		}
	}
}

// Collect 收集 Generator 的所有输出到切片
//
// 参数：
//   - gen - Generator 实例
//
// 返回：
//   - 所有数据的切片和第一个错误
//
// 注意：
//   - 遇到第一个错误时停止收集
//   - 适用于小数据集，大数据集建议流式处理
//
// 示例：
//
//	results, err := Collect(gen)
//	if err != nil {
//	    return err
//	}
//	fmt.Println(results)
func Collect[T any](gen Generator[T]) ([]T, error) {
	var results []T
	var firstErr error

	gen(func(data T, err error) bool {
		if err != nil {
			firstErr = err
			return false // 遇到错误立即停止
		}
		results = append(results, data)
		return true
	})

	return results, firstErr
}

// Take 从 Generator 中取前 n 个元素
//
// 参数：
//   - gen - Generator 实例
//   - n - 要取的元素数量
//
// 返回：
//   - 新的 Generator，最多产生 n 个元素
//
// 示例：
//
//	first5 := Take(gen, 5)
//	for data, err := range first5 {
//	    // 最多处理 5 个元素
//	}
func Take[T any](gen Generator[T], n int) Generator[T] {
	return func(yield func(T, error) bool) {
		count := 0
		gen(func(data T, err error) bool {
			if count >= n {
				return false // 停止上游生成器
			}
			count++
			return yield(data, err) // 传递下游的停止信号
		})
	}
}

// Filter 过滤 Generator 输出
//
// 参数：
//   - gen - Generator 实例
//   - predicate - 过滤条件函数
//
// 返回：
//   - 新的 Generator，仅包含满足条件的元素
//
// 示例：
//
//	filtered := Filter(gen, func(data T) bool {
//	    return data.Score > 0.5
//	})
func Filter[T any](gen Generator[T], predicate func(T) bool) Generator[T] {
	return func(yield func(T, error) bool) {
		gen(func(data T, err error) bool {
			if err != nil {
				var zero T
				return yield(zero, err)
			}
			if predicate(data) {
				return yield(data, nil)
			}
			return true // 跳过不匹配的元素，继续迭代
		})
	}
}

// Map 映射 Generator 输出
//
// 参数：
//   - gen - Generator 实例
//   - mapper - 映射函数
//
// 返回：
//   - 新的 Generator，包含映射后的元素
//
// 示例：
//
//	mapped := Map(gen, func(data T) R {
//	    return transform(data)
//	})
func Map[T, R any](gen Generator[T], mapper func(T) R) Generator[R] {
	return func(yield func(R, error) bool) {
		gen(func(data T, err error) bool {
			if err != nil {
				var zero R
				return yield(zero, err)
			}
			return yield(mapper(data), nil)
		})
	}
}
