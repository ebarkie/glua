package syntax

import (
	"fmt"
	"strings"
	"testing"
)

// sexpr renders a tree compactly, to check its shape.
func sexpr(n any) string {
	switch n := n.(type) {
	case nil:
		return "_"
	case *NilExpr:
		return "nil"
	case *TrueExpr:
		return "true"
	case *FalseExpr:
		return "false"
	case *VarargExpr:
		return "..."
	case *IntExpr:
		return fmt.Sprint(n.Value)
	case *FloatExpr:
		return fmt.Sprintf("%gf", n.Value)
	case *StringExpr:
		return fmt.Sprintf("%q", n.Value)
	case *NameExpr:
		return n.Name
	case *IndexExpr:
		return "(idx " + sexpr(n.Obj) + " " + sexpr(n.Key) + ")"
	case *CallExpr:
		return "(call " + sexpr(n.Fn) + list(n.Args) + ")"
	case *MethodCallExpr:
		return "(mcall " + sexpr(n.Obj) + " " + n.Name + list(n.Args) + ")"
	case *FunctionExpr:
		va := ""
		if n.IsVararg {
			va = " ..."
		}
		return "(fn " + n.Name + " [" + strings.Join(n.Params, " ") + va + "] " + sexpr(n.Body) + ")"
	case *BinaryExpr:
		return "(" + n.Op.String() + " " + sexpr(n.L) + " " + sexpr(n.R) + ")"
	case *UnaryExpr:
		op := n.Op.String()
		if n.Op == Minus {
			op = "neg"
		}
		return "(" + op + " " + sexpr(n.X) + ")"
	case *TableExpr:
		var parts []string
		for _, f := range n.Fields {
			if f.Key == nil {
				parts = append(parts, sexpr(f.Value))
			} else {
				parts = append(parts, "["+sexpr(f.Key)+"]="+sexpr(f.Value))
			}
		}
		return "{" + strings.Join(parts, " ") + "}"
	case *ParenExpr:
		return "(paren " + sexpr(n.X) + ")"
	case *Block:
		var parts []string
		for _, s := range n.Stmts {
			parts = append(parts, sexpr(s))
		}
		return "{" + strings.Join(parts, "; ") + "}"
	case *AssignStmt:
		return "(set" + list(n.Targets) + " =" + list(n.Values) + ")"
	case *CallStmt:
		return sexpr(n.Call)
	case *DoStmt:
		return "(do " + sexpr(n.Body) + ")"
	case *WhileStmt:
		return "(while " + sexpr(n.Cond) + " " + sexpr(n.Body) + ")"
	case *RepeatStmt:
		return "(repeat " + sexpr(n.Body) + " " + sexpr(n.Cond) + ")"
	case *IfStmt:
		s := "(if"
		for i := range n.Conds {
			s += " " + sexpr(n.Conds[i]) + " " + sexpr(n.Blocks[i])
		}
		if n.Else != nil {
			s += " else " + sexpr(n.Else)
		}
		return s + ")"
	case *NumericForStmt:
		return "(for " + n.Var + " " + sexpr(n.Start) + " " + sexpr(n.Limit) + " " + sexpr(n.Step) + " " + sexpr(n.Body) + ")"
	case *GenericForStmt:
		return "(forin [" + strings.Join(n.Names, " ") + "]" + list(n.Exprs) + " " + sexpr(n.Body) + ")"
	case *FunctionStmt:
		return "(function " + sexpr(n.Target) + " " + sexpr(n.Func) + ")"
	case *LocalFunctionStmt:
		return "(local function " + n.Name + " " + sexpr(n.Func) + ")"
	case *LocalStmt:
		var names []string
		for i, name := range n.Names {
			if n.Attribs[i] != "" {
				name += "<" + n.Attribs[i] + ">"
			}
			names = append(names, name)
		}
		return "(local [" + strings.Join(names, " ") + "]" + list(n.Values) + ")"
	case *ReturnStmt:
		return "(return" + list(n.Values) + ")"
	case *BreakStmt:
		return "break"
	case *GotoStmt:
		return "(goto " + n.Label + ")"
	case *LabelStmt:
		return "(label " + n.Name + ")"
	}
	return fmt.Sprintf("?%T", n)
}

func list(es []Expr) string {
	s := ""
	for _, e := range es {
		s += " " + sexpr(e)
	}
	return s
}

