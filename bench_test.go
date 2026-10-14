package glua

import (
	"context"
	"math"
	"runtime"
	"strings"
	"testing"
)

// VM benchmarks: one op is one run of the whole program. `go run ./bench/compare` times
// the same programs in C Lua, in interleaved rounds.

func benchProgram(b *testing.B, p *Proto) {
	ex := newExecutor()
	ex.maxSteps = math.MaxInt64
	cl := closureOf(p)
	ctx := context.Background()
	for b.Loop() {
		if err := ex.call(ctx, cl, 0, 0); err != nil {
			b.Fatal(err)
		}
		ex.reset()
	}
}

func BenchmarkFloatLoop(b *testing.B) { benchProgram(b, progFloatLoop(1_000_000)) }
func BenchmarkFib27(b *testing.B)     { benchProgram(b, progFib(27)) }
func BenchmarkFieldRead(b *testing.B) { benchProgram(b, progFieldRead(1_000_000)) }

// Boundary benchmarks: one op is one call from Go.

// privateFunc1 is a typed function on an executor of its own, as a private VM has.
func privateFunc1[A, R any](ex *executor, cl *Closure) func(context.Context, A) (R, error) {
	enc, _ := encoderFor[A]()
	dec := decoderFor[R]()
	return func(ctx context.Context, a A) (r R, err error) {
		ex.stack[1] = enc(a)
		if err = ex.call(ctx, cl, 0, 1); err == nil {
			r, err = dec(ex.stack[0])
		}
		ex.reset()
		return r, err
	}
}

func BenchmarkCallPrivate(b *testing.B) {
	f := privateFunc1[int64, int64](newExecutor(), closureOf(progAdd1()))
	ctx := context.Background()
	for i := int64(0); b.Loop(); i++ {
		if _, err := f(ctx, i); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCallPooled(b *testing.B) {
	f := bindFunc1[int64, int64](newEngine(), closureOf(progAdd1()))
	ctx := context.Background()
	for i := int64(0); b.Loop(); i++ {
		if _, err := f(ctx, i); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkStringRoundTrip(b *testing.B) {
	f := privateFunc1[string, string](newExecutor(), closureOf(progIdentity()))
	ctx := context.Background()
	s := strings.Repeat("x", 46)
	for b.Loop() {
		if _, err := f(ctx, s); err != nil {
			b.Fatal(err)
		}
	}
}

// The rule reads three fields of a request that lives on the heap and is reused.
func BenchmarkRuleReusedPointer(b *testing.B) {
	f := bindFunc1[*request, bool](newEngine(), closureOf(progRule()))
	ctx := context.Background()
	req := &request{Method: "GET", Size: 10, Admin: true}
	for b.Loop() {
		if ok, err := f(ctx, req); !ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}

// Same rule, but req.method does not share a pointer with the constant, as a string
// parsed off the wire would not.
func BenchmarkRuleOtherPointer(b *testing.B) {
	f := bindFunc1[*request, bool](newEngine(), closureOf(progRule()))
	ctx := context.Background()
	req := &request{Method: strings.Clone("GET"), Size: 10, Admin: true}
	for b.Loop() {
		if ok, err := f(ctx, req); !ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}

var method = strings.Clone("GET")

func BenchmarkRuleEvalPerCall(b *testing.B) {
	rule := newRule[request, bool](newEngine(), closureOf(progRule()))
	ctx := context.Background()
	for b.Loop() {
		req := request{Method: method, Size: 10, Admin: true}
		if ok, err := rule.Eval(ctx, &req); !ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}

func BenchmarkFuncByValuePerCall(b *testing.B) {
	f := bindFunc1[request, bool](newEngine(), closureOf(progRule()))
	ctx := context.Background()
	for b.Loop() {
		if ok, err := f(ctx, request{Method: method, Size: 10, Admin: true}); !ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}

func BenchmarkFuncPointerPerCall(b *testing.B) {
	f := bindFunc1[*request, bool](newEngine(), closureOf(progRule()))
	ctx := context.Background()
	for b.Loop() {
		if ok, err := f(ctx, &request{Method: method, Size: 10, Admin: true}); !ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}

func BenchmarkHostCall(b *testing.B) {
	host := &goFunc{f: func(c *Call) error {
		x, _ := c.Value(0).Int()
		c.return1(Int(x + 1))
		return nil
	}}
	f := privateFunc1[int64, int64](newExecutor(), closureOf(progCallHost(), goFnValue(host)))
	ctx := context.Background()
	for i := int64(0); b.Loop(); i++ {
		if _, err := f(ctx, i); err != nil {
			b.Fatal(err)
		}
	}
}

// GC-marking variants: a background goroutine keeps a heap of 3 million nodes and runs
// collections back to back, so the write barrier is on for nearly the whole run.

type gcNode struct {
	next *gcNode
	pad  [2]*gcNode
}

func gcPressure() (stop func()) {
	var head *gcNode
	for range 3_000_000 {
		head = &gcNode{next: head}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			select {
			case <-done:
				runtime.KeepAlive(head)
				return
			default:
				runtime.GC()
			}
		}
	}()
	return func() { close(done); <-finished }
}

func benchProgramGC(b *testing.B, p *Proto) {
	stop := gcPressure()
	defer stop()
	benchProgram(b, p)
}

func BenchmarkGCFloatLoop(b *testing.B) { benchProgramGC(b, progFloatLoop(1_000_000)) }
func BenchmarkGCFib27(b *testing.B)     { benchProgramGC(b, progFib(27)) }
func BenchmarkGCFieldRead(b *testing.B) { benchProgramGC(b, progFieldRead(1_000_000)) }
