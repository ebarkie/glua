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
    local fn, err = load(src, "=" .. name)
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

// testGlobals returns a globals table with the few library functions the cases use before
// the libraries exist.
func testGlobals() *Table {
	g := NewTable(0, 4)
	g.Set(String("setmetatable"), goFnValue(&goFunc{name: "setmetatable", f: func(c *Call) error {
		t := c.Value(0)
		if !t.isObj(kindTable) || !(c.Value(1).IsNil() || c.Value(1).isObj(kindTable)) {
			return c.Errorf("bad argument to 'setmetatable'")
		}
		if mt := c.Value(1); mt.IsNil() {
			(*Table)(t.p).meta = nil
		} else {
			(*Table)(t.p).meta = (*Table)(mt.p)
		}
		return c.ReturnValues(t)
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

func TestCompileCorpus(t *testing.T) {
	cases := readCases(t, "testdata/compile.lua")
	want := expectations(t, "testdata/compile.lua", "testdata/compile.expected")
	for _, c := range cases {
		t.Run(strings.ReplaceAll(c.name, " ", "_"), func(t *testing.T) {
			exp, ok := want[c.name]
			if !ok {
				t.Fatalf("no expectation for %q", c.name)
			}
			res, err := runSource(c.name, c.src)
			var got string
			if err != nil {
				got = "error: " + err.Error()
			} else {
				got = formatResults(res)
			}
			if got != exp {
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

// Whatever parses must compile to code the verifier accepts: the compiler panics otherwise.
func FuzzCompile(f *testing.F) {
	for _, c := range []string{
		"local a, b = 1, 2 a, b = b, a return a",
		"local t = {f(), ...} for k, v in next, t do t[k] = v and v or nil end",
		"local x <close> = nil goto l ::l:: repeat local y = x until y",
		"return function(...) local a <const> = 1 return a + select('#', ...) end",
		"while x do if y then break elseif z then goto c end ::c:: end",
		"local s = '' for i = 10, 1, -1 do s = s .. i end return #s > 3 or not s",
	} {
		f.Add(c)
	}
	f.Fuzz(func(t *testing.T, src string) {
		compileChunk("f", src)
	})
}
