package glua

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Boundary benchmarks through the public API: one op is one call from Go.

const apiSrc = `
function add1(x) return x + 1 end
function id(s) return s end
function rule(r) return r.method == "GET" and r.size < 100 and r.admin end
function host(x) local y = inc(x) return y end
function hostraw(x) local y = incraw(x) return y end
`

const apiRule = `method == "GET" and size < 100 and admin`

// apiFast selects the fast loop for the benchmarks, with GLUA_BENCH_FAST=1.
var apiFast = os.Getenv("GLUA_BENCH_FAST") == "1"

func apiRuntime(opts ...Option) *Runtime {
	if apiFast {
		opts = append(opts, withLoop(false))
	}
	return MustNew(opts...)
}

func apiScript(b *testing.B) *Script {
	rt := apiRuntime(
		Global("inc", Fn1(func(x int64) int64 { return x + 1 })),
		Global("incraw", GoFunc(func(c *Call) error {
			x, _ := c.Value(0).Int()
			return c.ReturnValues(Int(x + 1))
		})),
	)
	s, err := rt.Load(context.Background(), "api.lua", apiSrc)
	if err != nil {
		b.Fatal(err)
	}
	return s
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func BenchmarkAPICallPrivate(b *testing.B) {
	vm := must(apiScript(b).NewVM(context.Background()))
	f := must(vm.Func1[int64, int64]("add1"))
	ctx := context.Background()
	for i := int64(0); b.Loop(); i++ {
		if _, err := f(ctx, i); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAPICallPooled(b *testing.B) {
	f := must(apiScript(b).Func1[int64, int64]("add1"))
	ctx := context.Background()
	for i := int64(0); b.Loop(); i++ {
		if _, err := f(ctx, i); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAPIStringRoundTrip(b *testing.B) {
	f := must(apiScript(b).Func1[string, string]("id"))
	ctx := context.Background()
	s := strings.Repeat("x", 46)
	for b.Loop() {
		if _, err := f(ctx, s); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAPIRuleReusedPointer(b *testing.B) {
	f := must(apiScript(b).Func1[*request, bool]("rule"))
	ctx := context.Background()
	req := &request{Method: "GET", Size: 10, Admin: true}
	for b.Loop() {
		if ok, err := f(ctx, req); !ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}

func BenchmarkAPIRuleOtherPointer(b *testing.B) {
	f := must(apiScript(b).Func1[*request, bool]("rule"))
	ctx := context.Background()
	req := &request{Method: strings.Clone("GET"), Size: 10, Admin: true}
	for b.Loop() {
		if ok, err := f(ctx, req); !ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}

func BenchmarkAPIRuleEvalPerCall(b *testing.B) {
	rule := must(apiRuntime().Expr[request, bool]("rule", apiRule))
	ctx := context.Background()
	for b.Loop() {
		req := request{Method: method, Size: 10, Admin: true}
		if ok, err := rule.Eval(ctx, &req); !ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}

func BenchmarkAPIFuncByValuePerCall(b *testing.B) {
	f := must(apiScript(b).Func1[request, bool]("rule"))
	ctx := context.Background()
	for b.Loop() {
		if ok, err := f(ctx, request{Method: method, Size: 10, Admin: true}); !ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}

func BenchmarkAPIFuncPointerPerCall(b *testing.B) {
	f := must(apiScript(b).Func1[*request, bool]("rule"))
	ctx := context.Background()
	for b.Loop() {
		if ok, err := f(ctx, &request{Method: method, Size: 10, Admin: true}); !ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}

func BenchmarkAPIHostCall(b *testing.B) {
	f := must(apiScript(b).Func1[int64, int64]("host"))
	ctx := context.Background()
	for i := int64(0); b.Loop(); i++ {
		if _, err := f(ctx, i); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAPIHostCallRaw(b *testing.B) {
	f := must(apiScript(b).Func1[int64, int64]("hostraw"))
	ctx := context.Background()
	for i := int64(0); b.Loop(); i++ {
		if _, err := f(ctx, i); err != nil {
			b.Fatal(err)
		}
	}
}
