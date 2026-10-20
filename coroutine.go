package glua

import (
	"errors"
	"fmt"
	"unsafe"
)

// A coroutine is a second register stack and frame list, not a goroutine. resume swaps the
// executor's per-thread state with the coroutine's and drives it; yield returns errYield,
// which the driver passes up to resume, leaving the coroutine's frames in place. A yield may
// cross pcall, metamethods, iterators and __close, which are VM frames, but not a Go
// function: that would need the Go stack to be suspended.

type coStatus uint8

const (
	coSuspended coStatus = iota
	coRunning
	coNormal // it resumed another coroutine
	coDead
)

var coStatusNames = [...]string{"suspended", "running", "normal", "dead"}

// threadState is everything that belongs to one thread of execution.
type threadState struct {
	stack   []Value
	pframes []uint64
	nvar    []int32
	cont    []int32
	tbc     []int
	st      state
	top     int
}

type coroutine struct {
	threadState
	fn      Value // the body, until it starts
	status  coStatus
	main    bool // stands for the main thread in coroutine.running
	started bool
	errVal  *Error      // set when the body died in error, for close
	waiting threadState // while it is normal: its state, waiting in resume, for debug.traceback

	nnyBase   int       // ex.nny when resumed: a yield must come from exactly one Go call above it
	yieldFn   int       // stack index where a pending yield's results go
	yieldKind yieldKind // how to deliver them
	yielded   []Value
	yieldBuf  []Value // backs yielded, reused from yield to yield
}

type yieldKind uint8

const (
	yieldCall  yieldKind = iota // a CALL or TFORCALL: the results as that call expects them
	yieldMeta                   // a metamethod: finishOp completes the instruction
	yieldPcall                  // pcall(coroutine.yield, ...): true, then the results
)

// errYield is how coroutine.yield leaves the coroutine's driver.
var errYield = errors.New("glua: yield")

func threadValue(co *coroutine) Value { return Value{p: unsafe.Pointer(co), n: kindThread << 56} }

func (ex *executor) saveThread() threadState {
	return threadState{ex.stack, ex.pframes, ex.nvar, ex.cont, ex.tbc, ex.st, ex.top}
}

func (ex *executor) loadThread(t threadState) {
	ex.stack, ex.pframes, ex.nvar, ex.cont, ex.tbc, ex.st, ex.top = t.stack, t.pframes, t.nvar, t.cont, t.tbc, t.st, t.top
}

// trampoline is function(f, ...) return f(...) end, the body of a coroutine whose
// function is a Go function.
var trampoline = func() *closure {
	p, _, err := compileChunk("=coroutine", "return function(f, ...) return f(...) end")
	if err != nil {
		panic(err)
	}
	return &closure{p: p.protos[0]}
}()

// coStackSize is the register stack a coroutine starts with: room for its body's frame,
// which pushFrame wants with twice stackSlack above it, so that the first resume need not
// move it.
const coStackSize = 2 * initStack

// newCoroutine creates a coroutine for function f, charging its stack to the budget.
func (ex *executor) newCoroutine(f Value) (*coroutine, error) {
	if err := ex.charge(coStackSize*16 + 4<<10); err != nil {
		return nil, err
	}
	co := &coroutine{fn: f}
	if n := len(ex.coStacks); n > 0 {
		co.stack, co.pframes = ex.coStacks[n-1].stack, ex.coStacks[n-1].pframes
		ex.coStacks[n-1] = coStack{}
		ex.coStacks = ex.coStacks[:n-1]
	} else {
		co.stack = make([]Value, coStackSize)
		co.pframes = make([]uint64, 16)
	}
	return co, nil
}

// coStack is the register stack and frame list of a coroutine that returned, kept for the
// next one made in the same entry: a dead coroutine never runs again, and nothing outside it
// points into its stack.
type coStack struct {
	stack   []Value
	pframes []uint64
}

