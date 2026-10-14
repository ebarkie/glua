package glua

import (
	"context"
	"fmt"
)

// GoFunc is the one native signature for host functions. Everything else adapts to it.
type GoFunc func(c *Call) error

// Call is the view a host function has of its invocation. It holds indices, never
// addresses, so it survives stack growth. It is valid only until the function returns,
// and only on the goroutine that received it.
type Call struct {
	ex    *executor
	base  int // index of the first argument
	nargs int
	res   []Value // results, copied to the stack when the function returns

	fromLua bool // called by the VM for Lua code, not by pcall or another Go function
}

func (c *Call) reset() {
	clear(c.res[:cap(c.res)])
	c.res = c.res[:0]
}

// Context returns the context of the outermost entry from Go.
func (c *Call) Context() context.Context { return c.ex.ctx }

// Len returns the number of arguments.
func (c *Call) Len() int { return c.nargs }

// Value returns argument i, counting from 0, or nil if there is no such argument.
func (c *Call) Value(i int) Value {
	if uint(i) >= uint(c.nargs) {
		return Value{}
	}
	return c.ex.stack[c.base+i]
}

// ReturnValues sets the function's results. The last call wins.
func (c *Call) ReturnValues(vs ...Value) error {
	c.res = append(c.res[:0], vs...)
	return nil
}

// Errorf returns an error with a formatted message, for a host function to return.
func (c *Call) Errorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// return1 sets a single result without the variadic slice.
func (c *Call) return1(v Value) {
	c.res = append(c.res[:0], v)
}

// Invoke calls fn with args from inside a host function and returns its first result.
// It shares the budgets of the outermost entry.
func (c *Call) Invoke(fn Value, args ...Value) (Value, error) {
	ex := c.ex
	if !fn.isObj(kindLuaFn) {
		return Value{}, ex.errorf("attempt to call a %s value", typeName(fn))
	}
	if ex.depth >= ex.maxHostDepth {
		return Value{}, ex.terminal(ErrHostDepth)
	}
	st := &ex.st
	cl, base, pc := st.cl, st.base, st.pc
	slot := max(base+int(cl.p.maxstack), c.base+c.nargs) // first free slot above the calling Lua frame
	if err := ex.ensure(slot + 1 + len(args)); err != nil {
		return Value{}, err
	}
	copy(ex.stack[slot+1:], args)
	ex.depth++
	ex.nny++
	err := ex.run((*Closure)(fn.p), slot, len(args))
	ex.nny--
	ex.depth--
	st.cl, st.base, st.pc = cl, base, pc
	if err != nil {
		return Value{}, err
	}
	var r Value
	if ex.top > slot {
		r = ex.stack[slot]
	}
	return r, nil
}

// hostCall calls the Go function of the CALL at pc-1.
func (ex *executor) hostCall() error {
	i := ex.instr(ex.st.pc - 1)
	fn := ex.st.base + decA(i)
	nargs := decB(i) - 1
	if decB(i) == 0 {
		var err error
		if nargs, err = ex.openCount(fn + 1); err != nil {
			return err
		}
	}
	return ex.callGo((*goFunc)(ex.stack[fn].p), fn, nargs)
}

// callGo calls g with the nargs arguments above stack[fn], for the CALL at pc-1, and puts
// its results where that CALL expects them.
func (ex *executor) callGo(g *goFunc, fn, nargs int) error {
	if g == builtinPcall || g == builtinXpcall {
		return ex.pcallValue(g, fn, nargs)
	}
	n, err := ex.callGoRawFrom(g, fn, nargs, true)
	if err == errYield {
		return ex.yieldedAt(fn, yieldCall)
	}
	if err != nil {
		return err
	}
	ex.adjustResults(ex.instr(ex.st.pc-1), fn, n)
	return nil
}

// callGoRaw calls g with the nargs arguments above stack[fn] and leaves its n results at
// stack[fn]. Call records live in a slice indexed by host-call depth, so nesting allocates
// nothing and each host frame has its own record.
func (ex *executor) callGoRaw(g *goFunc, fn, nargs int) (int, error) {
	return ex.callGoRawFrom(g, fn, nargs, false)
}

// callGoRawFrom is callGoRaw; fromLua tells the function that the VM called it for Lua code,
// which error's level argument needs.
func (ex *executor) callGoRawFrom(g *goFunc, fn, nargs int, fromLua bool) (int, error) {
	if ex.depth >= ex.maxHostDepth {
		return 0, ex.terminal(ErrHostDepth)
	}
	for len(ex.calls) <= ex.depth { // Invoke also counts as depth, so this may skip a record
		ex.calls = append(ex.calls, &Call{ex: ex})
	}
	c := ex.calls[ex.depth] // one record per depth, so a nested call cannot overwrite its caller's
	c.base, c.nargs, c.fromLua = fn+1, nargs, fromLua
	st := &ex.st
	cl, base, pc := st.cl, st.base, st.pc // the host function may re-enter the VM
	ex.depth++
	ex.nny++
	ex.hostCalls++
	err := g.f(c)
	ex.hostCalls--
	ex.nny--
	ex.depth--
	st.cl, st.base, st.pc = cl, base, pc
	if err == errYield {
		c.reset()
		return 0, err // coroutine.yield: the caller records where the results go
	}
	if err != nil {
		c.reset()
		return 0, ex.hostError(err)
	}
	n := len(c.res)
	if err := ex.ensure(fn + n); err != nil {
		return 0, err
	}
	copy(ex.stack[fn:], c.res)
	c.reset()
	st.hi = max(st.hi, fn+n)
	return n, nil
}
