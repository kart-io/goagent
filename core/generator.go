package core

import (
	"context"
	"iter"
)

// Generator defines a generator type based on Go 1.25 iter.Seq2.
//
// Generator provides lazily evaluated streaming with these advantages over
// channels:
//   - Zero allocation (no channel or goroutine overhead)
//   - Early termination (through the yield return value)
//   - A uniform iteration interface (for-range loops)
//
// Example:
//
//	gen := agent.RunGenerator(ctx, input)
//	for output, err := range gen {
//	    if err != nil {
//	        return err
//	    }
//	    fmt.Println(output)
//	}
type Generator[T any] iter.Seq2[T, error]

// GeneratorFunc converts a function into a Generator.
//
// Parameters:
//   - fn - the generator function, which takes a yield function
//
// Returns:
//   - a Generator
//
// Example:
//
//	gen := GeneratorFunc(func(yield func(T, error) bool) {
//	    for i := 0; i < 10; i++ {
//	        if !yield(data, nil) {
//	            return  // early termination
//	        }
//	    }
//	})
func GeneratorFunc[T any](fn func(yield func(T, error) bool)) Generator[T] {
	return Generator[T](fn)
}

// ToChannel converts a Generator into a channel.
//
// Parameters:
//   - ctx - the context (for cancellation)
//   - gen - the Generator
//   - bufferSize - the channel buffer size (0 means unbuffered)
//
// Returns:
//   - a receive-only channel of StreamChunk
//
// Notes:
//   - This function starts a goroutine that consumes the Generator
//   - The channel closes automatically when ctx is canceled or the Generator
//     ends
//   - On ctx cancellation, a cancellation error chunk is sent only if the
//     consumer is receiving or the buffer has room; otherwise the channel just
//     closes. After the channel closes, check ctx.Err() to tell normal
//     completion from cancellation
//
// Example:
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
		// Generator 是宿主代码，它在本 goroutine 中的 panic 调用方无法 recover，转为最后一个块的错误。
		var panicErr error
		defer func() {
			if panicErr != nil {
				select {
				case ch <- StreamChunk[T]{Error: panicErr}:
				case <-ctx.Done():
				}
			}
		}()
		defer RecoverPanic(ctx, "generator", "to_channel", &panicErr)

		gen(func(data T, err error) bool {
			select {
			case <-ctx.Done():
				// 消费方可能已放弃读取，不阻塞；channel 关闭时调用方应以 ctx.Err() 判断是否因取消结束。
				select {
				case ch <- StreamChunk[T]{Error: ctx.Err()}:
				default:
				}
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

// FromChannel converts a channel into a Generator.
//
// Parameters:
//   - ch - the input channel
//
// Returns:
//   - a Generator
//
// Example:
//
//	gen := FromChannel(ch)
//	for data, err := range gen {
//	    // process data
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

// Collect collects all Generator output into a slice.
//
// Parameters:
//   - gen - the Generator
//
// Returns:
//   - a slice of all data and the first error
//
// Notes:
//   - Collection stops at the first error
//   - Suitable for small datasets; stream large datasets instead
//
// Example:
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

// Take takes the first n elements from a Generator.
//
// Parameters:
//   - gen - the Generator
//   - n - the number of elements to take
//
// Returns:
//   - a new Generator that yields at most n elements
//
// Example:
//
//	first5 := Take(gen, 5)
//	for data, err := range first5 {
//	    // process at most 5 elements
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

// Filter filters Generator output.
//
// Parameters:
//   - gen - the Generator
//   - predicate - the filter function
//
// Returns:
//   - a new Generator containing only elements that satisfy predicate
//
// Example:
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

// Map maps Generator output.
//
// Parameters:
//   - gen - the Generator
//   - mapper - the mapping function
//
// Returns:
//   - a new Generator containing the mapped elements
//
// Example:
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
