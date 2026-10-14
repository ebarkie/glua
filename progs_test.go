package glua

import (
	"context"
	"testing"
)

// The spike's programs, assembled by hand. Each comment gives the Lua they stand for.

// local s = 0.0; for i = 1, n do s = s + i * 1.5 end; return s
func progFloatLoop(n int64) *Proto {
	a := newAsm("floatloop", 0, 6)
	a.abx(opLoadK, 0, a.konst(Float(0)))
	a.loadI(1, 1)
	a.abx(opLoadK, 2, a.konst(Int(n)))
	a.loadI(3, 1)
	a.forPrep(1, "exit")
	a.label("body")
	a.abc(opMulK, 5, 4, int(a.kfloat(1.5)))
	a.abc(opAdd, 0, 0, 5)
	a.forLoop(1, "body")
	a.label("exit")
	a.abc(opReturn1, 0, 0, 0)
	return a.done()
}

// local function fib(n) if n < 2 then return n end return fib(n-1) + fib(n-2) end
func progFibProto() *Proto {
	a := newAsm("fib", 1, 4)
	a.upval(true, 0) // fib captures itself from the enclosing function's R0
	a.cjump(opLtKJmp, 0, int(a.kint(2)), "rec")
	a.abc(opReturn1, 0, 0, 0)
	a.label("rec")
	a.abc(opGetUpval, 1, 0, 0)
	a.abc(opSubK, 2, 0, int(a.kint(1)))
	a.abc(opCall, 1, 2, 2)
	a.abc(opGetUpval, 2, 0, 0)
	a.abc(opSubK, 3, 0, int(a.kint(2)))
	a.abc(opCall, 2, 2, 2)
	a.abc(opAdd, 1, 1, 2)
	a.abc(opReturn1, 1, 0, 0)
	return a.done()
}

// <fib>; return fib(n)
func progFib(n int64) *Proto {
	a := newAsm("fibmain", 0, 3)
	a.abx(opClosure, 0, a.proto(progFibProto()))
	a.abc(opMove, 1, 0, 0)
	a.abx(opLoadK, 2, a.konst(Int(n)))
	a.abc(opCall, 1, 2, 2)
	a.abc(opReturn1, 1, 0, 0)
	return a.done()
}

// local t = {x = 1}; local s = 0; for i = 1, n do s = s + t.x end; return s
func progFieldRead(n int64) *Proto {
	a := newAsm("fieldread", 0, 7)
	a.newTable(0, 0, 1)
	a.loadI(1, 1)
	a.field(opSetField, 0, int(a.kstr("x")), 1)
	a.loadI(1, 0)
	a.loadI(2, 1)
	a.abx(opLoadK, 3, a.konst(Int(n)))
	a.loadI(4, 1)
	a.forPrep(2, "exit")
	a.label("body")
	a.field(opGetField, 6, 0, int(a.kstr("x")))
	a.abc(opAdd, 1, 1, 6)
	a.forLoop(2, "body")
	a.label("exit")
	a.abc(opReturn1, 1, 0, 0)
	return a.done()
}

// function(x) return x + 1 end
func progAdd1() *Proto {
	a := newAsm("add1", 1, 2)
	a.abc(opAddK, 1, 0, int(a.kint(1)))
	a.abc(opReturn1, 1, 0, 0)
	return a.done()
}

// function(s) return s end
func progIdentity() *Proto {
	a := newAsm("identity", 1, 1)
	a.abc(opReturn1, 0, 0, 0)
	return a.done()
}

// function(r) return r.method == "GET" and r.size < 1000 and r.admin end
func progRule() *Proto {
	a := newAsm("rule", 1, 2)
	a.field(opGetField, 1, 0, int(a.kstr("method")))
	a.cjump(opEqKJmp, 1, int(a.kstr("GET")), "false")
	a.field(opGetField, 1, 0, int(a.kstr("size")))
	a.cjump(opLtKJmp, 1, int(a.kint(1000)), "false")
	a.field(opGetField, 1, 0, int(a.kstr("admin")))
	a.abc(opReturn1, 1, 0, 0)
	a.label("false")
	a.abc(opLoadFalse, 1, 0, 0)
	a.abc(opReturn1, 1, 0, 0)
	return a.done()
}

// function(x) return f(x) end, where f is the closure's upvalue 0
func progCallHost() *Proto {
	a := newAsm("callhost", 1, 3)
	a.upval(false, 0)
	a.abc(opGetUpval, 1, 0, 0)
	a.abc(opMove, 2, 0, 0)
	a.abc(opCall, 1, 2, 2)
	a.abc(opReturn1, 1, 0, 0)
	return a.done()
}

// local c, last = 0, nil; for i = init, limit, step do c = c + 1; last = i end; return c, last
func progForCount(init, limit, step Value) *Proto {
	a := newAsm("forcount", 0, 7)
	a.loadI(0, 0)
	a.abc(opLoadNil, 1, 0, 0)
	a.abx(opLoadK, 2, a.konst(init))
	a.abx(opLoadK, 3, a.konst(limit))
	a.abx(opLoadK, 4, a.konst(step))
	a.forPrep(2, "exit")
	a.label("body")
	a.abc(opAddK, 0, 0, int(a.kint(1)))
	a.abc(opMove, 1, 5, 0)
	a.forLoop(2, "body")
	a.label("exit")
	a.abc(opReturn, 0, 3, 0)
	return a.done()
}

// for i = 1, n do end
func progEmptyLoop(n int64) *Proto {
	a := newAsm("emptyloop", 0, 4)
	a.loadI(0, 1)
	a.abx(opLoadK, 1, a.konst(Int(n)))
	a.loadI(2, 1)
	a.forPrep(0, "exit")
	a.label("body")
	a.forLoop(0, "body")
	a.label("exit")
	a.abc(opReturn0, 0, 0, 0)
	return a.done()
}

func closureOf(p *Proto, up ...Value) *Closure {
	return &Closure{p: p, up: up}
}

// runProto runs p on a fresh executor and returns its results.
func runProto(t testing.TB, p *Proto, args ...Value) ([]Value, error) {
	t.Helper()
	return runClosure(context.Background(), newExecutor(), closureOf(p), args...)
}

func runClosure(ctx context.Context, ex *executor, cl *Closure, args ...Value) ([]Value, error) {
	copy(ex.stack[1:], args)
	err := ex.call(ctx, cl, 0, len(args))
	var res []Value
	if err == nil {
		res = append(res, ex.stack[:ex.top]...)
	}
	ex.reset()
	return res, err
}
