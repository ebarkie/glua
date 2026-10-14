package glua

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// runLimited loads src under rt and calls its function f.
func runLimited(t *testing.T, rt *Runtime, ctx context.Context, src string) (Value, error) {
	t.Helper()
	s, err := rt.Load(context.Background(), "hostile.lua", src)
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.Func0[Value]("f")
	if err != nil {
		t.Fatal(err)
	}
	return f(ctx)
}

func TestHostileScripts(t *testing.T) {
	ctx := context.Background()
	rt := MustNew(LibSafe|LibCoroutine|LibLoad, Limits{Steps: 200_000, AllocBytes: 1 << 20})
	for _, tc := range []struct {
		name string
		src  string
		want error
	}{
		{"loop", "function f() while true do end end", ErrSteps},
		{"pcall loop", "local function g() end function f() while true do pcall(g) end end", ErrSteps},
		{"pcall spin", "local function spin() while true do end end function f() while true do pcall(spin) end end", ErrSteps},
		{"xpcall handler", "function f() while true do xpcall(function() while true do end end, print) end end", ErrSteps},
		{"coroutine", "function f() local co = coroutine.wrap(function() while true do end end) while true do pcall(co) end end", ErrSteps},
		{"recursion in pcall", "local function r() return pcall(r) end function f() while true do r() end end", ErrSteps},
		{"array growth", "function f() local t = {} for i = 1, 1e9 do t[i] = i end end", ErrAlloc},
		{"hash growth", "function f() local t = {} for i = 1, 1e9 do t['k' .. i] = i end end", ErrAlloc},
		{"rawset growth", "function f() local t = {} for i = 1, 1e9 do rawset(t, i, i) end end", ErrAlloc},
		{"table.insert", "function f() local t = {} while true do table.insert(t, 1) end end", ErrAlloc},
		{"concat doubling", "function f() local s = 'x' while true do s = s .. s end end", ErrAlloc},
		{"string.rep", "function f() return string.rep('x', 1 << 22) end", ErrAlloc},
		{"rep then upper", "function f() local s = string.rep('x', 1 << 19) for i = 1, 100 do s:upper() end end", ErrAlloc},
		{"closures", "function f() local t = {} for i = 1, 1e9 do t[i] = function() return i end end end", ErrAlloc},
		{"coroutines", "function f() local t = {} for i = 1, 1e9 do t[i] = coroutine.create(print) end end", ErrAlloc},
		{"gsub", "function f() local s = string.rep('x', 1 << 16) while true do s = s:gsub('x', 'xx') end end", ErrAlloc},
		{"format", "function f() local s = 'x' while true do s = string.format('%s%s', s, s) end end", ErrAlloc},
		{"table.concat", "function f() local t = {} for i = 1, 100 do t[i] = string.rep('y', 1 << 14) end for i = 1, 100 do table.concat(t) end end", ErrAlloc},
		{"load", "function f() local s = string.rep(' ', 1 << 18) for i = 1, 10 do load(s) end end", ErrAlloc},
		{"pcall swallows nothing", "function f() local ok = pcall(string.rep, 'x', 1 << 22) return ok end", ErrAlloc},
		{"error handler", "function f() return xpcall(error, function() while true do end end) end", ErrSteps},
	} {
		_, err := runLimited(t, rt, ctx, tc.src)
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
		var le *Error
		if errors.As(err, &le) && !strings.HasPrefix(le.Error(), "hostile.lua:") {
			t.Errorf("%s: no position: %v", tc.name, le)
		}
	}
}

func TestStackOverflowCatchable(t *testing.T) {
	rt := MustNew(Limits{CallDepth: 1000})
	v, err := runLimited(t, rt, context.Background(), `
		local function r(n) return 1 + r(n + 1) end
		function f() local ok, e = pcall(r, 1) return tostring(ok) .. " " .. e end`)
	if err != nil || !strings.Contains(v.String(), "false hostile.lua:2: stack overflow") {
		t.Errorf("got %v, %v", v, err)
	}
}

