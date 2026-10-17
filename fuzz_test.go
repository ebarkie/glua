package glua

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Fuzz targets for the compiler and the VM. Run them with, for example,
//
//	go test -run '^$' -fuzz '^FuzzRun$' -fuzztime 10m
//	go test -tags lua_stackstress -run '^$' -fuzz '^FuzzRun$' -fuzztime 10m
//
// The lexer and parser have their own in internal/syntax, the pattern matcher FuzzPatterns,
// mutated bytecode FuzzVerifiedCode, and long entries that renew their budgets FuzzLongEntry.

// fuzzSeeds adds the chunks of the corpus files and the benchmark scripts as seeds.
func fuzzSeeds(f *testing.F) {
	files, _ := filepath.Glob("testdata/*.lua")
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			f.Fatal(err)
		}
		for chunk := range strings.SplitSeq(string(b), "\n--== ") {
			if _, body, ok := strings.Cut(chunk, "\n"); ok {
				f.Add(body)
			}
		}
	}
	scripts, _ := filepath.Glob("bench/lua/suite/*.lua")
	for _, file := range scripts {
		b, _ := os.ReadFile(file)
		f.Add(string(b))
	}
}

// FuzzCompile compiles any source that parses. The compiler must not panic, and the verifier,
// which every compile ends with, must accept what it produced.
func FuzzCompile(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > 1<<14 {
			return
		}
		_, _, err := compileChunk("fuzz", src)
		var se interface{ Error() string }
		if err != nil && !errors.As(err, &se) {
			t.Fatal(err)
		}
	})
}

// fuzzLimits keep a fuzzed program small: a hostile one ends in a limit error.
var fuzzLimits = Limits{Steps: 20_000, AllocBytes: 1 << 20, CallDepth: 200, HostDepth: 20}

// addrs hides addresses, which differ between two runs of the same program.
var addrs = regexp.MustCompile(`0x[0-9a-f]+`)

// fuzzOutcome runs src on one loop and renders what happened.
func fuzzOutcome(t *testing.T, src string, checked bool) string {
	rt := MustNew(LibSafe|LibCoroutine|LibLoad, fuzzLimits, RandSeed(1), withLoop(checked))
	s, err := rt.Load(context.Background(), "fuzz", src)
	if err != nil {
		if pe, ok := errors.AsType[*PanicError](err); ok {
			t.Fatalf("checked=%v: the VM panicked: %v\n%s", checked, pe.Value, pe.Stack)
		}
		return "error: " + addrs.ReplaceAllString(err.Error(), "0x")
	}
	var parts []string
	for _, v := range s.result {
		switch v.Kind() {
		case KindTable, KindFunction, KindThread, KindUserdata:
			parts = append(parts, v.Kind().String())
		default:
			parts = append(parts, addrs.ReplaceAllString(v.String(), "0x"))
		}
	}
	return strings.Join(parts, "\t")
}

// FuzzRun runs any program under tight limits on the checked and the fast loop. Neither may
// panic, and both must do the same thing.
func FuzzRun(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > 1<<14 {
			return
		}
		if _, _, err := compileChunk("fuzz", src); err != nil {
			return
		}
		checked, fast := fuzzOutcome(t, src, true), fuzzOutcome(t, src, false)
		if checked != fast {
			t.Fatalf("the loops disagree\nchecked: %s\nfast:    %s", checked, fast)
		}
	})
}
