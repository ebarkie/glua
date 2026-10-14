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

	fromLua bool    // called by the VM for Lua code, not by pcall or another Go function
	fn      *goFunc // the function being called, for messages

	unwinding bool  // Arg is panicking with argErr
	argErr    error // the argument error Arg raised
}

func (c *Call) reset() {
	clear(c.res[:cap(c.res)])
	c.res = c.res[:0]
	c.unwinding, c.argErr = false, nil
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

// Invoke calls fn with args from inside a host function and returns its first result. It
// shares the budgets of the outermost entry.
func (c *Call) Invoke(fn Value, args ...Value) (Value, error) {
	var buf [1]Value
	res, err := c.ex.callSyncInto(buf[:0], fn, args)
	if err != nil || len(res) == 0 {
		return Value{}, err
	}
	return res[0], nil
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
	switch g {
	case builtinPcall, builtinXpcall:
		return ex.pcallValue(g, fn, nargs)
	case builtinPairs:
		if h := ex.metamethod(ex.stack[fn+1], "__pairs"); nargs > 0 && !h.IsNil() {
			ex.stack[fn], ex.stack[fn+1], ex.stack[fn+2] = luaFnValue(pairsMeta), h, ex.stack[fn+1]
			ex.st.hi = max(ex.st.hi, fn+3)
			return ex.pushLua(pairsMeta, fn, 2, 0)
		}
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
	c.base, c.nargs, c.fromLua, c.fn = fn+1, nargs, fromLua, g
	st := &ex.st
	cl, base, pc := st.cl, st.base, st.pc // the host function may re-enter the VM
	ex.depth++
	ex.nny++
	ex.hostCalls++
	err := invokeGo(g, c)
	ex.hostCalls--
	ex.nny--
	ex.depth--
	st.cl, st.base, st.pc = cl, base, pc
	if ex.fatal != nil && err != errYield {
		err = ex.fatal // a terminal error stands, whatever the function did with it
	}
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

// invokeGo calls the host function g. An argument error that Arg raised by panic is
// recovered here; any other panic passes through untouched, with its stack.
func invokeGo(g *goFunc, c *Call) (err error) {
	defer func() {
		if c.unwinding {
			recover()
			c.unwinding = false
			err = c.argErr
		}
	}()
	return g.f(c)
}
