package glua

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
)

// runInEnv runs src in a new environment of rt, on one of its executors, under its call
// limits, and returns the results.
func runInEnv(t *testing.T, rt *Runtime, src string) ([]Value, error) {
	t.Helper()
	p, info, err := compileChunk("test", src)
	if err != nil {
		t.Fatal(err)
	}
	ld := newEnvLoader(rt)
	cl := newMainClosure(p, info, rt.newEnv(ld))
	ex := rt.pool.Get().(*executor)
	ex.setLimits(&rt.run)
	res, err := runClosure(context.Background(), ex, cl)
	release(&rt.pool, ex)
	return res, err
}

func TestNewLibs(t *testing.T) {
	for _, tc := range []struct {
		opts []Option
		src  string
		want string
	}{
		{nil, "return type(string), type(math), type(coroutine), type(load), type(require)", "table\ttable\tnil\tnil\tfunction"},
		{[]Option{LibBase | LibLoad | LibCoroutine}, "return type(string), type(coroutine), type(load)", "nil\ttable\tfunction"},
		{[]Option{LibNone}, "return 1", "1"},
		{[]Option{LibNone | LibMath}, "return math.max(1, 2)", "2"},
		{nil, "return ('x'):rep(3)", "xxx"},
		{nil, "return _G == _ENV, _G._G == _G", "true\ttrue"},
	} {
		rt, err := New(tc.opts...)
		if err != nil {
			t.Fatal(err)
		}
		res, err := runInEnv(t, rt, tc.src)
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		if got := joinValues(res); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.src, got, tc.want)
		}
	}
	rt := MustNew(LibNone)
	if _, err := runInEnv(t, rt, "return print"); err != nil {
		t.Fatal(err)
	}
	if _, err := runInEnv(t, rt, "print(1)"); err == nil || !strings.Contains(err.Error(), "global 'print'") {
		t.Errorf("LibNone: print(1) gave %v", err)
	}
}

func TestWithout(t *testing.T) {
	for _, tc := range []struct {
		opts []Option
		src  string
		want string
	}{
		{[]Option{Without("collectgarbage", "string.rep", "debug.traceback")},
			"return collectgarbage, string.rep, ('x').rep, debug.traceback, type(string.sub), type(debug)",
			"nil\tnil\tnil\tnil\tfunction\ttable"},
		{[]Option{Without("string")}, "return string, ('x'):rep(2), pcall(function() getmetatable('').__index.rep = 1 end)",
			"nil\txx\tfalse\ttest:1: attempt to modify a frozen table"},
		{[]Option{Without("string", "string.rep")}, "return string, ('x').rep", "nil\tnil"},
		{[]Option{Without("print")}, "rawset(_G, 'print', 1) return print", "1"},
		{[]Option{LibSafe | LibLoad, Without("_G", "package", "require", "load")}, "return _G, package, require, load", "nil\tnil\tnil\tnil"},
		{[]Option{Without("require")}, "return type(package.loaded)", "table"},
		{[]Option{Without("print", "print")}, "print = 1 return print", "1"},
		{[]Option{Without("print"), Global("print", Fn1(func(s string) string { return "host " + s }))}, "return print('x')", "host x"},
		{[]Option{Without("require"), Global("require", Fn1(func(s string) string { return "module " + s }))}, "return require('m')", "module m"},
		{[]Option{Without("math"), Module("math", Members{"pi": 3})}, "return math.pi, math.floor", "3\tnil"},
		{[]Option{LibSafe, Global("load", Fn1(func(s string) string { return "host " + s }))}, "return load('x')", "host x"},
		{[]Option{LibNone, Global("require", Fn1(func(s string) string { return "module " + s }))}, "return require('m')", "module m"},
		{nil, "return type(collectgarbage), type(string.rep)", "function\tfunction"}, // other Runtimes keep them
	} {
		rt, err := New(tc.opts...)
		if err != nil {
			t.Fatal(err)
		}
		res, err := runInEnv(t, rt, tc.src)
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		if got := joinValues(res); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.src, got, tc.want)
		}
	}
	rt := MustNew(Without("print"), StrictGlobals())
	if _, err := runInEnv(t, rt, "print(1)"); err == nil || !strings.Contains(err.Error(), "undefined global 'print'") {
		t.Errorf("print left out: got %v", err)
	}
	vm, err := mustLoad(t, MustNew(Without("print")), "").NewVM(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.Set("print", 1); err != nil {
		t.Errorf("VM.Set of a name left out: %v", err)
	}
}

