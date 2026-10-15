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
| glua (checked loop) | 1.40x |
| wangshu | 4.15x |
| GopherLua | 5.18x |
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
| glua | v0.2.0 | Lua 5.4 | The bounds-checked interpreter loop, the current default |
| GopherLua | v1.1.2 | Lua 5.1 | Default options, except that the data stack may grow (`RegistryMaxSize`), which two scripts need |
| golua | v0.4.0 | Lua 5.5 | Default runtime with the standard library |
| wangshu | v0.2.0 | Lua 5.1 | Default options |

Times in ms, medians of three rounds of three runs each, with the ratio to C Lua:

| Script | C Lua 5.4.9 | glua (checked loop) | GopherLua v1.1.2 | golua v0.4.0 | wangshu v0.2.0 |
| --- | --- | --- | --- | --- | --- |
| binarytrees | 35.6 | 40.4 (1.13x) | – | 144.3 (4.05x) | – |
| closures | 49.7 | 73.2 (1.47x) | 258.1 (5.19x) | 411.6 (8.28x) | 278.7 (5.61x) |
| concat | 62.1 | 50.6 (0.81x) | 123.3 (1.99x) | 195.4 (3.15x) | 203.2 (3.27x) |
| coroutine | 17.2 | 32.1 (1.86x) | 53.2 (3.09x) | 266.9 (15.51x) | 78.3 (4.55x) |
| fannkuch | 39.1 | 62.2 (1.59x) | 236.0 (6.03x) | 255.6 (6.53x) | 275.7 (7.04x) |
| fib | 18.6 | 25.9 (1.40x) | 81.9 (4.42x) | 181.0 (9.76x) | 93.2 (5.02x) |
| fieldread | 12.0 | 17.6 (1.47x) | 81.3 (6.77x) | 110.1 (9.16x) | 48.3 (4.02x) |
| floatloop | 11.2 | 18.1 (1.61x) | 91.6 (8.17x) | 100.2 (8.93x) | 43.9 (3.92x) |
| format | 77.4 | 82.7 (1.07x) | 288.5 (3.73x) | 130.1 (1.68x) | 106.0 (1.37x) |
| gsub | 19.1 | 31.0 (1.63x) | 302.8 (15.86x) | 79.5 (4.16x) | 62.6 (3.28x) |
| json | 32.8 | 40.3 (1.23x) | – | 153.6 (4.69x) | – |
| mathcalls | 71.9 | 95.8 (1.33x) | – | 485.2 (6.74x) | – |
| nbody | 56.1 | 65.9 (1.17x) | 318.1 (5.67x) | 329.5 (5.87x) | 282.4 (5.03x) |
| oop | 60.3 | 118.3 (1.96x) | 291.0 (4.83x) | 271.0 (4.50x) | 251.9 (4.18x) |
| pairs | 18.6 | 28.4 (1.52x) | 95.0 (5.10x) | 154.5 (8.29x) | 78.1 (4.19x) |
| parse | 37.3 | 60.9 (1.63x) | 212.7 (5.71x) | 140.9 (3.78x) | 110.7 (2.97x) |
| policy | 42.7 | 52.1 (1.22x) | 183.4 (4.29x) | 256.0 (5.99x) | 209.9 (4.91x) |
| sieve | 57.4 | 95.3 (1.66x) | 476.7 (8.31x) | 543.8 (9.47x) | 619.7 (10.80x) |
| sort | 37.2 | 65.4 (1.76x) | – | 193.8 (5.21x) | – |
| spectralnorm | 38.2 | 41.5 (1.09x) | 131.0 (3.43x) | 195.4 (5.12x) | 127.3 (3.33x) |
| vararg | 30.0 | 44.5 (1.48x) | – | 249.3 (8.31x) | 98.6 (3.28x) |
| Geometric mean of the ratios, over the scripts the engine ran | | 1.40x (21 scripts) | 5.18x (16 scripts) | 5.97x (21 scripts) | 4.15x (17 scripts) |

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
a resource-limited mode, and glua the faster loop below. Each engine runs as its documentation
shows for embedding, with default settings.

