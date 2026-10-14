package glua

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func runWithOutput(t *testing.T, src string) (string, []Value) {
	t.Helper()
	p, _, err := compileChunk("t", src)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	ex := newExecutor()
	ex.output = &out
	res, err := runClosure(context.Background(), ex, &Closure{p: p, env: testGlobals()})
	if err != nil {
		t.Fatal(err)
	}
	return out.String(), res
}

func TestPrint(t *testing.T) {
	out, _ := runWithOutput(t, `
		print(1, 2.5, "x", nil, true)
		print()
		print(setmetatable({}, {__tostring = function() return "obj" end}))
		print(setmetatable({}, {__name = "Point"}))`)
	if !strings.HasPrefix(out, "1\t2.5\tx\tnil\ttrue\n\nobj\nPoint: 0x") {
		t.Fatalf("got %q", out)
	}
}

func TestPrintDiscardsByDefault(t *testing.T) {
	if _, err := runSource("t", `print("nowhere")`); err != nil {
		t.Fatal(err)
	}
}

func TestDebugTraceback(t *testing.T) {
	_, res := runWithOutput(t, `
		local function inner() return debug.traceback("msg") end
		local function outer() return inner() .. "" end
		local r = outer() -- not a tail call, so the main chunk keeps its frame
		return r`)
	tb := res[0].String()
	for _, want := range []string{"msg\nstack traceback:", "t:2: in function 'inner'", "t:3: in function 'outer'", "t:4: in main chunk"} {
		if !strings.Contains(tb, want) {
			t.Errorf("traceback lacks %q:\n%s", want, tb)
		}
	}
}

func TestWarn(t *testing.T) {
	out, _ := runWithOutput(t, `warn("hidden") warn("@on") warn("shown ", "here") warn("@off") warn("hidden")`)
	if out != "Lua warning: shown here\n" {
		t.Fatalf("got %q", out)
	}
}