func TestNewErrors(t *testing.T) {
	for _, tc := range []struct {
		opts []Option
		want string
	}{
		{[]Option{Hardened(), LibSafe | LibLoad}, "hardened profile refuses LibLoad"},
		{[]Option{Hardened(), LibCoroutine}, "hardened profile refuses LibCoroutine"},
		{[]Option{Hardened(), Limits{Steps: NoLimit}}, "refuses NoLimit for Limits.Steps"},
		{[]Option{Limits{AllocBytes: -2}}, "Limits.AllocBytes is negative"},
		{[]Option{Global("x", 1), Global("x", 2)}, "cannot bind 'x': the name is already defined"},
		{[]Option{Global("print", 1)}, "cannot bind 'print': the name is already defined"},
		{[]Option{Global("require", 1)}, "cannot bind 'require': the name is already defined"},
		{[]Option{Global("end", 1)}, "not a Lua name"},
		{[]Option{Global("a.b", 1)}, "not a Lua name"},
		{[]Option{Global("f", strings.ToUpper)}, "needs an adapter"},
		{[]Option{Module("m", Members{"f": strings.ToUpper})}, "cannot bind 'm.f'"},
		{[]Option{Global("c", make(chan int))}, "unsupported type chan int"},
		{[]Option{Global("u", uint64(math.MaxUint64))}, "does not fit"},
		{[]Option{SourceModule("m", "return +")}, "m:1:"},
		{[]Option{Module("m", Members{}), SourceModule("m", "return 1")}, "module is already defined"},
		{[]Option{Without("printf")}, "cannot bind 'printf': the libraries define no such name"},
		{[]Option{Without("string.repp")}, "cannot bind 'string.repp'"},
		{[]Option{Without("strin.rep")}, "cannot bind 'strin.rep'"},
		{[]Option{Without("print.x")}, "cannot bind 'print.x'"},
		{[]Option{Without("load")}, "cannot bind 'load'"},
		{[]Option{Without("_G.print")}, "cannot bind '_G.print'"},
		{[]Option{LibNone | LibMath, Without("require")}, "cannot bind 'require'"},
		{[]Option{Without("string.rep"), Global("string", 1)}, "cannot bind 'string': the name is already defined"},
	} {
		_, err := New(tc.opts...)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("got %v, want %q", err, tc.want)
		}
	}
	var se *SyntaxError
	if _, err := New(SourceModule("m", "x = = 1")); !errors.As(err, &se) || se.Line != 1 {
		t.Errorf("source module syntax error: %v", err)
	}
	var be *BindError
	if _, err := New(Global("f", func() {})); !errors.As(err, &be) || be.Name != "f" {
		t.Errorf("bare func: %v", err)
	}
}

