package channels

import (
	"context"
	"sync"
)

// Tee2 splits one stream into two identical copies.
// Each consumer must drain its channel or the producer will block.
func Tee2[A any](ctx context.Context, in <-chan A) (<-chan A, <-chan A) {
	out1 := make(chan A)
	out2 := make(chan A)
	go func() {
		defer close(out1)
		defer close(out2)
		for {
			select {
			case <-ctx.Done():
				return
			case a, ok := <-in:
				if !ok {
					return
				}
				// Send to both. If one consumer is slow, the other waits.
				// For independent processing speeds, use TeeBuffered.
				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					defer wg.Done()
					select {
					case <-ctx.Done():
					case out1 <- a:
					}
				}()
				go func() {
					defer wg.Done()
					select {
					case <-ctx.Done():
					case out2 <- a:
					}
				}()
				wg.Wait()
			}
		}
	}()
	return out1, out2
}

// Tee3 splits one stream into three identical copies.
func Tee3[A any](ctx context.Context, in <-chan A) (<-chan A, <-chan A, <-chan A) {
	out1 := make(chan A)
	out2 := make(chan A)
	out3 := make(chan A)
	go func() {
		defer close(out1)
		defer close(out2)
		defer close(out3)
		for {
			select {
			case <-ctx.Done():
				return
			case a, ok := <-in:
				if !ok {
					return
				}
				var wg sync.WaitGroup
				wg.Add(3)
				send := func(c chan<- A) {
					defer wg.Done()
					select {
					case <-ctx.Done():
					case c <- a:
					}
				}
				go send(out1)
				go send(out2)
				go send(out3)
				wg.Wait()
			}
		}
	}()
	return out1, out2, out3
}

// Merge fans N streams of the same type into one. Closes when all inputs close.
func Merge[A any](ctx context.Context, ins ...<-chan A) <-chan A {
	out := make(chan A)
	var wg sync.WaitGroup
	wg.Add(len(ins))
	for _, in := range ins {
		go func(c <-chan A) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case a, ok := <-c:
					if !ok {
						return
					}
					select {
					case <-ctx.Done():
						return
					case out <- a:
					}
				}
			}
		}(in)
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

// Zip3Into reads one element from each of three channels (in lockstep), applies
// a fusion function, and emits the result. Closes when any input closes.
//
// This is the key combinator for "split, parallel-process, recombine":
//
//	a, b, c := Tee3(ctx, src)
//	xs := Map(transformX)(ctx, b)
//	ys := Map(transformY)(ctx, c)
//	out := Zip3Into(ctx, a, xs, ys, fuse)
func Zip3Into[A, B, C, D any](
	ctx context.Context,
	as <-chan A, bs <-chan B, cs <-chan C,
	fuse func(A, B, C) D,
) <-chan D {
	out := make(chan D)
	go func() {
		defer close(out)
		for {
			var a A
			var b B
			var c C
			var ok bool

			select {
			case <-ctx.Done():
				return
			case a, ok = <-as:
				if !ok {
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case b, ok = <-bs:
				if !ok {
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case c, ok = <-cs:
				if !ok {
					return
				}
			}

			d := fuse(a, b, c)
			select {
			case <-ctx.Done():
				return
			case out <- d:
			}
		}
	}()
	return out
}

// Pool runs n concurrent workers, each pulling from in and pushing to out.
// Order is NOT preserved. Use when work is independent and you want parallelism.
func Pool[A, B any](n int, fn func(context.Context, A) B) Agent[A, B] {
	if n < 1 {
		n = 1
	}
	return func(ctx context.Context, in <-chan A) <-chan B {
		out := make(chan B)
		var wg sync.WaitGroup
		wg.Add(n)
		for i := 0; i < n; i++ {
			go func() {
				defer wg.Done()
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
		}
		go func() {
			wg.Wait()
			close(out)
		}()
		return out
	}
}

// Tap consumes from in, calls observer for each value (non-blocking),
// and forwards the value to out unchanged. Used for tracing/metrics.
func Tap[A any](observer func(A)) Agent[A, A] {
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
					observer(a)
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
