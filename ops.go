package glua

import (
	"math"
	"strings"
	"unsafe"
)

// The driver's side of the instruction set: everything fast hands back. On entry st.pc
// points one past the primary word. A handler that finishes at once skips the instruction's
// aux word itself. One that calls a Lua metamethod leaves st.pc alone: the continuation
// frame records it, and finishOp completes the instruction when the metamethod returns.

// cell is a captured local that is assigned after its declaration. Its register, and the
// upvalues of closures that capture it, hold the cell.
type cell struct {
	v      Value
	frozen bool
}

func cellValue(c *cell) Value { return Value{p: unsafe.Pointer(c), n: kindCell << 56} }
func (v Value) cell() *cell   { return (*cell)(v.p) }

func (ex *executor) skipAux(op uint8) {
	if hasAux[op] {
		ex.st.pc++
	}
}

// reg returns a pointer to register r of the current frame. It is valid until the stack grows.
func (ex *executor) reg(r int) *Value { return &ex.stack[ex.st.base+r] }

// slowOp does the instruction at pc-1 that fast handed back.
func (ex *executor) slowOp() error {
	st := &ex.st
	i := ex.instr(st.pc - 1)
	op := uint8(i)
	a, b, c := decA(i), decB(i), decC(i)
	p := st.cl.p
	switch op {
	case opMove:
		*ex.reg(a) = *ex.reg(b)
	case opLoadK:
		*ex.reg(a) = p.k[decBx(i)]
	case opLoadKX:
		*ex.reg(a) = p.k[ex.aux()]
	case opLoadI:
		*ex.reg(a) = Int(int64(int16(decBx(i))))
	case opLoadF:
		*ex.reg(a) = Float(float64(int16(decBx(i))))
	case opLoadNil:
		clear(ex.stack[st.base+a : st.base+a+b+1])
	case opLoadFalse:
		*ex.reg(a) = Bool(false)
	case opLoadTrue:
		*ex.reg(a) = Bool(true)
	case opGetUpval:
		*ex.reg(a) = st.cl.up[b]
	case opGetUpCell:
		*ex.reg(a) = st.cl.up[b].cell().v
	case opSetUpCell:
		cl := st.cl.up[b].cell()
		if cl.frozen {
			return ex.errorf("attempt to assign to frozen upvalue")
		}
		cl.v = *ex.reg(a)
	case opNewCell:
		*ex.reg(a) = cellValue(&cell{v: *ex.reg(a)})
	case opGetCell:
		*ex.reg(a) = ex.reg(b).cell().v
	case opSetCell:
		cl := ex.reg(a).cell()
		if cl.frozen {
			return ex.errorf("attempt to assign to frozen upvalue")
		}
		cl.v = *ex.reg(b)

	case opGetGlobal:
		return ex.index(op, envValue(st.cl), p.k[decBx(i)], a)
	case opSetGlobal:
		return ex.newindex(op, envValue(st.cl), p.k[decBx(i)], *ex.reg(a))
	case opGetEnv:
		*ex.reg(a) = envValue(st.cl)
	case opGetTable:
		return ex.index(op, *ex.reg(b), *ex.reg(c), a)
	case opGetIndex:
		return ex.index(op, *ex.reg(b), Int(int64(c)), a)
	case opGetField:
		return ex.index(op, *ex.reg(b), p.k[c], a)
	case opSelf:
		obj := *ex.reg(b)
		*ex.reg(a + 1) = obj
		return ex.index(op, obj, p.k[c], a)
	case opSetTable:
		return ex.newindex(op, *ex.reg(a), *ex.reg(b), *ex.reg(c))
	case opSetIndex:
		return ex.newindex(op, *ex.reg(a), Int(int64(b)), *ex.reg(c))
	case opSetField:
		return ex.newindex(op, *ex.reg(a), p.k[b], *ex.reg(c))
	case opNewTable:
		nhash := 0
		if b > 0 {
			nhash = 1 << (b - 1)
		}
		*ex.reg(a) = tableValue(NewTable(int(ex.aux()), nhash))
	case opSetList:
		n := b
		if n == 0 {
			n = ex.top - (st.base + a) - 1
		}
		t := (*Table)(ex.reg(a).p)
		if t.Frozen() {
			return ex.errorf("attempt to modify a frozen table")
		}
		off := int64(ex.aux())
		for j := 1; j <= n; j++ {
			t.set(Int(off+int64(j)), ex.stack[st.base+a+j])
		}

	case opAdd, opSub, opMul, opDiv, opMod, opPow, opIDiv, opBAnd, opBOr, opBXor, opShl, opShr:
		return ex.arithOp(op, a, *ex.reg(b), *ex.reg(c))
	case opAddK, opSubK, opMulK, opDivK, opModK, opPowK, opIDivK, opBAndK, opBOrK, opBXorK, opShlK, opShrK:
		return ex.arithOp(op-(opAddK-opAdd), a, *ex.reg(b), p.k[c])
	case opUnm:
		x := *ex.reg(b)
		if n, ok := toNumber(x); ok {
			r, _ := numArith(opSub, Int(0), n)
			if n.p == tFloat() {
				r = Float(-math.Float64frombits(n.n))
			}
			*ex.reg(a) = r
			return nil
		}
		return ex.errorf("attempt to perform arithmetic on a %s value", typeName(x))
	case opBNot:
		x := *ex.reg(b)
		if n, ok := toNumber(x); ok {
			if v, ok := toIntExact(n); ok {
				*ex.reg(a) = Int(^v)
				return nil
			}
			return ex.errorf("number has no integer representation")
		}
		return ex.errorf("attempt to perform bitwise operation on a %s value", typeName(x))
	case opNot:
		*ex.reg(a) = Bool(!ex.reg(b).truthy())
	case opLen:
		x := *ex.reg(b)
		switch {
		case x.isString():
			*ex.reg(a) = Int(int64(x.n & lenMask))
		case x.isObj(kindTable):
			*ex.reg(a) = Int(int64((*Table)(x.p).Len()))
		default:
			return ex.errorf("attempt to get length of a %s value", typeName(x))
		}
	case opConcat:
		return ex.concat(a, b)

	case opJmp:
		off := int(int16(decBx(i)))
		st.pc += off
		if off < 0 {
			return ex.tick()
		}
	case opJmpX:
		off := int(int32(ex.aux())) + 1
		st.pc += off
		if off < 0 {
			return ex.tick()
		}
		return nil
	case opEqJmp:
		ex.jumpAux(rawEqual(*ex.reg(a), *ex.reg(b)) == (c != 0))
		return nil
	case opEqKJmp:
		ex.jumpAux(rawEqual(*ex.reg(a), p.k[b]) == (c != 0))
		return nil
	case opLtJmp, opLeJmp, opLtKJmp, opLeKJmp, opGtKJmp, opGeKJmp:
		x, y := *ex.reg(a), Value{}
		switch op {
		case opLtJmp, opLeJmp:
			y = *ex.reg(b)
		case opGtKJmp, opGeKJmp:
			x, y = p.k[b], x
		default:
			y = p.k[b]
		}
		var r bool
		var err error
		if op == opLtJmp || op == opLtKJmp || op == opGtKJmp {
			r, err = ex.lessThan(x, y)
		} else {
			r, err = ex.lessEqual(x, y)
		}
		if err != nil {
			return err
		}
		ex.jumpAux(r == (c != 0))
		return nil
	case opTestJmp:
		ex.jumpAux(ex.reg(a).truthy() == (c != 0))
		return nil
	case opTestSet:
		v := *ex.reg(b)
		if v.truthy() != (c != 0) {
			*ex.reg(a) = v
		}
		ex.jumpAux(v.truthy() == (c != 0))
		return nil

	case opForPrep:
		return ex.forPrep(i)
	case opForLoop:
		return ex.forLoop(i)
	case opTForPrep:
		if err := ex.markTBC(st.base + a + 3); err != nil {
			return err
		}
		st.pc += decBx(i)
	case opTForCall:
		base := st.base + a
		if err := ex.ensure(base + 7); err != nil {
			return err
		}
		copy(ex.stack[base+4:base+7], ex.stack[base:base+3])
		return ex.callValue(base+4, 2)
	case opTForLoop:
		if r := ex.reg(a + 4); !r.IsNil() {
			*ex.reg(a + 2) = *r
			st.pc -= decBx(i)
			return ex.tick()
		}

	case opCall:
		fn := st.base + a
		nargs := b - 1
		if b == 0 {
			nargs = ex.top - fn - 1
		}
		return ex.callValue(fn, nargs)
	case opTailCall:
		fn := st.base + a
		nargs := b - 1
		if b == 0 {
			nargs = ex.top - fn - 1
		}
		return ex.tailCall(fn, nargs)
	case opReturn:
		n := b - 1
		if b == 0 {
			n = ex.top - (st.base + a)
		}
		return ex.doReturn(st.base+a, n)
	case opReturn0:
		return ex.doReturn(st.base, 0)
	case opReturn1:
		return ex.doReturn(st.base+a, 1)
	case opClosure:
		np := p.protos[decBx(i)]
		ncl := &Closure{p: np, up: make([]Value, len(np.upvals)), env: st.cl.env}
		*ex.reg(a) = luaFnValue(ncl) // stored before the captures, so a local function can capture itself
		for j, u := range np.upvals {
			if u.fromStack {
				ncl.up[j] = *ex.reg(int(u.index))
			} else {
				ncl.up[j] = st.cl.up[u.index]
			}
		}
	case opVarargPrep:
		// the frame was adjusted when it was pushed
	case opVararg:
		return ex.vararg(a, c)
	case opTBC:
		return ex.markTBC(st.base + a)
	case opClose:
		return ex.closeTo(st.base + a)

	default:
		return ex.errorf("bad opcode %d", op)
	}
	ex.skipAux(op)
	return nil
}

