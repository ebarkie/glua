package glua

import (
	"context"
	"sync"
	"sync/atomic"
)

// defaultRuntime is the Runtime of Eval: LibSafe and the default limits.
var defaultRuntime = sync.OnceValue(func() *Runtime { return MustNew() })

// Eval loads src on a default Runtime (LibSafe, default limits) and decodes the chunk's first
// result, or, for a Pair, its first two.
func Eval[R any](ctx context.Context, src string) (R, error) {
	var r R
	dec, err := resultDecoderFor[R]()
	if err != nil {
		return r, err
	}
	s, err := defaultRuntime().Load(ctx, "eval", src)
	if err != nil {
		return r, err
	}
	return dec.decode(s.result)
}

// used is set by the first Runtime: from then on the hash seed is fixed.
var used atomic.Bool

// SetHashSeed fixes the process-wide hash seed, and with it the order of pairs and the
// values tostring shows in place of addresses, for tests and replay. It must be called
// before any other use of the package; afterwards it panics.
func SetHashSeed(seed uint64) {
	if used.Load() {
		panic("glua: SetHashSeed after the first Runtime was made")
	}
	s := seed
	hashSeed = splitmix(&s)
	hashMul = splitmix(&s) | 1
	addrKey = splitmix(&s) | 1
	proxyMeta.rehash(0) // the one table made before any Runtime
}

// splitmix is SplitMix64: it derives well-mixed words from one seed.
func splitmix(s *uint64) uint64 {
	*s += 0x9e3779b97f4a7c15
	z := *s
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	return z ^ z>>31
}