// maxCoStacks is how many such stacks an executor keeps: enough for coroutines made one
// after another, as generators are.
const maxCoStacks = 4

// keepStack takes the stacks of co, which returned, for the next coroutine. A register
// stack that grew is not kept: its growth was charged, and the next coroutine's would not be.
func (ex *executor) keepStack(co *coroutine) {
	if len(ex.coStacks) < maxCoStacks && len(co.stack) == coStackSize {
		clear(co.stack[:co.st.hi]) // above the high-water mark every slot is nil
		ex.coStacks = append(ex.coStacks, coStack{co.stack, co.pframes})
	}
	co.stack, co.pframes = nil, nil
}

// resume runs co with args until it yields, returns or fails. ok is false when the body
// failed; vals are then the error value. Terminal errors come back as err.
func (ex *executor) resume(co *coroutine, args []Value) (vals []Value, ok bool, err error) {
	switch {
	case co.status == coDead:
		return []Value{String("cannot resume dead coroutine")}, false, nil
	case co.status != coSuspended:
		return []Value{String("cannot resume non-suspended coroutine")}, false, nil
	case ex.depth >= ex.maxHostDepth-1:
		// the coroutine could not call Go: refuse, as C Lua's resume does near LUAI_MAXCCALLS
		return []Value{String("C stack overflow")}, false, nil
	}
	prev := ex.co
	if prev == nil {
		prev = ex.mainThread()
	}
	// the caller's state waits in prev, where tracebacks find it, and comes back from there
	prev.status, prev.waiting = coNormal, ex.saveThread()
	saved := &prev.waiting
	ex.loadThread(co.threadState)
	ex.st.ticks = saved.st.ticks // the step budget belongs to the entry, not the coroutine
	ex.co = co
	co.status = coRunning
	co.nnyBase = ex.nny
	returned := false
	defer func() {
		co.threadState = ex.saveThread()
		saved.st.ticks = ex.st.ticks
		ex.loadThread(*saved)
		prev.status, prev.waiting = coRunning, threadState{}
		if ex.co = prev; prev == ex.mainCo {
			ex.co = nil // the main thread runs as no coroutine
		}
		if le, ok := err.(*Error); ok && le.terminal && le.where == "" {
			le.where = ex.where() // raised by a Go body, which has no position: the resume's
		}
		if returned {
			ex.keepStack(co)
		}
	}()

	if !co.started {
		co.started = true
		f := co.fn
		co.fn = Value{}
		if !f.isObj(kindLuaFn) {
			// a Go body starts from a Lua trampoline, so that it is called as Lua calls it:
			// pcall runs as a catch frame that can yield, and yield itself can be the body
			args = append([]Value{f}, args...)
			f = luaFnValue(trampoline)
		}
		if err := ex.ensure(1 + len(args) + stackSlack); err != nil {
			return nil, false, err
		}
		ex.stack[0] = f
		copy(ex.stack[1:], args)
		ex.st.hi = max(ex.st.hi, 1+len(args))
		if err := ex.pushFrame((*closure)(f.p), 0, len(args), 0, 1, frGoEntry); err != nil {
			return nil, false, err
		}
	} else {
		// the resume values are the results of the pending yield
		n := len(args)
		if err := ex.ensure(co.yieldFn + n); err != nil {
			return nil, false, err
		}
		copy(ex.stack[co.yieldFn:], args)
		ex.st.hi = max(ex.st.hi, co.yieldFn+n)
		switch co.yieldKind {
		case yieldMeta:
			// completing a concatenation can call another metamethod, which may yield or fail
			if err = ex.finishOp(co.yieldFn, n); err != nil && err != errYield {
				err = ex.unwind(err, 0)
			}
		case yieldPcall:
			ex.stack[co.yieldFn-1] = Bool(true)
			ex.adjustResults(ex.instr(ex.st.pc-1), co.yieldFn-1, n+1)
		default:
			ex.adjustResults(ex.instr(ex.st.pc-1), co.yieldFn, n)
		}
	}
	if err == nil {
		err = ex.drive(0)
	}
	switch {
	case err == nil:
		co.status, returned = coDead, true
		return append([]Value(nil), ex.stack[0:ex.top]...), true, nil
	case err == errYield:
		co.status = coSuspended
		vals, co.yielded = co.yielded, nil
		return vals, true, nil
	}
	return ex.coFailed(co, err)
}

