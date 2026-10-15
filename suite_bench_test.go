package glua

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The benchmark suite: small scripts in bench/lua/suite, each returning its work function. `go run ./bench/compare -suite` times them against C Lua.

const benchSuiteDir = "bench/lua/suite"

// suiteScripts returns the suite's scripts by name, with the result C Lua gives.
func suiteScripts(tb testing.TB) (names []string, expected map[string]string) {
	f, err := os.Open(filepath.Join(benchSuiteDir, "expected.txt"))
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	expected = map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, want, _ := strings.Cut(sc.Text(), "\t")
		names = append(names, name)
		expected[name] = want
	}
	return names, expected
}

// suiteWork loads a suite script and returns its work function.
func suiteWork(tb testing.TB, name string, checked bool) func(context.Context) (Value, error) {
	src, err := os.ReadFile(filepath.Join(benchSuiteDir, name+".lua"))
	if err != nil {
		tb.Fatal(err)
	}
	rt := MustNew(LibSafe|LibCoroutine, withLoop(checked),
		Limits{Steps: NoLimit, AllocBytes: NoLimit, InitSteps: NoLimit, InitAllocBytes: NoLimit})
	s, err := rt.Load(context.Background(), name+".lua", string(src))
	if err != nil {
		tb.Fatal(err)
	}
	work, err := s.Func0[Value]("")
	if err != nil {
		tb.Fatal(err)
	}
	return work
}

func TestBenchSuiteResults(t *testing.T) {
	names, expected := suiteScripts(t)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if testing.Short() || raceEnabled || stackStress {
				t.Skip("slow")
			}
			for _, checked := range []bool{true, false} {
				v, err := suiteWork(t, name, checked)(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if got := v.String(); got != expected[name] {
					t.Errorf("checked=%v: got %s, C Lua gives %s", checked, got, expected[name])
				}
			}
		})
	}
}

func benchSuite(b *testing.B, checked, gc bool) {
	names, _ := suiteScripts(b)
	for _, name := range names {
		b.Run(name, func(b *testing.B) {
			work := suiteWork(b, name, checked)
			ctx := context.Background()
			if _, err := work(ctx); err != nil { // warm up, as the C Lua driver does
				b.Fatal(err)
			}
			if gc {
				defer gcPressure()()
			}
			for b.Loop() {
				if _, err := work(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSuite(b *testing.B)       { benchSuite(b, true, false) }
func BenchmarkSuiteFast(b *testing.B)   { benchSuite(b, false, false) }
func BenchmarkSuiteGC(b *testing.B)     { benchSuite(b, true, true) }
func BenchmarkSuiteFastGC(b *testing.B) { benchSuite(b, false, true) }
