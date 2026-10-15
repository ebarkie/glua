package glua

import (
	"fmt"

	"strings"
)

// verify checks what the loop trusts, in one linear pass: register operands below maxstack,
// including the ranges of calls, loops and lists; constant, upvalue, prototype and hint
// indices in range; field names that are strings; jumps that land on the first word of an
// instruction; aux words consumed by the instruction before them; and the padding word at
// the end, so pc never points past the allocation. Every compile ends with it, and the VM
// trusts verified code, not the compiler.
func verify(p *proto) error {
	n := len(p.code)
	if n == 0 || p.code[n-1] != opBad {
		return fmt.Errorf("%s: missing padding word", p.name)
	}
	if p.maxstack < p.nparams {
		return fmt.Errorf("%s: maxstack below nparams", p.name)
	}
	starts := make([]bool, n)
	for pc := 0; pc < n-1; pc++ {
		op := uint8(p.code[pc])
		if int(op) >= numOps || op == opBad {
			return fmt.Errorf("%s:%d: unknown opcode %d", p.name, pc, op)
		}
		starts[pc] = true
		if hasAux[op] {
			pc++
			if pc >= n-1 {
				return fmt.Errorf("%s:%d: missing aux word", p.name, pc-1)
			}
		}
	}
	v := &verifier{p: p, starts: starts}
	for pc := 0; pc < n-1; pc++ {
		if err := v.instr(pc); err != nil {
			return err
		}
		if hasAux[uint8(p.code[pc])] {
			pc++
		}
	}
	return nil
}

type verifier struct {
	p      *proto
	starts []bool
}

func (v *verifier) instr(pc int) error {
	p := v.p
	i := p.code[pc]
	op := uint8(i)
	info := &opTable[op]
	a, b, c, bx := decA(i), decB(i), decC(i), decBx(i)
	next := pc + 1
	var aux uint32
	if info.aux != oNone {
		aux = p.code[pc+1]
		next++
	}
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%s:%d: %s: %s", p.name, pc, info.name, fmt.Sprintf(format, args...))
	}
	reg := func(r int) error {
		if r < 0 || r >= int(p.maxstack) {
			return fail("register %d not below maxstack %d", r, p.maxstack)
		}
		return nil
	}
	target := func(t int) error {
		if t < 0 || t >= len(p.code)-1 || !v.starts[t] {
			return fail("jump to %d is not an instruction", t)
		}
		return nil
	}
	check := func(kind operand, x int) error {
		switch kind {
		case oReg:
			return reg(x)
		case oK, oKX:
			if x >= len(p.k) {
				return fail("constant %d out of range", x)
			}
		case oKStr:
			if x >= len(p.k) {
				return fail("constant %d out of range", x)
			}
			if !p.k[x].isString() {
				return fail("constant %d is not a string", x)
			}
		case oUp:
			if x >= len(p.upvals) {
				return fail("upvalue %d out of range", x)
			}
		case oProto:
			if x >= len(p.protos) {
				return fail("prototype %d out of range", x)
			}
		case oJump:
			return target(next + int(int16(x)))
		case oFwd:
			return target(next + x)
		case oBack:
			return target(next - x)
		case oSite:
			if x >= len(p.hints) {
				return fail("hint site %d out of range", x)
			}
		case oJumpX:
			return target(next + int(int32(uint32(x))))
		}
		return nil
	}
	if info.wide {
		if err := check(info.b, bx); err != nil {
			return err
		}
	} else {
		if err := check(info.b, b); err != nil {
			return err
		}
		if err := check(info.c, c); err != nil {
			return err
		}
	}
	if err := check(info.a, a); err != nil {
		return err
	}
	if info.aux == oJumpX || info.aux == oKX || info.aux == oSite {
		if err := check(info.aux, int(aux)); err != nil {
			return err
		}
	}

	// ranges and operand values the table cannot express
	switch op {
	case opLoadNil:
		return reg(a + b)
	case opSelf:
		return reg(a + 1)
	case opNewTable:
		if b > 32 {
			return fail("hash size 2^%d too large", b)
		}
	case opSetList:
		if b > 0 {
			return reg(a + b)
		}
	case opConcat:
		if b < 2 {
			return fail("fewer than two operands")
		}
		return reg(a + b - 1)
	case opEqJmp, opLtJmp, opLeJmp, opEqKJmp, opLtKJmp, opLeKJmp, opGtKJmp, opGeKJmp, opTestJmp, opTestSet:
		if c > 1 {
			return fail("polarity %d is not 0 or 1", c)
		}
		// conditional jumps do not tick, so they may only go forward: every loop must go
		// through a JMP, JMPX, FORLOOP or TFORLOOP, which charge the step budget
		if int32(aux) < 0 {
			return fail("conditional jump backwards")
		}
	case opForPrep, opForLoop, opTForPrep, opTForLoop:
		if op == opTForLoop {
			return reg(a + 4)
		}
		return reg(a + 3)
	case opTForCall:
		if c < 2 {
			return fail("no loop variables")
		}
		if err := reg(a + 6); err != nil { // the call's copy of iterator, state and control
			return err
		}
		return reg(a + 2 + c)
	case opCall, opTailCall:
		if b > 0 {
			if err := reg(a + b - 1); err != nil {
				return err
			}
		}
		if c > 1 && op == opCall {
			return reg(a + c - 2)
		}
	case opReturn:
		if b > 1 {
			return reg(a + b - 2)
		}
	case opVarargPrep:
		if a != int(p.nparams) {
			return fail("%d fixed parameters, the prototype has %d", a, p.nparams)
		}
	case opVararg:
		if c > 1 {
			return reg(a + c - 2)
		}
	case opFastCall:
		// the CALL it stands for must follow, with the same function and arguments
		if next >= len(p.code)-1 || uint8(p.code[next]) != opCall {
			return fail("not followed by a CALL")
		}
		if ci := p.code[next]; decA(ci) != a || decB(ci) != b || b < 1 {
			return fail("does not match its CALL")
		}
		if aux == 0 || aux >= numFast {
			return fail("unknown built-in %d", aux)
		}
		return reg(a + b - 1)
	case opClosure:
		for _, u := range p.protos[bx].upvals {
			if u.fromStack {
				if err := reg(int(u.index)); err != nil {
					return err
				}
			} else if int(u.index) >= len(p.upvals) {
				return fail("captured upvalue %d out of range", u.index)
			}
		}
	}
	return nil
}

