package glua

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type scriptReq struct {
	Method string
	Path   string
	Cost   int64
}

func mustLoad(t *testing.T, rt *Runtime, src string) *Script {
	t.Helper()
	s, err := rt.Load(context.Background(), "policy.lua", src)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestLoadAndCall(t *testing.T) {
	ctx := context.Background()
	s := mustLoad(t, MustNew(), `
		local prefix = "/api/"
		calls = 0
		function allow(req, limit)
			if req.method ~= "GET" then return false end
			return req.path:sub(1, #prefix) == prefix and req.cost <= limit
		end
		handlers = {deny = function() return false, "denied" end}
		function count() calls = calls + 1 end
		local n = 0
		function bump() n = n + 1 return n end
		function echo(...) return ... end
		return function(x) return x * 2 end`)
	allow, err := s.Func2[*scriptReq, int64, bool]("allow")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		req  scriptReq
		want bool
	}{
		{scriptReq{"GET", "/api/x", 3}, true},
		{scriptReq{"POST", "/api/x", 3}, false},
		{scriptReq{"GET", "/web", 3}, false},
		{scriptReq{"GET", "/api/x", 30}, false},
	} {
		if got, err := allow(ctx, &tc.req, 10); err != nil || got != tc.want {
			t.Errorf("allow(%+v) = %v, %v", tc.req, got, err)
		}
	}
	deny, err := s.Func0[Pair[bool, string]]("handlers.deny")
	if err != nil {
		t.Fatal(err)
	}
	if p, err := deny(ctx); err != nil || p.First || p.Second != "denied" {
		t.Errorf("deny: %+v %v", p, err)
	}
	double, err := s.Func1[int64, int64]("")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := double(ctx, 21); err != nil || n != 42 {
		t.Errorf("double: %v %v", n, err)
	}
	// a shared script cannot keep state
	count, _ := s.Func0[struct{}]("count")
	if _, err := count(ctx); err == nil || !strings.Contains(err.Error(), "attempt to modify a frozen table") {
		t.Errorf("count: %v", err)
	}
	bump, _ := s.Func0[int64]("bump")
	if _, err := bump(ctx); err == nil || !strings.Contains(err.Error(), "policy.lua:11: attempt to assign to frozen upvalue") {
		t.Errorf("bump: %v", err)
	}
	if _, err := s.Func0[int64]("nope"); !errors.Is(err, ErrUndefined) {
		t.Errorf("nope: %v", err)
	}
	if _, err := s.Func0[int64]("handlers.x.y"); !errors.Is(err, ErrUndefined) {
		t.Errorf("handlers.x.y: %v", err)
	}
	if _, err := s.Func0[int64]("calls"); !errors.Is(err, ErrNotFunction) {
		t.Errorf("calls: %v", err)
	}
	var be *BindError
	if _, err := s.Func1[chan int, int64]("echo"); !errors.As(err, &be) || be.Name != "echo" {
		t.Errorf("chan argument: %v", err)
	}
	// a Go function as the target
	upper, err := s.Func1[string, string]("string.upper")
	if err != nil {
		t.Fatal(err)
	}
	if u, err := upper(ctx, "abc"); err != nil || u != "ABC" {
		t.Errorf("upper: %v %v", u, err)
	}
	// Call and CallN
	echo := s.Globals().Get(String("echo"))
	res, err := s.CallN(ctx, nil, echo, Int(1), String("b"), Bool(true))
	if err != nil || joinValues(res) != "1\tb\ttrue" {
		t.Errorf("CallN: %v %v", joinValues(res), err)
	}
	if v, err := s.Call(ctx, s.Result(), Int(4)); err != nil || v.String() != "8" {
		t.Errorf("Call: %v %v", v, err)
	}
	if _, err := s.Call(ctx, Int(1)); !errors.Is(err, ErrNotFunction) {
		t.Errorf("Call(1): %v", err)
	}
	if v, err := s.Call(ctx, s.Globals().Get(String("tostring")), Int(5)); err != nil || v.String() != "5" {
		t.Errorf("Call(tostring): %v %v", v, err)
	}
}

