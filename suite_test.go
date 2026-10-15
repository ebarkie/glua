package glua

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The official Lua 5.4.9 test suite, vendored under testdata/lua-tests. Its MANIFEST
// lists every file with a status:
//
//	run       the file runs unmodified; its SHA-256 must match the release's
//	patched   testdata/lua-tests/patched/<file> runs instead; the reason says what changed
//	excluded  the file does not run; the reason says why
//
// The suite runs under the test-only globals of suiteGlobals: require, package.loaded, a
// small os, and the usual _soft and _port flags.

const suiteDir = "testdata/lua-tests"

type suiteEntry struct {
	file, status, sum, reason string
}

func readManifest(t *testing.T) []suiteEntry {
	t.Helper()
	f, err := os.Open(filepath.Join(suiteDir, "MANIFEST"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var entries []suiteEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.SplitN(line, " ", 4)
		if len(fields) < 3 {
			t.Fatalf("MANIFEST: malformed line %q", line)
		}
		e := suiteEntry{file: fields[0], status: fields[1], sum: fields[2]}
		if len(fields) == 4 {
			e.reason = strings.TrimSpace(fields[3])
		}
		switch e.status {
		case "run", "patched", "excluded":
		default:
			t.Fatalf("MANIFEST: %s: unknown status %q", e.file, e.status)
		}
		if e.status != "run" && e.reason == "" {
			t.Fatalf("MANIFEST: %s: %s without a reason", e.file, e.status)
		}
		entries = append(entries, e)
	}
	return entries
}

// TestSuiteManifest checks that the manifest covers every vendored file, and that every
// vendored file is the release's, byte for byte.
func TestSuiteManifest(t *testing.T) {
	entries := readManifest(t)
	listed := map[string]bool{}
	for _, e := range entries {
		listed[e.file] = true
		src, err := os.ReadFile(filepath.Join(suiteDir, e.file))
		if err != nil {
			t.Error(err)
			continue
		}
		sum := sha256.Sum256(src)
		if got := hex.EncodeToString(sum[:]); got != e.sum {
			t.Errorf("%s: sha256 %s, the release's is %s", e.file, got, e.sum)
		}
		if e.status == "patched" {
			if _, err := os.Stat(filepath.Join(suiteDir, "patched", e.file)); err != nil {
				t.Errorf("%s: patched, but %v", e.file, err)
			}
		}
	}
	files, _ := filepath.Glob(filepath.Join(suiteDir, "*.lua"))
	for _, f := range files {
		if !listed[filepath.Base(f)] {
			t.Errorf("%s is not in the MANIFEST", f)
		}
	}
}

// TestSuite runs the suite under both loops: the checked one, the default, and the fast one.
func TestSuite(t *testing.T) {
	entries := readManifest(t)
	for _, checked := range []bool{true, false} {
		name := "fast"
		if checked {
			name = "checked"
		}
		t.Run(name, func(t *testing.T) {
			saved := defaultChecked
			defaultChecked = checked
			defer func() { defaultChecked = saved }()
			runSuite(t, entries)
		})
	}
}

func runSuite(t *testing.T, entries []suiteEntry) {
	for _, e := range entries {
		if e.status == "excluded" {
			continue
		}
		t.Run(strings.TrimSuffix(e.file, ".lua"), func(t *testing.T) {
			path := filepath.Join(suiteDir, e.file)
			if e.status == "patched" {
				path = filepath.Join(suiteDir, "patched", e.file)
			}
			var out bytes.Buffer
			if err := runSuiteFile(path, &out); err != nil {
				var trace strings.Builder
				if le, ok := err.(*Error); ok {
					for _, f := range le.Traceback() {
						fmt.Fprintf(&trace, "\t%v\n", f)
					}
				}
				t.Fatalf("%v\n%soutput:\n%s", err, trace.String(), out.String())
			}
		})
	}
}

// runSuiteFile runs one test file the way the suite's all.lua would.
func runSuiteFile(path string, out *bytes.Buffer) error {
	g := suiteGlobals()
	f, err := loadSuiteChunk(path, g)
	if err != nil {
		return err
	}
	ex := newExecutor()
	ex.strMeta = testStrMeta
	ex.maxSteps = 4_000_000_000
	ex.maxCallDepth = 200_000
	ex.output = writerOutput(out)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, err = runClosure(ctx, ex, f)
	return err
}

// loadSuiteChunk compiles a file as luaL_loadfile does: "@name" for its chunk name, and a
// first line starting with '#' skipped.
func loadSuiteChunk(path string, g *Table) (*closure, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(src) > 0 && src[0] == '#' {
		if i := bytes.IndexByte(src, '\n'); i >= 0 {
			src = src[i:] // keep the newline, so line numbers hold
		} else {
			src = nil
		}
	}
	name := "@" + filepath.Base(path)
	p, info, err := compileChunk(chunkID(name), string(src))
	if err != nil {
		return nil, err
	}
	setChunkName(p, name)
	return newMainClosure(p, info, g), nil
}

// suiteGlobals is testGlobals with what the suite needs from the libraries glua leaves out.
func suiteGlobals() *Table {
	g := testGlobals()
	openIO(g, nil) // the subset: the suite uses io.stdin and io.write as a userdata and a library function
	g.Set(String("gofirst"), Value{})
	g.Set(String("_soft"), Bool(true)) // skip the long-running tests
	g.Set(String("_port"), Bool(true)) // skip the tests that depend on the platform
	addSuiteDebug(g)

	loaded := NewTable(0, 16)
	for _, name := range []string{"_G", "string", "table", "math", "utf8", "coroutine", "debug", "io"} {
		if name == "_G" {
			loaded.Set(String(name), tableValue(g))
		} else {
			loaded.Set(String(name), g.Get(String(name)))
		}
	}
	// tracegc marks garbage collections, which glua leaves to Go: a module that does nothing
	noop := goFnValue(&goFunc{name: "noop", f: func(c *Call) error { return nil }})
	tracegc := NewTable(0, 2)
	tracegc.Set(String("start"), noop)
	tracegc.Set(String("stop"), noop)
	loaded.Set(String("tracegc"), tableValue(tracegc))
	preload := NewTable(0, 4)
	pkg := NewTable(0, 2)
	pkg.Set(String("loaded"), tableValue(loaded))
	pkg.Set(String("preload"), tableValue(preload))
	g.Set(String("package"), tableValue(pkg))
	loaded.Set(String("package"), tableValue(pkg))

	// require finds modules in package.loaded, then as files in the suite's directory
	g.Set(String("require"), goFnValue(&goFunc{name: "require", f: func(c *Call) error {
		name, err := c.checkString(0, "require")
		if err != nil {
			return err
		}
		if v := loaded.Get(String(name)); !v.IsNil() {
			return c.ReturnValues(v)
		}
		path := ":preload:"
		f := preload.Get(String(name))
		if f.IsNil() {
			path = filepath.Join(suiteDir, name+".lua")
			cl, err := loadSuiteChunk(path, g)
			if err != nil {
				return c.libErrorf("module '%s' not found: %v", name, err)
			}
			f = luaFnValue(cl)
		}
		res, err := c.call(f, String(name), String(path))
		if err != nil {
			return err
		}
		v := Bool(true)
		if len(res) > 0 && !res[0].IsNil() {
			v = res[0]
		}
		loaded.Set(String(name), v)
		return c.ReturnValues(v, String(path))
	}}))

	start := time.Now()
	osLib := newLib("os", map[string]GoFunc{
		"clock": func(c *Call) error { return c.ReturnValues(Float(time.Since(start).Seconds())) },
		"time":  func(c *Call) error { return c.ReturnValues(Int(time.Now().Unix())) },
		"getenv": func(c *Call) error {
			return c.ReturnValues(Value{})
		},
		// only the C locale exists
		"setlocale": func(c *Call) error {
			if l := c.Value(0); l.IsNil() || l.isString() && (l.str() == "C" || l.str() == "") {
				return c.ReturnValues(String("C"))
			}
			return c.ReturnValues(Value{})
		},
	})
	g.Set(String("os"), tableValue(osLib))
	loaded.Set(String("os"), tableValue(osLib))
	return g
}
