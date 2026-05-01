package channels

import "context"

// Compose is the categorical composition g ∘ f for agents.
// (Agent[A, B], Agent[B, C]) -> Agent[A, C]
func Compose[A, B, C any](f Agent[A, B], g Agent[B, C]) Agent[A, C] {
	return func(ctx context.Context, in <-chan A) <-chan C {
		return g(ctx, f(ctx, in))
	}
}

// Pipe2 is sugar for Compose. Reads top-to-bottom.
func Pipe2[A, B, C any](f Agent[A, B], g Agent[B, C]) Agent[A, C] {
	return Compose(f, g)
}

// Pipe3 chains three stages.
func Pipe3[A, B, C, D any](
	f Agent[A, B], g Agent[B, C], h Agent[C, D],
) Agent[A, D] {
	return func(ctx context.Context, in <-chan A) <-chan D {
		return h(ctx, g(ctx, f(ctx, in)))
	}
}

// Pipe4 chains four stages.
func Pipe4[A, B, C, D, E any](
	f Agent[A, B], g Agent[B, C], h Agent[C, D], i Agent[D, E],
) Agent[A, E] {
	return func(ctx context.Context, in <-chan A) <-chan E {
		return i(ctx, h(ctx, g(ctx, f(ctx, in))))
	}
}

// Pipe5 chains five stages.
func Pipe5[A, B, C, D, E, F any](
	f Agent[A, B], g Agent[B, C], h Agent[C, D], i Agent[D, E], j Agent[E, F],
) Agent[A, F] {
	return func(ctx context.Context, in <-chan A) <-chan F {
		return j(ctx, i(ctx, h(ctx, g(ctx, f(ctx, in)))))
	}
}

// Pipe6 chains six stages.
func Pipe6[A, B, C, D, E, F, G any](
	f Agent[A, B], g Agent[B, C], h Agent[C, D], i Agent[D, E], j Agent[E, F], k Agent[F, G],
) Agent[A, G] {
	return func(ctx context.Context, in <-chan A) <-chan G {
		return k(ctx, j(ctx, i(ctx, h(ctx, g(ctx, f(ctx, in))))))
	}
}

// Decorator wraps an Agent[A, B] and returns a new Agent[A, B] with extra behavior.
// This is the type that resilience combinators (Retry, Timeout, Logged) inhabit.
type Decorator[A, B any] func(Agent[A, B]) Agent[A, B]

// Decorate applies decorators in order, innermost-first.
// Decorate(base, Retry, Timeout, Logged) means: Logged wraps Timeout wraps Retry wraps base.
// Reads left-to-right as "first retry, then timeout, then log".
func Decorate[A, B any](base Agent[A, B], decorators ...Decorator[A, B]) Agent[A, B] {
	wrapped := base
	for _, d := range decorators {
		wrapped = d(wrapped)
	}
	return wrapped
}
