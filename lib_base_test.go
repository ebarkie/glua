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
	ex.output = writerOutput(&out)
	g := testGlobals()
	ex.strMeta = testStrMeta
	res, err := runClosure(context.Background(), ex, &closure{p: p, env: g})
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

// debug.traceback goes on past a library function that called back into Lua, and shows it
// and pcall as [C] lines, as in C Lua.
func TestDebugTracebackGoLevels(t *testing.T) {
	out, _ := runWithOutput(t, `
		local function f() return debug.traceback("tb") .. "" end
		local function g() local _, r = pcall(f) return r .. "" end
		local function h() local s = ("x"):gsub("x", g) return s end
		print(h())
		print(select(3, pcall(pcall, f)))
		print(select(2, xpcall(f, print)))
		print(tostring(setmetatable({}, {__tostring = function() local s = f() return s end})))
		pcall(function()
			local x <close> = setmetatable({}, {__close = function() print(f()) end})
			error("e")
		end)`)
	want := `tb
stack traceback:
	t:2: in function 'f'
	[C]: in function 'pcall'
	t:3: in function 'g'
	[C]: in function 'string.gsub'
	t:4: in function 'h'
	t:5: in main chunk
	[C]: in ?
tb
stack traceback:
	t:2: in function 'f'
	[C]: in function 'pcall'
	[C]: in function 'pcall'
	t:6: in main chunk
	[C]: in ?
tb
stack traceback:
	t:2: in function 'f'
	[C]: in function 'xpcall'
	t:7: in main chunk
	[C]: in ?
tb
stack traceback:
	t:2: in function 'f'
	t:8: in function <t:8>
	[C]: in function 'tostring'
	t:8: in main chunk
	[C]: in ?
tb
stack traceback:
	t:2: in function 'f'
	t:10: in function <t:10>
	[C]: in function 'pcall'
	t:9: in main chunk
	[C]: in ?
`
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

// debug.traceback takes a thread and a level, as in C Lua: level 1, the caller, by default in
// the running thread, and level 0, where it waits, in another one.
func TestDebugTracebackThreadAndLevel(t *testing.T) {
	out, _ := runWithOutput(t, `
		local function show(s) print(s) print("--") end
		local function inner() coroutine.yield() end
		local function outer() inner() end
		local co = coroutine.create(outer)
		coroutine.resume(co)
		show(debug.traceback(co, "suspended"))
		show(debug.traceback(co, "level 1", 1))
		show(debug.traceback(co, "past the end", 5))
		print(type(debug.traceback(co, {})))
		local dead = coroutine.create(function() return 1 end)
		coroutine.resume(dead)
		show(debug.traceback(dead, "returned"))
		local err = coroutine.create(function() local x x() end)
		coroutine.resume(err)
		show(debug.traceback(err, "failed"))
		show(debug.traceback(coroutine.create(print), "not started"))
		show(debug.traceback("default"))
		show(debug.traceback("zero", 0))
		local self
		self = coroutine.create(function() show(debug.traceback(self, "self")) end)
		coroutine.resume(self)
		local outerco
		local innerco = coroutine.create(function() show(debug.traceback(outerco, "normal")) end)
		outerco = coroutine.create(function() coroutine.resume(innerco) end)
		coroutine.resume(outerco)
		print(pcall(debug.traceback, co, "m", "x"))`)
	want := `suspended
stack traceback:
	[C]: in function 'coroutine.yield'
	t:3: in function 'inner'
	t:4: in function 'outer'
--
level 1
stack traceback:
	t:3: in function 'inner'
	t:4: in function 'outer'
--
past the end
stack traceback:
--
table
returned
stack traceback:
--
failed
stack traceback:
	t:14: in function <t:14>
--
not started
stack traceback:
--
default
stack traceback:
	t:18: in main chunk
	[C]: in ?
--
zero
stack traceback:
	[C]: in function 'debug.traceback'
	t:19: in main chunk
	[C]: in ?
--
self
stack traceback:
	t:21: in function <t:21>
--
normal
stack traceback:
	[C]: in function 'coroutine.resume'
	t:25: in function <t:25>
--
false	bad argument #3 to 'debug.traceback' (number expected, got string)
`
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestWarn(t *testing.T) {
	out, _ := runWithOutput(t, `warn("hidden") warn("@on") warn("shown ", "here") warn("@off") warn("hidden")`)
	if out != "Lua warning: shown here\n" {
		t.Fatalf("got %q", out)
	}
}

// A Go function that Go code called has no call-site name, even when it runs from the
// trampoline: as in C Lua, its argument errors use its library name.
func TestArgErrorNameThroughTrampoline(t *testing.T) {
	out, _ := runWithOutput(t, `
		print(select(2, pcall(pcall, string.rep)))
		print(coroutine.resume(coroutine.create(string.rep)))
		print(pcall(coroutine.wrap(string.rep)))`)
	want := strings.Repeat("false\tbad argument #1 to 'string.rep' (string expected, got no value)\n", 3)
	if out != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}
