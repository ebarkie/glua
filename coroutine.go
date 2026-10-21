package glua

import (
	"errors"
	"fmt"
	"unsafe"
)

// A coroutine is a second register stack and frame list, not a goroutine. resume points the
// executor at the coroutine's per-thread state and drives it; yield returns errYield,
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

// threadState is everything that belongs to one thread of execution. While the thread runs,
// the executor holds its counters and frame list, and they are zero here; it points at the
// threadBufs, which stay in place. While the thread waits in resume, or is suspended, all of
// it is here, where tracebacks and the live walk read it.
type threadState struct {
	threadBufs
	st      state
	pframes []uint64 // pc:32 | delta:24 | flags:8. No pointers, so no write barrier and nothing to scan
}

// threadBufs is the part of a thread's state that the executor reaches through a pointer:
// the loop reads the stack once per run, and the driver takes the hop.
type threadBufs struct {
	stack []Value
	nvar  []int32 // per frame: the number of extra arguments of a vararg function
	cont  []int32 // per frame: what a continuation frame needs to resume, such as CONCAT's operand count
	tbc   []int   // stack indices of active to-be-closed variables, innermost last
	top   int     // end of an open list of results or arguments, as a stack index
}

// thread returns the running thread: the coroutine, or the main thread's stand-in.
func (ex *executor) thread() *coroutine {
	if ex.co != nil {
		return ex.co
	}
	return ex.mainCo
}

// swap moves the executor from thread from, which keeps its counters and frame list, to
// thread to, whose own copy is zeroed: the running thread's state is the executor's alone,
// so no stale state outlives a switch. The step budget belongs to the entry: it goes with
// the executor.
func (ex *executor) swap(from, to *coroutine) {
	ticks := ex.st.ticks
	from.st, from.pframes = ex.st, ex.pframes
	ex.st, ex.pframes, ex.threadBufs = to.st, to.pframes, &to.threadBufs
	ex.st.ticks = ticks
	to.st, to.pframes = state{}, nil
}

// switchTo makes co the running thread in place of prev, which then waits in resume.
func (ex *executor) switchTo(prev, co *coroutine) {
	ex.swap(prev, co)
	prev.status, co.status = coNormal, coRunning
	co.depthBase = ex.depth
	ex.co = co
}

// switchBack returns to prev from co, which yielded, returned or failed and has its status set.
func (ex *executor) switchBack(co, prev *coroutine) {
	ex.swap(co, prev)
	prev.status = coRunning
	if ex.co = prev; prev == ex.mainCo {
		ex.co = nil // the main thread runs as no coroutine
	}
}