func TestLoadErrors(t *testing.T) {
	ctx := context.Background()
	rt := MustNew(LibSafe | LibCoroutine)
	for _, tc := range []struct{ src, want string }{
		{"x = = 1", "policy.lua:1: unexpected symbol near '='"},
		{"error('boom')", "policy.lua:1: boom"},
		{"co = coroutine.create(print)", "policy.lua: cannot freeze: global 'co' is a coroutine"},
		{"t = {f = coroutine.wrap(print)}", "global 't'.f is a coroutine.wrap function with hidden state"},
		{"it = string.gmatch('a', 'a')", "global 'it' is a gmatch iterator function with hidden state"},
		{"return coroutine.create(print)", "result 1 is a coroutine"},
		{"local co = coroutine.create(print) function f() return co end", "global 'f' (upvalue 'co') is a coroutine"},
		{"while true do end", "step limit exceeded"},
	} {
		_, err := rt.Load(ctx, "policy.lua", tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.src, err, tc.want)
		}
	}
	var se *SyntaxError
	if _, err := rt.Load(ctx, "x.lua", "local = 1"); !errors.As(err, &se) || se.Source != "x.lua" || se.Line != 1 {
		t.Errorf("syntax error: %#v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := rt.Load(cctx, "x.lua", "return 1"); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
	// a failed freeze leaves the libraries alone
	if _, err := rt.Load(ctx, "ok.lua", "string.x = 1"); err == nil {
		t.Error("a script modified the string library")
	}
}

func TestLoadModules(t *testing.T) {
	ctx := context.Background()
	runs := 0
	rt := MustNew(
		SourceModule("util", "local M = {n = 0} function M.get() return M.n end return M"),
		Loader(func(name string) (string, error) {
			runs++
			return "return {name = '" + name + "'}", nil
		}),
	)
	s := mustLoad(t, rt, `util = require("util") ext = require("ext") function late() return require("other") end
		function again() return require("ext").name end`)
	if v, err := s.Get[int]("util.n"); err != nil || v != 0 {
		t.Errorf("util.n: %v %v", v, err)
	}
	late, _ := s.Func0[Value]("late")
	if _, err := late(ctx); err == nil || !strings.Contains(err.Error(), "module 'other' not found: modules can be loaded only while the script loads") {
		t.Errorf("late require: %v", err)
	}
	again, _ := s.Func0[string]("again")
	if name, err := again(ctx); err != nil || name != "ext" {
		t.Errorf("again: %v %v", name, err)
	}
	if runs != 1 {
		t.Errorf("the loader ran %d times", runs)
	}
	if !s.Globals().Get(String("util")).isObj(kindTable) || !(*Table)(s.Globals().Get(String("util")).p).Frozen() {
		t.Error("the module is not frozen")
	}
}

func TestScriptGet(t *testing.T) {
	type config struct {
		Port  int
		Hosts []string
	}
	s := mustLoad(t, MustNew(), `config = {port = 8080, hosts = {"a", "b"}}`)
	cfg, err := s.Get[config]("config")
	if err != nil || cfg.Port != 8080 || len(cfg.Hosts) != 2 {
		t.Errorf("config: %+v %v", cfg, err)
	}
	tab, err := s.Get[*Table]("config")
	if err != nil || !tab.Frozen() {
		t.Fatalf("config table: %v", err)
	}
	if port, err := tab.Field[int]("port"); err != nil || port != 8080 {
		t.Errorf("port: %v %v", port, err)
	}
	if _, err := s.Get[int]("missing"); !errors.Is(err, ErrUndefined) {
		t.Errorf("missing: %v", err)
	}
	if _, err := s.Get[int]("config.hosts"); err == nil {
		t.Error("a table converted to int")
	}
}

func TestScriptStructByValue(t *testing.T) {
	ctx := context.Background()
	s := mustLoad(t, MustNew(), `
		function cost(r) return r.cost * 2 end
		function keep(r) return r end
		function wrap(a, b) return {a, b, n = a.cost + b.cost} end
		function write(r) r.cost = 1 end`)
	cost, err := s.Func1[scriptReq, int64]("cost")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := cost(ctx, scriptReq{Cost: 21}); err != nil || n != 42 {
		t.Errorf("cost: %v %v", n, err)
	}
	// a struct by value that escapes in the results is copied out of the slot
	keep, _ := s.Func1[scriptReq, Value]("keep")
	v, err := keep(ctx, scriptReq{Path: "/a"})
	if err != nil {
		t.Fatal(err)
	}
	if v.n&(flagCallscoped|flagReadonly) != 0 || (*scriptReq)(v.p).Path != "/a" {
		t.Errorf("escaped userdata: flags %x", v.n>>48&0xff)
	}
	v2, _ := keep(ctx, scriptReq{Path: "/b"})
	if (*scriptReq)(v.p).Path != "/a" || (*scriptReq)(v2.p).Path != "/b" {
		t.Error("an escaped result shares the slot")
	}
	kp, _ := s.Func1[scriptReq, *scriptReq]("keep")
	if p, err := kp(ctx, scriptReq{Cost: 3}); err != nil || p.Cost != 3 {
		t.Errorf("as pointer: %v %v", p, err)
	}
	wrap, _ := s.Func2[scriptReq, scriptReq, *Table]("wrap")
	tab, err := wrap(ctx, scriptReq{Cost: 1}, scriptReq{Cost: 2})
	if err != nil {
		t.Fatal(err)
	}
	a, b := tab.Get(Int(1)), tab.Get(Int(2))
	if a.p == b.p || (*scriptReq)(a.p).Cost != 1 || (*scriptReq)(b.p).Cost != 2 || a.n&flagCallscoped != 0 {
		t.Error("two arguments of one struct type share a slot, or stayed callscoped")
	}
	write, _ := s.Func1[scriptReq, struct{}]("write")
	if _, err := write(ctx, scriptReq{}); err == nil {
		t.Error("a script wrote a struct passed by value")
	}
}

func TestScriptConcurrent(t *testing.T) {
	s := mustLoad(t, MustNew(), `
		local base = {k = 10}
		function f(x) local t = {} for i = 1, 10 do t[i] = x + i end return t[10] + base.k end`)
	f, err := s.Func1[int64, int64]("f")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range int64(200) {
				x := int64(g)*1000 + i
				if n, err := f(context.Background(), x); err != nil || n != x+20 {
					t.Errorf("f(%d) = %d, %v", x, n, err)
					return
				}
			}
		})
	}
	wg.Wait()
}