func envValue(cl *Closure) Value {
	if cl.env == nil {
		return Value{}
	}
	return tableValue(cl.env)
}

// tick charges one step, as a back-edge does in fast.
func (ex *executor) tick() error {
	if ex.st.ticks--; ex.st.ticks < 0 {
		return ex.refill()
	}
	return nil
}

// index does R[dst] = obj[key] for the instruction op, with __index. It learns the hint of a
// GETFIELD or GETGLOBAL site.
func (ex *executor) index(op uint8, obj, key Value, dst int) error {
	st := &ex.st
	for hops := 0; hops < maxIndexHops; hops++ {
		switch {
		case obj.isObj(kindTable):
			t := (*Table)(obj.p)
			v, slot := t.getSlot(key)
			if slot >= 0 && hops == 0 && (op == opGetField || op == opGetGlobal || op == opSelf) {
				learn(&st.cl.p.hints[ex.aux()], slot)
			}
			if !v.IsNil() || t.meta == nil {
				*ex.reg(dst) = v
				ex.skipAux(op)
				return nil
			}
			h := t.meta.Get(String("__index"))
			if h.IsNil() {
				*ex.reg(dst) = Value{}
				ex.skipAux(op)
				return nil
			}
			if h.isObj(kindLuaFn) || h.isObj(kindGoFn) {
				return ex.callMeta(h, obj, key)
			}
			obj = h
		case obj.isObj(kindUser):
			ti := (*ex.types)[uint32(obj.n)]
			name, _ := key.Str()
			for j := range ti.fields {
				if ti.fields[j].name == name {
					if op == opGetField || op == opSelf {
						learn(&st.cl.p.hints[ex.aux()], j)
					}
					*ex.reg(dst) = ti.fields[j].load(obj.p)
					ex.skipAux(op)
					return nil
				}
			}
			return ex.errorf("no field '%s' on %s", name, ti.rt.Name())
		case obj.isString():
			// strings index the string library through their metatable, which comes with the library
			return ex.errorf("attempt to index a string value")
		default:
			if hops == 0 {
				return ex.errorf("attempt to index a %s value", typeName(obj))
			}
			return ex.errorf("attempt to index a %s value ('__index')", typeName(obj))
		}
	}
	return ex.errorf("'__index' chain too long; possibly a loop")
}

