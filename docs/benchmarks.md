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
| glua (checked loop) | 1.46x |
| wangshu | 4.29x |
| GopherLua | 5.23x |
| golua | 6.02x |

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
| glua | main at d3b869f, after v0.1.1 | Lua 5.4 | The bounds-checked interpreter loop, the current default |
| GopherLua | v1.1.2 | Lua 5.1 | Default options, except that the data stack may grow (`RegistryMaxSize`), which two scripts need |
| golua | v0.4.0 | Lua 5.5 | Default runtime with the standard library |
| wangshu | v0.2.0 | Lua 5.1 | Default options |

Times in ms, medians of three rounds of three runs each, with the ratio to C Lua:

| Script | C Lua 5.4.9 | glua (checked loop) | GopherLua v1.1.2 | golua v0.4.0 | wangshu v0.2.0 |
| --- | --- | --- | --- | --- | --- |
| binarytrees | 31.0 | 31.8 (1.03x) | – | 122.4 (3.95x) | – |
| closures | 39.1 | 55.1 (1.41x) | 209.2 (5.35x) | 325.2 (8.32x) | 264.7 (6.77x) |
| concat | 56.3 | 35.4 (0.63x) | 101.4 (1.80x) | 161.5 (2.87x) | 158.8 (2.82x) |
| coroutine | 15.1 | 26.1 (1.73x) | 40.9 (2.71x) | 201.6 (13.35x) | 64.9 (4.30x) |
| fannkuch | 35.7 | 61.3 (1.71x) | 207.6 (5.81x) | 247.8 (6.93x) | 267.6 (7.49x) |
| fib | 16.4 | 24.1 (1.47x) | 76.7 (4.67x) | 165.9 (10.11x) | 88.9 (5.42x) |
| fieldread | 11.3 | 17.8 (1.57x) | 84.6 (7.46x) | 109.3 (9.63x) | 48.1 (4.24x) |
| floatloop | 10.6 | 18.5 (1.74x) | 72.9 (6.87x) | 82.8 (7.81x) | 31.7 (2.99x) |
| format | 72.7 | 77.5 (1.07x) | 278.3 (3.83x) | 121.1 (1.66x) | 104.4 (1.44x) |
| gsub | 18.1 | 31.2 (1.73x) | 272.6 (15.08x) | 75.3 (4.17x) | 55.5 (3.07x) |
| json | 29.7 | 38.1 (1.28x) | – | 155.1 (5.23x) | – |
| mathcalls | 64.8 | 86.9 (1.34x) | – | 465.3 (7.18x) | – |
| nbody | 48.2 | 59.3 (1.23x) | 301.1 (6.24x) | 298.1 (6.18x) | 287.0 (5.95x) |
| oop | 53.8 | 97.5 (1.81x) | 247.0 (4.59x) | 230.0 (4.28x) | 237.7 (4.42x) |
| pairs | 17.7 | 25.9 (1.47x) | 91.2 (5.16x) | 122.0 (6.91x) | 66.6 (3.77x) |
| parse | 33.0 | 55.1 (1.67x) | 203.8 (6.17x) | 137.5 (4.16x) | 109.3 (3.31x) |
| policy | 41.5 | 46.2 (1.11x) | 168.0 (4.04x) | 241.1 (5.80x) | 206.7 (4.97x) |
| sieve | 49.0 | 100.1 (2.04x) | 404.3 (8.25x) | 491.4 (10.02x) | 622.4 (12.70x) |
| sort | 36.3 | 62.8 (1.73x) | – | 180.9 (4.99x) | – |
| spectralnorm | 24.6 | 38.6 (1.57x) | 122.3 (4.97x) | 184.8 (7.52x) | 109.8 (4.47x) |
| vararg | 28.3 | 65.5 (2.31x) | – | 233.7 (8.25x) | 92.4 (3.26x) |
| Geometric mean of the ratios, over the scripts the engine ran | | 1.46x (21 scripts) | 5.23x (16 scripts) | 6.02x (21 scripts) | 4.29x (17 scripts) |

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
| binarytrees | 32.60 (30.50–32.94) | 30.50 (30.48–31.69) | 0.94x | 29.50 (28.45–41.35) | 0.91x | 91.76 (83.21–91.82) | 2.82x |
| closures | 38.36 (37.46–40.21) | 54.48 (54.41–54.67) | 1.42x | 43.95 (43.24–44.38) | 1.15x | 125.64 (123.32–146.51) | 3.28x |
| concat | 58.09 (58.03–59.72) | 35.19 (34.50–36.71) | 0.61x | 32.80 (32.02–34.11) | 0.56x | 99.34 (90.51–102.39) | 1.71x |
| coroutine | 14.78 (14.20–15.87) | 27.45 (25.60–27.94) | 1.86x | 25.77 (24.02–26.34) | 1.74x | 104.70 (104.04–104.88) | 7.08x |
| fannkuch | 34.91 (34.77–37.25) | 61.68 (57.16–75.85) | 1.77x | 51.69 (49.66–59.23) | 1.48x | 76.40 (75.96–80.95) | 2.19x |
| fib | 18.20 (17.09–18.67) | 23.62 (23.54–24.37) | 1.30x | 18.44 (17.97–18.73) | 1.01x | 54.93 (53.35–57.18) | 3.02x |
| fieldread | 12.41 (11.78–12.43) | 18.09 (17.76–18.40) | 1.46x | 14.14 (13.85–14.76) | 1.14x | 20.14 (19.33–26.69) | 1.62x |
| floatloop | 12.19 (10.72–12.39) | 18.84 (17.37–19.22) | 1.55x | 14.25 (12.47–14.32) | 1.17x | 20.13 (19.19–39.66) | 1.65x |
| format | 70.37 (69.57–106.91) | 81.53 (80.94–171.03) | 1.16x | 92.42 (76.41–106.63) | 1.31x | 137.14 (131.86–186.47) | 1.95x |
| gsub | 20.21 (18.27–25.26) | 29.01 (28.62–29.15) | 1.44x | 28.68 (28.18–28.93) | 1.42x | 65.45 (62.73–67.59) | 3.24x |
| json | 32.00 (31.60–32.92) | 37.80 (37.72–37.88) | 1.18x | 34.32 (33.44–36.05) | 1.07x | 99.68 (94.40–145.99) | 3.12x |
| mathcalls | 70.95 (70.83–71.53) | 90.84 (88.58–94.13) | 1.28x | 78.78 (78.78–83.54) | 1.11x | 209.58 (208.38–221.18) | 2.95x |
| nbody | 46.89 (43.33–49.72) | 60.42 (59.74–62.27) | 1.29x | 47.12 (46.95–48.22) | 1.00x | 89.00 (87.77–91.53) | 1.90x |
| oop | 52.58 (50.20–54.99) | 84.86 (82.53–93.04) | 1.61x | 76.51 (75.41–79.95) | 1.46x | 197.65 (193.89–203.40) | 3.76x |
| pairs | 19.67 (19.01–19.81) | 27.93 (25.65–29.88) | 1.42x | 26.20 (24.38–26.48) | 1.33x | 84.38 (82.79–86.96) | 4.29x |
| parse | 34.09 (33.89–35.53) | 53.94 (53.85–54.88) | 1.58x | 62.10 (54.66–62.88) | 1.82x | 123.40 (121.97–124.23) | 3.62x |
| policy | 42.53 (40.76–45.18) | 49.99 (48.78–51.86) | 1.18x | 40.23 (37.80–41.66) | 0.95x | 113.12 (110.42–117.73) | 2.66x |
| sieve | 53.00 (52.88–53.49) | 117.27 (114.24–118.46) | 2.21x | 102.99 (98.51–103.83) | 1.94x | 173.67 (169.23–181.21) | 3.28x |
| sort | 37.07 (37.07–37.31) | 62.54 (61.78–63.40) | 1.69x | 59.35 (58.25–62.48) | 1.60x | 160.29 (148.10–161.52) | 4.32x |
| spectralnorm | 25.97 (25.47–27.63) | 37.83 (37.65–38.35) | 1.46x | 28.19 (27.96–29.81) | 1.09x | 69.44 (66.85–80.52) | 2.67x |
| vararg | 29.02 (28.49–29.65) | 65.21 (63.75–78.45) | 2.25x | 59.72 (58.53–63.11) | 2.06x | 214.63 (209.96–216.45) | 7.40x |
| Geometric mean | | | 1.41x | | 1.25x | | 2.99x |
| Worst | | | 2.25x | | 2.06x | | 7.40x |

