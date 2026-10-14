package syntax

import (
	"strings"
	"testing"
)

func resolve(t *testing.T, src string) (*FunctionExpr, *Info, error) {
	t.Helper()
	fn, err := Parse("t", src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	info, err := Resolve("t", fn)
	return fn, info, err
}

// The expected messages, including the line C Lua reports them at, come from C Lua 5.4.9.
func TestResolveErrors(t *testing.T) {
	tests := []struct{ src, err string }{
		{"x = 1\nbreak\ny = 2\n", "t:4: break outside loop at line 2"},
		{"do\n goto nowhere\nend\nz = 1\n", "t:5: no visible label 'nowhere' for <goto> at line 2"},
		{"local function f()\n goto nowhere\nend\nz = 1\n", "t:4: no visible label 'nowhere' for <goto> at line 2"},
		{"do\n goto l\n local y\n y = 1\n ::l::\n print(y)\nend\n", "t:6: <goto l> at line 2 jumps into the scope of local 'y'"},
		{"::a::\ndo\n ::a::\nend\n", "t:4: label 'a' already defined on line 1"},
		{"local x <const> = 1\nx\n=\n2\n", "t:3: attempt to assign to const variable 'x'"},
		{"local x <close> = nil\nx = 1", "t:2: attempt to assign to const variable 'x'"},
		{"local x <const> = 1\nlocal function f() x = 2 end", "t:2: attempt to assign to const variable 'x'"},
		{"if true then goto l end local a ::l:: print(a)", "t:1: <goto l> at line 1 jumps into the scope of local 'a'"},
		{"goto l; do ::l:: end", "t:1: no visible label 'l' for <goto> at line 1"},
		{"local x\ngoto l\nlocal y\n::l::\nlocal z\n::m::\nprint(z)", "t:5: <goto l> at line 2 jumps into the scope of local 'y'"},
		{"for i = 1, 2 do\n local function f() break end\nend", "t:3: break outside loop at line 2"},
		{"repeat\n local x\n goto l\n local y\n ::l::\nuntil y", "t:6: <goto l> at line 3 jumps into the scope of local 'y'"},
		{"::a:: ::a::", "t:1: label 'a' already defined on line 1"},
		{"goto x", "t:1: no visible label 'x' for <goto> at line 1"},
		{"break", "t:1: break outside loop at line 1"},
		{"do local x goto l local y ::l:: print(y) end", "t:1: <goto l> at line 1 jumps into the scope of local 'y'"},
		{"local x <const> = 1; x = 2", "t:1: attempt to assign to const variable 'x'"},
		{"local x = 1 local x <const> = 2 x = 3", "t:1: attempt to assign to const variable 'x'"},
		{"repeat goto l; local x ::l:: until x", "t:1: <goto l> at line 1 jumps into the scope of local 'x'"},
	}
	for _, tt := range tests {
		_, _, err := resolve(t, tt.src)
		if err == nil || err.Error() != tt.err {
			t.Errorf("%q: got %v, want %q", tt.src, err, tt.err)
		}
	}
	for _, src := range []string{
		"do ::a:: end ::a::",
		"do goto l; local x; ::l:: end",
		"do goto l; local x; ::l:: ; ; end",
		"do goto l; local x; ::l:: ::m:: end",
		"while true do goto l; local x; ::l:: end",
		"while 1 do local x goto continue local y ::continue:: end",
		"goto continue; local x; ::continue::",
		"repeat local x until x",
		"local x = 1; x = 2; local function f() return x end",
		"for i = 1, 3 do if i == 2 then goto next end ::next:: end",
		"::top:: local x = 1 if x then goto top end",
		"while true do do break end end",
		"_ENV = {}", "local _ENV = {}", "return _ENV",
	} {
		if _, _, err := resolve(t, src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

func TestResolveLimits(t *testing.T) {
	var b strings.Builder
	for i := range 201 {
		b.WriteString("local a")
		b.WriteString(string(rune('a'+i%26)) + strings.Repeat("x", i/26) + "\n")
	}
	if _, _, err := resolve(t, b.String()); err == nil || !strings.Contains(err.Error(), "too many local variables (limit is 200) in main function") {
		t.Errorf("201 locals: %v", err)
	}
	// Locals of a closed block do not count.
	if _, _, err := resolve(t, strings.Repeat("do local a, b, c end\n", 100)); err != nil {
		t.Errorf("block locals: %v", err)
	}
	b.Reset()
	b.WriteString("local function f()\n")
	for i := range 150 {
		b.WriteString("local u")
		b.WriteString(string(rune('a'+i%26)) + strings.Repeat("x", i/26) + "\n")
	}
	b.WriteString("local function g()\nlocal function h()\nreturn ")
	for i := range 150 {
		b.WriteString("u" + string(rune('a'+i%26)) + strings.Repeat("x", i/26) + " + ")
	}
	b.WriteString("x1 + x2 + x3")
	// more than 255 distinct upvalues: the 150 locals of f plus globals do not count, so
	// add locals of g as well
	src := strings.Replace(b.String(), "local function g()\n", "local function g()\n"+locals("v", 120), 1)
	src = strings.Replace(src, "x1 + x2 + x3", sum("v", 120)+"0 end end end", 1)
	if _, _, err := resolve(t, src); err == nil || !strings.Contains(err.Error(), "too many upvalues (limit is 255) in function at line") {
		t.Errorf("270 upvalues: %v", err)
	}
}

func locals(prefix string, n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString("local " + prefix + string(rune('a'+i%26)) + strings.Repeat("y", i/26) + "\n")
	}
	return b.String()
}

func sum(prefix string, n int) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(prefix + string(rune('a'+i%26)) + strings.Repeat("y", i/26) + " + ")
	}
	return b.String()
}

// findVar returns the variables declared under a name, in source order.
func findVars(info *Info, name string) []*Var {
	var vs []*Var
	for _, list := range info.Decls {
		for _, v := range list {
			if v.Name == name {
				vs = append(vs, v)
			}
		}
	}
	return vs
}

func TestCaptureRules(t *testing.T) {
	tests := []struct {
		src            string
		name           string
		captured, cell bool
	}{
		{"local x = 1; return function() return x end", "x", true, false},
		{"local x = 1; x = 2; return function() return x end", "x", true, true},
		{"local x = 1; local f = function() x = 2 end", "x", true, true},
		{"local x = 1; x = 2; return x", "x", false, false},
		{"local x; return function() return x end", "x", true, false},
		{"local function f() return f end", "f", true, false},
		{"for i = 1, 3 do local g = function() return i end end", "i", true, false},
		{"for i = 1, 3 do i = i + 1; local g = function() return i end end", "i", true, true},
		{"local a = {}; return function() return function() return a end end", "a", true, false},
	}
	for _, tt := range tests {
		_, info, err := resolve(t, tt.src)
		if err != nil {
			t.Fatalf("%q: %v", tt.src, err)
		}
		vs := findVars(info, tt.name)
		if len(vs) != 1 {
			t.Fatalf("%q: %d variables named %s", tt.src, len(vs), tt.name)
		}
		if vs[0].Captured != tt.captured || vs[0].Cell() != tt.cell {
			t.Errorf("%q: captured %v cell %v, want %v %v", tt.src, vs[0].Captured, vs[0].Cell(), tt.captured, tt.cell)
		}
	}
}

func TestBindings(t *testing.T) {
	src := `
local a = 1
local function f(b)
  local c = a + b + g
  return function() return a + c + h end
end
`
	fn, info, err := resolve(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if info.ExplicitEnv {
		t.Error("ExplicitEnv set for a chunk that never touches _ENV")
	}
	refs := map[string][]RefKind{}
	for n, r := range info.Names {
		refs[n.Name] = append(refs[n.Name], r.Kind)
	}
	want := map[string][]RefKind{
		"a": {RefUpval, RefUpval}, "b": {RefLocal}, "c": {RefUpval}, "g": {RefGlobal}, "h": {RefGlobal},
	}
	for name, kinds := range want {
		got := refs[name]
		if len(got) != len(kinds) {
			t.Errorf("%s: %v, want %v", name, got, kinds)
			continue
		}
		for _, k := range kinds {
			found := false
			for _, g := range got {
				found = found || g == k
			}
			if !found {
				t.Errorf("%s: %v, want %v", name, got, kinds)
			}
		}
	}
	// the inner function reaches a through f's upvalue
	f := fn.Body.Stmts[1].(*LocalFunctionStmt).Func
	inner := f.Body.Stmts[1].(*ReturnStmt).Values[0].(*FunctionExpr)
	fi := info.Funcs[inner]
	if len(fi.Upvals) != 2 || fi.Upvals[0].Name != "a" || fi.Upvals[0].Parent == nil || fi.Upvals[1].Parent != nil {
		t.Errorf("inner upvalues: %+v", fi.Upvals)
	}
}

func TestExplicitEnv(t *testing.T) {
	_, info, err := resolve(t, "local _ENV = {x = 1}; return x, function() return y end")
	if err != nil {
		t.Fatal(err)
	}
	if !info.ExplicitEnv {
		t.Fatal("ExplicitEnv not set")
	}
	for n, r := range info.Names {
		if n.Name == "x" || n.Name == "y" {
			if r.Kind != RefGlobal || r.Env == nil || r.Env.Var == nil || r.Env.Var.Name != "_ENV" || r.Env.Var == info.EnvVar {
				t.Errorf("%s: %+v", n.Name, r)
			}
		}
	}
	_, info, err = resolve(t, "_ENV = {}; return x")
	if err != nil {
		t.Fatal(err)
	}
	if !info.EnvVar.Assigned || !info.EnvVar.Captured || len(info.Main.Upvals) != 1 {
		t.Errorf("main _ENV: %+v, upvals %d", info.EnvVar, len(info.Main.Upvals))
	}
}
