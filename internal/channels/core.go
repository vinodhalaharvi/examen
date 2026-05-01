// Package channels provides generic, interface-free combinators for
// composing concurrent agent pipelines.
//
// An Agent is a typed stream transformation: Agent[A, B] = func(<-chan A) <-chan B.
// Agents compose via Pipe, fan out via Tee, fuse via Zip, and can be decorated
// with cross-cutting concerns like Retry and Timeout.
package channels

import (
	"context"
)

// Agent is the central abstraction: a stream transformation from In to Out.
// Always cancellation-aware via context.
type Agent[In, Out any] func(ctx context.Context, in <-chan In) <-chan Out

// Map lifts a pure function A -> B into Agent[A, B].
// This is the functor's action on arrows.
func Map[A, B any](fn func(A) B) Agent[A, B] {
	return func(ctx context.Context, in <-chan A) <-chan B {
		out := make(chan B)
		go func() {
			defer close(out)
			for {
				select {
				case <-ctx.Done():
					return
				case a, ok := <-in:
					if !ok {
						return
					}
					b := fn(a)
					select {
					case <-ctx.Done():
						return
					case out <- b:
					}
				}
			}
		}()
		return out
	}
}

// MapCtx is like Map but the function receives the context.
// Useful for agents that need to make cancellable LLM calls.
func MapCtx[A, B any](fn func(context.Context, A) B) Agent[A, B] {
	return func(ctx context.Context, in <-chan A) <-chan B {
		out := make(chan B)
		go func() {
			defer close(out)
			for {
				select {
				case <-ctx.Done():
					return
				case a, ok := <-in:
					if !ok {
						return
					}
					b := fn(ctx, a)
					select {
					case <-ctx.Done():
						return
					case out <- b:
					}
				}
			}
		}()
		return out
	}
}

// Filter keeps only values for which pred returns true.
func Filter[A any](pred func(A) bool) Agent[A, A] {
	return func(ctx context.Context, in <-chan A) <-chan A {
		out := make(chan A)
		go func() {
			defer close(out)
			for {
				select {
				case <-ctx.Done():
					return
				case a, ok := <-in:
					if !ok {
						return
					}
					if !pred(a) {
						continue
					}
					select {
					case <-ctx.Done():
						return
					case out <- a:
					}
				}
			}
		}()
		return out
	}
}

// FlatMap turns each input into zero-or-more outputs.
// One-to-many fan-out without spawning extra goroutines.
func FlatMap[A, B any](fn func(A) []B) Agent[A, B] {
	return func(ctx context.Context, in <-chan A) <-chan B {
		out := make(chan B)
		go func() {
			defer close(out)
			for {
				select {
				case <-ctx.Done():
					return
				case a, ok := <-in:
					if !ok {
						return
					}
					for _, b := range fn(a) {
						select {
						case <-ctx.Done():
							return
						case out <- b:
						}
					}
				}
			}
		}()
		return out
	}
}

// Just emits a single value and closes. Useful for seeding pipelines in tests
// or one-shot requests.
func Just[A any](ctx context.Context, a A) <-chan A {
	out := make(chan A, 1)
	out <- a
	close(out)
	return out
}

// FromSlice emits each element of a slice in order.
func FromSlice[A any](ctx context.Context, as []A) <-chan A {
	out := make(chan A)
	go func() {
		defer close(out)
		for _, a := range as {
			select {
			case <-ctx.Done():
				return
			case out <- a:
			}
		}
	}()
	return out
}

// Collect drains a channel into a slice. Blocks until the channel closes
// or context cancels.
func Collect[A any](ctx context.Context, in <-chan A) []A {
	var result []A
	for {
		select {
		case <-ctx.Done():
			return result
		case a, ok := <-in:
			if !ok {
				return result
			}
			result = append(result, a)
		}
	}
}

// Drain consumes and discards values, returning when the channel closes.
// Used to ensure goroutines finish in tests.
func Drain[A any](ctx context.Context, in <-chan A) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-in:
			if !ok {
				return
			}
		}
	}
}
