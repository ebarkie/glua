package glua

import "fmt"

// Debug information is cold: it is read only to build error messages and tracebacks.

// lineAt returns the source line of the code word at pc, or 0.
func (p *Proto) lineAt(pc int) int {
	if pc >= 0 && pc < len(p.lines) {
		return int(p.lines[pc])
	}
	return 0
}

// localName returns the name of the local in register reg at pc, or "".
func (p *Proto) localName(reg, pc int) string {
	name := ""
	for _, lv := range p.locvars {
		if lv.reg == reg && lv.startpc <= pc && (lv.endpc < 0 || pc < lv.endpc) {
			name = lv.name // the innermost, declared last, wins
		}
	}
	return name
}

// setsReg reports whether the instruction i writes register reg.
func setsReg(i uint32, reg int) bool {
	a, b, c := decA(i), decB(i), decC(i)
	switch uint8(i) {
	case opMove, opLoadK, opLoadKX, opLoadI, opLoadF, opLoadFalse, opLoadTrue, opGetUpval, opGetUpCell,
		opNewCell, opGetCell, opGetGlobal, opGetEnv, opGetTable, opGetIndex, opGetField, opNewTable,
		opAdd, opSub, opMul, opDiv, opMod, opPow, opIDiv, opBAnd, opBOr, opBXor, opShl, opShr,
		opAddK, opSubK, opMulK, opDivK, opModK, opPowK, opIDivK, opBAndK, opBOrK, opBXorK, opShlK, opShrK,
		opUnm, opNot, opLen, opBNot, opConcat, opTestSet, opClosure:
		return reg == a
	case opLoadNil:
		return a <= reg && reg <= a+b
	case opSelf:
		return reg == a || reg == a+1
	case opForPrep, opForLoop:
		return a <= reg && reg <= a+3
	case opTForCall:
		return reg >= a+4 && reg < a+3+c
	case opCall, opTailCall, opVararg:
		return reg >= a
	}
	return false
}

// findSetReg finds the last instruction before lastpc that sets reg, as C Lua's findsetreg
// does. An instruction inside a stretch that a jump skips may not have run, so it does not
// count.
func (p *Proto) findSetReg(lastpc, reg int) int {
	setreg, jmptarget := -1, 0
	for pc := 0; pc < lastpc; pc++ {
		i := p.code[pc]
		op := uint8(i)
		target := -1
		switch {
		case op == opJmp:
			target = pc + 1 + int(int16(decBx(i)))
		case hasAux[op] && opTable[op].aux == oJumpX:
			target = pc + 2 + int(int32(p.code[pc+1]))
		}
		if target > pc && target <= lastpc && target > jmptarget {
			jmptarget = target
		}
		if setsReg(i, reg) {
			if pc < jmptarget {
				setreg = -1 // conditional: it may not have run
			} else {
				setreg = pc
			}
		}
		if hasAux[op] {
			pc++
		}
	}
	return setreg
}

// varInfo describes where register reg got its value at pc, as C Lua's getobjname does:
// "local 'x'", "global 'x'", "field 'x'", "upvalue 'x'", "method 'x'" or "constant 'x'".
// It returns "" when it cannot tell.
func (p *Proto) varInfo(pc, reg int) string {
	kind, name := p.objName(pc, reg)
	if kind == "" {
		return ""
	}
	return fmt.Sprintf("%s '%s'", kind, name)
}

func (p *Proto) objName(lastpc, reg int) (kind, name string) {
	if n := p.localName(reg, lastpc); n != "" {
		return "local", n
	}
	pc := p.findSetReg(lastpc, reg)
	if pc < 0 {
		return "", ""
	}
	i := p.code[pc]
	switch uint8(i) {
	case opMove:
		if b := decB(i); b < decA(i) {
			return p.objName(pc, b)
		}
	case opGetCell:
		if n := p.localName(decB(i), pc); n != "" {
			return "local", n
		}
	case opGetUpval, opGetUpCell:
		if b := decB(i); b < len(p.upnames) {
			return "upvalue", p.upnames[b]
		}
	case opGetGlobal:
		return "global", p.k[decBx(i)].str()
	case opGetField:
		key := p.k[decC(i)].str()
		if k, n := p.objName(pc, decB(i)); n == "_ENV" && (k == "local" || k == "upvalue") {
			return "global", key
		}
		return "field", key
	case opGetTable:
		if k, n := p.objName(pc, decC(i)); k == "constant" {
			return "field", n
		}
		return "field", "?"
	case opGetIndex:
		return "field", "integer index"
	case opSelf:
		return "method", p.k[decC(i)].str()
	case opLoadK:
		if k := p.k[decBx(i)]; k.isString() {
			return "constant", k.str()
		}
	case opLoadKX:
		if k := p.k[p.code[pc+1]]; k.isString() {
			return "constant", k.str()
		}
	}
	return "", ""
}