// disasm lists a prototype's code, one instruction per line, for tests and debugging.
func (p *proto) disasm() string {
	var sb strings.Builder
	for pc := 0; pc < len(p.code)-1; pc++ {
		i := p.code[pc]
		op := uint8(i)
		if int(op) >= numOps {
			fmt.Fprintf(&sb, "%4d  ??? %08x\n", pc, i)
			continue
		}
		info := &opTable[op]
		fmt.Fprintf(&sb, "%4d  %-10s ", pc, info.name)
		var args []string
		show := func(kind operand, x int) {
			switch kind {
			case oNone:
			case oReg:
				args = append(args, fmt.Sprintf("R%d", x))
			case oK, oKStr, oKX:
				if x < len(p.k) {
					k := p.k[x]
					if k.isString() {
						args = append(args, fmt.Sprintf("K%d(%q)", x, k.str()))
					} else {
						args = append(args, fmt.Sprintf("K%d(%v)", x, k))
					}
				} else {
					args = append(args, fmt.Sprintf("K%d", x))
				}
			case oUp:
				args = append(args, fmt.Sprintf("U%d", x))
			case oProto:
				args = append(args, fmt.Sprintf("P%d", x))
			case oJump:
				args = append(args, fmt.Sprintf("->%d", pc+1+int(int16(x))))
			case oFwd:
				args = append(args, fmt.Sprintf("->%d", pc+1+x))
			case oBack:
				args = append(args, fmt.Sprintf("->%d", pc+1-x))
			default:
				args = append(args, fmt.Sprint(x))
			}
		}
		show(info.a, decA(i))
		if op == opLoadI || op == opLoadF {
			args = append(args, fmt.Sprint(int16(decBx(i))))
		} else if info.wide {
			show(info.b, decBx(i))
		} else {
			show(info.b, decB(i))
			show(info.c, decC(i))
		}
		if info.aux != oNone && pc+1 < len(p.code) {
			aux := p.code[pc+1]
			switch info.aux {
			case oJumpX:
				args = append(args, fmt.Sprintf("->%d", pc+2+int(int32(aux))))
			case oSite:
				args = append(args, fmt.Sprintf("site%d", aux))
			case oKX:
				show(oK, int(aux))
			default:
				args = append(args, fmt.Sprint(aux))
			}
			pc++
		}
		sb.WriteString(strings.Join(args, " "))
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}
