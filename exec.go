package glua

import (
	"context"
	"io"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	stackSlack   = 256      // free slots above any frame base, so a frame can be viewed as *[256]Value
	initStack    = 512      // 8 KB of registers
	maxPoolStack = 4096     // an executor whose stack grew beyond 64 KB is not pooled
	maxStack     = 1 << 20  // fixed cap: register stack slots
	tickQuantum  = 1024     // steps handed to the loop between refills
	defaultSteps = 10000000 // Limits.Steps default
	defaultDepth = 10000    // Limits.CallDepth default
	defaultHost  = 100      // Limits.HostDepth default
	defaultAlloc = 64 << 20 // Limits.AllocBytes default
	maxIndexHops = 2000     // fixed cap: __index and __newindex chains
)

// Frame flags, in the top byte of a frame record. RETURN tests them all at once.
const (
	frGoEntry    = 1 << (56 + iota) // pushed by run: unwinding and returning stop here
	frCatch                         // pcall
	frCont                          // a metamethod, iterator or closer: finish the interrupted instruction
	frVararg                        // a vararg function: its arguments lie below the frame
	frHandler                       // with frCatch: an xpcall, whose message handler is in the slot below the function
	frErrHandler                    // an xpcall message handler running on top of the failing frame
)

// Hint words are missCount<<24 | slot. The driver calls learn after a slow lookup.
const (
	hintSlotMask = 1<<24 - 1
	maxMisses    = 4
	megamorphic  = ^uint32(0)
)

// learn records where a slow lookup found its key. A site gives up after four misses: it is
// marked megamorphic and never written again, so it stops invalidating other cores' caches.
func learn(hint *uint32, slot int) {
	h := atomic.LoadUint32(hint)
	if h == megamorphic {
		return
	}
	if misses := h>>24 + 1; misses > maxMisses {
		atomic.StoreUint32(hint, megamorphic)
	} else {
		atomic.StoreUint32(hint, misses<<24|uint32(slot))
	}
}

// state is what the loop saves between runs. Only indices persist; the two addresses are
// valid only while fast is running, when nothing can move the stack.
type state struct {
	cl    *Closure
	base  int
	pc    int
	ticks int
	nf    int
	hi    int // high-water mark: registers below it may hold pointers and are cleared on reset
	nret  int // number of results of an inline RETURN0 or RETURN1

	limitAddr uintptr // last legal address of a CALL's function slot, plus one
	hiAddr    uintptr
}

// executor is a register stack, a frame list and counters. It holds no script state, so
// one pool per Runtime serves every Script and Rule. It is not the public VM, which is a
// private environment plus an executor of its own.
type executor struct {
	stack   []Value
	pframes []uint64 // pc:32 | delta:24 | flags:8. No pointers, so no write barrier and nothing to scan
	calls   []*Call  // one record per host-call depth
	depth   int
	slots   []unsafe.Pointer // by type ID: a *T made with new(T), for structs passed by value
	ctx     context.Context
	steps   int64                    // budget not yet handed to st.ticks
	top     int                      // end of an open list of results or arguments, as a stack index
	nvar    []int32                  // per frame: the number of extra arguments of a vararg function
	tbc     []int                    // stack indices of active to-be-closed variables, innermost last
	cont    []int32                  // per frame: what a continuation frame needs to resume, such as CONCAT's operand count
	strMeta *Table                   // the metatable all strings share; the string library sets it
	goErrs  map[unsafe.Pointer]error // Go errors by the data of their message, so raising it again keeps the chain
	co      *coroutine               // the running coroutine, nil on the main thread
	mainCo  *coroutine               // stands for the main thread in coroutine.running
	nny     int                      // Go calls and nested runs in progress: a yield cannot cross them
	st      state
	types   *[]*typeInfo
	rt      *Runtime // nil for executors the tests make

	maxSteps     int64
	maxCallDepth int
	errExtra     int // frames a message handler may use beyond maxCallDepth, as C Lua's error stack

	// under lua_stackstress, the stack moves between two buffers: spare is the other one,
	// whose first spareDirty slots hold poison
	spare        []Value
	spareDirty   int
	maxHostDepth int
	maxAlloc     int64 // bytes one entry may allocate
	allocLeft    int64 // what is left of it in the current entry

	checked    bool      // run the bounds-checked loop
	output     io.Writer // where print and warn write; nil discards
	warnOn     bool      // warn("@on") was called
	rand       [4]uint64 // math.random's state, per entry
	randSeeded bool
	randSeed   *uint64 // the RandSeed option, if set
	hostCalls  int     // Go functions running: a panic now is the host's, not the VM's
	fatal      error   // the terminal error of this entry, which stands once raised

	running, poisoned bool
}

// write sends text from print or warn to the output.
func (ex *executor) write(s string) error {
	if ex.output == nil {
		return nil
	}
	_, err := io.WriteString(ex.output, s)
	return err
}

