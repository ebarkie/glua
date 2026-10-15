package glua

import (
	"reflect"
	"strings"
	"testing"
)

func compileOK(t *testing.T, src string) *proto {
	t.Helper()
	p, _, err := compileChunk("t", src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	return p
}

// The code between VARARGPREP and the final RETURN, without pc numbers.
func body(p *proto) string {
	var lines []string
	for _, l := range strings.Split(p.disasm(), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 || f[1] == "VARARGPREP" {
			continue
		}
		lines = append(lines, strings.Join(f[1:], " "))
	}
	return strings.Join(lines, "; ")
}

func TestFolding(t *testing.T) {
	tests := []struct{ src, want string }{
		// folded
		{"return 2 * 3 + 1", "LOADI R0 7; RETURN1 R0; RETURN0"},
		{"return 2^10", "LOADF R0 1024; RETURN1 R0; RETURN0"},
		{"return 7 // 2, 7 % 3, 1 << 4, ~0, -(-3)", "LOADI R0 3; LOADI R1 1; LOADI R2 16; LOADI R3 -1; LOADI R4 3; RETURN R0 6; RETURN0"},
		{"return 1 / 4", "LOADK R0 K0(0.25); RETURN1 R0; RETURN0"},
		{"return 9223372036854775807 + 1", "LOADK R0 K0(-9223372036854775808); RETURN1 R0; RETURN0"},
		{"return -2.5", "LOADK R0 K0(-2.5); RETURN1 R0; RETURN0"},
		{"local k <const> = 4 return k * k", "LOADI R0 16; RETURN1 R0; RETURN0"},
		{"local a <const> = 2 local b <const> = a * 3 return function() return b + a end", "CLOSURE R0 P0; RETURN1 R0; RETURN0"},
		{"return not nil, not 1", "LOADTRUE R0; LOADFALSE R1; RETURN R0 3; RETURN0"},
		// not folded: would raise, or the result's bits are for run time to decide
		{"return 1 // 0", "LOADI R0 1; IDIVK R0 R0 K0(0); RETURN1 R0; RETURN0"},
		{"return 1 % 0", "LOADI R0 1; MODK R0 R0 K0(0); RETURN1 R0; RETURN0"},
		{"return 1 / 0", "LOADI R0 1; DIVK R0 R0 K0(0); RETURN1 R0; RETURN0"},
		{"return 0.0 / 0.0", "LOADF R0 0; DIVK R0 R0 K0(0.0); RETURN1 R0; RETURN0"},
		{"return -0.0", "LOADF R0 0; UNM R0 R0; RETURN1 R0; RETURN0"},
		{"return 1 - 1.0", "LOADI R0 1; SUBK R0 R0 K0(1.0); RETURN1 R0; RETURN0"},
		{"return 1.5 & 1", "LOADK R0 K1(1.5); BANDK R0 R0 K0(1); RETURN1 R0; RETURN0"},
		{"return '1' + 1", "LOADK R0 K0(\"1\"); ADDK R0 R0 K1(1); RETURN1 R0; RETURN0"},
	}
	for _, tt := range tests {
		p := compileOK(t, tt.src)
		if p.protos != nil && strings.HasPrefix(tt.src, "local a <const>") {
			if got := body(p.protos[0]); got != "LOADI R0 8; RETURN1 R0; RETURN0" {
				t.Errorf("%q, inner function: %s", tt.src, got)
			}
		}
		if got := body(p); got != tt.want {
			t.Errorf("%q:\n got %s\nwant %s", tt.src, got, tt.want)
		}
	}
}

// Folded results are the values the same operations give at run time.
func TestFoldingMatchesRuntime(t *testing.T) {
	for _, tt := range []struct{ x, op, y string }{
		{"2^53", "+", "1"}, {"3", "//", "-2"}, {"-3", "%", "5"}, {"5.5", "%", "-2"}, {"1 << 63", ">>", "1"},
		{"7", "//", "2.0"}, {"0x7fffffffffffffff", "*", "2"}, {"2", "^", "-1074"}, {"1e308", "*", "10"},
	} {
		folded, err := runSource("t", "return "+tt.x+" "+tt.op+" "+tt.y)
		if err != nil {
			t.Fatal(err)
		}
		// the same operation on a variable, which the compiler cannot fold
		runtime, err := runSource("t", "local x = "+tt.x+" return x "+tt.op+" "+tt.y)
		if err != nil {
			t.Fatal(err)
		}
		if !same(&folded[0], &runtime[0]) {
			t.Errorf("%s %s %s: folded %v (%v), at run time %v (%v)", tt.x, tt.op, tt.y, folded[0], folded[0].Kind(), runtime[0], runtime[0].Kind())
		}
	}
}

// Constants of different kinds or bits stay apart; equal ones are shared.
func TestConstantsKeptApart(t *testing.T) {
	p := compileOK(t, "local a, b, c, d, e, f = 1, 1.0, 1, 'x', 'x', 0.0 local g = -0.0 return a, b, c, d, e, f, g, 1.5, 1.5")
	var kinds []string
	for _, k := range p.k {
		kinds = append(kinds, k.Kind().String()+":"+k.String())
	}
	if got := strings.Join(kinds, " "); got != "string:x float:1.5" {
		// small integers and floats are loaded with LOADI and LOADF, which need no constant
		t.Errorf("constants: %s", got)
	}
	p = compileOK(t, "return 100000, 100000.0, 'a', 'a', 1e300, 1e300")
	if len(p.k) != 4 {
		t.Errorf("want 4 constants, got %d: %v", len(p.k), p.k)
	}
}

// String constants are canonical: the same text in different prototypes, and a bound
// struct field of that name, share one pointer, so the first word compare decides.
func TestCanonicalStrings(t *testing.T) {
	p := compileOK(t, "local t = {} t.method = 1 return function() return t.method end")
	var outer, inner Value
	for _, k := range p.k {
		if k.isString() && k.str() == "method" {
			outer = k
		}
	}
	for _, k := range p.protos[0].k {
		if k.isString() && k.str() == "method" {
			inner = k
		}
	}
	id := bindType(reflect.TypeFor[request]())
	field := (*types.Load())[id].fields[0].key
	if outer.p == nil || outer.p != inner.p || outer.p != field.p {
		t.Fatalf("pointers differ: %p %p %p", outer.p, inner.p, field.p)
	}
}

func TestTableSizing(t *testing.T) {
	p := compileOK(t, "return {1, 2, 3, x = 1, y = 2, z = 3, [4] = 4}")
	i := p.code[1] // after VARARGPREP
	if uint8(i) != opNewTable {
		t.Fatalf("first instruction is %s", opName(uint8(i)))
	}
	if narr, b := p.code[2], decB(i); narr != 3 || b != 3 { // 4 hash entries: 2^(3-1)
		t.Errorf("array size %d, hash size code %d", narr, b)
	}
}