func TestLimitsResolve(t *testing.T) {
	run, init, err := resolveLimits(Limits{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if run != (limits{defaultSteps, defaultAlloc, defaultDepth, defaultHost}) || init != run {
		t.Errorf("defaults: %+v %+v", run, init)
	}
	run, init, _ = resolveLimits(Limits{Steps: 5, InitSteps: NoLimit, AllocBytes: NoLimit, HostDepth: NoLimit}, false)
	if run.steps != 5 || init.steps != math.MaxInt64 || run.alloc != math.MaxInt64 || init.alloc != math.MaxInt64 || run.hostDepth != maxHostDepth {
		t.Errorf("NoLimit: %+v %+v", run, init)
	}
	run, init, _ = resolveLimits(Limits{Steps: 5, InitAllocBytes: 7, CallDepth: 1 << 30}, false)
	if init.steps != 5 || init.alloc != 7 || run.alloc != defaultAlloc || run.callDepth != maxCallFrames {
		t.Errorf("init defaults: %+v %+v", run, init)
	}
}

func TestGlobalsAndModules(t *testing.T) {
	cfg := NewTable(0, 1)
	cfg.Set(String("port"), Int(8080))
	rt := MustNew(
		Global("env", "prod"),
		Global("n", 42),
		Global("ratio", 0.5),
		Global("flags", []bool{true, false}),
		Global("cfg", cfg),
		Global("double", GoFunc(func(c *Call) error {
			i, _ := c.Value(0).Int()
			return c.ReturnValues(Int(2 * i))
		})),
		Module("kv", Members{
			"version": 3,
			"sub":     Members{"name": "inner"},
			"get":     func(c *Call) error { return c.ReturnValues(String("v:" + c.Value(0).String())) },
		}),
		SourceModule("util", "local M = {} function M.twice(x) return double(x) end return M"),
		SourceModule("noresult", "seen = (seen or 0) + 1"),
	)
	if !cfg.Frozen() {
		t.Error("a table given to Global is not frozen")
	}
	for _, tc := range []struct{ src, want string }{
		{"return env, n, ratio, flags[1], flags[2], cfg.port", "prod\t42\t0.5\ttrue\tfalse\t8080"},
		{"return double(21), kv.version, kv.sub.name, kv.get('a')", "42\t3\tinner\tv:a"},
		{"return require('kv') == kv, package.loaded.kv == kv", "true\ttrue"},
		{"local u = require('util') return u.twice(4), require('util') == u", "8\ttrue"},
		{"return require('noresult'), require('noresult'), seen", "true\ttrue\t1"},
		{"return pcall(require, 'nope')", "false\tmodule 'nope' not found"},
		{"local upper = 1 upper = 2 return upper", "2"},
		{"return pcall(function() env = 1 end)", "false\ttest:1: attempt to assign to reserved name 'env'"},
		{"return pcall(function() string = 1 end)", "false\ttest:1: attempt to assign to reserved name 'string'"},
		{"return pcall(function() _G.print = 1 end)", "false\ttest:1: attempt to assign to reserved name 'print'"},
		{"return pcall(rawset, _G, 'kv', 1)", "false\tattempt to assign to reserved name 'kv'"},
		{"return pcall(function() cfg.port = 1 end)", "false\ttest:1: attempt to modify a frozen table"},
		{"return pcall(function() kv.x = 1 end)", "false\ttest:1: attempt to modify a frozen table"},
		{"return pcall(function() string.x = 1 end)", "false\ttest:1: attempt to modify a frozen table"},
		{"x = 1 x = x + 1 _G.y = 3 return x, y", "2\t3"},
	} {
		res, err := runInEnv(t, rt, tc.src)
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		if got := joinValues(res); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.src, got, tc.want)
		}
	}
	// assigning a reserved name repeatedly takes the fast path's hint after the first miss
	if res, err := runInEnv(t, rt, "local ok = 0 for i = 1, 10 do if not pcall(function() env = i end) then ok = ok + 1 end end return ok, env"); err != nil || joinValues(res) != "10\tprod" {
		t.Errorf("repeated reserved assignment: %v %v", joinValues(res), err)
	}
}

func TestLoader(t *testing.T) {
	var asked []string
	rt := MustNew(LibSafe|LibLoad, Loader(func(name string) (string, error) {
		asked = append(asked, name)
		if name == "missing" {
			return "", errors.New("no such file")
		}
		if name == "bad" {
			return "return +", nil
		}
		return "return {name = ...}", nil
	}))
	res, err := runInEnv(t, rt, `
		local a = require("a")
		local ok1, e1 = pcall(require, "missing")
		local ok2, e2 = pcall(require, "bad")
		return a.name, require("a") == a, e1, e2, load("return x", "c", "t", {x = 5})(), load("return _G")() == _G`)
	if err != nil {
		t.Fatal(err)
	}
	want := "a\ttrue\tmodule 'missing' not found: no such file\terror loading module 'bad': bad:1: unexpected symbol near '+'\t5\ttrue"
	if got := joinValues(res); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if strings.Join(asked, ",") != "a,missing,bad" {
		t.Errorf("loader asked for %v", asked)
	}
}

func TestStrictGlobals(t *testing.T) {
	rt := MustNew(StrictGlobals())
	res, err := runInEnv(t, rt, "x = 1 local ok, e = pcall(function() return y end) return x, ok, e, rawget(_G, 'y')")
	if err != nil {
		t.Fatal(err)
	}
	if got := joinValues(res); got != "1\tfalse\ttest:1: undefined global 'y'\tnil" {
		t.Errorf("got %q", got)
	}
}

func TestOutput(t *testing.T) {
	var buf bytes.Buffer
	rt := MustNew(Output(&buf))
	if _, err := runInEnv(t, rt, "print('a', 1) print(nil)"); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "a\t1\nnil\n" {
		t.Errorf("output %q", buf.String())
	}
	if _, err := runInEnv(t, MustNew(), "print('discarded')"); err != nil {
		t.Fatal(err)
	}
}

func TestOutputSerialised(t *testing.T) {
	var buf bytes.Buffer // not safe for concurrent use: Output's lock must make it so
	rt := MustNew(Output(&buf))
	s := mustLoad(t, rt, "function f() for i = 1, 100 do print('line') end end")
	f, _ := s.Func0[struct{}]("f")
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { f(context.Background()) })
	}
	wg.Wait()
	if buf.String() != strings.Repeat("line\n", 400) {
		t.Errorf("output of %d bytes is torn", buf.Len())
	}
}