// coFailed ends a coroutine that died in error. Its frames stay, so that coroutine.close
// can close its to-be-closed variables; terminal errors pass through.
func (ex *executor) coFailed(co *coroutine, err error) ([]Value, bool, error) {
	co.status = coDead
	le, ok := err.(*Error)
	if !ok || le.terminal {
		return nil, false, err
	}
	co.errVal = le
	return []Value{le.Value}, false, nil
}

// yieldable reports whether the running coroutine may yield from a Go function called by
// the VM right now: no other Go function or nested run lies in between.
func (ex *executor) yieldable() bool {
	return ex.co != nil && !ex.co.main && ex.nny == ex.co.nnyBase+1
}

// The coroutine library.

func coArg(c *Call, fname string) (*coroutine, error) {
	v := c.Value(0)
	if !v.isObj(kindThread) {
		return nil, c.typeArgError(0, fname, "thread")
	}
	return (*coroutine)(v.p), nil
}

func coCreate(c *Call) error {
	f := c.Value(0)
	if !f.isObj(kindLuaFn) && !f.isObj(kindGoFn) {
		return c.typeArgError(0, "create", "function")
	}
	co, err := c.ex.newCoroutine(f)
	if err != nil {
		return err
	}
	return c.ReturnValues(threadValue(co))
}

// argView returns the arguments from index from on as a view of the stack: valid until the
// stack moves, which a resume does not do to the caller's.
func argView(c *Call, from int) []Value {
	if from >= c.nargs {
		return nil
	}
	return c.ex.stack[c.base+from : c.base+c.nargs : c.base+c.nargs]
}

func coResume(c *Call) error {
	co, err := coArg(c, "resume")
	if err != nil {
		return err
	}
	vals, ok, err := c.ex.resume(co, argView(c, 1))
	if err != nil {
		return err
	}
	c.res = append(append(c.res[:0], Bool(ok)), vals...)
	return nil
}

func coYield(c *Call) error {
	ex := c.ex
	switch {
	case ex.co == nil || ex.co.main:
		return fmt.Errorf("attempt to yield from outside a coroutine")
	case !ex.yieldable():
		return fmt.Errorf("attempt to yield across a C-call boundary")
	}
	co := ex.co
	co.yieldBuf = append(co.yieldBuf[:0], argView(c, 0)...)
	co.yielded = co.yieldBuf
	return errYield
}

func coStatusOf(c *Call) error {
	co, err := coArg(c, "status")
	if err != nil {
		return err
	}
	return c.ReturnValues(String(coStatusNames[co.status]))
}

func coRunningFn(c *Call) error {
	ex := c.ex
	if ex.co == nil {
		return c.ReturnValues(threadValue(ex.mainThread()), Bool(true))
	}
	return c.ReturnValues(threadValue(ex.co), Bool(ex.co.main))
}

// mainThread returns the coroutine that stands for the main thread, made on first use. While
// a coroutine runs, it keeps the main thread's state, waiting in resume.
func (ex *executor) mainThread() *coroutine {
	if ex.mainCo == nil {
		ex.mainCo = &coroutine{main: true, status: coRunning}
	}
	return ex.mainCo
}

func coIsYieldable(c *Call) error {
	ex := c.ex
	if c.Len() > 0 {
		co, err := coArg(c, "isyieldable")
		if err != nil {
			return err
		}
		if co != ex.co {
			return c.ReturnValues(Bool(!co.main)) // a coroutine that is not running can yield when resumed
		}
	}
	return c.ReturnValues(Bool(ex.co != nil && !ex.co.main && ex.nny == ex.co.nnyBase+1))
}

