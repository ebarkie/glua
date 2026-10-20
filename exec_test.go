package glua

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestFloatLoop(t *testing.T) {
	res, err := runProto(t, progFloatLoop(1000))
	if err != nil {
		t.Fatal(err)
	}
	want := 0.0
	for i := 1; i <= 1000; i++ {
		want += float64(i) * 1.5
	}
	if f, ok := res[0].Float(); !ok || res[0].Kind() != KindFloat || f != want {
		t.Fatalf("got %v, want %v", res[0], want)
	}
}

func TestFib(t *testing.T) {
	res, err := runProto(t, progFib(20))
	if err != nil {
		t.Fatal(err)
	}
	if i, _ := res[0].Int(); i != 6765 {
		t.Fatalf("fib(20) = %v", res[0])
	}
}

func TestFieldRead(t *testing.T) {
	res, err := runProto(t, progFieldRead(5000))
	if err != nil {
		t.Fatal(err)
	}
	if i, _ := res[0].Int(); i != 5000 {
		t.Fatalf("got %v", res[0])
	}
}

// The expected counts and last values come from C Lua 5.4.8.
func TestForLoopCorners(t *testing.T) {
	nan := Float(math.NaN())
	tests := []struct {
		init, limit, step Value
		n                 int64
		last              Value
	}{
		{Int(1), Int(10), Int(1), 10, Int(10)},
		{Int(1), Int(10), Int(3), 4, Int(10)},
		{Int(10), Int(1), Int(-1), 10, Int(1)},
		{Int(10), Int(1), Int(-3), 4, Int(1)},
		{Int(1), Int(0), Int(1), 0, Value{}},
		{Int(0), Int(1), Int(-1), 0, Value{}},
		{Int(9223372036854775806), Int(9223372036854775807), Int(1), 2, Int(9223372036854775807)},
		{Int(-9223372036854775808), Int(-9223372036854775806), Int(1), 3, Int(-9223372036854775806)},
		{Int(-9223372036854775806), Int(-9223372036854775808), Int(-1), 3, Int(-9223372036854775808)},
		{Int(9223372036854775807), Int(9223372036854775807), Int(-9223372036854775808), 1, Int(9223372036854775807)},
		{Int(0), Int(-9223372036854775808), Int(-9223372036854775808), 2, Int(-9223372036854775808)},
		{Int(-9223372036854775808), Int(9223372036854775807), Int(9223372036854775807), 3, Int(9223372036854775806)},
		{Int(9223372036854775807), Int(-9223372036854775808), Int(-9223372036854775808), 2, Int(-1)},
		{Int(1), Float(3.7), Int(1), 3, Int(3)},
		{Int(1), Float(-3.7), Int(-1), 5, Int(-3)},
		{Int(1), Float(1e+100), Int(9223372036854775807), 1, Int(1)},
		{Float(1), Int(3), Float(0.5), 5, Float(3)},
		{Int(3), Float(1), Float(-0.75), 3, Float(1.5)},
		{Float(0.1), Float(0.35), Float(0.1), 3, Float(0.30000000000000004)},
		{Int(1), nan, Int(1), 0, Value{}},
		{Float(1), nan, Int(1), 1, Float(1)},
	}
	for _, tt := range tests {
		res, err := runProto(t, progForCount(tt.init, tt.limit, tt.step))
		if err != nil {
			t.Errorf("for %v, %v, %v: %v", tt.init, tt.limit, tt.step, err)
			continue
		}
		if n, _ := res[0].Int(); n != tt.n || !same(&res[1], &tt.last) {
			t.Errorf("for %v, %v, %v: %v iterations, last %v (%v); want %d, %v (%v)",
				tt.init, tt.limit, tt.step, res[0], res[1], res[1].Kind(), tt.n, tt.last, tt.last.Kind())
		}
	}
	if _, err := runProto(t, progForCount(Int(1), Int(2), Int(0))); err == nil || !strings.Contains(err.Error(), "'for' step is zero") {
		t.Errorf("zero step: %v", err)
	}
}

// The step limit is exact: a loop with n back-edges runs under Steps = n and fails under n-1.
func TestStepLimitExact(t *testing.T) {
	for _, n := range []int64{1, 100, 1023, 1024, 1025, 5000} {
		p := progEmptyLoop(n + 1) // n+1 iterations take n back-edges
		for _, limit := range []int64{n, n - 1} {
			ex := newExecutor()
			ex.maxSteps = limit
			_, err := runClosure(context.Background(), ex, closureOf(p))
			if ok := limit >= n; ok != (err == nil) {
				t.Errorf("%d back-edges under Steps=%d: %v", n, limit, err)
			}
			if err != nil && !errors.Is(err, ErrSteps) {
				t.Errorf("want ErrSteps, got %v", err)
			}
		}
	}
}

// Nested entries share the budget of the outermost one.
func TestNestedEntriesShareBudget(t *testing.T) {
	inner := closureOf(progEmptyLoop(151)) // 150 back-edges
	host := &goFunc{f: func(c *Call) error {
		v, err := c.Invoke(luaFnValue(inner))
		if err != nil {
			return err
		}
		return c.ReturnValues(v)
	}}
	outer := closureOf(progCallHost(), goFnValue(host))
	for _, tt := range []struct {
		limit int64
		ok    bool
	}{{150, true}, {149, false}} {
		ex := newExecutor()
		ex.maxSteps = tt.limit
		_, err := runClosure(context.Background(), ex, outer, Int(0))
		if (err == nil) != tt.ok || (err != nil && !errors.Is(err, ErrSteps)) {
			t.Errorf("Steps=%d: %v", tt.limit, err)
		}
	}
}

func TestCancelledContextNeverRuns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ran := false
	host := &goFunc{f: func(c *Call) error { ran = true; return nil }}
	_, err := runClosure(ctx, newExecutor(), closureOf(progCallHost(), goFnValue(host)), Int(1))
	if !errors.Is(err, context.Canceled) || ran {
		t.Fatalf("err %v, ran %v", err, ran)
	}
}

