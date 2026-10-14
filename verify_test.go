package glua

import (
	"strings"
	"testing"
)

// mk builds a prototype from raw words without the assembler's checks, appending the padding.
func mk(nparams, maxstack uint8, k []Value, code ...uint32) *Proto {
	p := &Proto{name: "v", nparams: nparams, maxstack: maxstack, k: k, hints: make([]uint32, 2)}
	p.code = append(code, opBad)
	p.upvals = []upvalDesc{{fromStack: true}}
	return p
}

func TestVerifyRejects(t *testing.T) {
	str := []Value{String("x"), Int(1)}
	tests := []struct {
		name string
		p    *Proto
		want string
	}{
		{"no padding", &Proto{name: "v", maxstack: 1, code: []uint32{iABC(opReturn0, 0, 0, 0)}}, "missing padding"},
		{"unknown opcode", mk(0, 1, nil, 250), "unknown opcode"},
		{"register out of range", mk(0, 2, nil, iABC(opMove, 2, 0, 0)), "register 2 not below maxstack 2"},
		{"constant out of range", mk(0, 1, str, iABx(opLoadK, 0, 2)), "constant 2 out of range"},
		{"field name not a string", mk(0, 2, str, iABC(opGetField, 0, 1, 1), 0), "constant 1 is not a string"},
		{"hint site out of range", mk(0, 2, str, iABC(opGetField, 0, 1, 0), 9), "hint site 9 out of range"},
		{"upvalue out of range", mk(0, 1, nil, iABC(opGetUpval, 0, 1, 0)), "upvalue 1 out of range"},
		{"jump into aux word", mk(0, 2, str, iABx(opJmp, 0, 1), iABC(opGetField, 0, 1, 0), 0, iABx(opJmp, 0, uint16(0xfffd))), "jump to 2 is not an instruction"},
		{"jump past the end", mk(0, 1, nil, iABx(opJmp, 0, 5)), "is not an instruction"},
		{"backward FORPREP target", mk(0, 4, nil, iABx(opForLoop, 0, 3)), "jump to -2"},
		{"for loop range", mk(0, 3, nil, iABx(opForPrep, 0, 0), iABC(opReturn0, 0, 0, 0)), "register 3 not below maxstack 3"},
		{"call arguments", mk(0, 2, nil, iABC(opCall, 0, 3, 1)), "register 2 not below"},
		{"call results", mk(0, 2, nil, iABC(opCall, 0, 1, 4)), "register 2 not below"},
		{"concat of one", mk(0, 2, nil, iABC(opConcat, 0, 1, 0)), "fewer than two operands"},
		{"bad polarity", mk(0, 2, nil, iABC(opLtJmp, 0, 1, 2), 0, iABC(opReturn0, 0, 0, 0)), "polarity 2"},
		{"missing aux", mk(0, 2, str, iABC(opGetField, 0, 1, 0)), "missing aux word"},
		{"vararg prep mismatch", mk(1, 2, nil, iABC(opVarargPrep, 0, 0, 0)), "0 fixed parameters, the prototype has 1"},
		{"loadnil range", mk(0, 2, nil, iABC(opLoadNil, 1, 1, 0)), "register 2 not below"},
		{"tforcall range", mk(0, 5, nil, iABC(opTForCall, 0, 0, 2)), "register 5 not below"},
		{"closure prototype", mk(0, 1, nil, iABx(opClosure, 0, 0)), "prototype 0 out of range"},
		{"maxstack below params", &Proto{name: "v", nparams: 2, maxstack: 1, code: []uint32{opBad}}, "maxstack below nparams"},
	}
	for _, tt := range tests {
		err := verify(tt.p)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: got %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestVerifyAccepts(t *testing.T) {
	for _, p := range []*Proto{progFloatLoop(10), progFib(5), progFieldRead(10), progRule(), progCallHost(), progForCount(Int(1), Int(2), Int(1))} {
		if err := verify(p); err != nil {
			t.Errorf("%s: %v", p.name, err)
		}
	}
}

func TestOpTableComplete(t *testing.T) {
	for op := 1; op < numOps; op++ {
		if opTable[op].name == "" {
			t.Errorf("opcode %d has no entry", op)
		}
	}
	if numOps > 256 {
		t.Fatalf("%d opcodes do not fit in a byte", numOps)
	}
}

func TestDisasm(t *testing.T) {
	got := progRule().disasm()
	want := `   0  GETFIELD  R1 R0 K0("method") site0
   2  EQKJMP    R1 K1("GET") 1 ->11
   4  GETFIELD  R1 R0 K2("size") site1
   6  LTKJMP    R1 K3(1000) 1 ->11
   8  GETFIELD  R1 R0 K4("admin") site2
  10  RETURN1   R1`
	// the last RETURN1 and the false branch
	if !strings.HasPrefix(got, want[:strings.LastIndex(want, "\n")]) {
		t.Errorf("got\n%s", got)
	}
	if !strings.Contains(got, "LOADFALSE R1") {
		t.Errorf("got\n%s", got)
	}
}