## glua's two interpreter loops

glua has two interpreter loops generated from one source. The checked loop indexes everything
through bounds-checked slices, so even a VM bug is a recoverable panic; it is the default. The
fast loop reads through raw pointers that the bytecode verifier vouches for, and becomes the
default once a week of continuous fuzzing has run clean. The third column runs the checked
loop while a background goroutine keeps the Go garbage collector marking a 3-million-node heap,
which puts every pointer write in the VM behind a write barrier.

Times in ms, medians of three rounds of three runs each, with the range of the round means:

| Script, ms | C Lua 5.4.9 | glua, checked | / C Lua | glua, fast | / C Lua | checked, GC marking | / C Lua |
| --- | --- | --- | --- | --- | --- | --- | --- |
| binarytrees | 30.44 (30.11–33.41) | 31.77 (31.55–32.06) | 1.04x | 27.89 (27.68–29.49) | 0.92x | 82.69 (82.60–94.57) | 2.72x |
| closures | 40.18 (37.69–40.64) | 54.99 (54.52–55.43) | 1.37x | 44.04 (43.65–45.87) | 1.10x | 129.20 (127.87–219.90) | 3.22x |
| concat | 60.15 (54.77–70.10) | 35.26 (34.18–35.57) | 0.59x | 34.09 (33.56–35.84) | 0.57x | 96.94 (91.27–107.57) | 1.61x |
| coroutine | 15.57 (14.47–16.52) | 26.87 (26.58–26.89) | 1.73x | 25.99 (25.21–26.11) | 1.67x | 105.36 (102.05–110.40) | 6.77x |
| fannkuch | 39.13 (34.69–43.49) | 63.81 (60.76–66.26) | 1.63x | 49.88 (48.41–52.58) | 1.27x | 83.69 (81.32–83.96) | 2.14x |
| fib | 18.60 (17.56–19.58) | 25.69 (24.19–26.84) | 1.38x | 18.32 (18.06–18.56) | 0.98x | 55.07 (54.18–72.79) | 2.96x |
| fieldread | 12.28 (11.52–13.23) | 17.66 (17.09–17.80) | 1.44x | 13.87 (13.85–14.01) | 1.13x | 21.04 (20.03–25.11) | 1.71x |
| floatloop | 11.71 (11.50–11.72) | 18.11 (18.07–18.74) | 1.55x | 14.49 (13.89–16.72) | 1.24x | 20.27 (19.46–36.13) | 1.73x |
| format | 76.93 (70.45–112.24) | 82.18 (80.15–158.07) | 1.07x | 78.68 (78.40–102.20) | 1.02x | 139.63 (129.09–230.00) | 1.82x |
| gsub | 19.34 (18.13–20.02) | 29.11 (28.26–29.88) | 1.51x | 28.75 (27.62–30.25) | 1.49x | 60.85 (59.11–63.56) | 3.15x |
| json | 31.57 (30.27–32.46) | 37.18 (36.79–38.00) | 1.18x | 35.66 (34.33–36.99) | 1.13x | 110.41 (100.51–134.06) | 3.50x |
| mathcalls | 71.50 (68.32–72.87) | 97.52 (93.58–98.97) | 1.36x | 79.57 (78.60–84.58) | 1.11x | 222.84 (216.89–229.17) | 3.12x |
| nbody | 48.28 (46.96–51.54) | 65.98 (62.89–66.14) | 1.37x | 47.07 (45.53–49.56) | 0.98x | 91.76 (89.07–98.60) | 1.90x |
| oop | 52.23 (52.21–63.19) | 90.16 (88.17–90.58) | 1.73x | 82.20 (80.71–82.74) | 1.57x | 203.94 (196.88–204.55) | 3.90x |
| pairs | 19.27 (18.58–19.85) | 27.99 (26.44–28.99) | 1.45x | 26.52 (25.62–27.71) | 1.38x | 88.03 (86.05–96.75) | 4.57x |
| parse | 34.45 (34.15–50.93) | 54.92 (54.64–54.98) | 1.59x | 54.19 (53.85–55.55) | 1.57x | 121.48 (119.45–151.81) | 3.53x |
| policy | 41.31 (40.94–43.73) | 49.05 (48.86–49.06) | 1.19x | 44.44 (41.96–44.79) | 1.08x | 116.58 (112.31–122.46) | 2.82x |
| sieve | 53.34 (50.76–74.59) | 107.34 (106.96–124.18) | 2.01x | 93.70 (76.64–96.62) | 1.76x | 169.00 (159.48–170.59) | 3.17x |
| sort | 35.10 (34.63–39.57) | 62.34 (62.11–63.90) | 1.78x | 59.96 (58.74–61.29) | 1.71x | 158.91 (151.59–164.19) | 4.53x |
| spectralnorm | 26.63 (25.02–27.61) | 42.01 (38.83–42.29) | 1.58x | 29.87 (29.61–30.42) | 1.12x | 70.92 (65.84–74.36) | 2.66x |
| vararg | 30.89 (28.37–31.70) | 41.55 (41.43–45.48) | 1.35x | 40.96 (40.08–41.01) | 1.33x | 114.22 (114.12–119.38) | 3.70x |
| Geometric mean | | | 1.38x | | 1.21x | | 2.90x |
| Worst | | | 2.01x | | 1.76x | | 6.77x |