// runBase returns the frame index of the goEntry frame of the current run.
func (ex *executor) runBase() int {
	for f := ex.st.nf - 1; f > 0; f-- {
		if ex.pframes[f]&frGoEntry != 0 {
			return f
		}
	}
	return 0
}

// defaultChecked selects the loop new executors run. The checked loop is the default until
// milestone 4's fuzzing exit is met.
var defaultChecked = true

func newExecutor() *executor {
	return &executor{
		stack:        make([]Value, initStack),
		pframes:      make([]uint64, 64),
		maxSteps:     defaultSteps,
		maxCallDepth: defaultDepth,
		maxHostDepth: defaultHost,
		maxAlloc:     defaultAlloc,
		checked:      defaultChecked,
	}
}

// call is the only way in from Go. Arguments are already in stack[fn+1:], and the first
// result lands in stack[fn]; ex.top is one past the last result.
func (ex *executor) call(ctx context.Context, cl *Closure, fn, nargs int) (err error) {
	if ex.running {
		panic("glua: concurrent use of an executor")
	}
	if ctx.Err() != nil {
		return ctxError(ctx) // a cancelled context never runs script code
	}
	ex.running, ex.ctx, ex.types = true, ctx, ex.planSet()
	q := min(tickQuantum, ex.maxSteps)
	ex.steps, ex.st.ticks = ex.maxSteps-q, int(q) // budgets are set here only: nested entries share them
	ex.allocLeft = ex.maxAlloc
	completed := false
	defer func() {
		ex.running = false
		if completed {
			return
		}
		ex.poisoned = true // a panic or runtime.Goexit is passing through: never pool this one again
		if ex.hostCalls > 0 {
			// a host function's panic, or Goexit: it continues, unless the Runtime recovers
			// panics. recover returns nil for Goexit, which then goes on.
			if ex.rt != nil && ex.rt.recover {
				if r := recover(); r != nil {
					err = &PanicError{Value: r, Stack: debug.Stack()}
				}
			}
			return
		}
		// a panic inside the VM itself, such as a failed bounds check in the checked loop: a VM
		// bug must not take the process down
		if r := recover(); r != nil {
			err = &PanicError{Value: r, Stack: debug.Stack()}
		}
	}()
	err = ex.run(cl, fn, nargs)
	completed = true
	if le, ok := err.(*Error); ok && le.terminal {
		ex.poisoned = true // a terminal error may leave closers half run: start the next entry afresh
	}
	return err
}

// run pushes a goEntry frame and drives the loop until that frame returns. Host functions re-enter here.
func (ex *executor) run(cl *Closure, fn, nargs int) error {
	if err := ex.ensure(fn + 1 + stackSlack); err != nil {
		return err
	}
	st := &ex.st
	nf := st.nf
	ex.stack[fn] = luaFnValue(cl)
	if err := ex.pushFrame(cl, fn, nargs, 0, fn+1, frGoEntry); err != nil {
		return err
	}
	err := ex.drive(nf)
	if le, ok := err.(*Error); ok {
		ex.captureTrace(le, nf) // the error leaves this run uncaught
		// close what this run left open, with the error
		if le.terminal {
			ex.closeTerminal(fn, le)
		} else if v, cerr := ex.closeOnError(fn, le.Value); cerr != nil {
			err = cerr
		} else if !same(&v, &le.Value) {
			le.Value, le.cause = v, nil // a closer replaced the error
		}
	}
	st.nf = nf // on an error this is the unwinding: it never crosses a goEntry frame
	return err
}

// reset drops every pointer the call may have left behind.
func (ex *executor) reset() {
	if hi := ex.st.hi; hi <= 16 {
		for j := range hi { // a small call: plain stores beat memclrHasPointers and its bulk barrier
			ex.stack[j] = Value{}
		}
	} else {
		clear(ex.stack[:hi])
	}
	for _, c := range ex.calls {
		c.reset()
	}
	ex.st = state{}
	ex.ctx, ex.top, ex.depth = nil, 0, 0
	ex.tbc = ex.tbc[:0]
	clear(ex.goErrs)
	ex.co, ex.nny, ex.fatal = nil, 0, nil
	ex.randSeeded = false
}

// release returns an executor to the pool, unless it ended abnormally or has grown large.
func release(pool *sync.Pool, ex *executor) {
	if !ex.poisoned && len(ex.stack) <= maxPoolStack {
		pool.Put(ex)
	}
}

// frameLimit is the number of frames allowed now.
func (ex *executor) frameLimit() int { return ex.maxCallDepth + ex.errExtra }