// newindex does obj[key] = val for the instruction op, with __newindex.
func (ex *executor) newindex(op uint8, obj, key, val Value) error {
	for hops := 0; hops < maxIndexHops; hops++ {
		switch {
		case obj.isObj(kindTable):
			t := (*Table)(obj.p)
			if t.meta != nil {
				if old := t.Get(key); old.IsNil() {
					if h := t.meta.Get(String("__newindex")); !h.IsNil() {
						if h.isObj(kindLuaFn) || h.isObj(kindGoFn) {
							return ex.callMeta(h, obj, key, val)
						}
						obj = h
						continue
					}
				}
			}
			if t.Frozen() {
				return ex.errorf("attempt to modify a frozen table")
			}
			k, ok := normKey(key)
			if !ok {
				if key.IsNil() {
					return ex.errorf("index is nil")
				}
				return ex.errorf("index is NaN")
			}
			t.set(k, val)
			ex.skipAux(op)
			return nil
		default:
			if hops == 0 {
				return ex.errorf("attempt to index a %s value", typeName(obj))
			}
			return ex.errorf("attempt to index a %s value ('__newindex')", typeName(obj))
		}
	}
	return ex.errorf("'__newindex' chain too long; possibly a loop")
}

// callMeta calls a metamethod for the instruction at pc-1. A Lua handler runs in a
// continuation frame, and finishOp completes the instruction when it returns. A Go handler
// runs here, and the instruction is completed at once.
func (ex *executor) callMeta(h Value, args ...Value) error {
	st := &ex.st
	fn := max(st.base+int(st.cl.p.maxstack), ex.top) // above the frame and any open list
	if err := ex.ensure(fn + 1 + len(args) + stackSlack); err != nil {
		return err
	}
	ex.stack[fn] = h
	copy(ex.stack[fn+1:], args)
	st.hi = max(st.hi, fn+1+len(args)) // so that reset clears them
	if h.isObj(kindLuaFn) {
		return ex.pushLua((*Closure)(h.p), fn, len(args), frCont)
	}
	n, err := ex.callGoRaw((*goFunc)(h.p), fn, len(args))
	if err != nil {
		return err
	}
	return ex.finishOp(fn, n)
}

