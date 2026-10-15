// Command engines times the scripts of bench/lua/suite on glua and on other Lua
// implementations for Go, beside C Lua, for the benchmark page. It is a module of its own so
// that glua itself depends on none of them. Run it from the repository root:
//
//	bench/build-clua.sh
//	go run -C bench/engines . -rounds 3 > bench/engines.md
//
// Each round runs every script once in each engine: C Lua times itself with os.clock; the Go
// engines load the script once and time calls to the function it returns. An engine whose
// result differs from C Lua's, or that cannot run a script, gets no time for it.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// engine is one Lua implementation: load compiles a script and runs its top level, returning
// the work function as a closure that runs it once and renders its result.
type engine struct {
	name, version string
	load          func(name, src string) (func() (string, error), error)
}

func main() {
	rounds := flag.Int("rounds", 3, "interleaved rounds")
	runs := flag.Int("runs", 3, "timed runs of each script per round")
	luaBin := flag.String("lua", "../.clua/lua-5.4.9/src/lua", "C Lua binary, relative to bench/engines")
	only := flag.String("only", "", "a regular expression selecting scripts")
	flag.Parse()

	suite := "../lua/suite"
	expected := readExpected(filepath.Join(suite, "expected.txt"))
	var names []string
	sel := regexp.MustCompile(*only)
	for _, n := range expected.order {
		if sel.MatchString(n) {
			names = append(names, n)
		}
	}
	engines := []engine{gluaEngine(), gopherEngine(), goluaEngine(), wangshuEngine()}

	cTimes := map[string][]float64{}
	times := make([]map[string][]float64, len(engines))
	notes := make([]map[string]string, len(engines))
	for i := range engines {
		times[i], notes[i] = map[string][]float64{}, map[string]string{}
	}
	for r := range *rounds {
		fmt.Fprintf(os.Stderr, "round %d/%d\n", r+1, *rounds)
		for _, name := range names {
			path := filepath.Join(suite, name+".lua")
			cTimes[name] = append(cTimes[name], cluaRound(*luaBin, path, *runs))
			src, err := os.ReadFile(path)
			check(err)
			for i, e := range engines {
				if notes[i][name] != "" {
					continue
				}
				ms, note := goRound(e, name, string(src), expected.result[name], *runs)
				if note != "" {
					notes[i][name] = note
					continue
				}
				times[i][name] = append(times[i][name], ms)
			}
		}
	}

	out, _ := exec.Command(*luaBin, "-v").CombinedOutput()
	fmt.Printf("Machine: %s, %d CPUs. %s. %s.\n", cpuModel(), runtime.NumCPU(), runtime.Version(), strings.TrimSpace(string(out)))
	fmt.Printf("%d interleaved rounds of %d runs each; medians of the round means, in ms, and the ratio to C Lua. %s.\n\n",
		*rounds, *runs, time.Now().Format("2 January 2006"))
	fmt.Print("| Script | C Lua 5.4.9 |")
	for _, e := range engines {
		fmt.Printf(" %s %s |", e.name, e.version)
	}
	fmt.Print("\n| --- | --- |")
	for range engines {
		fmt.Print(" --- |")
	}
	fmt.Println()
	geo := make([]float64, len(engines))
	count := make([]int, len(engines))
	for _, name := range names {
		c := median(cTimes[name])
		fmt.Printf("| %s | %.1f |", name, c)
		for i := range engines {
			if note := notes[i][name]; note != "" {
				fmt.Printf(" – |")
				continue
			}
			g := median(times[i][name])
			geo[i] += math.Log(g / c)
			count[i]++
			fmt.Printf(" %.1f (%.2fx) |", g, g/c)
		}
		fmt.Println()
	}
	fmt.Print("| Geometric mean of the ratios, over the scripts the engine ran | |")
	for i := range engines {
		if count[i] == 0 {
			fmt.Print(" – |")
			continue
		}
		fmt.Printf(" %.2fx (%d scripts) |", math.Exp(geo[i]/float64(count[i])), count[i])
	}
	fmt.Println()
	fmt.Println()
	for i, e := range engines {
		for _, name := range names {
			if note := notes[i][name]; note != "" {
				fmt.Printf("- %s, %s: %s\n", e.name, name, note)
			}
		}
	}
}

// goRound loads a script in an engine and times runs calls of its work function, after a
// warm-up call whose result must match C Lua's.
func goRound(e engine, name, src, want string, runs int) (ms float64, note string) {
	defer func() {
		if r := recover(); r != nil {
			note = fmt.Sprintf("panicked: %v", r)
		}
	}()
	work, err := e.load(name+".lua", src)
	if err != nil {
		return 0, "does not load: " + firstLine(err.Error())
	}
	got, err := work()
	if err != nil {
		return 0, "fails: " + firstLine(err.Error())
	}
	if !sameResult(got, want) {
		return 0, fmt.Sprintf("returns %s, C Lua %s", got, want)
	}
	start := time.Now()
	for range runs {
		if _, err := work(); err != nil {
			return 0, "fails: " + firstLine(err.Error())
		}
	}
	return float64(time.Since(start).Nanoseconds()) / 1e6 / float64(runs), ""
}

// sameResult compares results as strings, or as numbers when both are numbers, so that
// 1e+06 and 1000000 agree.
func sameResult(got, want string) bool {
	if got == want {
		return true
	}
	g, err1 := strconv.ParseFloat(got, 64)
	w, err2 := strconv.ParseFloat(want, 64)
	return err1 == nil && err2 == nil && math.Abs(g-w) <= 1e-9*math.Abs(w)
}

func cluaRound(lua, script string, runs int) float64 {
	out, err := exec.Command(lua, script, strconv.Itoa(runs)).Output()
	check(err)
	var sum float64
	var n int
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		v, err := strconv.ParseFloat(strings.TrimSpace(sc.Text()), 64)
		check(err)
		sum += v
		n++
	}
	return sum / float64(n)
}

type expectations struct {
	order  []string
	result map[string]string
}

func readExpected(path string) expectations {
	b, err := os.ReadFile(path)
	check(err)
	e := expectations{result: map[string]string{}}
	for line := range strings.Lines(string(b)) {
		name, res, _ := strings.Cut(strings.TrimSuffix(line, "\n"), "\t")
		e.order = append(e.order, name)
		e.result[name] = res
	}
	return e
}

func median(v []float64) float64 {
	s := slices.Sorted(slices.Values(v))
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

func cpuModel() string {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return runtime.GOARCH
	}
	for line := range strings.Lines(string(b)) {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "model name" {
			return strings.TrimSpace(v)
		}
	}
	return runtime.GOARCH
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