func TestCancelDuringLoop(t *testing.T) {
	ctx, cancel := context.WithCancelCause(context.Background())
	stop := errors.New("stop")
	ticks := 0
	host := &goFunc{f: func(c *Call) error {
		ticks++
		cancel(stop)
		return nil
	}}
	// call the host function, which cancels, then spin: the next tick expiry must notice
	a := newAsm("cancel", 0, 6)
	a.upval(false, 0)
	a.abc(opGetUpval, 0, 0, 0)
	a.abc(opCall, 0, 1, 1)
	a.loadI(1, 1)
	a.abx(opLoadK, 2, a.konst(Int(1<<40)))
	a.loadI(3, 1)
	a.forPrep(1, "exit")
	a.label("body")
	a.forLoop(1, "body")
	a.label("exit")
	a.abc(opReturn0, 0, 0, 0)
	ex := newExecutor()
	ex.maxSteps = math.MaxInt64
	_, err := runClosure(ctx, ex, closureOf(a.done(), goFnValue(host)))
	if !errors.Is(err, stop) || ticks != 1 {
		t.Fatalf("err %v", err)
	}
}

// A host function calls back into Lua, which calls another host function. The outer one
// must still see its own arguments afterwards.
func TestNestedHostCalls(t *testing.T) {
	innerHost := &goFunc{f: func(c *Call) error {
		x, _ := c.Value(0).Int()
		return c.ReturnValues(Int(x * 10))
	}}
	lua := closureOf(progCallHost(), goFnValue(innerHost))
	outerHost := &goFunc{f: func(c *Call) error {
		before := c.Value(0)
		v, err := c.Invoke(luaFnValue(lua), Int(7))
		if err != nil {
			return err
		}
		if !c.Value(0).Equal(before) {
			return errors.New("outer arguments were overwritten")
		}
		x, _ := before.Int()
		y, _ := v.Int()
		return c.ReturnValues(Int(x + y))
	}}
	res, err := runClosure(context.Background(), newExecutor(), closureOf(progCallHost(), goFnValue(outerHost)), Int(5))
	if err != nil {
		t.Fatal(err)
	}
	if i, _ := res[0].Int(); i != 75 {
		t.Fatalf("got %v, want 75", res[0])
	}
}

func TestHostErrorKeepsGoError(t *testing.T) {
	sentinel := errors.New("not found")
	host := &goFunc{f: func(c *Call) error { return sentinel }}
	_, err := runClosure(context.Background(), newExecutor(), closureOf(progCallHost(), goFnValue(host)), Int(1))
	var le *Error
	if !errors.Is(err, sentinel) || !errors.As(err, &le) || le.Value.String() != "not found" {
		t.Fatalf("got %v", err)
	}
}

// Errorf and ArgError write their messages as luaL_error and luaL_argerror do: the position
// of the Lua code that called the host function, none for a call from Go, before the whole
// message when the error is wrapped, and the function as the call names it. The Go error
// stays behind the message, also when error(e, 0) raises it again, and the Go side shows the
// position once, Terminate's included.
func TestHostErrorsWithPosition(t *testing.T) {
	missing := errors.New("not found")
	lookup := func(c *Call) error { return c.Errorf("user %d: %w", c.Arg[int](0), missing) }
	rt := MustNew(
		Global("lookup", GoFunc(lookup)),
		Global("wrapped", GoFunc(func(c *Call) error { return fmt.Errorf("lookup: %w", lookup(c)) })),
		Global("deny", GoFunc(func(c *Call) error { return Terminate(lookup(c)) })),
		Global("check", GoFunc(func(c *Call) error { return c.ArgError(c.Len()-1, "positive expected") })),
		Global("invoke", GoFunc(func(c *Call) error {
			if _, err := c.Invoke(c.Value(0)); err != nil {
				return c.Errorf("in callback: %w", err)
			}
			return nil
		})))
	s, err := rt.Load(context.Background(), "t.lua", `
		local obj = { check = check }
		function f()
			return select(2, pcall(function() lookup(7) end)), select(2, pcall(lookup, 7)),
				select(2, pcall(function() wrapped(7) end)),
				select(2, pcall(function() check(1) end)), select(2, pcall(function() obj:check(1) end)),
				select(2, pcall(check, 1)), select(2, pcall(function() obj:check() end)),
				select(2, pcall(function() invoke(function() error("inner") end) end))
		end
		function g() local _, e = pcall(function() lookup(7) end) error(e, 0) end
		function h() lookup(7) end
		function d() pcall(deny, 7) end`)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.CallN(context.Background(), nil, s.Globals().Get(String("f")))
	want := []string{"t.lua:4: user 7: not found", "user 7: not found", "t.lua:5: lookup: user 7: not found",
		"t.lua:6: bad argument #1 to 'check' (positive expected)", "t.lua:6: bad argument #1 to 'check' (positive expected)",
		"bad argument #1 to 'check' (positive expected)", "t.lua:7: calling 'check' on bad self (positive expected)",
		"t.lua:8: in callback: t.lua:8: inner"}
	if err != nil || len(res) != len(want) {
		t.Fatalf("got %v, %v", res, err)
	}
	for i, w := range want {
		if res[i].String() != w {
			t.Errorf("message %d = %q, want %q", i+1, res[i], w)
		}
	}
	for name, want := range map[string]string{"g": "t.lua:10: user 7: not found", "h": "t.lua:11: user 7: not found",
		"d": "t.lua:12: user 7: not found"} {
		if _, err := s.Call(context.Background(), s.Globals().Get(String(name))); !errors.Is(err, missing) || err.Error() != want {
			t.Errorf("%s: got %v, want %q", name, err, want)
		}
	}
	// a terminal error stays terminal, wrapped
	limited := MustNew(Limits{Steps: 10_000}, Global("invoke", GoFunc(func(c *Call) error {
		_, err := c.Invoke(c.Value(0))
		return c.Errorf("in callback: %w", err)
	})))
	ls, err := limited.Load(context.Background(), "l.lua", "function f() pcall(invoke, function() while true do end end) end")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ls.Call(context.Background(), ls.Globals().Get(String("f"))); !errors.Is(err, ErrSteps) {
		t.Errorf("a wrapped step limit: %v", err)
	}
}

