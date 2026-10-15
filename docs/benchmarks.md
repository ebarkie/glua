# Benchmarks

The 21 scripts of [`bench/lua/suite`](../bench/lua/suite) timed in C Lua and in four Lua
implementations for Go: glua, [GopherLua](https://github.com/yuin/gopher-lua),
[golua](https://github.com/arnodel/golua) and [wangshu](https://github.com/Liam0205/wangshu).
The scripts are small programs of the kinds an embedded interpreter runs: recursion, loops
over numbers and tables, string building and matching, sorting, classes, closures, varargs,
coroutines and rule evaluation. Each returns a checksum, and an engine's time counts only if
its checksum matches C Lua's.

The engines implement different versions of Lua, which decides what they can run:

| Engine | Version tested | Language | Notes |
| --- | --- | --- | --- |
| C Lua | 5.4.9 | Lua 5.4 | Built from source with gcc at `-O2`; the reference |
| glua | this repository | Lua 5.4 | The bounds-checked interpreter loop, the current default |
| GopherLua | v1.1.2 | Lua 5.1 | Default options, except that the data stack may grow (`RegistryMaxSize`), which two scripts need |
| golua | v0.4.0 | Lua 5.5 | Default runtime with the standard library |
| wangshu | v0.2.0 | Lua 5.1 | Default options |

## Results

Machine: Intel(R) Core(TM) i7-7600U CPU @ 2.80GHz, 4 CPUs. go1.27.2. Lua 5.4.9  Copyright (C) 1994-2026 Lua.org, PUC-Rio.
3 interleaved rounds of 3 runs each; medians of the round means, in ms, and the ratio to C Lua. 15 October 2026.

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

## How the numbers are taken

`go run -C bench/engines . -rounds 3` runs each script once per round in every engine, in turn,
for three rounds. C Lua times itself with `os.clock` around calls to the script's work
function, after one warm-up call. The Go engines load the script once, make one warm-up call
that is also checked against C Lua's result, and are timed over three further calls. The
table gives the median of the round means. The harness is a module of its own, so glua
depends on none of the other engines.

The machine is a laptop with a `powersave` CPU governor, not a quiet benchmark machine, so
individual numbers move by 10 to 20 percent between runs; the ratios are more stable than the
times. All engines ran in one process each round, on Go 1.27.2.

These are interpreter benchmarks. They do not measure the cost of calls between Go and Lua,
which dominates many embedded uses; for glua, [`bench/m2-results.md`](../bench/m2-results.md)
measures that boundary. Some engines offer modes these runs do not use: wangshu has optional
compilation tiers, golua a resource-limited mode, and glua a faster unchecked loop that becomes
its default after a week of continuous fuzzing. Each engine is run as its documentation shows
for embedding, with default settings.
