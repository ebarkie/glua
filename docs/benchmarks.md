# Benchmarks

Three sets of measurements, all on the same machine:

- [Scripts across Lua implementations](#scripts-across-lua-implementations): 21 small scripts
  in C Lua, glua and three other Lua implementations for Go.
- [glua's two interpreter loops](#gluas-two-interpreter-loops): the same scripts on glua's
  checked loop and its fast loop, and while the Go garbage collector is marking.
- [Calls between Go and Lua](#calls-between-go-and-lua): what one call from Go into a script
  costs, which dominates many embedded uses.

## Summary

The geometric mean, over the suite's scripts, of each engine's time relative to C Lua 5.4.9;
lower is faster.

| Engine | Relative to C Lua |
| --- | --- |
| C Lua 5.4.9 | 1.00x |
| glua (fast loop) | 1.20x |
| wangshu | 4.26x |
| GopherLua | 5.22x |
| golua | 5.97x |

The Lua 5.1 engines, GopherLua and wangshu, cannot run every script; their means cover the
scripts they ran.

## Scripts across Lua implementations

The 21 scripts of [`bench/lua/suite`](../bench/lua/suite) are small programs of the kinds an
embedded interpreter runs: recursion, loops over numbers and tables, string building and
matching, sorting, classes, closures, varargs, coroutines and rule evaluation. Each returns a
checksum, and an engine's time counts only if its checksum matches C Lua's.

The engines implement different versions of Lua, which decides what they can run:

| Engine | Version tested | Language | Notes |
| --- | --- | --- | --- |
| C Lua | 5.4.9 | Lua 5.4 | Built from source with gcc at `-O2`; the reference |
| glua | v1.1.0 | Lua 5.4 | The fast interpreter loop, the default |
| GopherLua | v1.1.2 | Lua 5.1 | Default options, except that the data stack may grow (`RegistryMaxSize`), which two scripts need |
| golua | v0.4.0 | Lua 5.5 | Default runtime with the standard library |
| wangshu | v0.2.0 | Lua 5.1 | Default options |

Times in ms, medians of three rounds of three runs each, with the ratio to C Lua:

| Script | C Lua 5.4.9 | glua (fast loop) | GopherLua v1.1.2 | golua v0.4.0 | wangshu v0.2.0 |
| --- | --- | --- | --- | --- | --- |
| binarytrees | 29.7 | 27.7 (0.93x) | – | 111.7 (3.76x) | – |
| closures | 38.6 | 47.3 (1.22x) | 207.9 (5.38x) | 321.7 (8.33x) | 256.6 (6.64x) |
| concat | 54.2 | 33.8 (0.62x) | 100.8 (1.86x) | 151.5 (2.80x) | 149.3 (2.76x) |
| coroutine | 14.8 | 27.6 (1.87x) | 40.0 (2.70x) | 201.4 (13.60x) | 61.2 (4.13x) |
| fannkuch | 32.9 | 46.2 (1.41x) | 199.2 (6.05x) | 231.1 (7.02x) | 247.0 (7.50x) |
| fib | 16.7 | 18.0 (1.08x) | 76.2 (4.55x) | 159.7 (9.54x) | 80.5 (4.81x) |
| fieldread | 11.2 | 12.3 (1.10x) | 81.1 (7.21x) | 105.2 (9.36x) | 46.5 (4.14x) |
| floatloop | 10.0 | 12.8 (1.29x) | 69.6 (6.98x) | 79.1 (7.93x) | 31.3 (3.14x) |
| format | 67.0 | 48.7 (0.73x) | 259.6 (3.87x) | 119.4 (1.78x) | 99.2 (1.48x) |
| gsub | 17.7 | 25.0 (1.41x) | 260.3 (14.69x) | 72.8 (4.11x) | 53.3 (3.01x) |
| json | 29.7 | 32.5 (1.10x) | – | 143.7 (4.84x) | – |
| mathcalls | 63.3 | 80.2 (1.27x) | – | 462.6 (7.31x) | – |
| nbody | 44.3 | 46.2 (1.04x) | 288.9 (6.53x) | 273.4 (6.18x) | 274.4 (6.20x) |
| oop | 48.2 | 75.3 (1.56x) | 226.1 (4.69x) | 222.4 (4.61x) | 239.1 (4.96x) |
| pairs | 16.3 | 20.5 (1.25x) | 88.4 (5.42x) | 122.4 (7.51x) | 64.3 (3.94x) |
| parse | 32.6 | 48.6 (1.49x) | 204.1 (6.27x) | 129.4 (3.97x) | 104.1 (3.20x) |
| policy | 40.5 | 42.8 (1.06x) | 155.9 (3.85x) | 239.4 (5.91x) | 201.9 (4.99x) |
| sieve | 50.3 | 74.7 (1.49x) | 375.4 (7.46x) | 466.4 (9.27x) | 576.9 (11.47x) |
| sort | 34.8 | 48.0 (1.38x) | – | 168.2 (4.84x) | – |
| spectralnorm | 24.5 | 27.4 (1.12x) | 117.5 (4.80x) | 175.5 (7.16x) | 104.4 (4.26x) |
| vararg | 27.7 | 39.3 (1.42x) | – | 221.1 (7.99x) | 92.2 (3.33x) |
| Geometric mean of the ratios, over the scripts the engine ran | | 1.20x (21 scripts) | 5.22x (16 scripts) | 5.97x (21 scripts) | 4.26x (17 scripts) |

- GopherLua, binarytrees: does not load: <string> line:14(column:22) near '<':   syntax error
- GopherLua, json: returns 311640, C Lua 313140
- GopherLua, mathcalls: does not load: <string> line:5(column:87) near '/':   syntax error
- GopherLua, sort: returns 106692, C Lua 106590
- GopherLua, vararg: fails: <string>:13: attempt to call a non-function object
- wangshu, binarytrees: does not load: [string "binarytrees.lua"]:14: unexpected symbol near '<'
- wangshu, json: returns 311640, C Lua 313140
- wangshu, mathcalls: does not load: [string "mathcalls.lua"]:5: unexpected symbol near '/'
- wangshu, sort: returns 106692, C Lua 106590

A dash means the engine did not run the script, for the reason listed under the table. All of
them come from differences between Lua versions: `<<` and `//` are not Lua 5.1 syntax,
`table.unpack` is `unpack` in Lua 5.1, and in sort and json the checksum differs because Lua
5.1 has only float numbers, so the random number generator's arithmetic and `tostring` of
whole floats come out differently.

Some engines offer modes these runs do not use: wangshu has optional compilation tiers, golua
a resource-limited mode, and glua the bounds-checked loop below. Each engine runs as its documentation
shows for embedding, with default settings.

## glua's two interpreter loops

glua has two interpreter loops generated from one source. The fast loop, the default, reads
through raw pointers that the bytecode verifier vouches for. The checked loop, which `Hardened`
runs, indexes everything through bounds-checked slices, so even a VM bug is a recoverable
panic. The third column runs the checked loop while a background goroutine keeps the Go
garbage collector marking a 3-million-node heap, which puts every pointer write in the VM
behind a write barrier.

Times in ms, medians of three rounds of three runs each, with the range of the round means:

| Script, ms | C Lua 5.4.9 | glua, checked | / C Lua | glua, fast | / C Lua | checked, GC marking | / C Lua |
| --- | --- | --- | --- | --- | --- | --- | --- |
| binarytrees | 29.25 (28.68–31.79) | 29.79 (29.46–31.82) | 1.02x | 25.66 (25.39–27.19) | 0.88x | 78.47 (77.00–81.16) | 2.68x |
| closures | 36.45 (36.34–36.45) | 52.28 (50.29–54.32) | 1.43x | 41.70 (39.92–46.32) | 1.14x | 130.18 (129.08–151.25) | 3.57x |
| concat | 53.43 (53.40–56.75) | 31.74 (31.11–32.78) | 0.59x | 32.10 (30.84–34.56) | 0.60x | 95.84 (84.55–96.47) | 1.79x |
| coroutine | 14.06 (13.78–14.29) | 28.45 (26.31–28.64) | 2.02x | 26.59 (25.13–27.51) | 1.89x | 118.39 (117.68–120.84) | 8.42x |
| fannkuch | 33.65 (33.55–35.10) | 57.58 (57.43–60.18) | 1.71x | 51.31 (50.37–59.32) | 1.52x | 76.41 (73.45–77.60) | 2.27x |
| fib | 17.05 (16.44–18.03) | 23.75 (23.23–24.67) | 1.39x | 17.56 (17.53–18.24) | 1.03x | 57.30 (56.23–57.35) | 3.36x |
| fieldread | 12.76 (12.27–12.93) | 17.85 (16.30–18.40) | 1.40x | 13.21 (12.39–14.41) | 1.04x | 18.37 (17.55–20.46) | 1.44x |
| floatloop | 11.59 (11.09–11.89) | 16.72 (16.54–17.29) | 1.44x | 13.38 (13.24–13.61) | 1.15x | 19.22 (18.83–21.45) | 1.66x |
| format | 68.33 (67.97–72.71) | 48.65 (46.99–50.21) | 0.71x | 48.85 (47.19–51.64) | 0.71x | 88.89 (86.03–89.10) | 1.30x |
| gsub | 17.69 (17.41–18.76) | 26.28 (26.09–26.40) | 1.49x | 25.28 (24.54–25.93) | 1.43x | 55.17 (52.27–56.49) | 3.12x |
| json | 30.72 (29.49–31.02) | 32.48 (31.02–33.18) | 1.06x | 30.52 (29.68–31.51) | 0.99x | 86.92 (84.61–87.59) | 2.83x |
| mathcalls | 65.38 (64.80–69.26) | 90.53 (89.61–90.55) | 1.38x | 80.55 (80.17–81.19) | 1.23x | 213.53 (204.37–214.82) | 3.27x |
| nbody | 49.28 (48.57–49.33) | 58.49 (57.46–63.20) | 1.19x | 45.07 (44.34–47.02) | 0.91x | 89.25 (89.24–90.66) | 1.81x |
| oop | 52.64 (49.99–53.26) | 76.24 (73.01–78.68) | 1.45x | 73.16 (70.16–73.95) | 1.39x | 184.91 (184.04–184.96) | 3.51x |
| pairs | 18.61 (17.57–19.20) | 22.49 (22.45–23.34) | 1.21x | 19.87 (19.63–21.63) | 1.07x | 77.27 (73.46–79.41) | 4.15x |
| parse | 32.64 (32.45–33.30) | 46.62 (45.03–46.76) | 1.43x | 45.52 (44.39–45.80) | 1.39x | 106.43 (99.96–106.53) | 3.26x |
| policy | 42.06 (38.84–42.07) | 49.30 (49.09–52.59) | 1.17x | 39.51 (37.54–39.62) | 0.94x | 110.77 (108.47–114.80) | 2.63x |
| sieve | 48.82 (47.57–52.80) | 97.82 (78.13–99.71) | 2.00x | 85.97 (72.59–91.63) | 1.76x | 150.04 (148.09–153.93) | 3.07x |
| sort | 32.83 (32.70–33.31) | 53.19 (51.98–53.23) | 1.62x | 50.85 (48.74–52.87) | 1.55x | 140.26 (139.05–143.30) | 4.27x |
| spectralnorm | 24.90 (24.20–26.11) | 39.85 (38.14–41.01) | 1.60x | 28.37 (27.26–30.15) | 1.14x | 66.66 (65.23–66.76) | 2.68x |
| vararg | 29.44 (28.62–29.57) | 37.80 (36.96–38.15) | 1.28x | 36.57 (36.39–36.61) | 1.24x | 102.90 (102.76–103.38) | 3.50x |
| Geometric mean | | | 1.31x | | 1.15x | | 2.82x |
| Worst | | | 2.02x | | 1.89x | | 8.42x |

## Calls between Go and Lua

The cost of one call from Go into a loaded script, through the public API, from a typed
function to the result decoded back into Go. "Private VM" is a `VM` with its own executor;
the other rows call a shared `Script`, which takes an executor from a pool. None of the rows
allocate inside glua; the one allocation is the caller's own `&request{...}`.

Nanoseconds per call, medians of eight rounds, with the range:

| Call from Go | ns, checked loop | ns, fast loop | Allocations |
| --- | --- | --- | --- |
| `function(x) return x + 1 end`, typed, private VM | 89.7 (84.3–90.7) | 81.6 (80.3–89.1) | 0 |
| Same, shared Script, executor from the pool | 104.0 (98.3–108.7) | 101.8 (95.4–104.7) | 0 |
| 46-byte string in, same string out | 92.5 (91.0–97.5) | 91.5 (89.6–96.3) | 0 |
| Rule reading three fields of a `*request` that is reused | 160.5 (150.1–161.7) | 161.8 (151.9–166.5) | 0 |
| Same rule when `req.method` does not share a pointer with the constant | 156.0 (149.9–161.8) | 161.6 (152.3–164.5) | 0 |
| Same rule through `Expr` and `Rule.Eval`, struct built per call | 161.8 (151.9–164.6) | 162.2 (155.5–167.4) | 0 |
| Same rule through `Func1`, struct built per call and passed by value | 174.9 (168.7–180.5) | 182.4 (171.6–183.9) | 0 |
| Same rule through `Func1`, pointer to a struct built per call | 180.6 (179.7–183.2) | 184.9 (182.4–188.1) | 1 |
| Function that calls one Go host function made with `Fn1` | 192.0 (184.3–200.7) | 181.6 (179.4–192.1) | 0 |
| Same, host function written as a `GoFunc` | 171.4 (168.3–183.6) | 175.4 (166.9–178.8) | 0 |
| Function that passes a `*request` through a host function made with `Fn1` | 210.1 (197.4–212.5) | 205.2 (191.5–207.5) | 0 |
| Same, host function a `GoFunc` using `Arg` and `Return` | 213.0 (211.3–227.0) | 214.1 (206.8–221.7) | 0 |
| Same, called from every core at once, wall time per call | 122.8 (121.7–124.8) | 121.0 (119.6–122.5) | 0 |

The last three rows pass a pointer to a bound struct through the host function and back,
which converts without reflection. In v0.4.0 they took about 550, 530 and 600 ns: each
conversion went by reflection, and calls from several goroutines queued on a lock that
looked up the struct's type.

## How the numbers are taken

The machine is an Intel Core i7-7600U laptop with a `powersave` CPU governor, not a quiet
benchmark machine, so individual numbers move by 10 to 20 percent between runs; ratios are
more stable than times. Code layout moves them too: a change elsewhere in the binary can shift
where the CPU's branch predictor keeps its history for the interpreter loop's dispatch, which
has moved single rows of the call table by about 15 percent between builds with the same
instructions executed. Go 1.27.2 on linux/amd64; C Lua 5.4.9 built by
[`bench/build-clua.sh`](../bench/build-clua.sh) with gcc at `-O2`.

- Scripts across implementations: `go run -C bench/engines . -rounds 3`. Each round runs every
  script once in each engine, in turn. C Lua times itself with `os.clock` around calls to the
  script's work function, after a warm-up call. The Go engines load the script once, make a
  warm-up call that is checked against C Lua's result, and are timed over three further calls.
  The harness is a module of its own, so glua depends on none of the other engines.
- glua's loops: `go run ./bench/compare -suite -rounds 3 -runs 3`, interleaving C Lua and the three glua
  variants in each round.
- Calls between Go and Lua: `go run ./bench/compare -boundary -rounds 8`, Go benchmarks run in
  interleaved rounds on both loops.

Tables give medians of the round means; where a range is shown, it is the range of the round
means.
