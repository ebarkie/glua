package glua

import (
	"fmt"
	"math"
	"unique"
	"unsafe"
)

// Proto is a compiled function. It never changes after it is built, except for its hint
// array, which is written with atomics and never affects results.
type Proto struct {
	code     []uint32 // ends with a padding word, so pc never points past the allocation
	k        []Value
	khash    []uint64 // hash of each constant, for the inline probe of a megamorphic site
	hints    []uint32 // one per field-access site: misses<<24 | slot
	protos   []*Proto
	upvals   []upvalDesc
	nparams  uint8
	maxstack uint8
	source   string
	name     string

	handles []unique.Handle[string] // keep canonical constant strings alive as long as the proto
}

// upvalDesc says where a closure's capture comes from when CLOSURE runs.
type upvalDesc struct {
	fromStack bool // a register of the enclosing function; otherwise one of its upvalues
	index     uint8
}

// Closure is a Lua function value: a prototype and its captured values.
type Closure struct {
	p  *Proto
	up []Value
}

// goFunc is a Go function value.
type goFunc struct {
	f    GoFunc
	name string
}

func luaFnValue(cl *Closure) Value { return Value{p: unsafe.Pointer(cl), n: kindLuaFn << 56} }
func goFnValue(g *goFunc) Value    { return Value{p: unsafe.Pointer(g), n: kindGoFn << 56} }

// asm assembles a prototype by hand. It stands in for the compiler until milestone 1.
type asm struct {
	p      Proto
	labels map[string]int
	fixups []fixup
}

type fixup struct {
	at    int // index of the word to patch
	from  int // jump origin: the word after the instruction, or after its aux word
	label string
	wide  bool // the offset lives in the aux word; otherwise in Bx
	back  bool // Bx counts backwards
}

func newAsm(name string, nparams, maxstack uint8) *asm {
	return &asm{p: Proto{name: name, source: name, nparams: nparams, maxstack: maxstack}, labels: map[string]int{}}
}

// konst adds a constant and returns its index. Strings are made canonical with unique.Make,
// and constants are told apart by kind and bits, so 1 and 1.0 stay distinct.
func (a *asm) konst(v Value) int {
	for i := range a.p.k {
		if same(&a.p.k[i], &v) {
			return i
		}
	}
	if s, ok := v.Str(); ok {
		h := unique.Make(s)
		a.p.handles = append(a.p.handles, h)
		v = String(h.Value())
	}
	a.p.k = append(a.p.k, v)
	a.p.khash = append(a.p.khash, hashValue(v))
	return len(a.p.k) - 1
}

func (a *asm) kstr(s string) uint8    { return uint8(a.konst(String(s))) }
func (a *asm) kint(i int64) uint8     { return uint8(a.konst(Int(i))) }
func (a *asm) kfloat(f float64) uint8 { return uint8(a.konst(Float(f))) }

func (a *asm) emit(i uint32) { a.p.code = append(a.p.code, i) }

func (a *asm) label(name string) { a.labels[name] = len(a.p.code) }

func (a *asm) abc(op uint8, x, y, z int) { a.emit(iABC(op, uint8(x), uint8(y), uint8(z))) }
func (a *asm) abx(op uint8, x, bx int)   { a.emit(iABx(op, uint8(x), uint16(bx))) }
func (a *asm) loadI(x int, v int16)      { a.emit(iABx(opLoadI, uint8(x), uint16(v))) }

// site emits a field access with a fresh hint site in its aux word.
func (a *asm) field(op uint8, x, y, z int) {
	a.abc(op, x, y, z)
	a.emit(uint32(len(a.p.hints)))
	a.p.hints = append(a.p.hints, 0)
}

// cjump emits a compare-and-jump whose aux word jumps to label when the test fails.
func (a *asm) cjump(op uint8, x, y int, label string) {
	a.abc(op, x, y, 0)
	a.fixups = append(a.fixups, fixup{at: len(a.p.code), from: len(a.p.code) + 1, label: label, wide: true})
	a.emit(0)
}

func (a *asm) jmp(label string) {
	a.fixups = append(a.fixups, fixup{at: len(a.p.code), from: len(a.p.code) + 1, label: label})
	a.emit(iABx(opJmp, 0, 0))
}

// forPrep and forLoop emit a numeric for loop over R[x..x+3]; the body sits between them.
func (a *asm) forPrep(x int, exit string) {
	a.fixups = append(a.fixups, fixup{at: len(a.p.code), from: len(a.p.code) + 1, label: exit})
	a.emit(iABx(opForPrep, uint8(x), 0))
}

func (a *asm) forLoop(x int, body string) {
	a.fixups = append(a.fixups, fixup{at: len(a.p.code), from: len(a.p.code) + 1, label: body, back: true})
	a.emit(iABx(opForLoop, uint8(x), 0))
}

func (a *asm) proto(p *Proto) int {
	a.p.protos = append(a.p.protos, p)
	return len(a.p.protos) - 1
}

func (a *asm) upval(fromStack bool, index int) {
	a.p.upvals = append(a.p.upvals, upvalDesc{fromStack: fromStack, index: uint8(index)})
}