func TestShapes(t *testing.T) {
	tests := []struct{ src, want string }{
		{"return 1 + 2 * 3", "{(return (+ 1 (* 2 3)))}"},
		{"return 1 - 2 - 3", "{(return (- (- 1 2) 3))}"},
		{"return 2 ^ 3 ^ 2", "{(return (^ 2 (^ 3 2)))}"},
		{"return -2 ^ 2", "{(return (neg (^ 2 2)))}"},
		{"return 2 ^ -3", "{(return (^ 2 (neg 3)))}"},
		{"return 'a' .. 'b' .. 'c'", `{(return (.. "a" (.. "b" "c")))}`},
		{"return 1 + 2 .. 3", "{(return (.. (+ 1 2) 3))}"},
		{"return a or b and c", "{(return (or a (and b c)))}"},
		{"return not a == b", "{(return (== (not a) b))}"},
		{"return a < b == c", "{(return (== (< a b) c))}"},
		{"return 1 | 2 ~ 3 & 4 << 5", "{(return (| 1 (~ 2 (& 3 (<< 4 5)))))}"},
		{"return ~a // b % c", "{(return (% (// (~ a) b) c))}"},
		{"return #t + 1", "{(return (+ (# t) 1))}"},
		{"return - - 2", "{(return (neg (neg 2)))}"},
		{"x = a.b[c].d", `{(set x = (idx (idx (idx a "b") c) "d"))}`},
		{"f(1)(2):m 's' {3}", `{(call (mcall (call (call f 1) 2) m "s") {3})}`},
		{"x, y.z = 1, (f())", `{(set x (idx y "z") = 1 (paren (call f)))}`},
		{"local a <const>, b <close> = 1", "{(local [a<const> b<close>] 1)}"},
		{"local function f(a, ...) return ... end", "{(local function f (fn f [a ...] {(return ...)}))}"},
		{"function a.b:c(x) end", `{(function (idx (idx a "b") "c") (fn a.b:c [self x] {}))}`},
		{"for i = 1, 10 do end for k, v in pairs(t) do end", "{(for i 1 10 _ {}); (forin [k v] (call pairs t) {})}"},
		{"if a then elseif b then x() else y() end", "{(if a {} b {(call x)} else {(call y)})}"},
		{"while x do break end repeat local y until y", "{(while x {break}); (repeat {(local [y])} y)}"},
		{"::top:: goto top ;;; do end", "{(label top); (goto top); (do {})}"},
		{"t = {1, x = 2, [3] = 4; 5,}", `{(set t = {1 ["x"]=2 [3]=4 5})}`},
		{"return function() end", "{(return (fn <anonymous> [] {}))}"},
		{"return 1, 2;", "{(return 1 2)}"},
		{"x = f\n(g)", "{(set x = (call f g))}"},
		{"return 0x10, 1e2, 'q'", `{(return 16 100f "q")}`},
	}
	for _, tt := range tests {
		fn, err := Parse("t", tt.src)
		if err != nil {
			t.Errorf("%q: %v", tt.src, err)
			continue
		}
		if got := sexpr(fn.Body); got != tt.want {
			t.Errorf("%q:\n got %s\nwant %s", tt.src, got, tt.want)
		}
	}
}

