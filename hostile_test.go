package glua

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// Hostile scripts against the fixed caps, which hold even with NoLimit because what they
// prevent would kill the process: Go stack exhaustion, an allocation the machine cannot
// make, or a loop that never ends.

var noLimits = Limits{Steps: NoLimit, AllocBytes: NoLimit, CallDepth: NoLimit, HostDepth: NoLimit,
	InitSteps: NoLimit, InitAllocBytes: NoLimit}

// hostile loads src on a Runtime with every limit off but the given steps, and runs f.
func hostile(t *testing.T, src string, opts ...Option) (Value, error) {
	t.Helper()
	opts = append([]Option{LibSafe | LibLoad | LibCoroutine, noLimits}, opts...)
	rt := MustNew(opts...)
	s, err := rt.Load(context.Background(), "hostile.lua", src)
	if err != nil {
		return Value{}, err
	}
	f, err := s.Func0[Value]("f")
	if err != nil {
		t.Fatal(err)
	}
	return f(context.Background())
}

func TestFixedCaps(t *testing.T) {
	deepParens := strings.Repeat("(", 300) + "1" + strings.Repeat(")", 300)
	deepTables := strings.Repeat("{", 300) + strings.Repeat("}", 300)
	deepBlocks := strings.Repeat("do ", 300) + strings.Repeat("end ", 300)
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"syntax nesting, expressions", "x = " + deepParens, "chunk has too many syntax levels"},
		{"syntax nesting, tables", "x = " + deepTables, "chunk has too many syntax levels"},
		{"syntax nesting, blocks", deepBlocks, "chunk has too many syntax levels"},
		{"syntax nesting through load", "function f() return select(2, load('return ' .. string.rep('(', 300) .. '1' .. string.rep(')', 300))) end",
			"chunk has too many syntax levels"},
		{"pattern recursion", "function f() return string.find(string.rep('a', 1000), string.rep('a?', 1000) .. string.rep('a', 1000)) end",
			"pattern too complex"},
		{"pattern recursion with captures", "function f() return string.match(string.rep('(', 1), string.rep('(%(?)', 33)) end",
			"too many captures"},
		{"__index chain", "function f() local t = {} setmetatable(t, {__index = t}) return t.x end", "'__index' chain too long"},
		{"__index chain through tables", `function f()
			local t = {} for i = 1, 3000 do t = setmetatable({}, {__index = t}) end return t.x end`, "'__index' chain too long"},
		{"__newindex chain", "function f() local t = {} setmetatable(t, {__newindex = t}) t.x = 1 end", "'__newindex' chain too long"},
		{"__call chain", "function f() local t = {} setmetatable(t, {__call = t}) return t() end", "stack overflow"},
		{"register stack", "local function r(n) return 1 + r(n) end function f() return r(1) end", "stack overflow"},
		{"register stack, caught", "local function r(n) return 1 + r(n) end function f() return select(2, pcall(r, 1)) end", ""},
		{"vararg stack", "local function r(...) return 1 + r(1, 2, 3, ...) end function f() return r() end", "stack overflow"},
		{"unpack", "function f() return table.unpack({}, 1, 1e8) end", "too many results"},
		{"1 GiB string.rep", "function f() return string.rep('x', 1 << 31) end", "resulting string too large"},
		{"1 GiB format", "function f() return string.format('%s%s', string.rep('x', 1 << 20), 1) .. string.rep('y', 1 << 30) end", "allocation limit exceeded"},
		{"format width", "function f() return string.format('%100d', 1) end", "invalid conversion"},
		{"format precision", "function f() return string.format('%.100f', 1) end", "invalid conversion"},
		{"coroutines inside coroutines", `local function nest(n) return coroutine.wrap(function() return nest(n + 1)() end) end
			function f() return nest(1)() end`, "C stack overflow"},
		{"host depth through load", "function f() local function r() return load('return ...')(r()) end return r() end", "stack overflow"},
	} {
		start := time.Now()
		v, err := hostile(t, tc.src)
		if d := time.Since(start); d > time.Second {
			t.Logf("%s took %v", tc.name, d)
		}
		got := ""
		if err != nil {
			got = err.Error()
		} else if v.isString() {
			got = v.str()
		}
		if tc.want == "" {
			if !strings.Contains(got, "stack overflow") {
				t.Errorf("%s: got %q, a caught stack overflow expected", tc.name, got)
			}
			continue
		}
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestHostDepthCapWithNoLimit(t *testing.T) {
	call := Global("call", GoFunc(func(c *Call) error {
		r, err := c.Invoke(c.Value(0), c.Value(1))
		if err != nil {
			return err
		}
		return c.ReturnValues(r)
	}))
	_, err := hostile(t, "function f(n) return call(f, (n or 0) + 1) end", call)
	if !errors.Is(err, ErrHostDepth) {
		t.Errorf("got %v", err)
	}
}

func TestConversionDepthCap(t *testing.T) {
	// a Go value nested past the cap
	var deep any = 1
	for range 100 {
		deep = []any{deep}
	}
	if _, err := toValue(deep); err == nil || !strings.Contains(err.Error(), "nested more than 64 levels") {
		t.Errorf("encode: %v", err)
	}
	// a Go value that contains itself
	cyc := map[string]any{}
	cyc["self"] = cyc
	if _, err := toValue(cyc); err == nil || !strings.Contains(err.Error(), "nested more than 64 levels") {
		t.Errorf("encode a cycle: %v", err)
	}
	// a Lua table that contains itself, decoded into any
	res, err := runSource("cyc", "local t = {} t[1] = t return t")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := res[0].As[any](); err == nil || !strings.Contains(err.Error(), "nested more than 64 levels") {
		t.Errorf("decode a cycle: %v", err)
	}
	type node struct {
		Next *node
	}
	if _, err := res[0].As[[][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][][]int](); err == nil {
		t.Error("a deep slice type decoded a cycle")
	}
}

func TestLoadAbuse(t *testing.T) {
	rt := MustNew(LibSafe|LibLoad, Limits{Steps: 1_000_000, AllocBytes: 1 << 20})
	for _, src := range []string{
		// source bytes are charged
		"function f() local s = string.rep('-', 1 << 19) for i = 1, 10 do load(s) end end",
		// a reader function that never ends
		"function f() return load(function() return 'x = 1 ' end) end",
	} {
		s := mustLoad(t, rt, src)
		f, _ := s.Func0[Value]("f")
		if _, err := f(context.Background()); !errors.Is(err, ErrAlloc) && !errors.Is(err, ErrSteps) {
			t.Errorf("%s: %v", src, err)
		}
	}
}
