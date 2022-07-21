package daemon

import (
	"context"
	"sync/atomic"
)

func empty[T any]() T {
	var t T
	return t
}

func fallback[T comparable](src T, defaultVal T) T {
	if src == empty[T]() {
		return defaultVal
	}
	return src
}

func ternary[T any](condition bool, ifOutput T, elseOutput T) T {
	if condition {
		return ifOutput
	}
	return elseOutput
}

func async[A any](f func() A) chan A {
	ch := make(chan A)
	go func() {
		ch <- f()
	}()
	return ch
}

func reverse[S ~[]E, E any](s S) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func mapValues[M ~map[K]V, K comparable, V any](m M) []V {
	var i, mLen = 0, len(m)
	var result = make([]V, mLen)
	if mLen > 0 {
		for _, v := range m {
			result[i] = v
			i++
		}
	}
	return result
}

type atomicBool struct{ flag uint32 }

func (b *atomicBool) Store(value bool) {
	atomic.StoreUint32(&b.flag, ternary[uint32](value, 1, 0))
}

func (b *atomicBool) Load() bool {
	return atomic.LoadUint32(&b.flag) != 0
}

func (b *atomicBool) CAS(to bool) bool {
	if to {
		return atomic.CompareAndSwapUint32(&b.flag, 0, 1)
	}
	return atomic.CompareAndSwapUint32(&b.flag, 1, 0)
}

type cancelableContext struct {
	context.Context
	cancel context.CancelFunc
}

func newCancelableContext() *cancelableContext {
	var cc = cancelableContext{}
	cc.Context, cc.cancel = context.WithCancel(context.Background())
	return &cc
}

func (c *cancelableContext) escape() { c.cancel() }

type cancelableContextBatches []*cancelableContext

func (c cancelableContextBatches) cancel() {
	for i := 0; i < len(c); i++ {
		if c[i] != nil {
			c[i].cancel()
		}
	}
}
