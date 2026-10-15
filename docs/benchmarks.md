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
| glua (checked loop) | 1.55x |
| wangshu | 4.31x |
| GopherLua | 5.42x |
| golua | 6.09x |

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
| glua | v0.1.1 | Lua 5.4 | The bounds-checked interpreter loop, the current default |
| GopherLua | v1.1.2 | Lua 5.1 | Default options, except that the data stack may grow (`RegistryMaxSize`), which two scripts need |
| golua | v0.4.0 | Lua 5.5 | Default runtime with the standard library |
| wangshu | v0.2.0 | Lua 5.1 | Default options |

Times in ms, medians of three rounds, with the ratio to C Lua:

| Script | C Lua 5.4.9 | glua (checked loop) | GopherLua v1.1.2 | golua v0.4.0 | wangshu v0.2.0 |
| --- | --- | --- | --- | --- | --- |
| binarytrees | 33.6 | 36.6 (1.09x) | – | 130.9 (3.89x) | – |
| closures | 41.3 | 67.9 (1.64x) | 220.3 (5.34x) | 351.9 (8.53x) | 282.7 (6.85x) |
| concat | 59.6 | 40.6 (0.68x) | 119.1 (2.00x) | 184.2 (3.09x) | 181.8 (3.05x) |
| coroutine | 15.1 | 27.4 (1.81x) | 46.4 (3.07x) | 233.4 (15.46x) | 74.3 (4.92x) |
| fannkuch | 39.0 | 65.0 (1.67x) | 229.3 (5.88x) | 268.4 (6.88x) | 283.9 (7.28x) |
| fib | 17.5 | 30.9 (1.77x) | 99.1 (5.67x) | 175.2 (10.02x) | 94.8 (5.42x) |
| fieldread | 12.5 | 18.3 (1.46x) | 89.9 (7.18x) | 112.7 (9.00x) | 51.1 (4.08x) |
| floatloop | 11.7 | 19.8 (1.69x) | 82.9 (7.08x) | 93.5 (7.99x) | 34.8 (2.97x) |
| format | 77.4 | 87.4 (1.13x) | 296.6 (3.83x) | 148.2 (1.91x) | 112.0 (1.45x) |
| gsub | 20.2 | 37.8 (1.87x) | 317.9 (15.74x) | 85.8 (4.25x) | 62.8 (3.11x) |
| json | 35.2 | 42.9 (1.22x) | – | 165.7 (4.71x) | – |
| mathcalls | 74.7 | 95.3 (1.28x) | – | 503.6 (6.74x) | – |
| nbody | 49.1 | 66.7 (1.36x) | 340.1 (6.93x) | 312.3 (6.36x) | 300.7 (6.12x) |
| oop | 54.0 | 99.9 (1.85x) | 270.0 (5.00x) | 259.2 (4.80x) | 264.4 (4.89x) |
| pairs | 19.7 | 29.3 (1.49x) | 101.0 (5.12x) | 133.5 (6.77x) | 74.4 (3.77x) |
| parse | 37.5 | 66.4 (1.77x) | 241.0 (6.43x) | 155.3 (4.14x) | 120.0 (3.20x) |
| policy | 48.1 | 97.4 (2.03x) | 176.3 (3.67x) | 269.8 (5.61x) | 218.7 (4.55x) |
| sieve | 55.7 | 113.0 (2.03x) | 450.2 (8.09x) | 536.5 (9.64x) | 649.2 (11.66x) |
| sort | 39.5 | 71.0 (1.80x) | – | 195.6 (4.96x) | – |
| spectralnorm | 27.1 | 41.6 (1.54x) | 133.5 (4.93x) | 195.5 (7.23x) | 117.2 (4.33x) |
| vararg | 30.9 | 73.8 (2.39x) | – | 270.5 (8.77x) | 102.0 (3.31x) |
| Geometric mean of the ratios, over the scripts the engine ran | | 1.55x (21 scripts) | 5.42x (16 scripts) | 6.09x (21 scripts) | 4.31x (17 scripts) |

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
| binarytrees | 32.87 (31.22–32.93) | 38.58 (34.30–42.26) | 1.17x | 29.97 (28.18–35.98) | 0.91x | 87.14 (86.11–104.48) | 2.65x |
| closures | 40.91 (38.73–54.41) | 57.56 (55.47–57.88) | 1.41x | 46.74 (45.20–47.00) | 1.14x | 137.67 (136.96–182.81) | 3.37x |
| concat | 59.07 (56.42–61.74) | 36.14 (34.59–39.18) | 0.61x | 36.24 (33.51–41.13) | 0.61x | 106.02 (101.83–107.99) | 1.79x |
| coroutine | 15.79 (15.73–16.95) | 28.45 (27.63–28.87) | 1.80x | 25.58 (25.32–26.47) | 1.62x | 114.40 (106.61–139.16) | 7.25x |
| fannkuch | 37.67 (35.22–42.56) | 61.86 (61.12–62.02) | 1.64x | 51.36 (51.01–53.46) | 1.36x | 108.37 (82.16–111.71) | 2.88x |
| fib | 18.97 (18.25–20.38) | 26.15 (25.67–26.17) | 1.38x | 19.22 (19.21–20.20) | 1.01x | 58.76 (58.44–73.12) | 3.10x |
| fieldread | 13.37 (12.35–13.65) | 19.80 (17.94–20.96) | 1.48x | 14.46 (14.26–15.77) | 1.08x | 21.34 (19.71–24.82) | 1.60x |
| floatloop | 12.10 (11.49–12.18) | 19.44 (19.01–19.68) | 1.61x | 13.91 (13.37–14.32) | 1.15x | 20.30 (18.94–20.41) | 1.68x |
| format | 71.39 (70.83–84.64) | 88.11 (83.05–91.27) | 1.23x | 79.66 (79.26–81.36) | 1.12x | 133.79 (131.95–157.02) | 1.87x |
| gsub | 19.98 (19.51–20.10) | 35.06 (34.40–35.37) | 1.75x | 35.23 (31.26–38.60) | 1.76x | 69.08 (66.73–72.78) | 3.46x |
| json | 33.32 (32.85–33.61) | 42.09 (38.49–43.01) | 1.26x | 38.92 (37.60–40.32) | 1.17x | 101.90 (98.43–105.47) | 3.06x |
| mathcalls | 73.39 (66.19–73.99) | 90.79 (89.63–92.71) | 1.24x | 82.29 (81.30–84.32) | 1.12x | 235.48 (209.24–239.30) | 3.21x |
| nbody | 48.17 (48.00–48.92) | 65.48 (65.21–68.97) | 1.36x | 50.55 (46.85–52.00) | 1.05x | 111.06 (92.07–127.32) | 2.31x |
| oop | 55.71 (54.90–102.35) | 96.12 (93.37–112.04) | 1.73x | 90.87 (83.51–107.66) | 1.63x | 256.56 (211.69–266.99) | 4.61x |
| pairs | 19.51 (18.15–20.75) | 29.72 (28.47–31.03) | 1.52x | 29.23 (27.32–31.72) | 1.50x | 114.40 (93.55–158.99) | 5.86x |
| parse | 41.63 (35.47–52.76) | 96.45 (65.76–99.23) | 2.32x | 64.90 (60.71–88.62) | 1.56x | 150.92 (135.81–215.61) | 3.62x |
| policy | 41.65 (40.94–51.67) | 97.14 (92.40–104.20) | 2.33x | 94.41 (91.67–104.03) | 2.27x | 237.33 (228.94–249.18) | 5.70x |
| sieve | 54.12 (52.23–82.05) | 107.37 (99.64–139.23) | 1.98x | 100.72 (84.73–109.88) | 1.86x | 173.85 (162.34–179.33) | 3.21x |
| sort | 42.31 (40.09–44.44) | 66.94 (65.29–68.45) | 1.58x | 63.04 (60.88–102.65) | 1.49x | 163.96 (162.41–172.49) | 3.88x |
| spectralnorm | 26.37 (26.13–27.89) | 42.77 (39.61–44.45) | 1.62x | 28.83 (28.25–29.83) | 1.09x | 74.24 (70.80–80.64) | 2.82x |
| vararg | 30.24 (29.20–31.09) | 71.16 (66.84–73.75) | 2.35x | 67.98 (62.01–97.04) | 2.25x | 245.01 (239.54–246.47) | 8.10x |
| Geometric mean | | | 1.53x | | 1.31x | | 3.27x |
| Worst | | | 2.35x | | 2.27x | | 8.10x |