// finishOp completes the instruction at pc-1 after its metamethod returned n results at
// stack[fn], as luaV_finishOp does in C Lua.
func (ex *executor) finishOp(fn, n int) error {
	st := &ex.st
	i := ex.instr(st.pc - 1)
	op := uint8(i)
	var r Value
	if n > 0 {
		r = ex.stack[fn]
	}
	switch op {
	case opGetField, opGetTable, opGetIndex, opGetGlobal, opSelf:
		*ex.reg(decA(i)) = r
	case opSetField, opSetTable, opSetIndex, opSetGlobal:
	case opClose:
		st.pc-- // close the next variable
		return nil
	default:
		return ex.errorf("cannot resume %s", opName(op))
	}
	ex.skipAux(op)
	return nil
}

// arithOp does R[dst] = x op y for numbers and numeric strings.
func (ex *executor) arithOp(op uint8, dst int, x, y Value) error {
	a, ok1 := toNumber(x)
	b, ok2 := toNumber(y)
	if ok1 && ok2 {
		r, err := numArith(op, a, b)
		switch err {
		case arithDivZero:
			if op == opMod {
				return ex.errorf("attempt to perform 'n%%%%0'")
			}
			return ex.errorf("attempt to perform 'n//0'")
		case arithNoInt:
			return ex.errorf("number has no integer representation")
		}
		*ex.reg(dst) = r
		return nil
	}
	bad := x
	if ok1 {
		bad = y
	}
	switch op {
	case opBAnd, opBOr, opBXor, opShl, opShr:
		return ex.errorf("attempt to perform bitwise operation on a %s value", typeName(bad))
	}
	return ex.errorf("attempt to perform arithmetic on a %s value", typeName(bad))
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
	return false, ex.compareError(x, y)
}

// lessEqual is Lua's <= without metamethods.
func (ex *executor) lessEqual(x, y Value) (bool, error) {
	switch {
	case x.p == tInt() && y.p == tInt():
		return int64(x.n) <= int64(y.n), nil
	case x.isNumber() && y.isNumber():
		if x.p == tFloat() && math.IsNaN(math.Float64frombits(x.n)) || y.p == tFloat() && math.IsNaN(math.Float64frombits(y.n)) {
			return false, nil
		}
		return !numLess(y, x), nil
	case x.isString() && y.isString():
		return x.str() <= y.str(), nil
	}
	return false, ex.compareError(x, y)
}

func (ex *executor) compareError(x, y Value) error {
	tx, ty := typeName(x), typeName(y)
	if tx == ty {
		return ex.errorf("attempt to compare two %s values", tx)
	}
	return ex.errorf("attempt to compare %s with %s", tx, ty)
}

