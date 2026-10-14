// Command compare times the spike's VM benchmarks against C Lua in interleaved rounds and
// reports medians, spread and ratios as a Markdown table. Each round runs every workload
// once in C Lua and once in Go, back to back, so both see the same machine state. It also
// reports the GC-marking variants and the boundary benchmarks, and writes the raw Go
// results in benchstat format.
//
//	bench/build-clua.sh
//	go run ./bench/compare -rounds 12
package main

import (
	"bufio"
	"bytes"
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

type workload struct {
	name   string // as shown in the report
	script string // under bench/lua
	bench  string // Go benchmark, idle GC
	gc     string // Go benchmark, GC marking
}

var workloads = []workload{
	{"Float loop, 1M iterations of `s = s + i * 1.5`", "floatloop.lua", "BenchmarkFloatLoop", "BenchmarkGCFloatLoop"},
	{"`fib(27)`, recursive", "fib27.lua", "BenchmarkFib27", "BenchmarkGCFib27"},
	{"Field read, 1M iterations of `s = s + t.x`", "fieldread.lua", "BenchmarkFieldRead", "BenchmarkGCFieldRead"},
}

var boundary = []struct{ name, bench string }{
	{"`function(x) return x + 1 end`, typed, private executor", "BenchmarkCallPrivate"},
	{"Same, executor borrowed from a `sync.Pool`", "BenchmarkCallPooled"},
	{"46-byte string in, same string out", "BenchmarkStringRoundTrip"},
	{"Rule reading three fields of a `*request` that is reused", "BenchmarkRuleReusedPointer"},
	{"Same rule when `req.method` does not share a pointer with the constant", "BenchmarkRuleOtherPointer"},
	{"Same rule through `Rule.Eval`, struct built per call", "BenchmarkRuleEvalPerCall"},
	{"Same rule through a typed func, struct built per call and passed by value", "BenchmarkFuncByValuePerCall"},
	{"Same rule through a typed func, pointer to a struct built per call", "BenchmarkFuncPointerPerCall"},
	{"Function that calls one Go host function", "BenchmarkHostCall"},
}

func main() {
	rounds := flag.Int("rounds", 12, "interleaved rounds")
	runs := flag.Int("runs", 5, "runs of each workload per round")
	lua := flag.String("lua", "bench/.clua/lua-5.4.9/src/lua", "C Lua binary")
	out := flag.String("raw", "bench/results/go.txt", "file for the raw Go results, in benchstat format")
	flag.Parse()

	if _, err := os.Stat(*lua); err != nil {
		fatalf("no C Lua at %s: run bench/build-clua.sh first", *lua)
	}
	tmp, err := os.MkdirTemp("", "glua-compare")
	check(err)
	defer os.RemoveAll(tmp)
	testBin := filepath.Join(tmp, "glua.test")
	run("go", "test", "-c", "-o", testBin, ".")

	var raw bytes.Buffer
	cTimes := make([][]float64, len(workloads))  // per workload: round means, ms
	goTimes := make([][]float64, len(workloads)) // same, idle GC
	gcTimes := make([][]float64, len(workloads)) // same, GC marking
	for r := range *rounds {
		fmt.Fprintf(os.Stderr, "round %d/%d\n", r+1, *rounds)
		for i, w := range workloads {
			cTimes[i] = append(cTimes[i], luaRound(*lua, filepath.Join("bench/lua", w.script), *runs))
			ns, line := goRound(testBin, w.bench, *runs, false)
			goTimes[i] = append(goTimes[i], ns/1e6)
			raw.WriteString(line)
		}
	}
	for i, w := range workloads {
		for range *rounds {
			ns, line := goRound(testBin, w.gc, *runs, false)
			gcTimes[i] = append(gcTimes[i], ns/1e6)
			raw.WriteString(line)
		}
	}
	type bres struct {
		ns     []float64
		allocs string
	}
	bounds := make([]bres, len(boundary))
	for i, b := range boundary {
		for range *rounds {
			ns, line := goRound(testBin, b.bench, 0, true)
			bounds[i].ns = append(bounds[i].ns, ns)
			bounds[i].allocs = allocsOf(line)
			raw.WriteString(line)
		}
	}
	check(os.MkdirAll(filepath.Dir(*out), 0o755))
	check(os.WriteFile(*out, raw.Bytes(), 0o644))

	// report
	fmt.Printf("Machine: %s, %d CPUs, governor %s. %s. C Lua: %s.\n", cpuModel(), runtime.NumCPU(), governor(),
		goVersion(), luaInfo())
	fmt.Printf("%d interleaved rounds of %d runs each; medians of the round means, with the range of the round means. %s.\n\n",
		*rounds, *runs, time.Now().Format("2 January 2006"))
	fmt.Println("| Workload, ms | C Lua 5.4.9 | glua | glua / C Lua | glua, GC marking | GC marking / idle |")
	fmt.Println("| --- | --- | --- | --- | --- | --- |")
	geo, geoGC := 0.0, 0.0
	for i, w := range workloads {
		c, g, m := median(cTimes[i]), median(goTimes[i]), median(gcTimes[i])
		geo += math.Log(g / c)
		geoGC += math.Log(m / c)
		fmt.Printf("| %s | %s | %s | %.2fx | %s | %.2fx |\n", w.name, spread(cTimes[i]), spread(goTimes[i]), g/c, spread(gcTimes[i]), m/g)
	}
	n := float64(len(workloads))
	fmt.Printf("| Geometric mean against C Lua | | | %.2fx | %.2fx | |\n\n", math.Exp(geo/n), math.Exp(geoGC/n))

	fmt.Println("| Call from Go | ns | Allocations |")
	fmt.Println("| --- | --- | --- |")
	for i, b := range boundary {
		fmt.Printf("| %s | %s | %s |\n", b.name, spreadNs(bounds[i].ns), bounds[i].allocs)
	}
	fmt.Printf("\nRaw Go results for benchstat: %s\n", *out)
}

// luaRound runs a Lua workload script and returns the mean of its runs, in ms.
func luaRound(lua, script string, runs int) float64 {
	outp := run(lua, script, strconv.Itoa(runs))
	var sum float64
	var n int
	sc := bufio.NewScanner(strings.NewReader(outp))
	for sc.Scan() {
		v, err := strconv.ParseFloat(strings.TrimSpace(sc.Text()), 64)
		check(err)
		sum += v
		n++
	}
	if n == 0 {
		fatalf("%s printed no times", script)
	}
	return sum / float64(n)
}

var benchLine = regexp.MustCompile(`^(Benchmark\S+)\s+\d+\s+([\d.]+) ns/op.*$`)

// goRound runs one Go benchmark once and returns its ns/op and the result line. runs > 0
// fixes the number of iterations; otherwise the benchmark picks its own.
func goRound(bin, name string, runs int, mem bool) (float64, string) {
	args := []string{"-test.run", "^$", "-test.bench", "^" + name + "$", "-test.count", "1"}
	if runs > 0 {
		args = append(args, "-test.benchtime", fmt.Sprintf("%dx", runs))
	}
	if mem {
		args = append(args, "-test.benchmem")
	}
	outp := run(bin, args...)
	for line := range strings.SplitSeq(outp, "\n") {
		if m := benchLine.FindStringSubmatch(line); m != nil {
			v, err := strconv.ParseFloat(m[2], 64)
			check(err)
			return v, line + "\n"
		}
	}
	fatalf("no result for %s:\n%s", name, outp)
	return 0, ""
}

func allocsOf(line string) string {
	f := strings.Fields(line)
	for i := range f {
		if f[i] == "allocs/op" && i > 0 {
			return f[i-1]
		}
	}
	return "?"
}

func median(v []float64) float64 {
	s := slices.Sorted(slices.Values(v))
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}

func spread(v []float64) string {
	return fmt.Sprintf("%.2f (%.2f–%.2f)", median(v), slices.Min(v), slices.Max(v))
}

func spreadNs(v []float64) string {
	return fmt.Sprintf("%.1f (%.1f–%.1f)", median(v), slices.Min(v), slices.Max(v))
}

func cpuModel() string {
	b, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return runtime.GOARCH
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "model name" {
			return strings.TrimSpace(v)
		}
	}
	return runtime.GOARCH
}

func governor() string {
	b, err := os.ReadFile("/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}

func goVersion() string { return strings.TrimSpace(run("go", "version")) }

func luaInfo() string {
	b, err := os.ReadFile("bench/.clua/BUILDINFO")
	if err != nil {
		return "unknown build"
	}
	var parts []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		if strings.HasPrefix(line, "CC=") || strings.HasPrefix(line, "CFLAGS=") {
			parts = append(parts, strings.Join(strings.Fields(line), " "))
		}
	}
	return strings.Join(parts, ", ")
}

func run(name string, args ...string) string {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	outp, err := cmd.Output()
	if err != nil {
		fatalf("%s %s: %v\n%s%s", name, strings.Join(args, " "), err, outp, stderr.Bytes())
	}
	return string(outp)
}

func check(err error) {
	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "compare: "+format+"\n", args...)
	os.Exit(1)
}
