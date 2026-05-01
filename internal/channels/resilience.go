package channels

import (
	"context"
	"log"
	"time"
)

// Result wraps a value with a possible error. We avoid Go's stdlib error
// interface inside agent payloads since we want type-parametric error handling.
type Result[T any] struct {
	Value T
	Err   error
}

// Ok constructs a successful Result.
func Ok[T any](v T) Result[T] {
	return Result[T]{Value: v}
}

// Fail constructs a failed Result.
func Fail[T any](err error) Result[T] {
	var zero T
	return Result[T]{Value: zero, Err: err}
}

// MapResult unwraps Ok values and discards failures, logging them.
// Useful as the final stage of a pipeline that emitted Results.
func MapResult[A any](onError func(error)) Agent[Result[A], A] {
	return func(ctx context.Context, in <-chan Result[A]) <-chan A {
		out := make(chan A)
		go func() {
			defer close(out)
			for {
				select {
				case <-ctx.Done():
					return
				case r, ok := <-in:
					if !ok {
						return
					}
					if r.Err != nil {
						if onError != nil {
							onError(r.Err)
						}
						continue
					}
					select {
					case <-ctx.Done():
						return
					case out <- r.Value:
					}
				}
			}
		}()
		return out
	}
}

// Backoff computes the delay between retry attempts.
type Backoff func(attempt int) time.Duration

// LinearBackoff returns the same delay each time.
func LinearBackoff(d time.Duration) Backoff {
	return func(attempt int) time.Duration { return d }
}

// ExponentialBackoff doubles the delay each attempt, capped at max.
func ExponentialBackoff(base, max time.Duration) Backoff {
	return func(attempt int) time.Duration {
		d := base
		for i := 0; i < attempt && d < max; i++ {
			d *= 2
		}
		if d > max {
			d = max
		}
		return d
	}
}

// Retry wraps an Agent[A, Result[B]], retrying failed Results up to n times.
// The wrapped function MUST return Result[B] so we can detect failure without
// using Go's error interface (which doesn't compose well with generics).
func Retry[A, B any](n int, backoff Backoff) Decorator[A, Result[B]] {
	return func(base Agent[A, Result[B]]) Agent[A, Result[B]] {
		return func(ctx context.Context, in <-chan A) <-chan Result[B] {
			out := make(chan Result[B])
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
						var result Result[B]
						for attempt := 0; attempt <= n; attempt++ {
							// Run the base agent on a single-element channel
							single := make(chan A, 1)
							single <- a
							close(single)
							r := <-base(ctx, single)
							result = r
							if r.Err == nil {
								break
							}
							if attempt < n {
								select {
								case <-ctx.Done():
									return
								case <-time.After(backoff(attempt)):
								}
							}
						}
						select {
						case <-ctx.Done():
							return
						case out <- result:
						}
					}
				}
			}()
			return out
		}
	}
}

// Timeout wraps an agent so each input has at most d to produce its output.
// On timeout the input is silently dropped (caller should layer Retry on top
// if recovery is desired, or use TimeoutResult for explicit error signaling).
func Timeout[A, B any](d time.Duration) Decorator[A, B] {
	return func(base Agent[A, B]) Agent[A, B] {
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
						subCtx, cancel := context.WithTimeout(ctx, d)
						single := make(chan A, 1)
						single <- a
						close(single)
						sub := base(subCtx, single)
						select {
						case b, ok := <-sub:
							if ok {
								select {
								case <-ctx.Done():
									cancel()
									return
								case out <- b:
								}
							}
						case <-subCtx.Done():
							// timed out; drop
						}
						cancel()
					}
				}
			}()
			return out
		}
	}
}

// Logged wraps an agent with input/output logging using the agent's name.
func Logged[A, B any](name string) Decorator[A, B] {
	return func(base Agent[A, B]) Agent[A, B] {
		return func(ctx context.Context, in <-chan A) <-chan B {
			out := make(chan B)
			tapped := Tap(func(a A) {
				log.Printf("[%s] in", name)
			})(ctx, in)
			downstream := base(ctx, tapped)
			go func() {
				defer close(out)
				for {
					select {
					case <-ctx.Done():
						return
					case b, ok := <-downstream:
						if !ok {
							return
						}
						log.Printf("[%s] out", name)
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
}

// Window collects every n inputs into a slice and emits the slice.
// On context cancel, drops any partial window.
func Window[A any](n int) Agent[A, []A] {
	return func(ctx context.Context, in <-chan A) <-chan []A {
		out := make(chan []A)
		go func() {
			defer close(out)
			buf := make([]A, 0, n)
			for {
				select {
				case <-ctx.Done():
					return
				case a, ok := <-in:
					if !ok {
						if len(buf) > 0 {
							select {
							case <-ctx.Done():
							case out <- buf:
							}
						}
						return
					}
					buf = append(buf, a)
					if len(buf) == n {
						select {
						case <-ctx.Done():
							return
						case out <- buf:
						}
						buf = make([]A, 0, n)
					}
				}
			}
		}()
		return out
	}
}