// concat does R[a] = R[a] .. ... .. R[a+n-1] for strings and numbers, in one allocation.
func (ex *executor) concat(a, n int) error {
	regs := ex.stack[ex.st.base+a : ex.st.base+a+n]
	size := 0
	for _, v := range regs {
		switch {
		case v.isString():
			size += int(v.n & lenMask)
		case v.isNumber():
			size += len(v.String())
		default:
			bad := v
			return ex.errorf("attempt to concatenate a %s value", typeName(bad))
		}
	}
	var sb strings.Builder
	sb.Grow(size)
	for _, v := range regs {
		if v.isString() {
			sb.WriteString(v.str())
		} else {
			sb.WriteString(v.String())
		}
	}
	regs[0] = String(sb.String())
	return nil
}

// callValue calls the value at stack[fn] with nargs arguments, for the CALL or TFORCALL at pc-1.
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

// pushLua starts a call of cl at stack[fn] with nargs arguments as a new frame of the loop.
// The frame record returns to the current frame at st.pc.
func (ex *executor) pushLua(cl *Closure, fn, nargs int, flags uint64) error {
	if err := ex.pushFrame(cl, fn, nargs, ex.st.pc, ex.st.base, flags); err != nil {
		return err
	}
	return ex.tick() // a call ticks
}

// pushFrame pushes a frame for cl at stack[fn], which returns to the frame at callerBase,
// resuming at pc. A vararg function's arguments stay below its frame: the function and its
// fixed parameters are copied above them, as C Lua does.
func (ex *executor) pushFrame(cl *Closure, fn, nargs, pc, callerBase int, flags uint64) error {
	st := &ex.st
	if err := ex.ensure(fn + 1 + max(nargs, int(cl.p.nparams)) + 1 + stackSlack); err != nil {
		return err
	}
	if st.nf == len(ex.pframes) {
		if len(ex.pframes) >= ex.maxCallDepth {
			return ex.errorf("stack overflow")
		}
		ex.pframes = append(ex.pframes, make([]uint64, min(len(ex.pframes), ex.maxCallDepth-len(ex.pframes)))...)
	}
	np := int(cl.p.nparams)
	if nargs < np {
		clear(ex.stack[fn+1+nargs : fn+1+np])
		nargs = np
	}
	base := fn + 1
	if cl.p.vararg {
		nextra := nargs - np
		base = fn + 1 + nargs + 1
		ex.stack[base-1] = ex.stack[fn]
		copy(ex.stack[base:base+np], ex.stack[fn+1:fn+1+np])
		clear(ex.stack[fn+1 : fn+1+np])
		flags |= frVararg
		for len(ex.nvar) < len(ex.pframes) {
			ex.nvar = append(ex.nvar, 0)
		}
		ex.nvar[st.nf] = int32(nextra)
	}
	ex.pframes[st.nf] = uint64(uint32(pc)) | uint64(base-callerBase)<<32 | flags
	st.nf++
	st.cl, st.base, st.pc = cl, base, 0
	st.hi = max(st.hi, base+int(cl.p.maxstack))
	return nil
}

// funcSlot returns the stack index of the current frame's function as its caller placed it:
// below the arguments, for a vararg frame.
func (ex *executor) funcSlot() int {
	st := &ex.st
	fr := ex.pframes[st.nf-1]
	if fr&frVararg == 0 {
		return st.base - 1
	}
	return st.base - int(st.cl.p.nparams) - int(ex.nvar[st.nf-1]) - 2
}

// tailCall calls the value at stack[fn] in place of the current frame.
func (ex *executor) tailCall(fn, nargs int) error {
	st := &ex.st
	v := ex.stack[fn]
	if !v.isObj(kindLuaFn) {
		// a Go function: call it, and let the RETURN after the TAILCALL return its results
		return ex.callValue(fn, nargs)
	}
	dst := ex.funcSlot()
	fr := ex.pframes[st.nf-1]
	callerBase := st.base - int(fr>>32&0xffffff)
	copy(ex.stack[dst:dst+1+nargs], ex.stack[fn:fn+1+nargs])
	clear(ex.stack[dst+1+nargs : fn+1+nargs])
	st.nf--
	if err := ex.pushFrame((*Closure)(v.p), dst, nargs, int(uint32(fr)), callerBase, fr&(frGoEntry|frCont)); err != nil {
		return err
	}
	return ex.tick()
}

