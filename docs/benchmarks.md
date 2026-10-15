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
| glua (checked loop) | 1.43x |
| wangshu | 4.34x |
| GopherLua | 5.35x |
| golua | 6.05x |

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
| glua | v0.3.0 | Lua 5.4 | The bounds-checked interpreter loop, the current default |
| GopherLua | v1.1.2 | Lua 5.1 | Default options, except that the data stack may grow (`RegistryMaxSize`), which two scripts need |
| golua | v0.4.0 | Lua 5.5 | Default runtime with the standard library |
| wangshu | v0.2.0 | Lua 5.1 | Default options |

Times in ms, medians of three rounds of three runs each, with the ratio to C Lua:

| Script | C Lua 5.4.9 | glua (checked loop) | GopherLua v1.1.2 | golua v0.4.0 | wangshu v0.2.0 |
| --- | --- | --- | --- | --- | --- |
| binarytrees | 34.8 | 35.3 (1.02x) | – | 123.1 (3.54x) | – |
| closures | 37.2 | 61.4 (1.65x) | 219.3 (5.89x) | 324.1 (8.71x) | 269.8 (7.25x) |
| concat | 57.2 | 36.5 (0.64x) | 105.6 (1.85x) | 164.5 (2.88x) | 158.0 (2.76x) |
| coroutine | 14.9 | 25.7 (1.72x) | 44.9 (3.01x) | 227.1 (15.23x) | 66.3 (4.45x) |
| fannkuch | 37.1 | 62.0 (1.67x) | 215.5 (5.81x) | 249.3 (6.72x) | 270.1 (7.28x) |
| fib | 17.2 | 22.9 (1.33x) | 77.0 (4.48x) | 162.1 (9.43x) | 88.1 (5.13x) |
| fieldread | 11.3 | 18.2 (1.62x) | 77.3 (6.86x) | 110.8 (9.83x) | 47.6 (4.22x) |
| floatloop | 10.8 | 18.6 (1.73x) | 77.9 (7.24x) | 86.1 (8.00x) | 32.9 (3.06x) |
| format | 68.6 | 81.5 (1.19x) | 270.6 (3.95x) | 124.8 (1.82x) | 103.1 (1.50x) |
| gsub | 17.2 | 29.5 (1.71x) | 294.6 (17.12x) | 72.6 (4.22x) | 55.9 (3.25x) |
| json | 31.5 | 35.8 (1.14x) | – | 148.5 (4.72x) | – |
| mathcalls | 64.8 | 90.7 (1.40x) | – | 468.2 (7.23x) | – |
| nbody | 45.2 | 62.0 (1.37x) | 307.5 (6.80x) | 286.9 (6.35x) | 287.7 (6.36x) |
| oop | 52.3 | 91.0 (1.74x) | 252.5 (4.83x) | 244.8 (4.68x) | 262.7 (5.02x) |
| pairs | 21.6 | 27.1 (1.25x) | 102.2 (4.73x) | 138.0 (6.39x) | 74.3 (3.44x) |
| parse | 37.2 | 63.8 (1.71x) | 229.8 (6.18x) | 149.4 (4.02x) | 110.0 (2.96x) |
| policy | 41.8 | 51.3 (1.23x) | 171.5 (4.10x) | 246.2 (5.88x) | 200.9 (4.80x) |
| sieve | 52.1 | 99.9 (1.92x) | 423.5 (8.13x) | 482.9 (9.27x) | 612.0 (11.74x) |
| sort | 37.2 | 57.8 (1.55x) | – | 192.6 (5.17x) | – |
| spectralnorm | 27.1 | 41.9 (1.54x) | 133.1 (4.90x) | 200.6 (7.39x) | 136.2 (5.02x) |
| vararg | 27.2 | 44.7 (1.64x) | – | 251.8 (9.25x) | 95.1 (3.49x) |
| Geometric mean of the ratios, over the scripts the engine ran | | 1.43x (21 scripts) | 5.35x (16 scripts) | 6.05x (21 scripts) | 4.34x (17 scripts) |

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
| binarytrees | 32.96 (31.55–35.15) | 35.81 (32.79–47.92) | 1.09x | 31.58 (31.49–52.15) | 0.96x | 114.92 (100.92–162.87) | 3.49x |
| closures | 42.52 (40.65–67.06) | 71.95 (57.17–104.72) | 1.69x | 54.28 (48.04–74.38) | 1.28x | 144.15 (141.37–244.63) | 3.39x |
| concat | 58.37 (58.21–61.35) | 38.88 (38.08–41.66) | 0.67x | 38.45 (35.52–42.81) | 0.66x | 119.46 (105.33–130.32) | 2.05x |
| coroutine | 17.63 (15.54–23.90) | 28.34 (28.17–38.84) | 1.61x | 27.44 (26.97–28.31) | 1.56x | 117.22 (114.98–127.46) | 6.65x |
| fannkuch | 39.98 (37.41–40.13) | 65.56 (64.63–72.87) | 1.64x | 57.96 (57.54–68.73) | 1.45x | 87.57 (85.06–90.57) | 2.19x |
| fib | 19.31 (19.15–21.44) | 26.95 (25.58–27.59) | 1.40x | 20.15 (18.99–20.71) | 1.04x | 60.39 (59.88–69.26) | 3.13x |
| fieldread | 13.33 (13.26–13.84) | 19.36 (19.22–20.15) | 1.45x | 14.25 (13.78–15.30) | 1.07x | 21.26 (20.36–35.45) | 1.60x |
| floatloop | 12.31 (11.64–12.68) | 19.51 (19.20–19.66) | 1.58x | 14.40 (14.02–14.58) | 1.17x | 21.31 (20.15–23.31) | 1.73x |
| format | 77.54 (75.07–93.23) | 85.76 (82.20–85.80) | 1.11x | 87.64 (82.91–87.87) | 1.13x | 155.90 (143.55–169.53) | 2.01x |
| gsub | 20.57 (19.97–23.53) | 31.60 (30.87–32.82) | 1.54x | 31.36 (30.35–32.31) | 1.52x | 69.86 (68.15–84.60) | 3.40x |
| json | 34.35 (31.04–47.46) | 36.99 (36.22–38.05) | 1.08x | 35.09 (34.95–37.23) | 1.02x | 100.22 (99.15–110.23) | 2.92x |
| mathcalls | 71.80 (70.33–77.86) | 100.41 (97.96–116.40) | 1.40x | 85.53 (85.49–89.29) | 1.19x | 250.11 (226.73–258.96) | 3.48x |
| nbody | 52.09 (51.32–67.18) | 69.59 (66.79–88.05) | 1.34x | 53.09 (51.49–54.24) | 1.02x | 118.64 (106.34–138.66) | 2.28x |
| oop | 55.90 (55.78–56.04) | 92.62 (91.63–96.99) | 1.66x | 87.03 (85.00–99.10) | 1.56x | 220.95 (213.65–229.73) | 3.95x |
| pairs | 19.38 (17.95–20.69) | 28.75 (28.36–28.87) | 1.48x | 27.10 (26.87–30.39) | 1.40x | 90.20 (89.27–98.01) | 4.65x |
| parse | 36.85 (36.71–50.18) | 60.24 (58.19–60.47) | 1.63x | 56.58 (56.12–56.67) | 1.54x | 142.45 (135.84–149.87) | 3.87x |
| policy | 45.20 (44.38–48.37) | 54.07 (50.54–54.48) | 1.20x | 42.48 (40.44–64.78) | 0.94x | 124.66 (124.48–127.14) | 2.76x |
| sieve | 58.46 (57.36–65.46) | 116.53 (113.17–117.70) | 1.99x | 102.26 (80.01–104.83) | 1.75x | 169.18 (164.60–173.29) | 2.89x |
| sort | 40.07 (36.91–40.30) | 61.41 (58.63–80.83) | 1.53x | 56.13 (54.93–56.25) | 1.40x | 160.92 (150.75–161.26) | 4.02x |
| spectralnorm | 29.17 (27.88–29.45) | 44.24 (41.99–44.59) | 1.52x | 32.68 (31.90–53.36) | 1.12x | 79.12 (72.35–80.88) | 2.71x |
| vararg | 30.64 (29.71–32.52) | 43.74 (41.96–46.07) | 1.43x | 42.64 (39.72–42.78) | 1.39x | 116.63 (115.80–140.23) | 3.81x |
| Geometric mean | | | 1.40x | | 1.22x | | 3.01x |
| Worst | | | 1.99x | | 1.75x | | 6.65x |

