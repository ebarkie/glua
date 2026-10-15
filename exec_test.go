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