func TestHostPanicPoisonsExecutor(t *testing.T) {
	host := &goFunc{f: func(c *Call) error { panic("boom") }}
	ex := newExecutor()
	func() {
		defer func() {
			if r := recover(); r != "boom" {
				t.Fatalf("recovered %v", r)
			}
		}()
		runClosure(context.Background(), ex, closureOf(progCallHost(), goFnValue(host)), Int(1))
	}()
	if !ex.poisoned || ex.running {
		t.Fatal("executor not poisoned after a panic")
	}
	var pool sync.Pool
	release(&pool, ex)
	if pool.Get() != nil {
		t.Fatal("a poisoned executor was pooled")
	}
}

func TestConcurrentEntryPanics(t *testing.T) {
	ex := newExecutor()
	host := &goFunc{f: func(c *Call) error {
		defer func() {
			if r := recover(); r == nil || !strings.Contains(r.(string), "concurrent use") {
				t.Errorf("recovered %v", r)
			}
		}()
		ex.call(context.Background(), closureOf(progAdd1()), 10, 0)
		return nil
	}}
	runClosure(context.Background(), ex, closureOf(progCallHost(), goFnValue(host)), Int(1))
}

// An error three frames deep leaves pointers in registers. Reset must clear all of them.
func TestResetAfterDeepError(t *testing.T) {
	// f3 calls nil; f2 calls f3; f1 calls f2
	f3 := newAsm("f3", 1, 3)
	f3.abc(opLoadNil, 1, 0, 0)
	f3.abc(opCall, 1, 1, 1)
	f3.abc(opReturn0, 0, 0, 0)
	p3 := f3.done()
	mk := func(name string, next *proto) *proto {
		a := newAsm(name, 1, 3)
		a.abx(opClosure, 1, a.proto(next))
		a.abc(opMove, 2, 0, 0)
		a.abc(opCall, 1, 2, 1)
		a.abc(opReturn0, 0, 0, 0)
		return a.done()
	}
	p1 := mk("f1", mk("f2", p3))
	ex := newExecutor()
	big := String(strings.Repeat("x", 100))
	copy(ex.stack[1:], []Value{big})
	err := ex.call(context.Background(), closureOf(p1), 0, 1)
	if err == nil || !strings.Contains(err.Error(), "attempt to call a nil value") {
		t.Fatalf("got %v", err)
	}
	if ex.st.hi < 5 {
		t.Fatalf("high-water mark %d does not cover the frames", ex.st.hi)
	}
	ex.reset()
	for i, v := range ex.stack {
		if v.p != nil {
			t.Fatalf("register %d still holds %v after reset", i, v)
		}
	}
}

type request struct {
	Method string
	Size   int64
	Admin  bool
	Path   string
}

func TestRule(t *testing.T) {
	e := newEngine()
	rule := newRule[request, bool](e, closureOf(progRule()))
	ctx := context.Background()
	for _, tt := range []struct {
		r    request
		want bool
	}{
		{request{Method: "GET", Size: 10, Admin: true}, true},
		{request{Method: "GET", Size: 10, Admin: false}, false},
		{request{Method: "POST", Size: 10, Admin: true}, false},
		{request{Method: "GET", Size: 5000, Admin: true}, false},
		{request{Method: strings.Clone("GET"), Size: 1, Admin: true}, true}, // not the constant's pointer
	} {
		got, err := rule.Eval(ctx, &tt.r)
		if err != nil || got != tt.want {
			t.Errorf("%+v: got %v, %v", tt.r, got, err)
		}
	}
}

// Eval allocates nothing when the struct is built per call, and the slot is empty afterwards.
func TestRuleEvalNoAlloc(t *testing.T) {
	if raceEnabled {
		t.Skip("sync.Pool drops items in race builds")
	}
	e := newEngine()
	rule := newRule[request, bool](e, closureOf(progRule()))
	ctx := context.Background()
	method := strings.Clone("GET")
	allocs := testing.AllocsPerRun(1000, func() {
		r := request{Method: method, Size: 10, Admin: true}
		if ok, err := rule.Eval(ctx, &r); !ok || err != nil {
			t.Fatal(ok, err)
		}
	})
	if allocs != 0 && !stackStress { // lua_stackstress allocates a stack at every check
		t.Fatalf("Eval allocates %v times per call", allocs)
	}
	ex := e.pool.Get().(*executor)
	if slot := (*request)(ex.slots[rule.id*maxSlotArgs]); *slot != (request{}) {
		t.Fatalf("slot still holds %+v", *slot)
	}
}

func TestTypedFuncs(t *testing.T) {
	e := newEngine()
	ctx := context.Background()
	add1 := bindFunc1[int64, int64](e, closureOf(progAdd1()))
	if r, err := add1(ctx, 41); r != 42 || err != nil {
		t.Fatal(r, err)
	}
	id := bindFunc1[string, string](e, closureOf(progIdentity()))
	s := strings.Repeat("s", 46)
	if r, err := id(ctx, s); r != s || err != nil {
		t.Fatal(r, err)
	}
	byPtr := bindFunc1[*request, bool](e, closureOf(progRule()))
	req := &request{Method: "GET", Size: 1, Admin: true}
	if r, err := byPtr(ctx, req); !r || err != nil {
		t.Fatal(r, err)
	}
	if r, err := byPtr(ctx, nil); err == nil || !strings.Contains(err.Error(), "attempt to index a nil value") {
		t.Fatal(r, err)
	}
	byVal := bindFunc1[request, bool](e, closureOf(progRule()))
	if r, err := byVal(ctx, *req); !r || err != nil {
		t.Fatal(r, err)
	}
	if raceEnabled || stackStress {
		return // sync.Pool drops items in race builds; lua_stackstress allocates stacks
	}
	for name, f := range map[string]func(){
		"add1":     func() { add1(ctx, 1) },
		"identity": func() { id(ctx, s) },
		"pointer":  func() { byPtr(ctx, req) },
		"value":    func() { byVal(ctx, request{Method: "GET", Size: 1, Admin: true}) },
	} {
		if a := testing.AllocsPerRun(1000, f); a != 0 {
			t.Errorf("%s allocates %v times per call", name, a)
		}
	}
}

