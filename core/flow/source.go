// Package flow is an experimental synchronous, read-only incremental pipeline.
// Sources must yield serially, honor errors/cancellation, and stop producers before returning.
package flow

import (
	"context"
	"errors"
	"fmt"
)

// Source yields data synchronously; nil return is successful completion.
type Source[T any] func(context.Context, func(T) error) error

// Map transforms each value once, retaining errors even if a source ignores yield failures.
func Map[A, B any](source Source[A], fn func(context.Context, A) (B, error)) Source[B] {
	return func(ctx context.Context, yield func(B) error) error {
		if source == nil || fn == nil || yield == nil {
			return fmt.Errorf("source, mapper and yield required")
		}
		var first error
		err := source(ctx, func(a A) error {
			if first != nil {
				return first
			}
			if first = ctx.Err(); first != nil {
				return first
			}
			var b B
			b, first = fn(ctx, a)
			if first == nil {
				first = yield(b)
			}
			return first
		})
		return errors.Join(err, first, ctx.Err())
	}
}