func coWrap(c *Call) error {
	f := c.Value(0)
	if !f.isObj(kindLuaFn) && !f.isObj(kindGoFn) {
		return c.typeArgError(0, "wrap", "function")
	}
	co, err := c.ex.newCoroutine(f)
	if err != nil {
		return err
	}
	g := &goFunc{name: "wrap", qualname: "coroutine.wrap", stateful: true, co: co}
	g.f = func(c *Call) error {
		vals, ok, err := c.ex.resume(co, argView(c, 0))
		if err != nil {
			return err
		}
		if !ok {
			v := vals[0]
			if co.status == coDead {
				// as C Lua's auxwrap: close the coroutine's variables, whose error wins
				errv, err := c.ex.closeCoroutine(co)
				if err != nil {
					return err
				}
				if errv != nil {
					v = *errv
				}
			}
			if v.isString() {
				// as C Lua's auxwrap: add the position of the caller
				if pos := c.ex.whereLevel(c, 1); pos != "" {
					v = String(pos + ": " + v.str())
				}
			}
			return &Error{Value: v}
		}
		return c.ReturnValues(vals...)
	}
	return c.ReturnValues(goFnValue(g))
}

func coClose(c *Call) error {
	co, err := coArg(c, "close")
	if err != nil {
		return err
	}
	switch co.status {
	case coRunning:
		return fmt.Errorf("cannot close a running coroutine")
	case coNormal:
		return fmt.Errorf("cannot close a normal coroutine")
	}
	errv, err := c.ex.closeCoroutine(co)
	if err != nil {
		return err
	}
	if errv != nil {
		return c.ReturnValues(Bool(false), *errv)
	}
	return c.ReturnValues(Bool(true))
}

// closeCoroutine kills a suspended or dead coroutine: it closes the variables the coroutine
// left open, and returns the error the coroutine died with, or the one its closers raised.
func (ex *executor) closeCoroutine(co *coroutine) (*Value, error) {
	var errv *Value
	if co.errVal != nil {
		v := co.errVal.Value
		errv = &v
	}
	if len(co.tbc) > 0 {
		// close the variables the coroutine left open, on its own stack
		saved := ex.saveThread()
		ex.loadThread(co.threadState)
		// the closers run as the coroutine, which cannot yield meanwhile
		prev := ex.co
		if prev == nil {
			prev = ex.mainThread()
		}
		ex.co, co.status = co, coRunning
		prev.status, prev.waiting = coNormal, saved
		nnyBase := co.nnyBase
		co.nnyBase = ex.nny + 1
		ex.nny++
		value := Value{}
		if errv != nil {
			value = *errv
		}
		v, cerr := ex.closeOnError(0, value, true)
		ex.nny--
		co.nnyBase = nnyBase
		prev.status, prev.waiting = coRunning, threadState{}
		if ex.co = prev; prev == ex.mainCo {
			ex.co = nil
		}
		co.threadState = ex.saveThread()
		ex.loadThread(saved)
		if cerr != nil {
			return nil, cerr
		}
		if !same(&v, &value) {
			errv = &v
		}
	}
	co.status = coDead
	co.errVal = nil
	co.stack, co.pframes = nil, nil
	return errv, nil
}

// coroutineLib returns the coroutine library table.
func coroutineLib() *Table {
	return newLib("coroutine", map[string]GoFunc{
		"create": coCreate, "resume": coResume, "yield": coYield, "status": coStatusOf,
		"running": coRunningFn, "isyieldable": coIsYieldable, "wrap": coWrap, "close": coClose,
	})
}

// yieldedAt records where the results of the yield that just happened go, for the Go call
// at stack[fn]. It returns errYield, for the caller to pass on.
func (ex *executor) yieldedAt(fn int, kind yieldKind) error {
	ex.co.yieldFn, ex.co.yieldKind = fn, kind
	return errYield
}
