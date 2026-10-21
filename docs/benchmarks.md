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
| glua (fast loop) | 1.13x |
| wangshu | 4.22x |
| GopherLua | 5.28x |
| golua | 5.89x |

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
| glua | main (0127c76) | Lua 5.4 | The fast interpreter loop, the default |
| GopherLua | v1.1.2 | Lua 5.1 | Default options, except that the data stack may grow (`RegistryMaxSize`), which two scripts need |
| golua | v0.4.0 | Lua 5.5 | Default runtime with the standard library |
| wangshu | v0.2.0 | Lua 5.1 | Default options |

Times in ms, medians of three rounds of three runs each, with the ratio to C Lua:

| Script | C Lua 5.4.9 | glua (fast loop) | GopherLua v1.1.2 | golua v0.4.0 | wangshu v0.2.0 |
| --- | --- | --- | --- | --- | --- |
| binarytrees | 29.7 | 24.6 (0.83x) | – | 118.1 (3.98x) | – |
| closures | 37.4 | 47.0 (1.26x) | 206.0 (5.51x) | 316.8 (8.48x) | 256.5 (6.87x) |
| concat | 53.1 | 32.8 (0.62x) | 103.9 (1.96x) | 154.6 (2.91x) | 158.5 (2.99x) |
| coroutine | 13.8 | 20.1 (1.46x) | 43.5 (3.16x) | 193.4 (14.02x) | 62.8 (4.55x) |
| fannkuch | 35.8 | 46.5 (1.30x) | 203.0 (5.67x) | 232.6 (6.49x) | 247.2 (6.90x) |
| fib | 16.8 | 18.1 (1.08x) | 76.9 (4.58x) | 157.3 (9.37x) | 86.3 (5.14x) |
| fieldread | 10.8 | 12.6 (1.16x) | 77.9 (7.19x) | 108.6 (10.02x) | 48.3 (4.46x) |
| floatloop | 10.6 | 14.1 (1.33x) | 80.1 (7.59x) | 79.2 (7.50x) | 31.0 (2.93x) |
| format | 72.6 | 47.7 (0.66x) | 268.7 (3.70x) | 116.9 (1.61x) | 97.6 (1.34x) |
| gsub | 18.1 | 26.2 (1.45x) | 274.7 (15.19x) | 70.9 (3.92x) | 58.9 (3.26x) |
| json | 28.7 | 31.5 (1.10x) | – | 143.2 (4.99x) | – |
| mathcalls | 68.9 | 74.6 (1.08x) | – | 462.2 (6.71x) | – |
| nbody | 48.3 | 44.5 (0.92x) | 289.8 (6.00x) | 288.5 (5.97x) | 271.4 (5.62x) |
| oop | 52.4 | 68.3 (1.30x) | 233.0 (4.44x) | 215.6 (4.11x) | 227.6 (4.34x) |
| pairs | 17.3 | 16.5 (0.96x) | 91.9 (5.33x) | 118.1 (6.84x) | 63.9 (3.70x) |
| parse | 34.8 | 49.1 (1.41x) | 202.4 (5.82x) | 135.9 (3.91x) | 104.3 (3.00x) |
| policy | 38.7 | 39.0 (1.01x) | 158.2 (4.09x) | 235.6 (6.08x) | 189.7 (4.90x) |
| sieve | 47.3 | 77.7 (1.64x) | 379.0 (8.01x) | 456.5 (9.65x) | 556.7 (11.77x) |
| sort | 34.8 | 45.7 (1.31x) | – | 174.0 (5.01x) | – |
| spectralnorm | 25.0 | 28.9 (1.15x) | 122.9 (4.91x) | 177.2 (7.09x) | 104.6 (4.18x) |
| vararg | 27.6 | 39.7 (1.44x) | – | 223.5 (8.10x) | 94.5 (3.43x) |
| Geometric mean of the ratios, over the scripts the engine ran | | 1.13x (21 scripts) | 5.28x (16 scripts) | 5.89x (21 scripts) | 4.22x (17 scripts) |

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
| binarytrees | 31.41 (29.22–32.92) | 26.13 (26.11–26.74) | 0.83x | 24.54 (23.46–25.08) | 0.78x | 75.75 (73.42–76.74) | 2.41x |
| closures | 39.38 (38.82–39.41) | 52.12 (51.60–53.52) | 1.32x | 41.56 (38.96–41.92) | 1.06x | 132.29 (123.73–146.18) | 3.36x |
| concat | 53.67 (52.75–54.14) | 32.49 (31.58–33.62) | 0.61x | 31.25 (30.60–33.02) | 0.58x | 99.70 (93.73–112.02) | 1.86x |
| coroutine | 15.17 (14.56–15.51) | 21.63 (21.60–21.70) | 1.43x | 20.43 (18.17–20.50) | 1.35x | 88.39 (88.06–90.61) | 5.83x |
| fannkuch | 35.77 (33.42–36.24) | 58.79 (54.67–58.96) | 1.64x | 50.55 (46.16–50.59) | 1.41x | 72.37 (71.18–75.90) | 2.02x |
| fib | 17.97 (16.61–18.45) | 25.47 (23.66–26.09) | 1.42x | 19.42 (18.41–19.77) | 1.08x | 55.19 (51.26–55.71) | 3.07x |
| fieldread | 11.48 (11.43–12.11) | 17.13 (16.95–17.99) | 1.49x | 12.98 (12.97–13.98) | 1.13x | 19.99 (18.74–20.37) | 1.74x |
| floatloop | 10.45 (10.29–11.23) | 18.37 (17.07–18.53) | 1.76x | 12.86 (12.76–13.59) | 1.23x | 18.35 (18.27–21.10) | 1.76x |
| format | 72.27 (68.92–78.52) | 50.35 (49.24–51.65) | 0.70x | 46.95 (46.54–48.72) | 0.65x | 85.87 (85.36–88.94) | 1.19x |
| gsub | 18.90 (17.17–18.94) | 27.41 (26.11–27.79) | 1.45x | 25.43 (24.52–25.69) | 1.35x | 55.49 (53.49–58.53) | 2.94x |
| json | 31.11 (30.70–31.68) | 30.94 (30.75–31.23) | 0.99x | 29.57 (29.04–29.98) | 0.95x | 85.07 (81.84–87.01) | 2.73x |
| mathcalls | 65.14 (65.06–69.30) | 88.47 (87.78–89.49) | 1.36x | 78.05 (72.83–78.80) | 1.20x | 200.80 (199.13–203.88) | 3.08x |
| nbody | 45.25 (43.76–45.29) | 60.28 (60.17–60.34) | 1.33x | 47.81 (47.66–47.97) | 1.06x | 88.63 (85.54–89.10) | 1.96x |
| oop | 53.31 (49.66–53.62) | 70.46 (69.92–71.00) | 1.32x | 64.15 (64.07–68.18) | 1.20x | 178.73 (176.59–181.17) | 3.35x |
| pairs | 18.03 (17.72–19.28) | 19.61 (17.31–19.86) | 1.09x | 17.38 (17.00–23.95) | 0.96x | 50.99 (49.95–56.52) | 2.83x |
| parse | 34.82 (33.13–34.92) | 48.70 (47.22–49.71) | 1.40x | 47.25 (46.57–48.56) | 1.36x | 105.66 (104.15–106.48) | 3.03x |
| policy | 40.03 (39.83–41.31) | 49.30 (45.58–49.58) | 1.23x | 38.47 (35.65–39.88) | 0.96x | 109.13 (107.06–114.00) | 2.73x |
| sieve | 51.63 (47.70–53.97) | 100.09 (85.12–100.93) | 1.94x | 89.38 (71.29–91.16) | 1.73x | 169.97 (165.09–180.35) | 3.29x |
| sort | 32.89 (32.61–35.83) | 53.71 (51.40–54.15) | 1.63x | 47.96 (47.50–49.58) | 1.46x | 135.00 (132.79–139.13) | 4.10x |
| spectralnorm | 25.50 (24.58–26.44) | 37.24 (36.91–47.30) | 1.46x | 28.61 (27.18–28.67) | 1.12x | 66.10 (64.19–69.39) | 2.59x |
| vararg | 29.58 (26.63–29.68) | 37.80 (37.40–37.97) | 1.28x | 38.61 (37.99–39.05) | 1.31x | 107.31 (104.10–111.84) | 3.63x |
| Geometric mean | | | 1.27x | | 1.10x | | 2.68x |
| Worst | | | 1.94x | | 1.73x | | 5.83x |