// An integer can carry the bits of a userdata or a table. GETFIELD must treat it like
// any other non-table.
func TestGetFieldRejectsForgedValues(t *testing.T) {
	id := bindType(reflect.TypeFor[request]())
	a := newAsm("forged", 1, 2)
	a.field(opGetField, 1, 0, int(a.kstr("method")))
	a.abc(opReturn1, 1, 0, 0)
	p := a.done()
	for _, v := range []Value{
		Int(int64(kindUser<<56 | id)),
		Int(int64(kindTable << 56)),
		Float(math.Float64frombits(kindUser<<56 | id)),
		{},
	} {
		p.hints[0] = 0 // a trained hint would make no difference, but start clean
		_, err := runProto(t, p, v)
		if err == nil || !strings.Contains(err.Error(), "attempt to index a") {
			t.Errorf("%v: got %v", v, err)
		}
	}
}

// One site, two table shapes: the hint gives up after four misses and the inline probe
// takes over. Results stay correct throughout, also from two goroutines at once.
func TestMegamorphicSite(t *testing.T) {
	a := newAsm("getx", 1, 2)
	a.field(opGetField, 1, 0, int(a.kstr("x")))
	a.abc(opReturn1, 1, 0, 0)
	p := a.done()
	// four shapes with x in four different slots, so that the site misses at every change
	shapes := make([]*Table, 0, 4)
	used := map[int]bool{}
	for n := 0; len(shapes) < 4; n++ {
		t := NewTable(0, 8)
		for j := range n % 7 {
			t.Set(String(strings.Repeat("k", j+1+n/7)), Int(int64(j)))
		}
		t.Set(String("x"), Int(int64(100+len(shapes))))
		if _, slot := t.getSlot(String("x")); !used[slot] {
			used[slot] = true
			shapes = append(shapes, t)
		}
	}
	var wg sync.WaitGroup
	for g := range 2 {
		wg.Go(func() {
			ex := newExecutor()
			for n := range 200 {
				i := (n + g) % len(shapes)
				res, err := runClosure(context.Background(), ex, closureOf(p), tableValue(shapes[i]))
				if err != nil {
					t.Error(err)
					return
				}
				if v, _ := res[0].Int(); v != int64(100+i) {
					t.Errorf("shape %d: got %v", i, res[0])
					return
				}
			}
		})
	}
	wg.Wait()
	if p.hints[0] != megamorphic {
		t.Errorf("hint is %x, want megamorphic", p.hints[0])
	}
}

func TestStringKeyBehindOtherPointer(t *testing.T) {
	tbl := NewTable(0, 4)
	tbl.Set(String(strings.Clone("x")), Int(9)) // not the canonical constant
	a := newAsm("getx", 1, 2)
	a.field(opGetField, 1, 0, int(a.kstr("x")))
	a.abc(opReturn1, 1, 0, 0)
	res, err := runProto(t, a.done(), tableValue(tbl))
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := res[0].Int(); v != 9 {
		t.Fatalf("got %v", res[0])
	}
}

func TestCallDepthLimit(t *testing.T) {
	// local function f() return f() + 1 end
	a := newAsm("rec", 0, 2)
	a.upval(true, 0)
	a.abc(opGetUpval, 0, 0, 0)
	a.abc(opCall, 0, 1, 2)
	a.abc(opAddK, 0, 0, int(a.kint(1)))
	a.abc(opReturn1, 0, 0, 0)
	m := newAsm("main", 0, 2)
	m.abx(opClosure, 0, m.proto(a.done()))
	m.abc(opMove, 1, 0, 0)
	m.abc(opCall, 1, 1, 2)
	m.abc(opReturn1, 1, 0, 0)
	_, err := runProto(t, m.done())
	if err == nil || !strings.Contains(err.Error(), "stack overflow") {
		t.Fatalf("got %v", err)
	}
}

func TestStackGrowth(t *testing.T) {
	// deep but legal recursion moves the stack many times
	res, err := runProto(t, progFib(18))
	if err != nil {
		t.Fatal(err)
	}
	if i, _ := res[0].Int(); i != 2584 {
		t.Fatal(res[0])
	}
	ex := newExecutor()
	ex.maxCallDepth = 100000
	// f(n) = n == 0 and 0 or f(n-1)
	a := newAsm("down", 1, 3)
	a.upval(true, 0)
	a.cjump(opLtKJmp, 0, int(a.kint(1)), "rec")
	a.abc(opReturn1, 0, 0, 0)
	a.label("rec")
	a.abc(opGetUpval, 1, 0, 0)
	a.abc(opSubK, 2, 0, int(a.kint(1)))
	a.abc(opCall, 1, 2, 2)
	a.abc(opReturn1, 1, 0, 0)
	m := newAsm("main", 0, 3)
	m.abx(opClosure, 0, m.proto(a.done()))
	m.abc(opMove, 1, 0, 0)
	m.abx(opLoadK, 2, m.konst(Int(5000)))
	m.abc(opCall, 1, 2, 2)
	m.abc(opReturn1, 1, 0, 0)
	res, err = runClosure(context.Background(), ex, closureOf(m.done()))
	if err != nil {
		t.Fatal(err)
	}
	if i, _ := res[0].Int(); i != 0 || len(ex.stack) <= initStack {
		t.Fatalf("got %v, stack %d", res[0], len(ex.stack))
	}
}

