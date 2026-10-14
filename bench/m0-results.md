# Milestone 0 results

Measured on 14 October 2026 at commit `df514c1` plus the reset changes committed with this file.
The exit criterion of milestone 0: the spike's ratios hold within 1.3x of C Lua 5.4 on Go 1.27,
or the loop design is revisited before any real code exists.

## Setup

- Intel Core i7-7600U (Kaby Lake laptop, 2 cores / 4 threads), `powersave` governor, not a
  quiet machine. Ranges between rounds are wide, up to 30% on some rows.
- Go 1.27.2, linux/amd64, `GOAMD64=v1`.
- C Lua 5.4.9 built from source by `bench/build-clua.sh`: gcc 16.2.1, `-O2 -std=gnu99`, target
  `linux`. For context, also the Fedora package of C Lua 5.4.8, which is built with `-O2 -flto`.
- `go run ./bench/compare -rounds 12 -runs 5`: each round runs every workload in C Lua and then
  in Go, back to back. The figures are medians of the round means, with the range in brackets.

## VM speed

| Workload, ms | C Lua 5.4.9 | glua | glua / C Lua | glua, GC marking | GC marking / idle |
| --- | --- | --- | --- | --- | --- |
| Float loop, 1M iterations of `s = s + i * 1.5` | 12.44 (11.10–15.08) | 16.56 (13.39–21.90) | 1.33x | 19.04 (14.56–34.11) | 1.15x |
| `fib(27)`, recursive | 19.60 (17.86–22.89) | 21.20 (18.16–27.90) | 1.08x | 55.35 (47.43–80.43) | 2.61x |
| Field read, 1M iterations of `s = s + t.x` | 13.43 (12.18–17.58) | 15.15 (12.86–18.60) | 1.13x | 15.98 (14.26–22.96) | 1.05x |
| Geometric mean against C Lua | | | 1.18x | 1.73x | |

Against the LTO build of C Lua 5.4.8 (8 rounds):

| Workload | glua / C Lua 5.4.8 |
| --- | --- |
| Float loop | 1.50x |
| `fib(27)` | 1.05x |
| Field read | 1.38x |
| Geometric mean | 1.29x |

## Boundary

| Call from Go | ns | Allocations |
| --- | --- | --- |
| `function(x) return x + 1 end`, typed, private executor | 58.2 (55.1–81.6) | 0 |
| Same, executor borrowed from a `sync.Pool` | 83.9 (80.3–132.7) | 0 |
| 46-byte string in, same string out | 58.4 (54.3–97.5) | 0 |
| Rule reading three fields of a `*request` that is reused | 120.6 (107.1–130.0) | 0 |
| Same rule when `req.method` does not share a pointer with the constant | 116.3 (109.9–141.8) | 0 |
| Same rule through `Rule.Eval`, struct built per call | 131.7 (121.5–150.7) | 0 |
| Same rule through a typed func, struct built per call and passed by value | 133.8 (125.4–156.2) | 0 |
| Same rule through a typed func, pointer to a struct built per call | 150.2 (142.7–212.9) | 1 |
| Function that calls one Go host function | 124.0 (116.2–168.9) | 0 |

For scale: an empty cgo call takes 44 ns on this machine with Go 1.27.2. In quieter single runs
the private call measured 53–54 ns and the pooled call 70–72 ns.

## Assessment

- **Interpreter loop: holds, narrowly.** All three workloads are within 1.33x of C Lua 5.4.9 at
  `-O2`, with a geometric mean of 1.18x. Calls are at parity. Against an LTO build of C Lua the
  float loop and the field read are at 1.4x to 1.5x.
- **GC marking behaves as the design predicted.** Arithmetic and field loops lose 5% to 15%.
  Call-heavy code is 2.6x slower, from the closure pointer written into the function slot on
  every call.
- **The boundary does not hold.** The design's spike entered Lua in 33 ns, between half and two
  thirds of a cgo call. Here a trivial call costs 54–58 ns, more than a cgo call. The library still
  allocates nothing per call, and `Rule.Eval` keeps a per-call struct on the caller's stack. The
  milestone 2 targets of 50 ns private and 75 ns pooled are met by the pooled call only.

## Findings against the design

1. **Listing all 256 opcode values prevents the jump table.** The compiler merges the unused
   values into one run, and the switch then fails the jump-table density test (cases × 4 ≥ width)
   and becomes a binary search. A `default` clause gives a jump table behind one range check.
2. **Every helper used in `fast` must be checked for inlining.** One that was not (`numPair`) put
   a call in the loop, and with it spills on every instruction. The machine-code gate now catches
   this.
3. **A constant can occupy a register across the loop.** Masking type IDs with the 48-bit
   `typeIDMask` kept the constant live in a register, and every dispatch went through a trampoline
   that reloaded it. Type IDs are now read as 32 bits.
4. **`pc` moves between two registers on amd64.** The head loads through one register and leaves
   `pc + 4` in another, so most opcodes return through a shared `MOV; JMP` block: two jumps per
   dispatch instead of one. Rewriting the head did not change the compiler's choice.
5. **`GETFIELD` spills on amd64.** Its table path keeps more values live than amd64 has registers,
   and spills `pc`, `k` and the table (9 stack accesses in the gate's baseline). arm64 has no spills
   in any hot opcode.
6. **Copies need the conditional tag write too.** Without it, `MOVE`, `LOADK`, `GETUPVAL` and
   `GETFIELD` take the write barrier on every store while the GC marks, which made the field-read
   loop 3x slower during marking. With `setValue` it is 1.05x.
7. **Mixed integer and float arithmetic needs its own fast path.** `i * 1.5` multiplies an integer
   by a float. Testing float-float before the general mixed case took the float loop from 1.7x to
   about 1.4x. Specialised opcodes for constant operands of known type would go further.

## Recommendation

Keep the loop design and go on to milestone 1. The interpreter is within the 1.3x bound against
C Lua built from source and at parity on calls, and the gaps that remain have causes identified
above that belong to milestone 3. The boundary is the open risk: a trivial call costs about twice
what the design measured. Profiling shows the time spread over entry bookkeeping (the entry guard,
budget setup, frame push and the run/drive/fast layering) rather than one hot spot. Milestone 2
should treat the 50 ns target as a design input, for example with a dedicated entry path for a
single Lua frame, instead of expecting it from the general machinery.