type coroutine struct {
	threadState
	fn      Value // the body, until it starts
	status  coStatus
	main    bool // stands for the main thread in coroutine.running
	started bool
	errVal  *Error // set when the body died in error, for close

	nnyBase   int       // ex.nny when resumed: a yield must come from exactly one Go call above it
	depthBase int       // ex.depth when resumed: the Go calls below it are on other threads' stacks
	yieldFn   int       // stack index where a pending yield's results go
	yieldKind yieldKind // how to deliver them
	// the values the pending yield passed: yieldN of them from stack[yieldArgs], where they
	// stay, and resume copies them out for whoever resumed
	yieldArgs, yieldN int
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

// resume runs co with args until it yields, returns or fails, and appends what it passes
// back to dst: its results, the values it yielded, or, with ok false, the error value.
// Terminal errors come back as err. Values that co's stack cannot hold are an error raised
// in the resumer, before anything changes.
func (ex *executor) resume(co *coroutine, args, dst []Value) (vals []Value, ok bool, err error) {
	switch {
	case co.status == coDead:
		return append(dst, String("cannot resume dead coroutine")), false, nil
	case co.status != coSuspended:
		return append(dst, String("cannot resume non-suspended coroutine")), false, nil
	case ex.depth >= ex.maxHostDepth-1:
		// the coroutine could not call Go: refuse, as C Lua's resume does near LUAI_MAXCCALLS
		return append(dst, String("C stack overflow")), false, nil
	}
	// the stack co needs, which runResumed ensures: at its yield, the values; at its start,
	// the frame of f, its body or the trampoline that calls a Go body, as pushFrame asks
	var f Value
	need := co.yieldFn + len(args)
	if !co.started {
		f = co.fn
		if !f.isObj(kindLuaFn) {
			// a Go body starts from a Lua trampoline, so that it is called as Lua calls it:
			// pcall runs as a catch frame that can yield, and yield itself can be the body
			args = append([]Value{f}, args...)
			f = luaFnValue(trampoline)
		}
		need = frameRoom((*closure)(f.p), 0, len(args))
	}
	if need+stackSlack > ex.stackLimit() {
		return dst, false, ex.errorf("stack overflow") // at the resumer, with its position
	}
	prev := ex.thread()
	// the caller's state waits in prev, where tracebacks find it; the executor runs on co's
	ex.switchTo(prev, co)
	co.nnyBase = ex.nny
	vals, ok, err = ex.runResumed(co, f, args, dst, need)
	// not deferred: a panic poisons the executor, so what state it is left in does not matter
	ex.switchBack(co, prev)
	if le, isLua := err.(*Error); isLua && le.terminal && le.where == "" {
		le.where = ex.where() // raised by a Go body, which has no position: the resume's
	}
	if ok && co.status == coDead {
		ex.keepStack(co) // the body returned, and its results are in vals
	}
	return vals, ok, err
}

// runResumed runs co, whose state the executor has taken, with the resume values args until
// it yields, returns or fails, as resume reports them. Before it starts, f is the function
// to call with args, and need the stack that takes, within the limit resume checked, so
// nothing fails before the body runs: co's status is the body's to set.
func (ex *executor) runResumed(co *coroutine, f Value, args, dst []Value, need int) (vals []Value, ok bool, err error) {
	if err := ex.ensure(need); err != nil {
		return dst, false, err // within the limit: ensure only grows the stack
	}
	if !co.started {
		ex.stack[0] = f
		copy(ex.stack[1:], args)
		ex.st.hi = max(ex.st.hi, 1+len(args))
		if err := ex.pushFrame((*closure)(f.p), 0, len(args), 0, 1, frGoEntry); err != nil {
			return dst, false, err
		}
		co.started, co.fn = true, Value{} // taken only once it runs
	} else {
		// the resume values are the results of the pending yield
		n := len(args)
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
		co.status = coDead
		return append(dst, ex.stack[0:ex.top]...), true, nil
	case err == errYield:
		co.status = coSuspended
		return append(dst, ex.stack[co.yieldArgs:co.yieldArgs+co.yieldN]...), true, nil
	}
	return ex.coFailed(co, err, dst)
}

// coFailed ends a coroutine that died in error. Its frames stay, so that coroutine.close
// can close its to-be-closed variables; terminal errors pass through.
func (ex *executor) coFailed(co *coroutine, err error, dst []Value) ([]Value, bool, error) {
	co.status = coDead
	le, ok := err.(*Error)
	if !ok || le.terminal {
		return dst, false, err
	}
	co.errVal = le
	return append(dst, le.Value), false, nil
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
	vals, ok, err := c.ex.resume(co, argView(c, 1), append(c.res[:0], Value{}))
	if err != nil {
		return err
	}
	vals[0] = Bool(ok)
	c.res = vals
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
	co.yieldArgs, co.yieldN = c.base, c.nargs // resume copies them out for the resumer
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
	co := ex.thread()
	return c.ReturnValues(threadValue(co), Bool(co.main))
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
	return c.ReturnValues(Bool(ex.yieldable()))
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
		vals, ok, err := c.ex.resume(co, argView(c, 0), c.res[:0])
		c.res = vals // also on failure, so that reset lets the error value go
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
		return nil
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
		// close the variables the coroutine left open, on its own stack: the closers run as
		// the coroutine, which cannot yield meanwhile
		prev := ex.thread()
		ex.switchTo(prev, co)
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
		ex.switchBack(co, prev)
		if cerr != nil {
			co.status = coDead // no thread runs it, whatever its closers did
			return nil, cerr
		}
		if !same(&v, &value) {
			errv = &v
		}
	}
	co.status = coDead
	co.errVal, co.fn = nil, Value{} // a body that never ran goes too
	ex.keepStack(co)                // for the next coroutine, as a body that returned leaves it
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