// doReturn returns n results starting at stack[first] from the current frame.
func (ex *executor) doReturn(first, n int) error {
	fn := ex.st.base - 1
	if err := ex.ensure(fn + n); err != nil {
		return err
	}
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

// frameReturned pops the frame whose n results start at stack[fn], its function slot, and
// returns to whoever started it. done reports that the goEntry frame of this run returned.
func (ex *executor) frameReturned(fn, n int) (done bool, err error) {
	st := &ex.st
	fr := ex.pframes[st.nf] // already popped
	if fr&frVararg != 0 {
		real := st.base - int(st.cl.p.nparams) - int(ex.nvar[st.nf]) - 2
		copy(ex.stack[real:real+n], ex.stack[fn:fn+n])
		fn = real
	}
	if fr&frGoEntry != 0 {
		ex.top = fn + n
		return true, nil
	}
	st.base -= int(fr >> 32 & 0xffffff)
	st.cl = (*Closure)(ex.stack[st.base-1].p)
	st.pc = int(uint32(fr))
	if fr&frCont != 0 {
		return false, ex.finishOp(fn, n)
	}
	ex.adjustResults(ex.instr(st.pc-1), fn, n)
	return false, nil
}

// adjustResults pads or drops the n results at stack[fn] to what the CALL or TFORCALL i expects.
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

// vararg copies the current frame's extra arguments to R[a], ...: c-1 of them, or all for c=0.
func (ex *executor) vararg(a, c int) error {
	st := &ex.st
	nextra := 0
	if fr := ex.pframes[st.nf-1]; fr&frVararg != 0 {
		nextra = int(ex.nvar[st.nf-1])
	}
	from := st.base - 1 - nextra
	n := c - 1
	if c == 0 {
		n = nextra
		if err := ex.ensure(st.base + a + n); err != nil {
			return err
		}
		from = st.base - 1 - nextra
		ex.top = st.base + a + n
		st.hi = max(st.hi, ex.top)
	}
	dst := ex.stack[st.base+a : st.base+a+n]
	m := copy(dst, ex.stack[from:from+min(n, nextra)])
	clear(dst[m:])
	return nil
}

// markTBC registers the to-be-closed variable at stack index r. Nil and false are ignored.
func (ex *executor) markTBC(r int) error {
	v := ex.stack[r]
	if !v.truthy() {
		return nil
	}
	if ex.metamethod(v, "__close").IsNil() {
		return ex.errorf("variable got a non-closable value")
	}
	ex.tbc = append(ex.tbc, r)
	return nil
}

// closeTo calls the __close metamethods of the to-be-closed variables at stack index level
// and above, the most recent first. A Lua handler runs in a continuation frame that comes
// back to the same CLOSE instruction, which then closes the next one.
func (ex *executor) closeTo(level int) error {
	for len(ex.tbc) > 0 && ex.tbc[len(ex.tbc)-1] >= level {
		r := ex.tbc[len(ex.tbc)-1]
		ex.tbc = ex.tbc[:len(ex.tbc)-1]
		v := ex.stack[r]
		h := ex.metamethod(v, "__close")
		switch {
		case h.isObj(kindLuaFn):
			return ex.callMeta(h, v, Value{})
		case h.isObj(kindGoFn):
			fn := max(ex.st.base+int(ex.st.cl.p.maxstack), ex.top)
			if err := ex.ensure(fn + 3 + stackSlack); err != nil {
				return err
			}
			ex.stack[fn], ex.stack[fn+1], ex.stack[fn+2] = h, v, Value{}
			ex.st.hi = max(ex.st.hi, fn+3)
			if _, err := ex.callGoRaw((*goFunc)(h.p), fn, 2); err != nil {
				return err
			}
		}
	}
	return nil
}

// metamethod returns the metamethod event of v, or nil.
func (ex *executor) metamethod(v Value, event string) Value {
	if v.isObj(kindTable) {
		if mt := (*Table)(v.p).meta; mt != nil {
			return mt.Get(String(event))
		}
	}
	return Value{}
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

// typeName is the Lua type name of v, as error messages use it.
func typeName(v Value) string {
	switch k := v.Kind(); k {
	case KindInt, KindFloat:
		return "number"
	default:
		return k.String()
	}
}