// done resolves jumps, appends the padding word and verifies the result.
func (a *asm) done() *Proto {
	for _, f := range a.fixups {
		target, ok := a.labels[f.label]
		if !ok {
			panic("asm: no label " + f.label)
		}
		off := target - f.from
		switch {
		case f.wide:
			a.p.code[f.at] = uint32(int32(off))
		case f.back:
			if off > 0 || -off > math.MaxUint16 {
				panic("asm: bad backward jump to " + f.label)
			}
			a.p.code[f.at] |= uint32(-off) << 16
		default:
			if off < 0 && a.p.code[f.at]&0xff == opForPrep || off < math.MinInt16 || off > math.MaxInt16 {
				panic("asm: bad jump to " + f.label)
			}
			a.p.code[f.at] |= uint32(uint16(int16(off))) << 16
		}
	}
	a.emit(opBad)
	p := a.p
	if err := verify(&p); err != nil {
		panic(err)
	}
	return &p
}

// verify checks what the loop trusts: register operands below maxstack, constant, upvalue
// and prototype indices in range, jumps landing on the first word of an instruction, aux
// words consumed, and the padding word at the end. It is the spike's subset of the verifier.
func verify(p *Proto) error {
	n := len(p.code)
	if n == 0 || p.code[n-1] != opBad {
		return fmt.Errorf("%s: missing padding word", p.name)
	}
	if p.maxstack < p.nparams {
		return fmt.Errorf("%s: maxstack below nparams", p.name)
	}
	starts := make([]bool, n)
	for pc := 0; pc < n-1; pc++ {
		starts[pc] = true
		if hasAux[uint8(p.code[pc])] {
			pc++
		}
	}
	reg := func(pc, r int) error {
		if r >= int(p.maxstack) {
			return fmt.Errorf("%s:%d: register %d not below maxstack %d", p.name, pc, r, p.maxstack)
		}
		return nil
	}
	kidx := func(pc, k int) error {
		if k >= len(p.k) {
			return fmt.Errorf("%s:%d: constant %d out of range", p.name, pc, k)
		}
		return nil
	}
	target := func(pc, t int) error {
		if t < 0 || t >= n-1 || !starts[t] {
			return fmt.Errorf("%s:%d: jump to %d is not an instruction", p.name, pc, t)
		}
		return nil
	}
	for pc := 0; pc < n-1; pc++ {
		i := p.code[pc]
		op, a, b, c := uint8(i), decA(i), decB(i), decC(i)
		next := pc + 1
		if hasAux[op] {
			if pc+1 >= n-1 {
				return fmt.Errorf("%s:%d: missing aux word", p.name, pc)
			}
			next++
		}
		var errs []error
		switch op {
		case opMove:
			errs = append(errs, reg(pc, a), reg(pc, b))
		case opLoadK:
			errs = append(errs, reg(pc, a), kidx(pc, decBx(i)))
		case opLoadI, opLoadFalse, opLoadTrue, opNewTable:
			errs = append(errs, reg(pc, a))
		case opLoadNil:
			errs = append(errs, reg(pc, a+b))
		case opGetUpval:
			errs = append(errs, reg(pc, a))
			if b >= len(p.upvals) {
				errs = append(errs, fmt.Errorf("%s:%d: upvalue %d out of range", p.name, pc, b))
			}
		case opGetField, opSetField:
			x, kk, z := a, c, b
			if op == opSetField {
				x, kk, z = a, b, c
			}
			errs = append(errs, reg(pc, x), reg(pc, z), kidx(pc, kk))
			if int(p.code[pc+1]) >= len(p.hints) {
				errs = append(errs, fmt.Errorf("%s:%d: hint site out of range", p.name, pc))
			}
		case opAdd, opSub, opMul:
			errs = append(errs, reg(pc, a), reg(pc, b), reg(pc, c))
		case opAddK, opSubK, opMulK:
			errs = append(errs, reg(pc, a), reg(pc, b), kidx(pc, c))
		case opJmp:
			errs = append(errs, target(pc, next+int(int16(decBx(i)))))
		case opLtJmp:
			errs = append(errs, reg(pc, a), reg(pc, b), target(pc, next+int(int32(p.code[pc+1]))))
		case opLtKJmp, opEqKJmp:
			errs = append(errs, reg(pc, a), kidx(pc, b), target(pc, next+int(int32(p.code[pc+1]))))
		case opForPrep:
			errs = append(errs, reg(pc, a+3), target(pc, next+decBx(i)))
		case opForLoop:
			errs = append(errs, reg(pc, a+3), target(pc, next-decBx(i)))
		case opCall:
			errs = append(errs, reg(pc, a))
			if b > 0 {
				errs = append(errs, reg(pc, a+b-1))
			}
			if c > 1 {
				errs = append(errs, reg(pc, a+c-2))
			}
		case opReturn:
			if b > 1 {
				errs = append(errs, reg(pc, a+b-2))
			} else {
				errs = append(errs, reg(pc, a))
			}
		case opReturn0:
		case opReturn1:
			errs = append(errs, reg(pc, a))
		case opClosure:
			errs = append(errs, reg(pc, a))
			if decBx(i) >= len(p.protos) {
				errs = append(errs, fmt.Errorf("%s:%d: prototype out of range", p.name, pc))
			} else {
				for _, u := range p.protos[decBx(i)].upvals {
					if u.fromStack {
						errs = append(errs, reg(pc, int(u.index)))
					} else if int(u.index) >= len(p.upvals) {
						errs = append(errs, fmt.Errorf("%s:%d: captured upvalue out of range", p.name, pc))
					}
				}
			}
		default:
			return fmt.Errorf("%s:%d: unknown opcode %d", p.name, pc, op)
		}
		for _, err := range errs {
			if err != nil {
				return err
			}
		}
		if hasAux[op] {
			pc++
		}
	}
	return nil
}
