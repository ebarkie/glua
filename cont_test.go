package glua

import (
	"context"
	"strings"
	"testing"
)

// function() return 1, 2, 3 end
func progThree() *Proto {
	a := newAsm("three", 0, 3)
	a.loadI(0, 1)
	a.loadI(1, 2)
	a.loadI(2, 3)
	a.abc(opReturn, 0, 4, 0)
	return a.done()
}

// echo returns the number of its arguments, then the arguments themselves.
var echo = &goFunc{name: "echo", f: func(c *Call) error {
	res := []Value{Int(int64(c.Len()))}
	for i := range c.Len() {
		res = append(res, c.Value(i))
	}
	return c.ReturnValues(res...)
}}

func ints(t *testing.T, vs []Value) []int64 {
	t.Helper()
	var out []int64
	for _, v := range vs {
		i, ok := v.Int()
		if !ok {
			if v.IsNil() {
				out = append(out, -1)
				continue
			}
			t.Fatalf("not an integer: %v", v)
		}
		out = append(out, i)
	}
	return out
}

func equalInts(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// return echo(three()): an open result list feeds an open argument list, and the host
// function's results go back open.
func TestOpenCallThroughHost(t *testing.T) {
	a := newAsm("main", 0, 2)
	a.upval(false, 0)
	a.abc(opGetUpval, 0, 0, 0)
	a.abx(opClosure, 1, a.proto(progThree()))
	a.abc(opCall, 1, 1, 0) // three() with open results: top = R1+3
	a.abc(opCall, 0, 0, 0) // echo(R1..top) with open results
	a.abc(opReturn, 0, 0, 0)
	res, err := runClosure(context.Background(), newExecutor(), closureOf(a.done(), goFnValue(echo)))
	if err != nil {
		t.Fatal(err)
	}
	if got := ints(t, res); !equalInts(got, []int64{3, 1, 2, 3}) {
		t.Fatalf("got %v", got)
	}
}

// local function f(a, b, c) return a, b, c end; return f(three())
// An open argument list into a Lua function, and a general return of an open list.
func TestOpenCallIntoLua(t *testing.T) {
	f := newAsm("f", 3, 3)
	f.abc(opReturn, 0, 4, 0)
	a := newAsm("main", 0, 2)
	a.abx(opClosure, 0, a.proto(f.done()))
	a.abx(opClosure, 1, a.proto(progThree()))
	a.abc(opCall, 1, 1, 0)
	a.abc(opCall, 0, 0, 0)
	a.abc(opReturn, 0, 0, 0)
	res, err := runProto(t, a.done())
	if err != nil {
		t.Fatal(err)
	}
	if got := ints(t, res); !equalInts(got, []int64{1, 2, 3}) {
		t.Fatalf("got %v", got)
	}
}

// The caller wants another count than the callee returns: drop extra results, pad with nil.
func TestResultAdjustment(t *testing.T) {
	one := newAsm("one", 0, 1)
	one.loadI(0, 7)
	one.abc(opReturn1, 0, 0, 0) // the inline return, which exits with exResults
	for _, tt := range []struct {
		name   string
		callee *Proto
		c      int
		want   []int64
	}{
		{"three to one", progThree(), 2, []int64{1}},
		{"three to three", progThree(), 4, []int64{1, 2, 3}},
		{"one to three", one.done(), 4, []int64{7, -1, -1}},
		{"three to none", progThree(), 1, nil},
	} {
		// Registers above the wanted results are undefined after the call, as in C Lua, so
		// return only the ones the call defines.
		a := newAsm("main", 0, 4)
		a.abc(opLoadTrue, 1, 0, 0) // poison: padding must overwrite these
		a.abc(opLoadTrue, 2, 0, 0)
		a.abx(opClosure, 0, a.proto(tt.callee))
		a.abc(opCall, 0, 1, tt.c)
		a.abc(opReturn, 0, tt.c, 0)
		res, err := runProto(t, a.done())
		if err != nil {
			t.Fatal(tt.name, err)
		}
		if got := ints(t, res); !equalInts(got, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

// A field read hands its work to a Lua __index function in a continuation frame. When the
// function returns, the driver finishes the GETFIELD, and the loop carries on.
func TestIndexContinuation(t *testing.T) {
	// function(t, k) return 41 end
	h := newAsm("__index", 2, 3)
	h.loadI(2, 41)
	h.abc(opReturn1, 2, 0, 0)
	handler := luaFnValue(closureOf(h.done()))

	// function(t) local v = t.x; return v + 1 end
	a := newAsm("main", 1, 2)
	a.field(opGetField, 1, 0, int(a.kstr("x")))
	a.abc(opAddK, 1, 1, int(a.kint(1)))
	a.abc(opReturn1, 1, 0, 0)
	p := a.done()

	meta := NewTable(0, 1)
	meta.Set(String("__index"), handler)
	tbl := NewTable(0, 1)
	tbl.meta = meta

	res, err := runProto(t, p, tableValue(tbl))
	if err != nil {
		t.Fatal(err)
	}
	if got := ints(t, res); !equalInts(got, []int64{42}) {
		t.Fatalf("got %v", got)
	}

	// A present key needs no metamethod; then deleting it must consult __index again,
	// although the site's hint still points at the key's slot.
	tbl.Set(String("x"), Int(1))
	for range 3 {
		res, err = runProto(t, p, tableValue(tbl))
		if err != nil || ints(t, res)[0] != 2 {
			t.Fatalf("present key: %v %v", res, err)
		}
	}
	tbl.Set(String("x"), Value{})
	res, err = runProto(t, p, tableValue(tbl))
	if err != nil || ints(t, res)[0] != 42 {
		t.Fatalf("deleted key: %v %v", res, err)
	}
}

// __index as a table, a chain of them, a handler that returns nothing, and a loop.
func TestIndexChains(t *testing.T) {
	a := newAsm("getx", 1, 2)
	a.field(opGetField, 1, 0, int(a.kstr("x")))
	a.abc(opReturn1, 1, 0, 0)
	p := a.done()

	withIndex := func(idx Value) *Table {
		m := NewTable(0, 1)
		m.Set(String("__index"), idx)
		t := NewTable(0, 1)
		t.meta = m
		return t
	}
	base := NewTable(0, 1)
	base.Set(String("x"), Int(5))
	chain := withIndex(tableValue(withIndex(tableValue(base))))
	res, err := runProto(t, p, tableValue(chain))
	if err != nil || ints(t, res)[0] != 5 {
		t.Fatalf("chain: %v %v", res, err)
	}

	none := newAsm("none", 2, 2)
	none.abc(opReturn0, 0, 0, 0)
	res, err = runProto(t, p, tableValue(withIndex(luaFnValue(closureOf(none.done())))))
	if err != nil || !res[0].IsNil() {
		t.Fatalf("no results: %v %v", res, err)
	}

	loop := NewTable(0, 1)
	m := NewTable(0, 1)
	m.Set(String("__index"), tableValue(loop))
	loop.meta = m
	_, err = runProto(t, p, tableValue(loop))
	if err == nil || !strings.Contains(err.Error(), "'__index' chain too long") {
		t.Fatalf("loop: %v", err)
	}
}

// An error inside the __index handler unwinds through the continuation frame, and the
// executor is clean afterwards.
func TestIndexContinuationError(t *testing.T) {
	h := newAsm("__index", 2, 3)
	h.abc(opLoadNil, 2, 0, 0)
	h.abc(opCall, 2, 1, 1) // call nil
	h.abc(opReturn0, 0, 0, 0)
	meta := NewTable(0, 1)
	meta.Set(String("__index"), luaFnValue(closureOf(h.done())))
	tbl := NewTable(0, 1)
	tbl.meta = meta

	a := newAsm("getx", 1, 2)
	a.field(opGetField, 1, 0, int(a.kstr("x")))
	a.abc(opReturn1, 1, 0, 0)
	ex := newExecutor()
	_, err := runClosure(context.Background(), ex, closureOf(a.done()), tableValue(tbl))
	if err == nil || !strings.Contains(err.Error(), "attempt to call a nil value") {
		t.Fatalf("got %v", err)
	}
	if ex.st.nf != 0 {
		t.Fatalf("%d frames left after the error", ex.st.nf)
	}
	for i, v := range ex.stack {
		if v.p != nil {
			t.Fatalf("register %d still holds %v", i, v)
		}
	}
}