// A string that doubles on every iteration runs out of the entry's allocation budget long
// before the step limit, and fails with ErrAlloc instead of exhausting memory.
func TestSingleAllocationCap(t *testing.T) {
	_, err := runSource("t", `local s = "x" for i = 1, 100 do s = s .. s end return #s`)
	if !errors.Is(err, ErrAlloc) {
		t.Fatalf("got %v, want ErrAlloc", err)
	}
}

// After a terminal error the closers still run, on a grace budget: a closer that loops
// forever is stopped, and the terminal error stands.
func TestCloseAfterTerminalError(t *testing.T) {
	src := `
		done = false
		local a <close> = setmetatable({}, {__close = function() done = true end})
		local b <close> = setmetatable({}, {__close = function() while true do end end})
		while true do end`
	p, _, err := compileChunk("t", src)
	if err != nil {
		t.Fatal(err)
	}
	g := testGlobals()
	ex := newExecutor()
	ex.maxSteps = 10_000
	_, err = runClosure(context.Background(), ex, &closure{p: p, env: g})
	if !errors.Is(err, ErrSteps) {
		t.Fatalf("got %v, want ErrSteps", err)
	}
	// b's closer used up the grace budget, so a's closer is abandoned
	if g.Get(String("done")).Truthy() {
		t.Error("a's closer ran after b's breached the grace budget")
	}
	src2 := `
		done = false
		local a <close> = setmetatable({}, {__close = function() done = true end})
		while true do end`
	p, _, _ = compileChunk("t", src2)
	g = testGlobals()
	ex = newExecutor()
	ex.maxSteps = 10_000
	runClosure(context.Background(), ex, &closure{p: p, env: g})
	if !g.Get(String("done")).Truthy() {
		t.Error("a closer did not run after a terminal error")
	}
}

// A yield cannot cross a Go function: Lua code called back from Go gets C Lua's error.
func TestYieldAcrossGoCall(t *testing.T) {
	p, _, err := compileChunk("t", `
		local co = coroutine.create(function()
			return each(function() coroutine.yield(1) end)
		end)
		return coroutine.resume(co)`)
	if err != nil {
		t.Fatal(err)
	}
	g := testGlobals()
	g.Set(String("each"), goFnValue(&goFunc{name: "each", f: func(c *Call) error {
		_, err := c.Invoke(c.Value(0))
		return err
	}}))
	res, err := runClosure(context.Background(), newExecutor(), &closure{p: p, env: g})
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Truthy() || !strings.Contains(res[1].String(), "attempt to yield across a C-call boundary") {
		t.Fatalf("got %v %v", res[0], res[1])
	}
}

// A coroutine that returned leaves its stack to the next one the entry makes, and a new
// coroutine's stack is already big enough for its body's frame, so a loop of short
// coroutines allocates no stacks after the first.
func TestCoroutineStacksReused(t *testing.T) {
	if stackStress {
		t.Skip("lua_stackstress allocates a stack at every check")
	}
	s := mustLoad(t, MustNew(LibSafe|LibCoroutine), `local f = function() return 1 end
		function work(n) for i = 1, n do coroutine.wrap(f)() end end`)
	work, _ := s.Func1[int64, struct{}]("work")
	ctx := context.Background()
	if _, err := work(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, err := work(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if per := (after.TotalAlloc - before.TotalAlloc) / 1000; per > 2<<10 {
		t.Errorf("%d bytes allocated for each coroutine", per)
	}
}

// A kept stack holds nothing of the coroutine that left it, and reset lets it go.
func TestCoroutineStacksKeptClean(t *testing.T) {
	var kept int
	check := GoFunc(func(c *Call) error {
		for _, k := range c.ex.coStacks {
			if len(k.stack) != coStackSize || slices.ContainsFunc(k.stack, func(v Value) bool { return !v.IsNil() }) {
				return c.Errorf("a kept stack of %d slots holds values", len(k.stack))
			}
		}
		kept = len(c.ex.coStacks)
		return nil
	})
	rt := MustNew(LibSafe|LibCoroutine, Global("check", check))
	_, err := runInEnv(t, rt, `
		local function wide(...) local a, b, c, d, e, f = {}, "x", 1, {}, "y", 2 return select("#", ...) end
		local function deep(n) if n == 0 then return 0 end return 1 + deep(n - 1) end
		coroutine.wrap(deep)(2000)
		coroutine.wrap(wide)(1, 2, 3)
		check()`)
	if err != nil {
		t.Fatal(err)
	}
	if kept != 1 && !stackStress {
		t.Errorf("%d stacks kept, want the one that did not grow", kept)
	}
	ex := rt.newExecutor()
	ex.coStacks = append(ex.coStacks, coStack{make([]Value, coStackSize), nil})
	ex.reset()
	if len(ex.coStacks) != 0 || ex.coStacks[:1][0].stack != nil {
		t.Error("reset kept the stacks")
	}
}

// A Go function in tail position returns its results from the caller's frame at once, or,
// for pcall, pairs with __pairs and a yield, through the RETURN after the TAILCALL.
func TestTailCallGo(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"local function f() return select('#') end return f()", "0"},
		{"local function f(...) return select(2, ...) end return f(1, 2, 3)", "2\t3"},
		{"local function f() return string.byte('abc', 1, 3) end local a, b = f() return a, b", "97\t98"},
		{"local function f() return string.byte('abc', 1, 3) end local t = {f(), f()} return #t", "4"},
		{"local function f() return tostring(nil) end return (f()) .. '!'", "nil!"},
		{"local function f(x) return setmetatable(x, {__index = {k = 1}}) end return f({}).k", "1"},
		{"local function f() return pcall(error, 'e') end return f()", "false\te"},
		{"local function f() return pcall(function(...) return ... end, 1, 2) end return f()", "true\t1\t2"},
		{"local function f(t) return pairs(t) end local n = 0 for _, v in f({3, 4}) do n = n + v end return n", "7"},
		{"local function f(t) return pairs(t) end local p = setmetatable({}, {__pairs = function() return next, {7}, nil end})\n" +
			"local n = 0 for _, v in f(p) do n = n + v end return n", "7"},
		{"local function f(x) return coroutine.yield(x) end\n" +
			"local co = coroutine.wrap(function(x) local a, b = f(x) return a + b end)\n" +
			"local y = co(5) return y, co(y, 1)", "5\t6"},
		{"local function f() return error('boom') end local ok, e = pcall(f) return ok, e", "false\ttest:1: boom"}, // the position of f, as C Lua gives
		{"return math.max(3, 4)", "4"},
	} {
		for _, checked := range []bool{false, true} {
			res, err := runInEnv(t, MustNew(LibSafe|LibCoroutine, withLoop(checked)), tc.src)
			if err != nil {
				t.Fatalf("%s: %v", tc.src, err)
			}
			if got := joinValues(res); got != tc.want {
				t.Errorf("%s (checked %v): got %q, want %q", tc.src, checked, got, tc.want)
			}
		}
	}
}