## Calls between Go and Lua

The cost of one call from Go into a loaded script, through the public API, from a typed
function to the result decoded back into Go. "Private VM" is a `VM` with its own executor;
the other rows call a shared `Script`, which takes an executor from a pool. None of the rows
allocate inside glua; the one allocation is the caller's own `&request{...}`.

Nanoseconds per call, medians of eight rounds, with the range:

| Call from Go | ns, checked loop | ns, fast loop | Allocations |
| --- | --- | --- | --- |
| `function(x) return x + 1 end`, typed, private VM | 84.1 (79.4–89.4) | 79.1 (76.1–82.8) | 0 |
| Same, shared Script, executor from the pool | 101.8 (99.1–108.9) | 99.7 (96.5–104.2) | 0 |
| 46-byte string in, same string out | 99.2 (93.2–107.2) | 97.1 (93.5–100.6) | 0 |
| Rule reading three fields of a `*request` that is reused | 159.2 (153.4–165.8) | 153.1 (144.3–193.8) | 0 |
| Same rule when `req.method` does not share a pointer with the constant | 159.9 (153.0–215.1) | 150.9 (143.3–201.9) | 0 |
| Same rule through `Expr` and `Rule.Eval`, struct built per call | 159.3 (153.6–187.8) | 147.2 (143.4–159.5) | 0 |
| Same rule through `Func1`, struct built per call and passed by value | 174.7 (170.3–181.5) | 165.1 (161.3–318.7) | 0 |
| Same rule through `Func1`, pointer to a struct built per call | 189.2 (185.1–212.7) | 180.3 (176.7–191.4) | 1 |
| Function that calls one Go host function made with `Fn1` | 200.9 (197.7–259.0) | 194.6 (181.7–247.3) | 0 |
| Same, host function written as a `GoFunc` | 185.1 (175.2–209.2) | 175.4 (169.8–229.9) | 0 |

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
