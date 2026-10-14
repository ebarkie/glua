# Milestone 2 results

Measured on 14 October 2026 at the commit that adds this file. The exit criterion of milestone 2:
the six examples run on Go 1.27 with checked `// Output:` comments, and the boundary suite meets
the 50 ns and 75 ns targets with no allocation inside the library.

## Examples

`example_test.go` runs the design's six examples (one-liner, rule, handlers, host functions,
config and VM, errors) with their output checked. They pass. One deviation: the host-functions
example logs through `fmt` rather than `slog`, so that its output can be checked.

## Boundary, through the public API

Same machine as milestones 0 and 1: Intel Core i7-7600U, `powersave` governor, not a quiet
machine; Go 1.27.2, linux/amd64. `go run ./bench/compare -boundary -rounds 8`: each round runs
every row on the checked loop, the default, and on the fast loop. Medians of the rounds, with
the range in brackets.

| Call from Go | ns, checked loop | ns, fast loop | Target | Allocations |
| --- | --- | --- | --- | --- |
| `function(x) return x + 1 end`, typed, private VM | 89.1 (84.4–97.0) | 87.7 (84.4–88.7) | 50 | 0 |
| Same, shared Script, executor from the pool | 111.1 (107.9–113.5) | 110.4 (108.7–123.5) | 75 | 0 |
| 46-byte string in, same string out | 105.3 (100.7–118.6) | 106.0 (102.4–108.9) |  | 0 |
| Rule reading three fields of a `*request` that is reused | 168.7 (160.8–249.1) | 156.1 (152.3–227.7) |  | 0 |
| Same rule when `req.method` does not share a pointer with the constant | 167.5 (163.7–239.3) | 158.8 (152.8–222.7) |  | 0 |
| Same rule through `Expr` and `Rule.Eval`, struct built per call | 170.9 (162.4–189.0) | 157.7 (151.3–168.2) |  | 0 |
| Same rule through `Func1`, struct built per call and passed by value | 187.1 (179.8–190.4) | 176.7 (171.4–184.8) |  | 0 |
| Same rule through `Func1`, pointer to a struct built per call | 204.4 (201.4–225.5) | 194.6 (190.9–241.2) |  | 1, by the caller |
| Function that calls one Go host function made with `Fn1` | 203.1 (199.2–250.7) | 202.8 (197.2–357.9) |  | 0 |
| Same, host function written as a `GoFunc` | 191.7 (184.9–367.3) | 188.9 (183.9–268.1) |  | 0 |

For scale, milestone 0's spike, rerun now at its own commit on this machine: 60 ns private, 78 ns
pooled, 126 ns for `Rule.Eval` and 120 ns for the host call.

## Assessment

- **Examples: pass.**
- **No allocation inside the library: holds.** The one allocation is the caller's `&request{...}`,
  as the design predicts for a typed func.
- **Latency targets: miss.** A private call takes 88 ns against 50, a pooled one 111 ns against 75.
  The design's targets came from a faster machine; on this one the spike already took 60 and 78 ns.
  The public API costs 28 to 33 ns more than the spike. That cost is spread over the entry: the
  budget and plan bookkeeping that limits and struct binding need, the deferred guard that keeps a
  VM bug from crashing the process, the Script and VM layers, and the frame push and loop entry.
  No single item is larger than about 5 ns.
- **Host calls are the widest gap:** about 90 ns per round trip from Lua to Go and back, against the
  design's 17 ns. Each one leaves the loop, goes through the driver and the call record, and enters
  the loop again. Milestone 3's `FASTCALL` and frame tuning are where this belongs.
- **The checked loop costs little here:** 0 to 8% at the boundary, against 19 to 30% inside loops.

Found and fixed while measuring:

- Every forward jump the compiler emits is a `JMPX`, which the hot loop left to the driver. Each
  `if`, `and` and `or` left the loop and paid its setup again. This dates from milestone 1, whose
  benchmarks happen to contain no forward jump on their hot path.
- Library and host global names did not share pointers with the compiler's constants, so every
  `string.format` or host-function lookup took the slow path. Keys that Go code puts in tables
  are now interned.
- An expression compiled as a vararg main function paid the vararg frame setup on every call.