## Calls between Go and Lua

The cost of one call from Go into a loaded script, through the public API, from a typed
function to the result decoded back into Go. "Private VM" is a `VM` with its own executor;
the other rows call a shared `Script`, which takes an executor from a pool. None of the rows
allocate inside glua; the one allocation is the caller's own `&request{...}`.

Nanoseconds per call, medians of eight rounds, with the range:

| Call from Go | ns, checked loop | ns, fast loop | Allocations |
| --- | --- | --- | --- |
| `function(x) return x + 1 end`, typed, private VM | 81.4 (80.4–88.4) | 80.7 (78.5–83.4) | 0 |
| Same, shared Script, executor from the pool | 104.2 (99.1–111.0) | 100.0 (97.3–107.1) | 0 |
| 46-byte string in, same string out | 101.3 (95.6–103.5) | 101.2 (93.9–102.9) | 0 |
| Rule reading three fields of a `*request` that is reused | 158.6 (152.2–165.6) | 148.4 (138.1–153.1) | 0 |
| Same rule when `req.method` does not share a pointer with the constant | 162.2 (152.2–165.9) | 148.7 (141.5–150.8) | 0 |
| Same rule through `Expr` and `Rule.Eval`, struct built per call | 157.2 (151.6–165.4) | 141.7 (135.9–147.1) | 0 |
| Same rule through `Func1`, struct built per call and passed by value | 175.1 (168.4–184.9) | 168.0 (155.7–173.4) | 0 |
| Same rule through `Func1`, pointer to a struct built per call | 183.1 (179.1–186.7) | 170.3 (167.2–171.6) | 1 |
| Function that calls one Go host function made with `Fn1` | 196.1 (188.9–203.9) | 194.9 (183.1–197.3) | 0 |
| Same, host function written as a `GoFunc` | 175.0 (173.6–187.2) | 171.6 (168.3–181.2) | 0 |
| Function that passes a `*request` through a host function made with `Fn1` | 203.9 (202.3–216.0) | 199.8 (198.4–211.7) | 0 |
| Same, host function a `GoFunc` using `Arg` and `Return` | 225.6 (217.5–234.0) | 226.9 (210.9–229.8) | 0 |
| Same, called from every core at once, wall time per call | 128.0 (126.5–128.9) | 124.4 (123.6–126.1) | 0 |

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
instructions executed. Go 1.27.3 on linux/amd64; C Lua 5.4.9 built by
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
