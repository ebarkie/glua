package glua

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type hostReq struct {
	Path    string
	headers map[string]string
}

func (r *hostReq) Header(name string) string { return r.headers[name] }

var errNotFound = errors.New("not found")

func lookupKV(ctx context.Context, key string) (string, error) {
	if ctx == nil {
		return "", errors.New("no context")
	}
	if key == "" {
		return "", errNotFound
	}
	return "v:" + key, nil
}

func TestHostFunctions(t *testing.T) {
	ctx := context.Background()
	clamp := GoFunc(func(c *Call) error {
		x, lo := c.Arg[float64](0), c.Arg[float64](1)
		hi := c.Opt[float64](2, 1)
		return c.Return(min(max(x, lo), hi))
	})
	split := GoFunc(func(c *Call) error {
		s, sep := c.Arg[string](0), c.Arg[string](1)
		head, tail, _ := strings.Cut(s, sep)
		return c.Return2(head, tail)
	})
	each := GoFunc(func(c *Call) error {
		items, fn := c.Arg[*Table](0), c.Value(1)
		for _, v := range items.Items() {
			if _, err := c.Invoke(fn, v); err != nil {
				return err
			}
		}
		return nil
	})
	collect := GoFunc(func(c *Call) error {
		res, err := c.InvokeN(nil, c.Value(0))
		if err != nil {
			return err
		}
		return c.Return(len(res))
	})
	var warned []string
	rt := MustNew(
		Global("upper", Fn1(strings.ToUpper)),
		Global("clamp", clamp),
		Global("log_warn", Do1(func(msg string) { warned = append(warned, msg) })),
		Global("sum3", Fn3(func(a, b, c int) int { return a + b + c })),
		Global("pair", Reflect(func(a int, rest ...string) (string, int) { return strings.Join(rest, "+"), a })),
		Global("fail", Do0E(func() error { return fmt.Errorf("wrapped: %w", errNotFound) })),
		Global("ctxname", Fn0E(func() (string, error) { return "none", nil })),
		Module("kv", Members{
			"get":     Fn2E(lookupKV),
			"split":   split,
			"each":    each,
			"collect": collect,
			"version": 3,
		}),
		Type[hostReq](Method("header", Fn2((*hostReq).Header))),
	)
	s := mustLoad(t, rt, `
		function run(req)
			local out = {}
			out[#out+1] = upper("hello")
			out[#out+1] = clamp(1.7, 0)
			out[#out+1] = clamp("0.5", 0, 2)
			out[#out+1] = kv.get("user:42")
			local head, tail = kv.split("a=b", "=")
			out[#out+1] = head .. "|" .. tail
			out[#out+1] = req:header("x-cost")
			local ok, err = pcall(kv.get, "")
			out[#out+1] = tostring(ok) .. "|" .. err
			local total = 0
			kv.each({1, 2, 3}, function(n) total = total + n end)
			out[#out+1] = total
			out[#out+1] = tostring(require("kv") == kv)
			out[#out+1] = sum3(1, 2, 3)
			out[#out+1] = select("#", pair(7, "a", "b")) .. pair(7, "a", "b")
			out[#out+1] = kv.collect(function() return 1, 2, 3 end)
			log_warn("careful")
			out[#out+1] = select(2, pcall(clamp))
			out[#out+1] = select(2, pcall(clamp, 1, {}))
			out[#out+1] = select(2, pcall(sum3, 1, 2.5, 3))
			out[#out+1] = select(2, pcall(upper))
			return table.concat(out, ",")
		end
		function failing() fail() end`)
	run, err := s.Func1[*hostReq, string]("run")
	if err != nil {
		t.Fatal(err)
	}
	got, err := run(ctx, &hostReq{headers: map[string]string{"x-cost": "3"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "HELLO,1.0,0.5,v:user:42,a|b,3,false|not found,6,true,6,2a+b,3," +
		"bad argument #1 to 'clamp' (number expected, got no value)," +
		"bad argument #2 to 'clamp' (number expected, got table)," +
		"bad argument #2 to 'sum3' (number has no integer representation)," +
		"bad argument #1 to 'upper' (string expected, got no value)"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if strings.Join(warned, ",") != "careful" {
		t.Errorf("warned %v", warned)
	}
	failing, _ := s.Func0[struct{}]("failing")
	_, err = failing(ctx)
	if !errors.Is(err, errNotFound) {
		t.Errorf("a Go error leaving Lua lost its chain: %v", err)
	}
	if err == nil || err.Error() != "policy.lua:27: wrapped: not found" {
		t.Errorf("message: %v", err)
	}
}

func TestArgFromLuaCall(t *testing.T) {
	// an argument error raised where Lua called the function carries its position
	rt := MustNew(Global("need", GoFunc(func(c *Call) error { return c.Return(c.Arg[int64](0) + 1) })))
	s := mustLoad(t, rt, "function f() return need('x') end function g() return need('41') end")
	f, _ := s.Func0[int64]("f")
	if _, err := f(context.Background()); err == nil || err.Error() != "policy.lua:1: bad argument #1 to 'need' (number expected, got string)" {
		t.Errorf("f: %v", err)
	}
	g, _ := s.Func0[int64]("g")
	if n, err := g(context.Background()); err != nil || n != 42 {
		t.Errorf("g: %v %v", n, err)
	}
}

// An argument of no Lua type that a parameter takes is named as C Lua's luaL_checktype and
// luaL_checkudata name it: the type wanted and the type given. A value of a type the parameter
// takes that does not convert, and a type with a conversion of its own, keep their messages.
func TestArgTypesAsLua(t *testing.T) {
	type blob struct{ N int }
	type other struct{ N int }
	type point struct{ X int }
	rt := MustNew(
		Convert(func(p *point) Value { return Int(int64(p.X)) }, func(v Value) (*point, error) {
			if n, ok := v.Int(); ok {
				return &point{X: int(n)}, nil
			}
			return nil, errors.New("not a point")
		}),
		Global("tab", GoFunc(func(c *Call) error { c.Arg[*Table](0); return nil })),
		Global("blob", GoFunc(func(c *Call) error { return c.Return(c.Arg[*blob](0) != nil) })),
		Global("pt", GoFunc(func(c *Call) error { c.Arg[*point](0); return nil })),
		Global("when", GoFunc(func(c *Call) error { c.Arg[*time.Time](0); return nil })),
		Global("newblob", Fn0(func() *blob { return &blob{} })),
		Global("newother", Fn0(func() *other { return &other{} })))
	s := mustLoad(t, rt, `
		local function e(...) return select(2, pcall(...)) end
		function f()
			return e(tab, 1), e(tab, newblob()), e(blob, 1), e(blob, newother()), e(blob, "x"),
				e(blob, newblob()), e(blob, {n = 1}), e(blob, {n = "x"}), e(pt, "x"), e(when, "garbage")
		end
		function g(b) return e(blob, b) end`)
	res, err := s.CallN(context.Background(), nil, s.Globals().Get(String("f")))
	want := []string{
		"bad argument #1 to 'tab' (table expected, got number)",
		"bad argument #1 to 'tab' (table expected, got userdata)",
		"bad argument #1 to 'blob' (blob expected, got number)",
		"bad argument #1 to 'blob' (blob expected, got userdata)",
		"bad argument #1 to 'blob' (blob expected, got string)",
		"true",
		"true",
		"bad argument #1 to 'blob' (.n: cannot convert string to int)",
		"bad argument #1 to 'pt' (cannot convert string to *glua.point: not a point)",
		"bad argument #1 to 'when' (cannot convert string to time.Time: ", // and the parse error
	}
	if err != nil || len(res) != len(want) {
		t.Fatalf("got %v, %v", res, err)
	}
	for i, w := range want {
		if got := res[i].String(); got != w && !(strings.HasSuffix(w, ": ") && strings.HasPrefix(got, w)) {
			t.Errorf("message %d = %q, want %q", i+1, got, w)
		}
	}
	// a blob given by value lives only during its call, and says so
	g, err := s.Func1[blob, string]("g")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := g(context.Background(), blob{}); err != nil || !strings.HasSuffix(got, "(cannot convert userdata to *glua.blob: the value lives only during the call)") {
		t.Errorf("a call-scoped blob: %q, %v", got, err)
	}
}

func TestHostNesting(t *testing.T) {
	// host, Lua and host again: the outer function still sees its own arguments
	var inner GoFunc = func(c *Call) error { return c.Return(c.Arg[string](0) + "!") }
	outer := GoFunc(func(c *Call) error {
		before := c.Arg[string](0)
		r, err := c.Invoke(c.Value(1), String("in"))
		if err != nil {
			return err
		}
		after := c.Arg[string](0)
		s, _ := r.Str()
		return c.Return(before + "|" + s + "|" + after)
	})
	rt := MustNew(Global("inner", inner), Global("outer", outer))
	s := mustLoad(t, rt, "function f() return outer('out', function(x) return inner(x) end) end")
	f, _ := s.Func0[string]("f")
	if got, err := f(context.Background()); err != nil || got != "out|in!|out" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestTerminalStands(t *testing.T) {
	swallow := GoFunc(func(c *Call) error {
		c.Invoke(c.Value(0)) // the step limit hits inside; the error is dropped
		return nil
	})
	rt := MustNew(Limits{Steps: 5000}, Global("swallow", swallow))
	s := mustLoad(t, rt, "function f() swallow(function() while true do end end) return 'escaped' end")
	f, _ := s.Func0[string]("f")
	if got, err := f(context.Background()); !errors.Is(err, ErrSteps) {
		t.Errorf("got %q, %v", got, err)
	}
	call := Global("call", GoFunc(func(c *Call) error {
		r, err := c.Invoke(c.Value(0), c.Value(1))
		if err != nil {
			return err
		}
		return c.ReturnValues(r)
	}))
	deep := mustLoad(t, MustNew(Limits{HostDepth: 10}, call), "function f(n) return pcall(call, f, n + 1) end")
	g, _ := deep.Func1[int64, bool]("f")
	if _, err := g(context.Background(), 0); !errors.Is(err, ErrHostDepth) {
		t.Errorf("host depth: %v", err)
	}
}

func TestAdapterPanicsOnBadTypes(t *testing.T) {
	for _, f := range []func(){
		func() { Fn1(func(chan int) int { return 0 }) },
		func() { Fn0(func() chan int { return nil }) },
		func() { Reflect(42) },
		func() { Reflect(func(chan int) {}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("no panic")
				}
			}()
			f()
		}()
	}
}

func TestHostCallNoAlloc(t *testing.T) {
	if raceEnabled || stackStress {
		t.Skip("sync.Pool drops items in race builds; lua_stackstress allocates stacks")
	}
	rt := MustNew(Global("add1", Fn1(func(x int64) int64 { return x + 1 })))
	s := mustLoad(t, rt, "function f(x) return add1(x) end")
	f, _ := s.Func1[int64, int64]("f")
	ctx := context.Background()
	if a := testing.AllocsPerRun(1000, func() { f(ctx, 1) }); a != 0 {
		t.Errorf("a host call allocates %v times", a)
	}
}

type (
	planBlob  struct{ N int }
	planOther struct{ N int }
	planLevel int64
	planRatio float64
	planName  string
	planFlag  bool
	planTemp  int64 // has a conversion, registered after the adapter is made
)

// planPair is a typed adapter for f and a GoFunc that decodes its argument with Arg, by the
// general rules: the adapter's planned decoding must give the same results and errors.
func planPair[T any](name string, f func(T) string) []Option {
	return []Option{
		Global(name, Fn1(f)),
		Global(name+"_arg", GoFunc(func(c *Call) error { return c.Return(f(c.Arg[T](0))) })),
	}
}

func TestTypedAdapterPlans(t *testing.T) {
	str := func(v any) string { return fmt.Sprint(v) }
	var opts []Option
	opts = append(opts, planPair("ptr", func(b *planBlob) string { return str(b) })...)
	opts = append(opts, planPair("val", func(b planBlob) string { return str(b) })...)
	opts = append(opts, planPair("lvl", func(l planLevel) string { return str(l) })...)
	opts = append(opts, planPair("ratio", func(r planRatio) string { return str(r) })...)
	opts = append(opts, planPair("name", func(n planName) string { return str(n) })...)
	opts = append(opts, planPair("flag", func(f planFlag) string { return str(f) })...)
	opts = append(opts,
		Global("temp", Fn1(func(c planTemp) planTemp { return c + 1 })), // planned before Convert
		Convert(func(c planTemp) Value { return String(fmt.Sprintf("%dC", c)) }, func(v Value) (planTemp, error) {
			var c planTemp
			_, err := fmt.Sscanf(v.String(), "%dC", &c)
			return c, err
		}),
		Global("newblob", Fn0(func() *planBlob { return &planBlob{N: 7} })),
		Global("blobval", Fn0(func() planBlob { return planBlob{N: 8} })),
		Global("newother", Fn0(func() *planOther { return &planOther{} })))
	s := mustLoad(t, MustNew(opts...), `
		local function show(ok, v) return tostring(ok) .. " " .. tostring(v) end
		local inputs = {1, 2.5, 2^63, "3", "x", true, {n = 1}, {n = "x"}}
		function compare(extra)
			local all = {newblob(), blobval(), newother(), extra}
			for _, v in ipairs(inputs) do all[#all + 1] = v end
			local bad = {}
			for _, f in ipairs{"ptr", "val", "lvl", "ratio", "name", "flag"} do
				for i = 1, #all + 1 do -- the last one is missing
					local a = show(pcall(_ENV[f], all[i]))
					local b = show(pcall(_ENV[f .. "_arg"], all[i]))
					if a ~= b:gsub(f .. "_arg", f) then
						bad[#bad + 1] = f .. "(" .. tostring(all[i]) .. "): " .. a .. " | " .. b
					end
				end
			end
			return table.concat(bad, "\n")
		end
		function temp20() return temp("20C") end`)
	ctx := context.Background()
	compare, err := s.Func1[planBlob, string]("compare") // a struct by value: a call-scoped userdata
	if err != nil {
		t.Fatal(err)
	}
	if bad, err := compare(ctx, planBlob{N: 9}); err != nil || bad != "" {
		t.Errorf("adapters and Arg differ:\n%s\n%v", bad, err)
	}
	if got, err := s.Call(ctx, s.Globals().Get(String("temp20"))); err != nil || got.String() != "21C" {
		t.Errorf("a conversion registered after the adapter: %v, %v", got, err)
	}
}

// A typed function's result decodes from a userdata of its type without reflection, and from
// anything else as before.
func TestTypedResultPlans(t *testing.T) {
	type blob struct{ N int }
	type other struct{ N int }
	rt := MustNew(
		Global("newblob", Fn0(func() *blob { return &blob{N: 7} })),
		Global("newother", Fn0(func() *other { return &other{} })))
	s := mustLoad(t, rt, `
		function ptr() return newblob() end
		function tab() return {n = 3} end
		function none() end
		function wrong() return newother() end`)
	ctx := context.Background()
	for name, want := range map[string]string{"ptr": "&{7} <nil>", "tab": "&{3} <nil>", "none": "<nil> <nil>",
		"wrong": "<nil> glua: cannot convert userdata to *glua.blob: a userdata of another type"} {
		f := must(s.Func0[*blob](name))
		if got := fmt.Sprint(f(ctx)); got != want {
			t.Errorf("%s as *blob: %s, want %s", name, got, want)
		}
	}
	for name, want := range map[string]string{"ptr": "{7} <nil>", "tab": "{3} <nil>",
		"wrong": "{0} glua: cannot convert userdata to glua.blob: a userdata of another type"} {
		f := must(s.Func0[blob](name))
		if got := fmt.Sprint(f(ctx)); got != want {
			t.Errorf("%s as blob: %s, want %s", name, got, want)
		}
	}
}