## Calls between Go and Lua

The cost of one call from Go into a loaded script, through the public API, from a typed
function to the result decoded back into Go. "Private VM" is a `VM` with its own executor;
the other rows call a shared `Script`, which takes an executor from a pool. None of the rows
allocate inside glua; the one allocation is the caller's own `&request{...}`.

Nanoseconds per call, medians of eight rounds, with the range:

| Call from Go | ns, checked loop | ns, fast loop | Allocations |
| --- | --- | --- | --- |
| `function(x) return x + 1 end`, typed, private VM | 89.6 (86.6–118.0) | 87.9 (87.0–91.0) | 0 |
| Same, shared Script, executor from the pool | 110.8 (106.8–118.1) | 108.8 (103.2–111.2) | 0 |
| 46-byte string in, same string out | 100.4 (97.2–106.2) | 102.8 (100.7–107.4) | 0 |
| Rule reading three fields of a `*request` that is reused | 176.8 (170.0–180.9) | 153.8 (147.4–177.3) | 0 |
| Same rule when `req.method` does not share a pointer with the constant | 173.8 (168.0–277.1) | 153.5 (145.8–215.8) | 0 |
| Same rule through `Expr` and `Rule.Eval`, struct built per call | 179.6 (171.8–258.6) | 160.5 (155.7–223.6) | 0 |
| Same rule through `Func1`, struct built per call and passed by value | 197.0 (188.9–209.8) | 175.9 (170.4–231.7) | 0 |
| Same rule through `Func1`, pointer to a struct built per call | 206.8 (201.8–224.9) | 189.3 (183.7–219.5) | 1 |
| Function that calls one Go host function made with `Fn1` | 205.9 (202.2–218.3) | 201.8 (195.0–232.4) | 0 |
| Same, host function written as a `GoFunc` | 194.6 (187.0–235.1) | 191.2 (182.7–252.1) | 0 |

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
