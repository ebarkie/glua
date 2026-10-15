package glua

import (
	"context"
	"fmt"
	"sync/atomic"
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
	clear(c.res) // past len, every slot is nil already: each reset clears what was used
	c.res = c.res[:0]
	c.unwinding, c.argErr = false, nil
}

// Context returns the context of the outermost entry from Go. It carries what is left of
// the entry's budgets: a Script called with it from the host function shares them.
func (c *Call) Context() context.Context {
	ex := c.ex
	e := ex.ectx
	if e == nil {
		e = &entryCtx{Context: ex.ctx}
		ex.ectx = e
		liveEntryCtx.Add(1)
	}
	e.steps.Store(ex.steps + int64(max(ex.st.ticks, 0)))
	e.alloc.Store(ex.allocLeft)
	e.depth.Store(int64(ex.maxHostDepth - ex.depth))
	return e
}

// entryCtx is the context host functions see: the entry's own, carrying what was left of its
// budgets when Context was last called. Entries made with it take at most that, and report
// what they used, which the entry pays when the host function returns. The fields are
// atomic because a host function may hand the context to another goroutine.
type entryCtx struct {
	context.Context
	steps, alloc, depth  atomic.Int64
	usedSteps, usedAlloc atomic.Int64
}

type budgetKey struct{}

// liveEntryCtx counts the entries whose host functions asked for their context. While it
// is zero, no context can carry a budget, and entries skip looking for one.
var liveEntryCtx atomic.Int64

// Value returns the entry's budgets for budgetKey, and otherwise the parent context's value.
func (e *entryCtx) Value(k any) any {
	if k == (budgetKey{}) {
		return e
	}
	return e.Context.Value(k)
}

// inherit limits an entry made with ctx to what its parent entry had left, if ctx came from
// a host function's Call. It returns the parent's context, or nil.
func (ex *executor) inherit(ctx context.Context) *entryCtx {
	if liveEntryCtx.Load() == 0 {
		return nil
	}
	e, _ := ctx.Value(budgetKey{}).(*entryCtx)
	if e == nil {
		return nil
	}
	ex.maxSteps = min(ex.maxSteps, e.steps.Load())
	ex.maxAlloc = min(ex.maxAlloc, e.alloc.Load())
	ex.maxHostDepth = min(ex.maxHostDepth, int(e.depth.Load())-1)
	return e
}

// payBorrowed charges the entry for what nested entries made with its Context used.
func (ex *executor) payBorrowed() error {
	e := ex.ectx
	if s := e.usedSteps.Swap(0); s > 0 {
		if err := ex.spend(int(min(s, 1<<62))); err != nil {
			return err
		}
	}
	if a := e.usedAlloc.Swap(0); a > 0 {
		return ex.charge(int(min(a, maxAlloc)))
	}
	return nil
}

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

// returnArgs makes the arguments from index from on the results, without allocating.
func (c *Call) returnArgs(from int) error {
	c.res = append(c.res[:0], c.ex.stack[c.base+from:c.base+c.nargs]...)
	return nil
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
	var err error
	if g.host {
		err = invokeGo(g, c)
	} else {
		err = g.f(c) // library functions return their argument errors; they never panic with them
	}
	ex.hostCalls--
	ex.nny--
	ex.depth--
	st.cl, st.base, st.pc = cl, base, pc
	if ex.ectx != nil && err == nil {
		err = ex.payBorrowed()
	}
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
	if g.host {
		for _, v := range c.res { // what the host made for the script counts toward its budget
			if v.isString() || v.isObj(kindTable) {
				if err := ex.chargeResult(v); err != nil {
					c.reset()
					return 0, err
				}
			}
		}
	}
	if err := ex.ensure(fn + n); err != nil {
		return 0, err
	}
	if n == 1 { // the common case, without a slice copy and a clear
		ex.stack[fn], c.res[0] = c.res[0], Value{}
		c.res = c.res[:0]
		c.unwinding, c.argErr = false, nil
	} else {
		copy(ex.stack[fn:], c.res)
		c.reset()
	}
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

// chargeResult charges a string or a table a host function returned. Library functions
// charge for what they make themselves.
func (ex *executor) chargeResult(v Value) error {
	if v.isString() {
		return ex.charge(int(v.n & lenMask))
	}
	return ex.charge(64 + (*Table)(v.p).memSize())
}