// The values a yield passes stay on the coroutine's stack until its next resume, and the
// resumer reads them there: after the stack grew, through nested resumes, and after a yield
// from inside pcall. The executor is back on the resumer's thread after an error too.
func TestYieldValuesInPlace(t *testing.T) {
	src := `
		local function deep(n) if n == 0 then return coroutine.yield("a", n, "c") end return deep(n - 1) end
		local co = coroutine.create(function(n) local x, y = deep(n); return x .. y end)
		local ok, a, b, c = coroutine.resume(co, 3000) -- the stack grows before the yield
		assert(ok and a == "a" and b == 0 and c == "c", tostring(a))
		ok, a = coroutine.resume(co, "p", "q")
		assert(ok and a == "pq", tostring(a))
		local inner = coroutine.wrap(function() for i = 1, 3 do coroutine.yield(i, i * i) end end)
		local outer = coroutine.wrap(function()
			for i = 1, 3 do
				local v, sq = inner() -- a nested resume, then a yield of what it gave
				coroutine.yield(v + sq)
			end
		end)
		assert(outer() == 2 and outer() == 6 and outer() == 12)
		local pc = coroutine.wrap(function() return pcall(coroutine.yield, "in pcall") end)
		assert(pc() == "in pcall")
		assert(select("#", pc("r1", "r2")) == 3) -- true, then the resume values
		local bad = coroutine.wrap(function() error("boom") end)
		local ok2, err = pcall(bad)
		assert(not ok2 and err:find("boom"), err)
		local co2, main = coroutine.running()
		assert(main and coroutine.status(co2) == "running")
		return coroutine.wrap(function() coroutine.yield("after") end)()`
	for _, checked := range []bool{true, false} {
		res, err := runInEnv(t, MustNew(LibSafe|LibCoroutine, withLoop(checked)), src)
		if err != nil || len(res) != 1 || res[0].String() != "after" {
			t.Errorf("checked=%v: %v %v", checked, res, err)
		}
	}
}

// A thread that runs keeps no copy of its counters and frames: they are the executor's. So
// an entry that resumed a coroutine leaves nothing behind in the main thread's record, and
// the pooled executor holds no closure of the script.
func TestRunningThreadKeepsNoState(t *testing.T) {
	p, _, err := compileChunk("t", `
		local co = coroutine.create(function() coroutine.yield(1) end)
		coroutine.resume(co)
		return coroutine.resume(co)`)
	if err != nil {
		t.Fatal(err)
	}
	ex := newExecutor()
	ex.strMeta = nil
	g := testGlobals()
	g.Set(String("coroutine"), tableValue(coroutineLib()))
	if _, err := runClosure(context.Background(), ex, &closure{p: p, env: g}); err != nil {
		t.Fatal(err)
	}
	if m := ex.mainCo; m.st.cl != nil || m.pframes != nil || m.st.hi != 0 {
		t.Errorf("the main thread's record after the entry: %+v", m.st)
	}
}

// A resume that fails before the body runs, for want of room for its values, is raised in
// the resumer and leaves the coroutine suspended where it was, so it can be resumed again
// and closed.
func TestResumeWithoutRoomLeavesCoroutine(t *testing.T) {
	if stackStress {
		t.Skip("moving a stack of a million slots at every check is slow")
	}
	src := `
		local function deep(n) if n == 0 then return (coroutine.yield("deep")) end return (deep(n - 1)) end
		local co = coroutine.create(deep)
		assert(select(2, coroutine.resume(co, 5000)) == "deep") -- its stack holds 5000 frames
		local big = {} -- values that fit the resumer's stack but not the coroutine's
		for i = 1, 1045000 do big[i] = i end
		local ok, err = pcall(coroutine.resume, co, table.unpack(big)) -- line 7
		assert(not ok and tostring(err):find("stack overflow"), tostring(err))
		assert(coroutine.status(co) == "suspended", coroutine.status(co))
		local ok2, v = coroutine.resume(co, "again")
		assert(ok2 and v == "again", tostring(v))
		assert(coroutine.status(co) == "dead")
		return err`
	rt := MustNew(LibSafe|LibCoroutine, Limits{Steps: NoLimit, AllocBytes: NoLimit})
	res, err := runInEnv(t, rt, src)
	if err != nil || len(res) != 1 || !strings.HasPrefix(res[0].String(), "test:7: stack overflow") {
		t.Errorf("%v %v", res, err) // the position is the resume's, not the coroutine's
	}
}