// ensure grows the stack so that n slots plus the slack exist. Under the lua_stackstress
// build tag it moves the stack every time, and poisons the old one.
func (ex *executor) ensure(n int) error {
	if n+stackSlack <= len(ex.stack) {
		if !stackStress || len(ex.stack) > stressMax {
			return nil
		}
		ex.moveStack(len(ex.stack))
		return nil
	}
	limit := maxStack
	if ex.errExtra > 0 {
		limit += handlerSlots
	}
	if n+stackSlack > limit {
		return ex.errorf("stack overflow")
	}
	ex.moveStack(min(max(2*len(ex.stack), n+stackSlack), limit))
	return nil
}

// stressMax is the largest stack lua_stackstress moves at every check; past it, moving the
// whole stack per call would make deep recursion quadratic.
const stressMax = 64 << 10

// stalePoison fills a stack that was moved away from, under lua_stackstress: a stale read
// shows up as this string in a result or a message.
var stalePoison = String("glua: stale stack slot")

// moveStack moves the stack to a new one of size slots.
func (ex *executor) moveStack(size int) {
	if stackStress {
		// only the part in use: above the high-water mark and top, every slot is nil
		used := min(len(ex.stack), max(ex.st.hi, ex.top)+stackSlack)
		s, dirty := ex.spare, ex.spareDirty
		if len(s) != size {
			s, dirty = make([]Value, size), 0
		}
		copy(s, ex.stack[:used])
		if dirty > used {
			clear(s[used:dirty])
		}
		for i := range used {
			ex.stack[i] = stalePoison
		}
		ex.spare, ex.spareDirty = ex.stack, used
		ex.stack = s
		return
	}
	s := make([]Value, size)
	copy(s, ex.stack)
	clear(ex.stack) // the old stack may stay reachable from a stale pointer; leave nothing in it
	ex.stack = s
}

// charge takes n bytes from the entry's allocation budget, before the allocation. Running
// out is a terminal error. Milestone 2 charges every allocation site; this covers the ones
// that can grow without bound.
func (ex *executor) charge(n int) error {
	if n > maxAlloc {
		return ex.terminal(ErrAlloc)
	}
	ex.allocLeft -= int64(n)
	if ex.allocLeft < 0 {
		return ex.terminal(ErrAlloc)
	}
	return nil
}

// refill runs when the tick counter expires: at most once per 1,024 back-edges and calls.
// The tick that brought us here has already happened, so it is paid for out of the new quantum.
//
//go:noinline
func (ex *executor) refill() error {
	if ex.ctx.Err() != nil { // never Done(): it allocates a channel on a fresh context
		return ctxError(ex.ctx)
	}
	if ex.steps == 0 {
		return ex.terminal(ErrSteps)
	}
	q := min(tickQuantum, ex.steps)
	ex.steps -= q
	ex.st.ticks = int(q) - 1
	return nil
}

// drive runs fast and does the work it hands back, until the goEntry frame of the
// current run, frame nf0, returns. Errors stop at catch frames above nf0.
func (ex *executor) drive(nf0 int) error {
	for {
		var err error
		var e exit
		if ex.checked {
			e = ex.checkedLoop()
		} else {
			e = ex.fast()
		}
		switch e {
		case exReturn:
			var done bool
			if done, err = ex.frameReturned(ex.st.base-1, ex.st.nret); done {
				return nil
			}
		case exResults:
			i := ex.instr(ex.st.pc - 1)
			fn := ex.st.base + decA(i)
			ex.adjustResults(i, fn, ex.st.nret)
		case exSlowOp:
			err = ex.slowOp()
		case exHostCall:
			err = ex.hostCall()
		case exGrowStack:
			fn := ex.st.base + decA(ex.instr(ex.st.pc-1))
			if err = ex.ensure(fn + 1 + stackSlack); err == nil {
				ex.st.pc-- // run the CALL again
			}
		case exGrowFrames:
			if len(ex.pframes) >= ex.frameLimit() {
				err = ex.errorf("stack overflow")
			} else {
				ex.pframes = append(ex.pframes, make([]uint64, min(len(ex.pframes), ex.frameLimit()-len(ex.pframes)))...)
				ex.st.pc--
			}
		case exTick:
			err = ex.refill()
		default:
			err = ex.errorf("bad opcode %d", uint8(ex.instr(ex.st.pc-1)))
		}
		if err == errDone {
			return nil
		}
		if err != nil {
			if err = ex.unwind(err, nf0); err == nil {
				continue
			}
			return err
		}
	}
}

// instr returns the instruction at index pc of the current function.
func (ex *executor) instr(pc int) uint32 { return ex.st.cl.p.code[pc] }

// aux returns the auxiliary word that follows the instruction at pc-1.
func (ex *executor) aux() uint32 { return ex.st.cl.p.code[ex.st.pc] }

// jumpAux takes or skips the jump in the aux word of a compare-and-jump at pc-1.
func (ex *executor) jumpAux(fallThrough bool) {
	if fallThrough {
		ex.st.pc++
	} else {
		ex.st.pc += int(int32(ex.aux())) + 1
	}
}
