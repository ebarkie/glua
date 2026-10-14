# Milestone 1 results

Measured on 14 October 2026 at the commit that adds this file. The exit criterion of milestone 1:
the manifest's files pass, and the float loop, `fib` and the field read, compiled from source,
run at 1.3x of C Lua or better.

## Test suite

`go test ./...` runs the Lua 5.4.9 test suite from `testdata/lua-tests` under both loops. Of its
33 files, 11 run unmodified and 12 run patched; the 10 excluded are the driver, two helper
modules, and the files that test the C API, the debug library, io and os, the garbage
collector, the standalone interpreter and `string.pack` (milestone 4). The `MANIFEST` gives the
reason for every patch and exclusion. All run files pass, and they pass again under the
`lua_stackstress` build tag.

## VM speed, compiled from source

Same machine and setup as milestone 0: Intel Core i7-7600U, `powersave` governor, not a quiet
machine; Go 1.27.2, linux/amd64; C Lua 5.4.9 built by `bench/build-clua.sh` with gcc 16.2.1 at
`-O2`. `go run ./bench/compare -source -rounds 10 -runs 5`: each round runs every workload in
C Lua, then on glua's checked loop, then on its fast loop. Medians of the round means, with
the range in brackets.

| Workload from source, ms | C Lua 5.4.9 | glua, checked loop | / C Lua | glua, fast loop | / C Lua |
| --- | --- | --- | --- | --- | --- |
| Float loop, 1M iterations of `s = s + i * 1.5` | 12.58 (11.16–16.39) | 18.25 (17.69–25.83) | 1.45x | 15.03 (13.26–18.28) | 1.20x |
| `fib(27)`, recursive | 19.30 (16.69–21.76) | 25.95 (22.48–31.02) | 1.34x | 19.85 (17.73–20.88) | 1.03x |
| Field read, 1M iterations of `s = s + t.x` | 13.24 (12.08–14.56) | 19.21 (17.99–21.47) | 1.45x | 15.19 (13.82–20.85) | 1.15x |
| Geometric mean against C Lua | | | 1.41x | | 1.12x |

The compiler emits the same code for these programs as the hand-assembled ones of milestone 0,
and the fast loop runs them at the same speed (milestone 0: 1.33x, 1.08x and 1.13x).

## Assessment

- **Fast loop: holds.** All three workloads compiled from source are within 1.2x of C Lua,
  with a geometric mean of 1.12x.
- **Checked loop, the default: misses.** It is 1.34x to 1.45x of C Lua, 19% to 30% slower than
  the fast loop, against the design's 0% to 14% for bounds checks. Closing that gap is
  milestone 3's work; until then, the fast loop meets the criterion and the default does not.