// The expected messages come from C Lua 5.4.9's load. The semantic checks C Lua does in
// its single pass (goto, labels, break, const) belong to scope resolution and are tested there.
func TestSyntaxErrors(t *testing.T) {
	tests := []struct{ src, err string }{
		{"x = 1 +", "t:1: unexpected symbol near <eof>"},
		{"x =", "t:1: unexpected symbol near <eof>"},
		{"f(", "t:1: unexpected symbol near <eof>"},
		{"local function", "t:1: <name> expected near <eof>"},
		{"if x then", "t:1: 'end' expected near <eof>"},
		{"if x then else", "t:1: 'end' expected near <eof>"},
		{"for i = 1 do end", "t:1: ',' expected near 'do'"},
		{"for i, j do end", "t:1: 'in' expected near 'do'"},
		{"for 1 = 1, 2 do end", "t:1: <name> expected near '1'"},
		{"local x <foo> = 1", "t:1: unknown attribute 'foo'"},
		{"local x <close>, y <close> = 1, 2", "t:1: multiple to-be-closed variables in local list"},
		{"x", "t:1: syntax error near <eof>"},
		{"x.y", "t:1: syntax error near <eof>"},
		{"1 = 2", "t:1: unexpected symbol near '1'"},
		{"(x) = 1", "t:1: syntax error near '='"},
		{"f() = 1", "t:1: syntax error near '='"},
		{"x, f() = 1, 2", "t:1: syntax error near '='"},
		{"function f(a, 1) end", "t:1: <name> or '...' expected near '1'"},
		{"local function f() return ... end", "t:1: cannot use '...' outside a vararg function near '...'"},
		{"return 1 return 2", "t:1: <eof> expected near 'return'"},
		{"return return", "t:1: unexpected symbol near 'return'"},
		{"a.b:c = 1", "t:1: function arguments expected near '='"},
		{"x = function", "t:1: '(' expected near <eof>"},
		{"x = {1, 2,, 3}", "t:1: unexpected symbol near ','"},
		{"x = a..b..", "t:1: unexpected symbol near <eof>"},
		{"while true do", "t:1: 'end' expected near <eof>"},
		{"repeat x = 1", "t:1: 'until' expected near <eof>"},
		{"local 1", "t:1: <name> expected near '1'"},
		{"x = #", "t:1: unexpected symbol near <eof>"},
		{"if x then elseif end", "t:1: unexpected symbol near 'end'"},
		{"x = 1 2", "t:1: unexpected symbol near '2'"},
		{"for i = 1, 2, 3, 4 do end", "t:1: 'do' expected near ','"},
		{"function a.b:c.d() end", "t:1: '(' expected near '.'"},
		{"return x = @", "t:1: <eof> expected near '='"},
		{"x = @", "t:1: unexpected symbol near '@'"},
		{"x = \x01", "t:1: unexpected symbol near '<\\1>'"},
		{"return [=[]]=]=]", "t:1: <eof> expected near '='"},
		{"if x then\n\nx = 1", "t:3: 'end' expected (to close 'if' at line 1) near <eof>"},
		{"f(\n1", "t:2: ')' expected (to close '(' at line 1) near <eof>"},
		{"x = 'a\nb'", "t:1: unfinished string near ''a'"},
	}
	for _, tt := range tests {
		_, err := Parse("t", tt.src)
		if err == nil || err.Error() != tt.err {
			t.Errorf("%q: got %v, want %q", tt.src, err, tt.err)
		}
	}
	for _, src := range []string{
		"function f(...) return ... end return ...", "f{}.x = 1", "x = ...", "local a <const>",
		"goto continue; local x; ::continue::", "repeat local x until x", "local t = {} t:m\"s\"",
		"x = 'a' .. 1 .. 2",
	} {
		if _, err := Parse("t", src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

func TestNestingLimits(t *testing.T) {
	ok := func(src string) {
		t.Helper()
		if _, err := Parse("t", src); err != nil {
			t.Errorf("%.40q...: %v", src, err)
		}
	}
	tooDeep := func(src string) {
		t.Helper()
		_, err := Parse("t", src)
		if err == nil || !strings.Contains(err.Error(), "chunk has too many syntax levels") {
			t.Errorf("%.40q...: got %v", src, err)
		}
	}
	ok("x = " + strings.Repeat("(", 100) + "1" + strings.Repeat(")", 100))
	tooDeep("x = " + strings.Repeat("(", 300) + "1" + strings.Repeat(")", 300))
	ok(strings.Repeat("do ", 100) + strings.Repeat("end ", 100))
	tooDeep(strings.Repeat("do ", 300) + strings.Repeat("end ", 300))
	tooDeep("x = " + strings.Repeat("{", 300) + strings.Repeat("}", 300))
	tooDeep("x = " + strings.Repeat("'a' .. ", 300) + "'a'") // right associative: recursion
	ok("x = 1" + strings.Repeat(" + 1", 1000))               // left associative: a loop
	tooDeep("x = 1" + strings.Repeat(" + 1", 3000))
	ok("x = a" + strings.Repeat(".b", 1000))
	tooDeep("x = a" + strings.Repeat(".b", 3000))
}

func FuzzParser(f *testing.F) {
	for _, s := range []string{
		"local x <const> = 1 return x + 2 * 3",
		"function a.b:c(...) return ... end",
		"for i = 1, 10 do if i % 2 == 0 then goto c end ::c:: end",
		"t = {1, 2, x = 3, [4] = {5}}; f(t):m 's' {}",
		"repeat local x = f() until x or not y",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		fn, err := Parse("t", src) // must not panic or hang
		if err == nil {
			Resolve("t", fn)
		}
	})
}