## Calls between Go and Lua

The cost of one call from Go into a loaded script, through the public API, from a typed
function to the result decoded back into Go. "Private VM" is a `VM` with its own executor;
the other rows call a shared `Script`, which takes an executor from a pool. None of the rows
allocate inside glua; the one allocation is the caller's own `&request{...}`.

Nanoseconds per call, medians of eight rounds, with the range:

| Call from Go | ns, checked loop | ns, fast loop | Allocations |
| --- | --- | --- | --- |
| `function(x) return x + 1 end`, typed, private VM | 94.0 (90.8–103.9) | 95.9 (88.0–109.0) | 0 |
| Same, shared Script, executor from the pool | 116.8 (108.9–135.7) | 116.8 (105.8–158.9) | 0 |
| 46-byte string in, same string out | 109.6 (101.6–131.4) | 110.2 (103.0–127.4) | 0 |
| Rule reading three fields of a `*request` that is reused | 188.6 (170.6–362.4) | 162.1 (153.8–223.0) | 0 |
| Same rule when `req.method` does not share a pointer with the constant | 187.1 (170.2–297.1) | 171.2 (154.2–262.0) | 0 |
| Same rule through `Expr` and `Rule.Eval`, struct built per call | 187.9 (169.4–277.0) | 173.0 (161.3–510.2) | 0 |
| Same rule through `Func1`, struct built per call and passed by value | 212.8 (182.2–489.7) | 201.8 (183.4–466.4) | 0 |
| Same rule through `Func1`, pointer to a struct built per call | 233.1 (210.3–328.9) | 211.8 (196.6–241.7) | 1 |
| Function that calls one Go host function made with `Fn1` | 218.5 (204.6–267.4) | 213.9 (205.0–257.3) | 0 |
| Same, host function written as a `GoFunc` | 210.4 (190.3–250.8) | 198.1 (188.5–222.2) | 0 |

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
