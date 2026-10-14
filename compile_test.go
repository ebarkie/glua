package glua

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "regenerate testdata expectations with C Lua (GLUA_CLUA or bench/.clua)")

type luaCase struct{ name, src string }

// readCases splits a file of chunks separated by lines that start with "--==".
func readCases(t *testing.T, path string) []luaCase {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []luaCase
	for part := range strings.SplitSeq(string(data), "\n--==") {
		name, src, _ := strings.Cut(part, "\n")
		if strings.HasPrefix(strings.TrimSpace(name), "--") || strings.HasPrefix(name, "-- ") {
			continue // the header comment
		}
		cases = append(cases, luaCase{strings.TrimSpace(name), src})
	}
	return cases
}

// formatResults renders values as the C Lua side of the test does.
func formatResults(vs []Value) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		if s, ok := v.Str(); ok {
			parts[i] = "str:" + hexString(s)
		} else {
			parts[i] = v.String()
		}
	}
	return strings.Join(parts, " | ")
}

func hexString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		fmt.Fprintf(&b, "%02x", s[i])
	}
	return b.String()
}

const cluaDriver = `
local path = ...
local f = assert(io.open(path))
local text = f:read("a")
f:close()
local function hex(s) return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end)) end
for part in (text .. "\n--=="):gmatch("(.-)\n%-%-==") do
  local name, src = part:match("^([^\n]*)\n(.*)$")
  if name and not name:match("^%-%-") then
    name = name:match("^%s*(.-)%s*$")
    local env = setmetatable({gofirst = function(a) return a end}, {__index = _G})
    local fn, err = load(src, "=" .. name, "t", env)
    local out
    if not fn then
      out = "compile error: " .. err
    else
      local r = table.pack(pcall(fn))
      if not r[1] then
        out = "runtime error: " .. tostring(r[2])
      else
        local parts = {}
        for i = 2, r.n do
          local v = r[i]
          parts[#parts + 1] = type(v) == "string" and ("str:" .. hex(v)) or tostring(v)
        end
        out = table.concat(parts, " | ")
      end
    end
    io.write(name, "\t", out, "\n")
  end
end
`

func cluaPath() string {
	if p := os.Getenv("GLUA_CLUA"); p != "" {
		return p
	}
	return "bench/.clua/lua-5.4.9/src/lua"
}

// expectations returns the C Lua results for a cases file, regenerating them with -update.
func expectations(t *testing.T, cases, expected string) map[string]string {
	t.Helper()
	if *update {
		driver := t.TempDir() + "/driver.lua"
		if err := os.WriteFile(driver, []byte(cluaDriver), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(cluaPath(), driver, cases).CombinedOutput()
		if err != nil {
			t.Fatalf("C Lua: %v\n%s", err, out)
		}
		if err := os.WriteFile(expected, out, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(expected)
	if err != nil {
		t.Fatalf("%v (run with -update and C Lua available)", err)
	}
	defer f.Close()
	want := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		name, res, _ := strings.Cut(sc.Text(), "\t")
		want[name] = res
	}
	return want
}

// testGlobals returns a globals table with the libraries that exist so far, and gofirst,
// a Go function that returns its first argument, for metamethods written in Go.
func testGlobals() *Table {
	g := NewTable(0, 32)
	openBase(g)
	g.Set(String("coroutine"), tableValue(coroutineLib()))
	g.Set(String("gofirst"), goFnValue(&goFunc{name: "gofirst", f: func(c *Call) error {
		return c.ReturnValues(c.Value(0))
	}}))
	return g
}

// runSource compiles and runs a chunk and returns its results.
func runSource(name, src string) ([]Value, error) {
	p, _, err := compileChunk(name, src)
	if err != nil {
		return nil, err
	}
	cl := &Closure{p: p, env: testGlobals()}
	ex := newExecutor()
	ex.maxSteps = 100_000_000
	ex.maxCallDepth = 200_000
	return runClosure(context.Background(), ex, cl)
}

// outcome renders what a chunk did the way the C Lua driver prints it.
func outcome(name, src string) string {
	res, err := runSource(name, src)
	if err == nil {
		return formatResults(res)
	}
	if le, ok := err.(*Error); ok {
		return "runtime error: " + le.Value.String()
	}
	return "compile error: " + err.Error()
}

// runCorpus runs a corpus under both loops: the checked one, the default, and the fast one.
func runCorpus(t *testing.T, cases, expected string) {
	want := expectations(t, cases, expected)
	for _, checked := range []bool{true, false} {
		name := "fast"
		if checked {
			name = "checked"
		}
		t.Run(name, func(t *testing.T) {
			saved := defaultChecked
			defaultChecked = checked
			defer func() { defaultChecked = saved }()
			runCases(t, cases, want)
		})
	}
}

func runCases(t *testing.T, cases string, want map[string]string) {
	for _, c := range readCases(t, cases) {
		t.Run(strings.ReplaceAll(c.name, " ", "_"), func(t *testing.T) {
			exp, ok := want[c.name]
			if !ok {
				t.Fatalf("no expectation for %q", c.name)
			}
			// the sign of a NaN differs between C libraries and CPUs, so it is not compared
			if got := outcome(c.name, c.src); nanSign.Replace(got) != nanSign.Replace(exp) {
				p, _, _ := compileChunk(c.name, c.src)
				listing := ""
				if p != nil {
					listing = p.disasm()
				}
				t.Errorf("\n got: %s\nwant: %s\n%s", got, exp, listing)
			}
		})
	}
}

var nanSign = strings.NewReplacer("-nan", "nan")

func TestCompileCorpus(t *testing.T) {
	runCorpus(t, "testdata/compile.lua", "testdata/compile.expected")
}
func TestErrorCorpus(t *testing.T) { runCorpus(t, "testdata/errors.lua", "testdata/errors.expected") }

func TestArithCorpus(t *testing.T) { runCorpus(t, "testdata/arith.lua", "testdata/arith.expected") }

func TestMetaCorpus(t *testing.T) { runCorpus(t, "testdata/meta.lua", "testdata/meta.expected") }

func TestPcallCorpus(t *testing.T) { runCorpus(t, "testdata/pcall.lua", "testdata/pcall.expected") }

func TestCoroutineCorpus(t *testing.T) {
	runCorpus(t, "testdata/coroutine.lua", "testdata/coroutine.expected")
}

// The checked loop is generated from the fast one; it must be up to date.
func TestCheckedLoopGenerated(t *testing.T) {
	out, err := exec.Command("go", "run", "./internal/cmd/genloop", "-check").CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestBaseCorpus(t *testing.T) { runCorpus(t, "testdata/base.lua", "testdata/base.expected") }
