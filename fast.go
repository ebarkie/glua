package glua

import (
	"math"
	"sync/atomic"
	"unsafe"
)

// kAt returns constant i of the constant array at k.
func kAt(k unsafe.Pointer, i uint32) *Value { return (*Value)(unsafe.Add(k, uintptr(i)*16)) }

// isNum reports whether v is an integer or a float. Small enough to inline into fast.
func isNum(v *Value) bool { return v.p == tInt() || v.p == tFloat() }

// toFloat converts a number to a float. Small enough to inline into fast.
func toFloat(v *Value) float64 {
	if v.p == tInt() {
		return float64(int64(v.n))
	}
	return math.Float64frombits(v.n)
}

// numPair loads two numbers as floats. ok is false unless both are numbers.
func numPair(x, y *Value) (a, b float64, ok bool) {
	if !isNum(x) || !isNum(y) {
		return 0, 0, false
	}
	return toFloat(x), toFloat(y), true
}

// fast is the hot loop. It makes no Go calls, so the register allocator keeps its state in
// machine registers; the only calls in its code are write barriers, bounds-check panics and
// morestack, which a CI gate checks. Anything rare or allocating leaves through an exit and
// is done by drive. Between runs only indices are kept, never addresses.
//
// One exit convention: pc always points one word past the primary instruction word, so the
// driver can re-decode the instruction at pc-1. It skips auxiliary words itself.
//
//go:noinline
func (ex *executor) fast() exit {
	cl := ex.st.cl
	stack0 := unsafe.Pointer(unsafe.SliceData(ex.stack))
	regs := (*[256]Value)(unsafe.Add(stack0, ex.st.base*16))
	k := unsafe.Pointer(unsafe.SliceData(cl.p.k))
	pc := unsafe.Add(unsafe.Pointer(unsafe.SliceData(cl.p.code)), ex.st.pc*4)
	ex.st.limitAddr = uintptr(stack0) + uintptr(len(ex.stack)-stackSlack)*16 // a frame base at or above it cannot fit a [256]Value view
	ex.st.hiAddr = uintptr(stack0) + uintptr(ex.st.hi)*16
	var e exit

dispatch:
	for {
		i := *(*uint32)(pc)
		pc = unsafe.Add(pc, 4)
		switch uint8(i) { // a jump table, after one range check
		case opMove:
			setValue(&regs[uint8(i>>8)], &regs[uint8(i>>16)])

		case opLoadK:
			setValue(&regs[uint8(i>>8)], kAt(k, i>>16))

		case opLoadI:
			setInt(&regs[uint8(i>>8)], uint64(int64(int16(i>>16))))

		case opLoadNil:
			a, b := uint8(i>>8), uint8(i>>16)
			for j := uint8(0); ; j++ { // an index loop: a range loop would become a memclr call
				regs[a+j] = Value{}
				if j == b {
					break
				}
			}

		case opLoadFalse:
			setBool(&regs[uint8(i>>8)], 0)

		case opLoadTrue:
			setBool(&regs[uint8(i>>8)], 1)

		case opGetUpval:
			setValue(&regs[uint8(i>>8)], (*Value)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(cl.up)), uintptr(uint8(i>>16))*16))) // index verified

		case opGetField: // R[A] = R[B][K[C]]; the aux word is the site number
			obj, key := &regs[uint8(i>>16)], kAt(k, i>>24)
			hint := atomic.LoadUint32((*uint32)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(cl.p.hints)), uintptr(*(*uint32)(pc))*4))) // site verified
			slot := hint & hintSlotMask
			if obj.n == kindTable<<56 && !isScalar(obj.p) {
				t := (*Table)(obj.p)
				if int(slot) < len(t.nodes) {
					// a hit needs the key, and a value or no metatable: a deleted key must still consult __index
					if nd := &t.nodes[slot]; same(&nd.k, key) && (nd.v.p != nil || t.meta == nil) {
						setValue(&regs[uint8(i>>8)], &nd.v)
						pc = unsafe.Add(pc, 4)
						continue
					}
				}
				if hint == megamorphic && len(t.nodes) > 0 {
					// the site has given up on its hint: probe inline with the hash computed at compile time
					h, mask := *(*uint64)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(cl.p.khash)), uintptr(i>>24)*8)), uint64(len(t.nodes)-1)
					for j := uint64(0); j < 8; j++ {
						nd := &t.nodes[(h+j)&mask]
						if same(&nd.k, key) {
							if nd.v.p == nil && t.meta != nil {
								break
							}
							setValue(&regs[uint8(i>>8)], &nd.v)
							pc = unsafe.Add(pc, 4)
							continue dispatch
						}
						if nd.k.p == nil || nd.k.n == key.n {
							break // absent, or an equal-length string behind another pointer: the driver decides
						}
					}
				}
			} else if obj.n>>56 == kindUser && !isScalar(obj.p) { // both halves: an integer can carry these bits
				ti := (*ex.types)[uint32(obj.n)] // the type ID: below 2^32, so no 48-bit mask constant ties up a register
				if int(slot) < len(ti.fields) && same(&ti.fields[slot].key, key) {
					fd, d := &ti.fields[slot], &regs[uint8(i>>8)]
					p := unsafe.Add(obj.p, fd.off)
					switch fd.kind {
					case fInt64:
						setInt(d, uint64(*(*int64)(p)))
					case fInt:
						setInt(d, uint64(*(*int)(p)))
					case fFloat64:
						setFloat(d, *(*float64)(p))
					case fBool:
						setBool(d, uint64(*(*uint8)(p)))
					case fString:
						if s := (*[2]uintptr)(p); s[1] == 0 {
							*d = Value{p: unsafe.Pointer(&emptyStr[0]), n: kindString << 56}
						} else {
							*d = Value{p: *(*unsafe.Pointer)(p), n: kindString<<56 | uint64(s[1])}
						}
					}
					pc = unsafe.Add(pc, 4)
					continue
				}
			}
			e = exSlowOp // pc still points at the aux word, one past the primary word
			break dispatch

		case opAdd, opAddK:
			x, y := &regs[uint8(i>>16)], &regs[uint8(i>>24)]
			if uint8(i) == opAddK {
				y = kAt(k, i>>24)
			}
			d := &regs[uint8(i>>8)]
			if x.p == tInt() && y.p == tInt() {
				setInt(d, x.n+y.n)
			} else if x.p == tFloat() && y.p == tFloat() {
				setFloat(d, math.Float64frombits(x.n)+math.Float64frombits(y.n))
			} else if isNum(x) && isNum(y) {
				setFloat(d, toFloat(x)+toFloat(y))
			} else {
				e = exSlowOp // strings and metamethods
				break dispatch
			}

		case opSub, opSubK:
			x, y := &regs[uint8(i>>16)], &regs[uint8(i>>24)]
			if uint8(i) == opSubK {
				y = kAt(k, i>>24)
			}
			d := &regs[uint8(i>>8)]
			if x.p == tInt() && y.p == tInt() {
				setInt(d, x.n-y.n)
			} else if x.p == tFloat() && y.p == tFloat() {
				setFloat(d, math.Float64frombits(x.n)-math.Float64frombits(y.n))
			} else if isNum(x) && isNum(y) {
				setFloat(d, toFloat(x)-toFloat(y))
			} else {
				e = exSlowOp
				break dispatch
			}

		case opMul, opMulK:
			x, y := &regs[uint8(i>>16)], &regs[uint8(i>>24)]
			if uint8(i) == opMulK {
				y = kAt(k, i>>24)
			}
			d := &regs[uint8(i>>8)]
			if x.p == tInt() && y.p == tInt() {
				setInt(d, x.n*y.n)
			} else if x.p == tFloat() && y.p == tFloat() {
				setFloat(d, math.Float64frombits(x.n)*math.Float64frombits(y.n))
			} else if isNum(x) && isNum(y) {
				setFloat(d, toFloat(x)*toFloat(y))
			} else {
				e = exSlowOp
				break dispatch
			}

		case opJmp:
			off := int(int16(i >> 16))
			pc = unsafe.Add(pc, off*4)
			if off < 0 {
				if ex.st.ticks--; ex.st.ticks < 0 { // every back-edge ticks
					e = exTick
					break dispatch
				}
			}

		case opLtJmp, opLtKJmp: // test R[A] < R[B] (or K[B]); fall through when it equals C, else jump by the aux word
			x, y := &regs[uint8(i>>8)], &regs[uint8(i>>16)]
			if uint8(i) == opLtKJmp {
				y = kAt(k, uint32(uint8(i>>16)))
			}
			var lt bool
			if x.p == tInt() && y.p == tInt() {
				lt = int64(x.n) < int64(y.n)
			} else if x.p == tFloat() && y.p == tFloat() {
				lt = math.Float64frombits(x.n) < math.Float64frombits(y.n)
			} else {
				e = exSlowOp // mixed numbers compare exactly in the driver; strings and metamethods too
				break dispatch
			}
			if lt == (i>>24 != 0) { // C is the polarity: fall through when the test equals it
				pc = unsafe.Add(pc, 4)
			} else {
				pc = unsafe.Add(pc, (int(int32(*(*uint32)(pc)))+1)*4)
			}

		case opEqKJmp: // test R[A] == K[B]; fall through when it equals C, else jump by the aux word
			x, y := &regs[uint8(i>>8)], kAt(k, uint32(uint8(i>>16)))
			var eq bool
			switch {
			case same(x, y):
				eq = x.p != tFloat() || x.n<<1 <= 0x7ff<<53 // the same bits are equal, unless they are a NaN
			case isScalar(x.p):
				if (x.p == tInt() || x.p == tFloat()) && (y.p == tInt() || y.p == tFloat()) {
					e = exSlowOp // 1 == 1.0 and 0.0 == -0.0: exact compare in the driver
					break dispatch
				}
			case x.n != y.n || x.n>>56 != kindString:
				// different kinds, lengths or objects. Tables and userdata with __eq are not constants.
			case x.n&lenMask <= 32: // equal length behind different pointers: compare inline
				eq = true
				for j := uintptr(0); j < uintptr(x.n&lenMask); j++ {
					if *(*byte)(unsafe.Add(x.p, j)) != *(*byte)(unsafe.Add(y.p, j)) {
						eq = false
						break
					}
				}
			default:
				e = exSlowOp // long strings: memequal is a call
				break dispatch
			}
			if eq == (i>>24 != 0) {
				pc = unsafe.Add(pc, 4)
			} else {
				pc = unsafe.Add(pc, (int(int32(*(*uint32)(pc)))+1)*4)
			}

		case opForPrep: // R[A] init, R[A+1] limit, R[A+2] step, R[A+3] the visible control variable
			a := uint8(i >> 8)
			init, limit, step := &regs[a], &regs[a+1], &regs[a+2]
			if init.p != tInt() || limit.p != tInt() || step.p != tInt() || int64(step.n) <= 0 {
				e = exSlowOp // float loops, float limits, negative and zero steps: the driver prepares them
				break dispatch
			}
			if int64(init.n) > int64(limit.n) {
				pc = unsafe.Add(pc, int(i>>16)*4) // no iteration at all
			} else {
				limit.n = (limit.n - init.n) / step.n // iterations still to come, as a uint64: it cannot overflow
				regs[a+3] = *init
			}

		case opForLoop:
			a := uint8(i >> 8)
			if regs[a+2].p != tInt() {
				e = exSlowOp // a float loop
				break dispatch
			}
			if cnt := regs[a+1].n; cnt > 0 {
				regs[a+1].n = cnt - 1
				idx := regs[a].n + regs[a+2].n // never wraps: the count ends the loop first
				regs[a].n = idx
				setInt(&regs[a+3], idx)
				pc = unsafe.Add(pc, -int(i>>16)*4)
				if ex.st.ticks--; ex.st.ticks < 0 { // every back-edge ticks
					e = exTick
					break dispatch
				}
			}

		case opCall: // R[A], ... = R[A](R[A+1], ..., R[A+B-1]); C-1 results are expected
			a := uint8(i >> 8)
			fn := &regs[a]
			if fn.n == kindLuaFn<<56 && !isScalar(fn.p) && uint8(i>>16) != 0 && !(*Closure)(fn.p).p.vararg {
				if uintptr(unsafe.Pointer(fn)) >= ex.st.limitAddr {
					e = exGrowStack
					break dispatch
				}
				if ex.st.nf == len(ex.pframes) {
					e = exGrowFrames
					break dispatch
				}
				// the record has no pointers: the caller's closure is read back from its own function slot
				code0 := uintptr(unsafe.Pointer(unsafe.SliceData(cl.p.code)))
				ex.pframes[ex.st.nf] = uint64(uint32((uintptr(pc)-code0)>>2)) | uint64(a+1)<<32
				ex.st.nf++
				cl = (*Closure)(fn.p)
				regs = (*[256]Value)(unsafe.Pointer(&regs[a+1]))
				for j := int(uint8(i>>16)) - 1; j < int(cl.p.nparams); j++ {
					regs[uint8(j)] = Value{} // only missing parameters are filled
				}
				if top := uintptr(unsafe.Pointer(regs)) + uintptr(cl.p.maxstack)*16; top > ex.st.hiAddr {
					ex.st.hiAddr = top // raised at push, so it is also right after an error
				}
				k = unsafe.Pointer(unsafe.SliceData(cl.p.k))
				pc = unsafe.Pointer(unsafe.SliceData(cl.p.code))
				if ex.st.ticks--; ex.st.ticks < 0 {
					e = exTick
					break dispatch
				}
			} else if fn.n == kindGoFn<<56 && !isScalar(fn.p) {
				e = exHostCall
				break dispatch
			} else {
				e = exSlowOp // open argument lists, vararg callees, __call and errors live in the driver
				break dispatch
			}

		case opReturn1, opReturn0:
			var r Value
			var n int
			if uint8(i) == opReturn1 {
				r, n = regs[uint8(i>>8)], 1
			}
			*(*Value)(unsafe.Add(unsafe.Pointer(regs), -16)) = r // the function slot is the result slot
			ex.st.nf--
			fr := ex.pframes[ex.st.nf]
			if fr>>56 != 0 {
				ex.st.nret = n
				e = exReturn // a frame the VM started: Go entry, or a continuation
				break dispatch
			}
			regs = (*[256]Value)(unsafe.Add(unsafe.Pointer(regs), -int(fr>>32&0xffffff)*16))
			cl = (*Closure)((*Value)(unsafe.Add(unsafe.Pointer(regs), -16)).p)
			k = unsafe.Pointer(unsafe.SliceData(cl.p.k))
			pc = unsafe.Add(unsafe.Pointer(unsafe.SliceData(cl.p.code)), int(uint32(fr))*4)
			if want := *(*uint32)(unsafe.Add(pc, -4)) >> 24; want != 2 { // C of the CALL at pc-1
				ex.st.nret = n
				e = exResults // zero, several or an open number of results: the driver pads or drops
				break dispatch
			}

		default: // everything else is done by the driver, which also rejects opBad. Listing all 256
			// values would not drop the range check: the compiler merges the unused ones into one run,
			// and the switch then fails the jump-table density test.
			e = exSlowOp
			break dispatch
		}
	}

	// indices, never addresses: nothing outside this function may hold a pointer into the stack
	// stack0 is recomputed rather than kept: one live register less in the loop
	stack0 = unsafe.Pointer(unsafe.SliceData(ex.stack))
	ex.st.cl = cl
	ex.st.base = int((uintptr(unsafe.Pointer(regs)) - uintptr(stack0)) / 16)
	ex.st.pc = int((uintptr(pc) - uintptr(unsafe.Pointer(unsafe.SliceData(cl.p.code)))) / 4)
	ex.st.hi = int((ex.st.hiAddr - uintptr(stack0)) / 16)
	return e
}
