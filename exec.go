package glua

import (
	"context"
	"math"
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
	maxIndexHops = 2000     // fixed cap: __index and __newindex chains
)

// Frame flags, in the top byte of a frame record. RETURN tests them all at once.
const (
	frGoEntry = 1 << (56 + iota) // pushed by run: unwinding and returning stop here
	frCatch                      // pcall
	frCont                       // a metamethod, iterator or closer: finish the interrupted instruction
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
	steps   int64 // budget not yet handed to st.ticks
	top     int   // end of an open list of results or arguments, as a stack index
	st      state
	types   *[]*typeInfo

	maxSteps     int64
	maxCallDepth int
	maxHostDepth int

	running, poisoned bool
}

func newExecutor() *executor {
	return &executor{
		stack:        make([]Value, initStack),
		pframes:      make([]uint64, 64),
		maxSteps:     defaultSteps,
		maxCallDepth: defaultDepth,
		maxHostDepth: defaultHost,
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
	ex.running, ex.ctx, ex.types = true, ctx, types.Load()
	q := min(tickQuantum, ex.maxSteps)
	ex.steps, ex.st.ticks = ex.maxSteps-q, int(q) // budgets are set here only: nested entries share them
	completed := false
	defer func() {
		ex.running = false
		if !completed {
			ex.poisoned = true // a panic or runtime.Goexit is passing through: never pool this one again
		}
	}()
	err = ex.run(cl, fn, nargs)
	completed = true
	return err
}

// run pushes a goEntry frame and drives the loop until that frame returns. Host functions re-enter here.
func (ex *executor) run(cl *Closure, fn, nargs int) error {
	if err := ex.ensure(fn + 1 + stackSlack); err != nil {
		return err
	}
	st := &ex.st
	nf := st.nf
	if nf == len(ex.pframes) {
		ex.pframes = append(ex.pframes, make([]uint64, len(ex.pframes))...)
	}
	ex.pframes[nf] = frGoEntry
	st.nf++
	st.cl, st.base, st.pc = cl, fn+1, 0
	st.hi = max(st.hi, fn+1+int(cl.p.maxstack))
	ex.stack[fn] = luaFnValue(cl)
	clear(ex.stack[fn+1+min(nargs, int(cl.p.nparams)) : fn+1+int(cl.p.nparams)]) // missing parameters
	err := ex.drive()
	st.nf = nf // on an error this is the unwinding: it never crosses a goEntry frame
	return err
}

// reset drops every pointer the call may have left behind.
func (ex *executor) reset() {
	clear(ex.stack[:ex.st.hi])
	for _, c := range ex.calls {
		c.reset()
	}
	ex.st = state{}
	ex.ctx, ex.top, ex.depth = nil, 0, 0
}

// release returns an executor to the pool, unless it ended abnormally or has grown large.
func release(pool *sync.Pool, ex *executor) {
	if !ex.poisoned && len(ex.stack) <= maxPoolStack {
		pool.Put(ex)
	}
}

// ensure grows the stack so that n slots plus the slack exist.
func (ex *executor) ensure(n int) error {
	if n+stackSlack <= len(ex.stack) {
		return nil
	}
	if n+stackSlack > maxStack {
		return ex.errorf("stack overflow")
	}
	size := min(max(2*len(ex.stack), n+stackSlack), maxStack)
	s := make([]Value, size)
	copy(s, ex.stack)
	clear(ex.stack) // the old stack may stay reachable from a stale pointer; leave nothing in it
	ex.stack = s
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
// current run returns.
func (ex *executor) drive() error {
	for {
		var err error
		switch ex.fast() {
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
			if len(ex.pframes) >= ex.maxCallDepth {
				err = ex.errorf("stack overflow")
			} else {
				ex.pframes = append(ex.pframes, make([]uint64, min(len(ex.pframes), ex.maxCallDepth-len(ex.pframes)))...)
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

// frameReturned pops the frame whose n results start at stack[fn] (its function slot) and
// returns to whoever started it. done reports that the goEntry frame of this run returned.
func (ex *executor) frameReturned(fn, n int) (done bool, err error) {
	st := &ex.st
	fr := ex.pframes[st.nf] // already popped by the loop or by doReturn
	if fr&frGoEntry != 0 {
		ex.top = fn + n
		return true, nil
	}
	// back to the caller
	st.base -= int(fr >> 32 & 0xffffff)
	st.cl = (*Closure)(ex.stack[st.base-1].p)
	st.pc = int(uint32(fr))
	if fr&frCont != 0 {
		return false, ex.finishOp(fn, n)
	}
	ex.adjustResults(ex.instr(st.pc-1), fn, n)
	return false, nil
}

// adjustResults pads or drops the n results at stack[fn] to what the CALL i expects.
func (ex *executor) adjustResults(i uint32, fn, n int) {
	want := decC(i) - 1
	if want < 0 {
		ex.top = fn + n // open: the next instruction consumes up to top
		return
	}
	if n < want {
		clear(ex.stack[fn+n : fn+want])
	}
}

// finishOp completes the instruction at pc-1 that a continuation frame interrupted, as
// luaV_finishOp does in C Lua. Its result, if any, is stack[fn].
func (ex *executor) finishOp(fn, n int) error {
	st := &ex.st
	i := ex.instr(st.pc - 1)
	var r Value
	if n > 0 {
		r = ex.stack[fn]
	}
	switch uint8(i) {
	case opGetField:
		ex.stack[st.base+decA(i)] = r
		st.pc++ // skip the aux word
	default:
		return ex.errorf("cannot resume %s", opName(uint8(i)))
	}
	return nil
}

// pushLua starts a call of cl at stack[fn] with nargs arguments as a new frame of the loop.
func (ex *executor) pushLua(cl *Closure, fn, nargs int, flags uint64) error {
	st := &ex.st
	if err := ex.ensure(fn + 1 + stackSlack); err != nil {
		return err
	}
	if st.nf == len(ex.pframes) {
		if len(ex.pframes) >= ex.maxCallDepth {
			return ex.errorf("stack overflow")
		}
		ex.pframes = append(ex.pframes, make([]uint64, min(len(ex.pframes), ex.maxCallDepth-len(ex.pframes)))...)
	}
	ex.pframes[st.nf] = uint64(uint32(st.pc)) | uint64(fn+1-st.base)<<32 | flags
	st.nf++
	if nargs < int(cl.p.nparams) {
		clear(ex.stack[fn+1+nargs : fn+1+int(cl.p.nparams)])
	}
	st.cl, st.base, st.pc = cl, fn+1, 0
	st.hi = max(st.hi, fn+1+int(cl.p.maxstack))
	if st.ticks--; st.ticks < 0 {
		return ex.refill()
	}
	return nil
}

// slowOp does the instruction at pc-1 that fast handed back.
func (ex *executor) slowOp() error {
	st := &ex.st
	i := ex.instr(st.pc - 1)
	a, b, c := decA(i), decB(i), decC(i)
	regs := ex.stack[st.base:]
	p := st.cl.p
	switch op := uint8(i); op {
	case opAdd, opSub, opMul, opAddK, opSubK, opMulK:
		x, y := regs[b], regs[c]
		if op >= opAddK {
			y = p.k[c]
		}
		r, err := ex.arith(op, x, y)
		if err != nil {
			return err
		}
		ex.stack[st.base+a] = r

	case opGetField:
		return ex.getField(i)

	case opSetField:
		obj, key, val := regs[a], p.k[b], regs[c]
		st.pc++ // skip the aux word
		if obj.isObj(kindTable) {
			t := (*Table)(obj.p)
			if t.Frozen() {
				return ex.errorf("attempt to modify a frozen table")
			}
			t.set(key, val)
			return nil
		}
		return ex.errorf("attempt to index a %s value", typeName(obj))

	case opNewTable:
		ex.stack[st.base+a] = tableValue(NewTable(b, c))

	case opClosure:
		np := p.protos[decBx(i)]
		ncl := &Closure{p: np, up: make([]Value, len(np.upvals))}
		ex.stack[st.base+a] = luaFnValue(ncl) // stored before the captures, so a local function can capture itself
		for j, u := range np.upvals {
			if u.fromStack {
				ncl.up[j] = ex.stack[st.base+int(u.index)]
			} else {
				ncl.up[j] = st.cl.up[u.index]
			}
		}

	case opLtJmp, opLtKJmp:
		x, y := regs[a], regs[b]
		if op == opLtKJmp {
			y = p.k[b]
		}
		lt, err := ex.lessThan(x, y)
		if err != nil {
			return err
		}
		ex.jumpAux(lt)

	case opEqKJmp:
		ex.jumpAux(rawEqual(regs[a], p.k[b]))

	case opForPrep:
		return ex.forPrep(i)

	case opForLoop:
		return ex.forLoop(i)

	case opCall:
		fn := st.base + a
		nargs := b - 1
		if b == 0 {
			nargs = ex.top - fn - 1
		}
		return ex.callValue(fn, nargs)

	case opReturn:
		n := b - 1
		if b == 0 {
			n = ex.top - (st.base + a)
		}
		return ex.doReturn(st.base+a, n)

	default:
		return ex.errorf("bad opcode %s", opName(op))
	}
	return nil
}

// callValue calls the value at stack[fn] with nargs arguments, for the CALL at pc-1.
func (ex *executor) callValue(fn, nargs int) error {
	v := ex.stack[fn]
	switch {
	case v.isObj(kindLuaFn):
		return ex.pushLua((*Closure)(v.p), fn, nargs, 0)
	case v.isObj(kindGoFn):
		return ex.callGo((*goFunc)(v.p), fn, nargs)
	}
	return ex.errorf("attempt to call a %s value", typeName(v))
}

// doReturn returns n results starting at stack[first] from the current frame.
func (ex *executor) doReturn(first, n int) error {
	fn := ex.st.base - 1
	copy(ex.stack[fn:fn+n], ex.stack[first:first+n])
	ex.st.nf--
	done, err := ex.frameReturned(fn, n)
	if err != nil {
		return err
	}
	if done {
		return errDone
	}
	return nil
}

// arith does arithmetic that fast handed back: string coercion and errors. Metamethods
// come with milestone 1.
func (ex *executor) arith(op uint8, x, y Value) (Value, error) {
	if x.p == tInt() && y.p == tInt() {
		switch op {
		case opAdd, opAddK:
			return Int(int64(x.n + y.n)), nil
		case opSub, opSubK:
			return Int(int64(x.n - y.n)), nil
		default:
			return Int(int64(x.n * y.n)), nil
		}
	}
	a, b, ok := numPair(&x, &y)
	if !ok {
		bad := x
		if x.isNumber() {
			bad = y
		}
		return Value{}, ex.errorf("attempt to perform arithmetic on a %s value", typeName(bad))
	}
	switch op {
	case opAdd, opAddK:
		return Float(a + b), nil
	case opSub, opSubK:
		return Float(a - b), nil
	default:
		return Float(a * b), nil
	}
}

// lessThan compares two values exactly, as Lua's < does without metamethods.
func (ex *executor) lessThan(x, y Value) (bool, error) {
	switch {
	case x.p == tInt() && y.p == tInt():
		return int64(x.n) < int64(y.n), nil
	case x.isNumber() && y.isNumber():
		return numLess(x, y), nil
	case x.isString() && y.isString():
		return x.str() < y.str(), nil
	}
	return false, ex.errorf("attempt to compare %s with %s", typeName(x), typeName(y))
}

// numLess is x < y for two numbers, exact for every mix of integers and floats.
func numLess(x, y Value) bool {
	if x.p == tFloat() && y.p == tFloat() {
		return math.Float64frombits(x.n) < math.Float64frombits(y.n)
	}
	if x.p == tInt() {
		i, f := int64(x.n), math.Float64frombits(y.n)
		if f != f {
			return false
		}
		if f >= 0x1p63 {
			return true
		}
		if f < -0x1p63 {
			return false
		}
		c := math.Ceil(f) // i < f  <=>  i < ceil(f), for f in range
		return i < int64(c)
	}
	f, i := math.Float64frombits(x.n), int64(y.n)
	if f != f {
		return false
	}
	if f >= 0x1p63 {
		return false
	}
	if f < -0x1p63 {
		return true
	}
	return int64(math.Floor(f)) < i // f < i  <=>  floor(f) < i
}

// forPrep prepares a loop that fast did not: float values, a float limit, or a step that
// is not positive.
func (ex *executor) forPrep(i uint32) error {
	st := &ex.st
	a := st.base + decA(i)
	r := ex.stack[a : a+4]
	init, limit, step := r[0], r[1], r[2]
	skip := func() { st.pc += decBx(i) }
	if init.p == tInt() && step.p == tInt() {
		s := int64(step.n)
		if s == 0 {
			return ex.errorf("'for' step is zero")
		}
		lim, ok := forLimit(limit, s)
		if !ok {
			if !limit.isNumber() {
				return ex.errorf("'for' limit must be a number")
			}
			skip()
			return nil
		}
		ini := int64(init.n)
		if (s > 0 && ini > lim) || (s < 0 && ini < lim) {
			skip()
			return nil
		}
		var count uint64
		if s > 0 {
			count = (uint64(lim) - uint64(ini)) / uint64(s)
		} else {
			count = (uint64(ini) - uint64(lim)) / (uint64(-(s + 1)) + 1)
		}
		r[1] = Value{p: tInt(), n: count}
		r[3] = init
		return nil
	}
	fi, ok1 := init.Float()
	fl, ok2 := limit.Float()
	fs, ok3 := step.Float()
	switch {
	case !ok1:
		return ex.errorf("'for' initial value must be a number")
	case !ok2:
		return ex.errorf("'for' limit must be a number")
	case !ok3:
		return ex.errorf("'for' step must be a number")
	case fs == 0:
		return ex.errorf("'for' step is zero")
	}
	if (fs > 0 && fl < fi) || (fs < 0 && fi < fl) {
		skip()
		return nil
	}
	r[0], r[1], r[2], r[3] = Float(fi), Float(fl), Float(fs), Float(fi)
	return nil
}

// forLimit converts a loop limit to an integer for an integer loop with step s. ok is
// false when the loop cannot run at all, or the limit is not a number.
func forLimit(limit Value, s int64) (int64, bool) {
	switch limit.p {
	case tInt():
		return int64(limit.n), true
	case tFloat():
		f := math.Float64frombits(limit.n)
		if f != f {
			return 0, false
		}
		if s > 0 {
			f = math.Floor(f)
			if f >= 0x1p63 {
				return math.MaxInt64, true
			}
			if f < -0x1p63 {
				return 0, false
			}
		} else {
			f = math.Ceil(f)
			if f < -0x1p63 {
				return math.MinInt64, true
			}
			if f >= 0x1p63 {
				return 0, false
			}
		}
		return int64(f), true
	}
	return 0, false
}

// forLoop steps a float loop.
func (ex *executor) forLoop(i uint32) error {
	st := &ex.st
	a := st.base + decA(i)
	r := ex.stack[a : a+4]
	idx, limit, step := math.Float64frombits(r[0].n), math.Float64frombits(r[1].n), math.Float64frombits(r[2].n)
	idx += step
	if (step > 0 && idx <= limit) || (step < 0 && limit <= idx) {
		r[0], r[3] = Float(idx), Float(idx)
		st.pc -= decBx(i)
		if st.ticks--; st.ticks < 0 {
			return ex.refill()
		}
	}
	return nil
}

// getField does a field read that fast handed back: a miss on the hint, a metatable, or a
// bound Go value. It teaches the site where the key was found.
func (ex *executor) getField(i uint32) error {
	st := &ex.st
	p := st.cl.p
	site := ex.aux()
	st.pc++ // skip the aux word
	obj, key := ex.stack[st.base+decB(i)], p.k[decC(i)]
	dst := st.base + decA(i)
	for hops := 0; hops < maxIndexHops; hops++ {
		switch {
		case obj.isObj(kindTable):
			t := (*Table)(obj.p)
			v, slot := t.getSlot(key)
			if slot >= 0 && hops == 0 {
				learn(&p.hints[site], slot)
			}
			if !v.IsNil() || t.meta == nil {
				ex.stack[dst] = v
				return nil
			}
			h := t.meta.Get(String("__index"))
			if h.IsNil() {
				ex.stack[dst] = Value{}
				return nil
			}
			if h.isObj(kindLuaFn) {
				return ex.callMeta(h, obj, key)
			}
			obj = h
		case obj.isObj(kindUser):
			ti := (*ex.types)[obj.n&typeIDMask]
			name, _ := key.Str()
			for j := range ti.fields {
				if ti.fields[j].name == name {
					learn(&p.hints[site], j)
					ex.stack[dst] = ti.fields[j].load(obj.p)
					return nil
				}
			}
			return ex.errorf("no field '%s' on %s", name, ti.rt.Name())
		default:
			if hops == 0 {
				return ex.errorf("attempt to index a %s value", typeName(obj))
			}
			return ex.errorf("attempt to index a %s value ('__index')", typeName(obj))
		}
	}
	return ex.errorf("'__index' chain too long; possibly a loop")
}

// callMeta calls the metamethod h(obj, key) in a continuation frame. When it returns, the
// driver finishes the interrupted instruction with its first result. The frame's pc points
// at the aux word, as the exit convention requires, so finishOp skips it.
func (ex *executor) callMeta(h, obj, key Value) error {
	st := &ex.st
	st.pc-- // back onto the aux word: the continuation records pc one past the primary word
	fn := st.base + int(st.cl.p.maxstack)
	if err := ex.ensure(fn + 3 + stackSlack); err != nil {
		return err
	}
	ex.stack[fn], ex.stack[fn+1], ex.stack[fn+2] = h, obj, key
	return ex.pushLua((*Closure)(h.p), fn, 2, frCont)
}

// typeName is the Lua type name of v, as error messages use it.
func typeName(v Value) string {
	switch k := v.Kind(); k {
	case KindInt, KindFloat:
		return "number"
	default:
		return k.String()
	}
}