## Calls between Go and Lua

The cost of one call from Go into a loaded script, through the public API, from a typed
function to the result decoded back into Go. "Private VM" is a `VM` with its own executor;
the other rows call a shared `Script`, which takes an executor from a pool. None of the rows
allocate inside glua; the one allocation is the caller's own `&request{...}`.

Nanoseconds per call, medians of eight rounds, with the range:

| Call from Go | ns, checked loop | ns, fast loop | Allocations |
| --- | --- | --- | --- |
| `function(x) return x + 1 end`, typed, private VM | 81.1 (79.6–87.5) | 84.1 (78.6–89.7) | 0 |
| Same, shared Script, executor from the pool | 103.4 (99.2–106.2) | 103.0 (97.3–110.9) | 0 |
| 46-byte string in, same string out | 93.2 (92.1–100.7) | 100.9 (93.0–105.2) | 0 |
| Rule reading three fields of a `*request` that is reused | 164.7 (154.5–179.0) | 146.3 (137.3–197.8) | 0 |
| Same rule when `req.method` does not share a pointer with the constant | 166.8 (153.0–196.1) | 149.4 (137.1–210.4) | 0 |
| Same rule through `Expr` and `Rule.Eval`, struct built per call | 165.5 (156.5–207.4) | 143.4 (138.4–150.2) | 0 |
| Same rule through `Func1`, struct built per call and passed by value | 186.2 (178.6–214.9) | 168.1 (158.3–197.4) | 0 |
| Same rule through `Func1`, pointer to a struct built per call | 191.4 (188.1–216.7) | 182.3 (174.6–191.3) | 1 |
| Function that calls one Go host function made with `Fn1` | 197.4 (183.7–252.3) | 193.6 (183.5–268.4) | 0 |
| Same, host function written as a `GoFunc` | 191.7 (172.1–264.9) | 184.6 (170.4–208.4) | 0 |

## How the numbers are taken

The machine is an Intel Core i7-7600U laptop with a `powersave` CPU governor, not a quiet
benchmark machine, so individual numbers move by 10 to 20 percent between runs; ratios are
more stable than times. Go 1.27.2 on linux/amd64; C Lua 5.4.9 built by
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