func TestScriptLimits(t *testing.T) {
	ctx := context.Background()
	rt := MustNew(Limits{Steps: 1000, InitSteps: 100000})
	s := mustLoad(t, rt, `for i = 1, 5000 do end function spin(n) for i = 1, n do end end`)
	spin, _ := s.Func1[int64, struct{}]("spin")
	if _, err := spin(ctx, 500); err != nil {
		t.Errorf("500 steps: %v", err)
	}
	if _, err := spin(ctx, 5000); !errors.Is(err, ErrSteps) {
		t.Errorf("5000 steps: %v", err)
	}
	big, err := s.WithLimits(Limits{Steps: 10000})
	if err != nil {
		t.Fatal(err)
	}
	spinBig, _ := big.Func1[int64, struct{}]("spin")
	if _, err := spinBig(ctx, 5000); err != nil {
		t.Errorf("5000 steps with more: %v", err)
	}
	if _, err := spin(ctx, 5000); !errors.Is(err, ErrSteps) {
		t.Errorf("the original script's limit changed: %v", err)
	}
	if _, err := MustNew(Hardened()).Load(ctx, "x", "return 1"); err != nil {
		t.Fatal(err)
	}
	hs := mustLoad(t, MustNew(Hardened()), "return 1")
	if _, err := hs.WithLimits(Limits{Steps: NoLimit}); err == nil {
		t.Error("the hardened profile took NoLimit")
	}
}

func TestPanics(t *testing.T) {
	ctx := context.Background()
	boom := Global("boom", GoFunc(func(c *Call) error { panic("host bug") }))
	s := mustLoad(t, MustNew(boom), "function f() return pcall(boom) end")
	f, _ := s.Func0[bool]("f")
	func() {
		defer func() {
			if r := recover(); r != "host bug" {
				t.Errorf("recovered %v", r)
			}
		}()
		f(ctx)
		t.Error("the panic did not continue")
	}()
	s = mustLoad(t, MustNew(boom, RecoverPanics()), "function f() return pcall(boom) end")
	f, _ = s.Func0[bool]("f")
	var pe *PanicError
	if _, err := f(ctx); !errors.As(err, &pe) || pe.Value != "host bug" || len(pe.Stack) == 0 {
		t.Errorf("RecoverPanics: %v", err)
	}
	// the executor that panicked is not pooled again; the next call works
	g, _ := s.Func0[int64]("string.len")
	_ = g
	ok, _ := s.Func1[string, int64]("string.len")
	if n, err := ok(ctx, "abc"); err != nil || n != 3 {
		t.Errorf("after a panic: %v %v", n, err)
	}
}

func TestObserve(t *testing.T) {
	var stats []CallStats
	rt := MustNew(Observe(func(cs CallStats) { stats = append(stats, cs) }))
	s := mustLoad(t, rt, "function f(n) local t = {} for i = 1, n do t[i] = i end return #t end")
	f, _ := s.Func1[int64, int64]("f")
	f(context.Background(), 100)
	if len(stats) != 2 {
		t.Fatalf("%d entries observed", len(stats))
	}
	if stats[0].Script != "policy.lua" || stats[0].Func != "" {
		t.Errorf("load: %+v", stats[0])
	}
	if cs := stats[1]; cs.Func != "f" || cs.Steps < 95 || cs.Steps > 110 || cs.AllocBytes <= 0 || cs.Err != nil {
		t.Errorf("call: %+v", cs)
	}
}

func TestScriptFuncNoAlloc(t *testing.T) {
	if raceEnabled || stackStress {
		t.Skip("sync.Pool drops items in race builds; lua_stackstress allocates stacks")
	}
	s := mustLoad(t, MustNew(), "function add(a, b) return a + b end function rule(r) return r.cost > 1 and r.method == 'GET' end")
	add, _ := s.Func2[int64, int64, int64]("add")
	rule, _ := s.Func1[scriptReq, bool]("rule")
	byPtr, _ := s.Func1[*scriptReq, bool]("rule")
	ctx := context.Background()
	req := &scriptReq{Method: "GET", Cost: 2}
	for name, f := range map[string]func(){
		"add":      func() { add(ctx, 1, 2) },
		"by value": func() { rule(ctx, scriptReq{Method: "GET", Cost: 2}) },
		"pointer":  func() { byPtr(ctx, req) },
	} {
		if a := testing.AllocsPerRun(1000, f); a != 0 {
			t.Errorf("%s allocates %v times per call", name, a)
		}
	}
}