// A panic in a coroutine's host function, recovered at the entry of a nested Script call,
// ends that entry and discards its executor; the entry that called it goes on, on its own
// thread, with its stack and the main thread as they were.
func TestNestedEntryPanicLeavesCallerThread(t *testing.T) {
	var inner *Script
	bang := GoFunc(func(c *Call) error { panic("bang") })
	nested := GoFunc(func(c *Call) error {
		f, _ := inner.Func0[int64]("go")
		_, err := f(c.Context())
		var pe *PanicError
		if !errors.As(err, &pe) {
			return c.Errorf("nested: %v", err)
		}
		return c.Return(int64(7))
	})
	rt := MustNew(LibSafe|LibCoroutine, RecoverPanics(), Global("bang", bang), Global("nested", nested))
	inner = mustLoad(t, rt, `function go() coroutine.resume(coroutine.create(function() bang() end)); return 1 end`)
	outer := mustLoad(t, rt, `function work()
		local t = {}
		for i = 1, 100 do t[i] = i end
		local v = nested()
		local s = 0
		for i = 1, 100 do s = s + t[i] end
		local _, main = coroutine.running()
		return v + s, main
	end`)
	work, _ := outer.Func0[Value]("work")
	for i := range 3 {
		res, err := work(context.Background())
		if err != nil || res.String() != "5057" {
			t.Fatalf("round %d: %v %v", i, res, err)
		}
	}
}