func TestCancellation(t *testing.T) {
	rt := MustNew(Limits{Steps: NoLimit})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := runLimited(t, rt, ctx, "local function g() end function f() while true do pcall(g) end end")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("deadline: %v", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v to stop", d)
	}
	var le *Error
	if !errors.As(err, &le) || !strings.HasPrefix(le.Error(), "hostile.lua:1:") {
		t.Errorf("no position: %v", err)
	}
	cctx, cancel2 := context.WithCancelCause(context.Background())
	cause := errors.New("tenant removed")
	cancel2(cause)
	if _, err := runLimited(t, rt, cctx, "function f() end"); !errors.Is(err, cause) {
		t.Errorf("cause: %v", err)
	}
}

func TestExactSteps(t *testing.T) {
	// a loop of n iterations takes n-1 back-edges; the limit is exact
	for _, n := range []int64{1, 100, 1023, 1024, 1025, 5000} {
		rt := MustNew(Limits{Steps: n})
		s := mustLoad(t, rt, "function f(n) for i = 1, n do end end")
		f, _ := s.Func1[int64, struct{}]("f")
		if _, err := f(context.Background(), n+1); err != nil {
			t.Errorf("%d steps: n+1 iterations failed: %v", n, err)
		}
		if _, err := f(context.Background(), n+2); !errors.Is(err, ErrSteps) {
			t.Errorf("%d steps: n+2 iterations: %v", n, err)
		}
	}
}

func TestNestedScriptsShareBudget(t *testing.T) {
	var inner *Script
	callInner := GoFunc(func(c *Call) error {
		v, err := inner.Call(c.Context(), inner.Globals().Get(String("spin")), c.Value(0))
		if err != nil {
			return err
		}
		return c.ReturnValues(v)
	})
	rt := MustNew(Limits{Steps: 10_000}, Global("call_inner", callInner))
	inner = mustLoad(t, rt, "function spin(n) for i = 1, n do end return n end")
	outer := mustLoad(t, rt, "function f(n) local s = 0 for i = 1, 4 do s = s + call_inner(n) end return s end")
	f, _ := outer.Func1[int64, int64]("f")
	// four inner loops of 2,000 fit in 10,000 steps together
	if n, err := f(context.Background(), 2000); err != nil || n != 8000 {
		t.Errorf("2000: %v %v", n, err)
	}
	// four of 3,000 do not, though each would on its own
	if _, err := f(context.Background(), 3000); !errors.Is(err, ErrSteps) {
		t.Errorf("3000: %v", err)
	}
	// a fresh context gives the inner script a budget of its own
	fresh := GoFunc(func(c *Call) error {
		v, err := inner.Call(context.Background(), inner.Globals().Get(String("spin")), c.Value(0))
		if err != nil {
			return err
		}
		return c.ReturnValues(v)
	})
	rt2 := MustNew(Limits{Steps: 10_000}, Global("call_inner", fresh))
	inner = mustLoad(t, rt2, "function spin(n) for i = 1, n do end return n end")
	outer2 := mustLoad(t, rt2, "function f(n) local s = 0 for i = 1, 4 do s = s + call_inner(n) end return s end")
	f2, _ := outer2.Func1[int64, int64]("f")
	if _, err := f2(context.Background(), 3000); err != nil {
		t.Errorf("fresh context: %v", err)
	}
}

func TestGraceBudgetForClose(t *testing.T) {
	var closed []string
	rt := MustNew(Limits{Steps: 2000}, Global("note", Do1(func(s string) { closed = append(closed, s) })))
	_, err := runLimited(t, rt, context.Background(), `
		function f()
			local a <close> = setmetatable({}, {__close = function() note("a") end})
			local b <close> = setmetatable({}, {__close = function() while true do end end})
			local c <close> = setmetatable({}, {__close = function() note("c") end})
			while true do end
		end`)
	if !errors.Is(err, ErrSteps) {
		t.Errorf("got %v", err)
	}
	// c runs; b breaches the grace budget, which abandons the rest
	if strings.Join(closed, ",") != "c" {
		t.Errorf("closed %v", closed)
	}
}

func TestHostResultsCharged(t *testing.T) {
	big := Global("big", Fn0(func() string { return strings.Repeat("x", 1<<16) }))
	rt := MustNew(Limits{AllocBytes: 1 << 20}, big)
	if _, err := runLimited(t, rt, context.Background(), "function f() for i = 1, 100 do big() end end"); !errors.Is(err, ErrAlloc) {
		t.Errorf("got %v", err)
	}
}