func TestOutputFunc(t *testing.T) {
	type tenantKey struct{}
	var got []string
	f := func(ctx context.Context, s string) error {
		if s == "full\n" {
			return errors.New("output is full")
		}
		tenant, _ := ctx.Value(tenantKey{}).(string)
		got = append(got, tenant+": "+s)
		return nil
	}
	invoke := GoFunc(func(c *Call) error { _, err := c.Invoke(c.Value(0)); return err })
	rt := MustNew(LibSafe|LibIO, Output(io.Discard), OutputFunc(f), Global("invoke", invoke))
	s, err := rt.Load(context.WithValue(context.Background(), tenantKey{}, "a"), "out.lua", `
		print("loading")
		function f()
			print("called", 1)
			warn("@on") warn("warned")
			io.write("written\n")
			invoke(function() print("from Go") end)
			return select(2, pcall(print, "full"))
		end`)
	if err != nil {
		t.Fatal(err)
	}
	call, err := s.Func0[string]("f")
	if err != nil {
		t.Fatal(err)
	}
	msg, err := call(context.WithValue(context.Background(), tenantKey{}, "b"))
	if err != nil || msg != "output is full" {
		t.Errorf("a failing write: got %q, %v", msg, err)
	}
	want := []string{"a: loading\n", "b: called\t1\n", "b: Lua warning: warned\n", "b: written\n", "b: from Go\n"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}

	var buf bytes.Buffer
	if _, err := runInEnv(t, MustNew(OutputFunc(f), Output(&buf)), "print('last wins')"); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "last wins\n" {
		t.Errorf("output %q", buf.String())
	}
	if _, err := runInEnv(t, MustNew(Output(&buf), Output(nil)), "print('discarded')"); err != nil || buf.String() != "last wins\n" {
		t.Errorf("Output(nil): %v, output %q", err, buf.String())
	}
}

func TestRandSeed(t *testing.T) {
	rt := MustNew(RandSeed(7))
	a, _ := runInEnv(t, rt, "math.randomseed() return math.random(1, 1000000), math.random(), math.randomseed()")
	b, _ := runInEnv(t, rt, "math.randomseed() return math.random(1, 1000000), math.random(), math.randomseed()")
	if joinValues(a) != joinValues(b) {
		t.Errorf("RandSeed: %s and %s differ", joinValues(a), joinValues(b))
	}
}

func TestFreezeRefuses(t *testing.T) {
	co := newTestCoroutine(t)
	for _, tc := range []struct {
		v    Value
		want string
	}{
		{co, "table.co is a coroutine"},
	} {
		tab := NewTable(0, 1)
		tab.Set(String("co"), tc.v)
		inner := NewTable(0, 1)
		tab.Set(String("inner"), tableValue(inner))
		err := tab.Freeze()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("got %v, want %q", err, tc.want)
		}
		if tab.Frozen() || inner.Frozen() {
			t.Error("a failed Freeze froze something")
		}
	}
	g := &goFunc{name: "gmatch iterator", stateful: true}
	tab := NewTable(1, 0)
	tab.Set(Int(1), goFnValue(g))
	if err := tab.Freeze(); err == nil || !strings.Contains(err.Error(), "table[1] is a gmatch iterator function with hidden state") {
		t.Errorf("stateful: %v", err)
	}
	// cycles, metatables and the cache of absent metamethods
	a, b, mt := NewTable(0, 1), NewTable(0, 1), NewTable(0, 1)
	a.Set(String("b"), tableValue(b))
	b.Set(String("a"), tableValue(a))
	a.meta = mt
	mt.Set(String("__index"), tableValue(b))
	if err := a.Freeze(); err != nil {
		t.Fatal(err)
	}
	if !a.Frozen() || !b.Frozen() || !mt.Frozen() {
		t.Error("not everything reachable was frozen")
	}
	if mt.flags&tabNoIndex != 0 || mt.flags&tabNoEq == 0 {
		t.Errorf("metatable cache flags %b", mt.flags)
	}
}

// newTestCoroutine returns a suspended coroutine.
func newTestCoroutine(t *testing.T) Value {
	res, err := runSource("co", "local co = coroutine.create(function() coroutine.yield() end) coroutine.resume(co) return co")
	if err != nil {
		t.Fatal(err)
	}
	return res[0]
}

// joinValues renders values as print does, separated by tabs.
func joinValues(vs []Value) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = v.String()
	}
	return strings.Join(parts, "\t")
}