// A coroutine closed after dying in error leaves its stack to the next one, as a body that
// returned does: a loop of wrapped bodies that fail allocates no stacks after the first.
func TestCoroutineStacksReusedAfterError(t *testing.T) {
	if stackStress {
		t.Skip("lua_stackstress allocates a stack at every check")
	}
	s := mustLoad(t, MustNew(LibSafe|LibCoroutine), `local f = function() error("x") end
		function work(n) for i = 1, n do pcall(coroutine.wrap(f)) end end`)
	work, _ := s.Func1[int64, struct{}]("work")
	ctx := context.Background()
	if _, err := work(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, err := work(ctx, 1000); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if per := (after.TotalAlloc - before.TotalAlloc) / 1000; per > 4<<10 {
		t.Errorf("%d bytes allocated for each coroutine", per)
	}
}

// Values that the coroutine's stack cannot hold are refused before the switch, with the
// resumer's position, exactly where its first frame would overflow the stack's limit.
func TestResumeAtStackLimit(t *testing.T) {
	if stackStress {
		t.Skip("a stack of a million slots moves slowly")
	}
	src := `
		local co = coroutine.create(function(...) return select("#", ...) end)
		local big = {}
		for i = 1, 1048063 do big[i] = i end -- one more slot than the limit allows a first frame
		local ok, err = pcall(coroutine.resume, co, table.unpack(big)) -- line 5
		assert(not ok, "resumed")
		assert(coroutine.status(co) == "suspended", coroutine.status(co))
		assert(select(2, coroutine.resume(co, 1, 2)) == 2)
		return err`
	rt := MustNew(LibSafe|LibCoroutine, Limits{Steps: NoLimit, AllocBytes: NoLimit})
	res, err := runInEnv(t, rt, src)
	if err != nil || len(res) != 1 || !strings.HasPrefix(res[0].String(), "test:5: stack overflow") {
		t.Errorf("%v %v", res, err)
	}
}

// The closers of a coroutine run on its own stack, placed by its frames, not above the
// arguments of the resume or close that runs them, which lie on another thread's stack: a
// close from deep in the main thread does not grow the coroutine's stack to that depth.
func TestCloseFromDeepResumer(t *testing.T) {
	src := `
		local co = coroutine.create(function()
			local x <close> = setmetatable({}, {__close = function() end})
			coroutine.yield()
		end)
		coroutine.resume(co)
		local function deep(n, ...)
			if n == 0 then return coroutine.close(co) end
			return (deep(n - 1, ...))
		end
		local wide = {}
		for i = 1, 150 do wide[i] = i end
		assert(deep(2000, table.unpack(wide)))
		local never = coroutine.create(function() end)
		assert(coroutine.close(never))
		return coroutine.status(co), coroutine.status(never)`
	rt := MustNew(LibSafe|LibCoroutine, Limits{AllocBytes: 16 << 20})
	res, err := runInEnv(t, rt, src)
	if err != nil || len(res) != 2 || res[0].String() != "dead" || res[1].String() != "dead" {
		t.Errorf("%v %v", res, err)
	}
}

// A host function that recovers a panic which unwound the VM through it ends its entry
// with the executor's counters, and the thread it runs on, as the panic left them: the
// entry completes, and the executor is never used again. The next entry of the VM runs
// coroutines on a fresh one.
func TestHostRecoveredPanicDiscardsExecutor(t *testing.T) {
	bang := GoFunc(func(c *Call) error { panic("bang") })
	swallow := GoFunc(func(c *Call) error {
		defer func() { recover() }()
		_, err := c.Invoke(c.Value(0))
		return err
	})
	rt := MustNew(LibSafe|LibCoroutine, withLoop(true), Global("bang", bang), Global("swallow", swallow))
	s := mustLoad(t, rt, `
		function hit() swallow(function() coroutine.resume(coroutine.create(function() bang() end)) end) end
		function gen()
			local s = 0
			for v in coroutine.wrap(function() for i = 1, 100 do coroutine.yield(i) end end) do s = s + v end
			return s
		end`)
	ctx := context.Background()
	vm, err := s.NewVM(ctx)
	if err != nil {
		t.Fatal(err)
	}
	hit, _ := vm.Func0[struct{}]("hit")
	gen, _ := vm.Func0[int64]("gen")
	for i := range 3 {
		hit(ctx) // whatever the damaged entry returns
		if got, err := gen(ctx); err != nil || got != 5050 {
			t.Fatalf("round %d after a recovered panic: %d, %v", i, got, err)
		}
	}
}

// A closer is placed above the arguments of the Go call it runs under, not above those of
// a call that returned, whose record may hold a position on another thread's stack.
func TestCloserPlacementIgnoresReturnedCalls(t *testing.T) {
	var sizes []int
	g := GoFunc(func(c *Call) error { return nil })
	a := GoFunc(func(c *Call) error { _, err := c.Invoke(c.Value(0)); return err })
	mark := GoFunc(func(c *Call) error { sizes = append(sizes, len(c.ex.stack)); return nil })
	rt := MustNew(LibSafe|LibCoroutine, Global("g", g), Global("a", a), Global("mark", mark))
	_, err := runInEnv(t, rt, `
		local co = coroutine.wrap(function()
			local function deep(n) if n == 0 then g() coroutine.yield() return end return (deep(n - 1)) end
			deep(600)
		end)
		co() -- g ran 600 frames up the coroutine's stack; its record at that depth stays
		mark()
		a(function() pcall(function() local x <close> = setmetatable({}, {__close = function() end}) error("e") end) end)
		mark()`)
	if err != nil {
		t.Fatal(err)
	}
	if sizes[1] != sizes[0] {
		t.Errorf("the main stack grew from %d to %d slots for a closer placed by a returned call", sizes[0], sizes[1])
	}
}

// A wrapped coroutine that fails leaves its error value nowhere: the call record that
// resume wrote it into lets it go when the host call returns.
func TestWrapFailureKeepsNothing(t *testing.T) {
	rt := MustNew(LibSafe | LibCoroutine)
	p, info, err := compileChunk("test", `local big = {}
		pcall(coroutine.wrap(function() error(big) end))
		return 1`)
	if err != nil {
		t.Fatal(err)
	}
	cl := newMainClosure(p, info, rt.newEnv(newEnvLoader(rt)))
	ex := rt.pool.Get().(*executor)
	ex.setLimits(&rt.run)
	if _, err := runClosure(context.Background(), ex, cl); err != nil {
		t.Fatal(err)
	}
	for d, c := range ex.calls {
		for i, v := range c.res[:cap(c.res)] {
			if !v.IsNil() {
				t.Errorf("call record %d holds %v at %d after the entry", d, v, i)
			}
		}
	}
	release(&rt.pool, ex)
}

// The remainder of floats with whole values, as code written for Lua 5.1 takes bits, is
// computed as integers, inline and in the driver: the same as fmod gives, sign of a zero
// included; values that are not whole, or too large, still go the long way.
func TestFloatModWhole(t *testing.T) {
	src := `
		local function same(a, b) return a == b and (a ~= 0 or 1 / a == 1 / b) end
		local K = 3.0
		local cases = {
			{ 7.0, 3.0, 1.0 }, { -7.0, 3.0, 2.0 }, { 7.0, -3.0, -2.0 }, { -7.0, -3.0, -1.0 },
			{ 6.0, 3.0, 0.0 }, { -6.0, 3.0, -0.0 }, { 6.0, -3.0, 0.0 }, { -0.0, 3.0, -0.0 }, { 0.0, -3.0, 0.0 },
			{ 7, 3.0, 1.0 }, { 7.0, 3, 1.0 }, { -6, 3.0, -0.0 }, { -5, -1.0, -0.0 }, { 6, -3.0, 0.0 }, { -7, 3.0, 2.0 },
			{ 5.5, 2.0, 1.5 }, { 7.0, 2.5, 2.0 }, { 2^53, 3.0, 2.0 }, { 2^60, 7.0, 1.0 }, { 16777216.0 * 65536, 256.0, 0.0 },
			{ 1e300, 7.0, 1.0 }, -- the last three go the long way, with the values C Lua gives
		}
		for i, c in ipairs(cases) do
			local x, y, want = c[1], c[2], c[3]
			local got = x % y
			assert(same(got, want), i .. ": " .. tostring(x) .. " % " .. tostring(y) .. " = " .. tostring(got))
			assert(math.type(got) == "float", i)
		end
		local x = 7.0
		assert(same(x % 3.0, 1.0) and same(-x % 3.0, 2.0) and same((x - 13) % 3.0, -0.0)) -- the constant form, MODK
		assert(same(K % 3, 0.0) and same(-K % 3, -0.0)) -- a float register with an integer constant
		local nan = 7.0 % 0.0
		assert(nan ~= nan)
		local bitbuf, room = 0, 1 -- a bit buffer as inflate keeps one, in floats
		for i = 1, 20 do bitbuf, room = bitbuf + (i % 256) * room, room * 256 end
		local v = 0
		for _ = 1, 50 do local p = 2 ^ 5; local r = bitbuf % p; v = v * 2 + r; bitbuf = (bitbuf - r) / p end
		return v`
	for _, checked := range []bool{true, false} {
		res, err := runInEnv(t, MustNew(withLoop(checked)), src)
		if err != nil || len(res) != 1 {
			t.Fatalf("checked=%v: %v %v", checked, res, err)
		}
		if f, _ := res[0].Float(); f != 5181448545894400 { // as C Lua computes it
			t.Errorf("checked=%v: bit buffer gave %v", checked, res[0])
		}
	}
}

// A float key with a whole value finds the integer key it normalises to, inline, in the
// array part and in the hash part; one without, or past the integers, goes the long way.
func TestFloatKeysInline(t *testing.T) {
	src := `
		local t = { 10, 20, 30, [0] = 5, [-1] = -5, [100] = 1000, [0.5] = "half", [2^53] = "big", [2^63] = "huge" }
		assert(t[1.0] == 10 and t[3.0] == 30 and t[0.0] == 5 and t[-0.0] == 5 and t[-1.0] == -5 and t[100.0] == 1000)
		assert(t[0.5] == "half" and t[2^53] == "big" and t[2^63] == "huge" and t[4.0] == nil and t[1e300] == nil)
		local nan = 0 / 0
		assert(t[nan] == nil)
		t[2.0] = 22
		t[0.0] = 55
		t[100.0] = 1001
		t[4.0] = 40 -- an append, as t[4] is
		assert(t[2] == 22 and t[0] == 55 and t[100] == 1001 and t[4] == 40 and #t == 4)
		local w = {}
		for k = 1, 10 do w[k + 0.0] = k * k end
		local s = 0
		for k = 1, 10 do s = s + w[k] end
		assert(s == 385 and #w == 10)
		return "ok"`
	for _, checked := range []bool{true, false} {
		res, err := runInEnv(t, MustNew(withLoop(checked)), src)
		if err != nil || len(res) != 1 || res[0].String() != "ok" {
			t.Errorf("checked=%v: %v %v", checked, res, err)
		}
	}
}
