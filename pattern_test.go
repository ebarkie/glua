package glua

import (
	"context"
	"testing"
)

// FuzzPatterns runs random subjects and patterns through find, match, gsub and gmatch.
// Malformed patterns must give Lua errors, never Go panics, and backtracking must stay
// within the step budget.
func FuzzPatterns(f *testing.F) {
	for _, seed := range [][2]string{
		{"hello world", "(%a+) (%a+)"}, {"f(a(b)c)", "%b()"}, {"THE fox", "%f[%a]%a+"},
		{"aaa", "a-$"}, {"x]y", "[]]"}, {"abc", "()b()"}, {"aaaaaaaaaaaaaaaaaaaa", "(a*)*b"},
		{"", "[^%"}, {"abc", "%1"}, {"ab", "(()"}, {"a.b", "[%a-]"},
		{"abc", "()%1"},
	} {
		f.Add(seed[0], seed[1])
	}
	p, _, err := compileChunk("fuzz", `
		local s, pat = ...
		pcall(string.find, s, pat)
		pcall(string.match, s, pat)
		pcall(string.gsub, s, pat, "<%0>")
		pcall(function() for a in string.gmatch(s, pat) do end end)`)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, s, pat string) {
		g := testGlobals()
		ex := newExecutor()
		ex.strMeta = testStrMeta
		ex.maxSteps = 20_000
		_, err := runClosure(context.Background(), ex, &closure{p: p, env: g}, String(s), String(pat))
		if pe, ok := err.(*PanicError); ok {
			t.Fatalf("panic: %v\n%s", pe.Value, pe.Stack)
		}
	})
}

func TestBackReferenceToPosition(t *testing.T) {
	v, err := Eval[bool](context.Background(), `return string.find("abc", "()%1") == nil and string.find("abc", "()a%1") == nil`)
	if err != nil || !v {
		t.Errorf("got %v, %v; want no match", v, err)
	}
}
