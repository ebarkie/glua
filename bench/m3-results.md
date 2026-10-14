# Milestone 3 results

Measured on 14 October 2026 at the commit that adds this file. The exit criterion of milestone 3:
on a suite of about 20 small scripts with the GC idle, a geometric mean within 1.2x of C Lua 5.4
and no script worse than 1.5x. The same suite is reported while the GC is marking.

## The suite

`bench/lua/suite` holds 21 scripts: recursion, float and integer loops, field access, n-body,
spectral norm, binary trees, fannkuch, a sieve, classes, closures, sorting, string building,
formatting, gsub, parsing with gmatch, a JSON-like encoder, pairs, math calls, varargs,
coroutines and policy rules. Each runs unchanged in C Lua and in glua, and a test checks that
glua returns C Lua's result for each.

Same machine and setup as before: Intel Core i7-7600U, `powersave` governor, not a quiet
machine; Go 1.27.2, linux/amd64; C Lua 5.4.9 at `-O2`. `go run ./bench/compare -suite -rounds 3
-runs 3`: each round runs every script in C Lua, then on glua's checked loop, its fast loop, and
its checked loop while a background goroutine keeps the GC marking. Medians of the round means.

| Script, ms | C Lua 5.4.9 | glua, checked | / C Lua | glua, fast | / C Lua | checked, GC marking | / C Lua |
| --- | --- | --- | --- | --- | --- | --- | --- |
| binarytrees | 35.41 (34.28–66.52) | 35.01 (33.34–49.15) | 0.99x | 30.79 (30.06–32.10) | 0.87x | 104.97 (95.22–156.46) | 2.96x |
| closures | 40.53 (39.64–41.86) | 56.39 (55.60–58.37) | 1.39x | 50.32 (45.10–50.93) | 1.24x | 150.02 (146.46–157.22) | 3.70x |
| concat | 60.20 (58.49–65.73) | 46.87 (37.83–56.09) | 0.78x | 37.29 (34.17–39.24) | 0.62x | 108.29 (101.34–122.50) | 1.80x |
| coroutine | 15.86 (15.48–16.77) | 27.39 (26.93–29.82) | 1.73x | 26.67 (25.45–28.37) | 1.68x | 119.13 (109.16–124.46) | 7.51x |
| fannkuch | 39.13 (38.40–39.32) | 64.77 (63.33–66.65) | 1.66x | 53.54 (50.50–55.24) | 1.37x | 89.36 (86.63–98.37) | 2.28x |
| fib | 19.32 (18.41–19.51) | 27.11 (27.06–27.29) | 1.40x | 19.62 (18.13–20.53) | 1.02x | 62.58 (62.56–69.95) | 3.24x |
| fieldread | 14.25 (12.97–14.55) | 18.84 (18.20–22.27) | 1.32x | 14.51 (13.61–15.56) | 1.02x | 24.07 (23.02–25.98) | 1.69x |
| floatloop | 11.95 (10.64–13.25) | 21.37 (19.79–21.83) | 1.79x | 15.47 (14.66–16.59) | 1.30x | 20.90 (20.49–28.67) | 1.75x |
| format | 83.87 (78.18–88.94) | 82.05 (81.64–82.15) | 0.98x | 87.52 (85.98–90.69) | 1.04x | 146.57 (141.41–163.91) | 1.75x |
| gsub | 24.57 (19.83–24.80) | 38.28 (36.05–39.79) | 1.56x | 36.55 (35.00–37.08) | 1.49x | 77.75 (70.75–88.43) | 3.16x |
| json | 36.68 (36.40–38.04) | 41.01 (40.44–52.62) | 1.12x | 37.87 (37.51–40.41) | 1.03x | 110.73 (105.33–118.53) | 3.02x |
| mathcalls | 82.66 (71.90–100.73) | 107.18 (98.23–130.05) | 1.30x | 87.30 (87.29–93.75) | 1.06x | 239.63 (232.97–260.60) | 2.90x |
| nbody | 50.30 (48.63–51.56) | 65.68 (65.09–66.29) | 1.31x | 53.69 (51.66–55.41) | 1.07x | 102.00 (98.08–102.84) | 2.03x |
| oop | 56.15 (54.75–59.96) | 92.62 (90.88–103.19) | 1.65x | 86.96 (84.39–90.63) | 1.55x | 226.05 (223.69–240.84) | 4.03x |
| pairs | 20.34 (19.95–20.45) | 29.49 (28.12–30.27) | 1.45x | 29.16 (27.93–30.90) | 1.43x | 102.78 (101.04–168.73) | 5.05x |
| parse | 38.68 (35.81–45.63) | 65.10 (63.52–65.14) | 1.68x | 64.55 (62.82–66.38) | 1.67x | 144.06 (140.88–166.40) | 3.72x |
| policy | 43.54 (42.46–57.78) | 100.50 (96.87–140.23) | 2.31x | 89.44 (86.38–141.69) | 2.05x | 239.98 (223.98–360.26) | 5.51x |
| sieve | 60.39 (54.65–73.05) | 104.76 (102.88–142.84) | 1.73x | 103.69 (90.06–116.32) | 1.72x | 198.41 (189.50–278.42) | 3.29x |
| sort | 40.25 (38.82–41.08) | 73.42 (72.61–99.36) | 1.82x | 66.38 (64.81–72.75) | 1.65x | 188.91 (181.13–233.44) | 4.69x |
| spectralnorm | 27.64 (26.65–28.35) | 46.02 (43.14–46.67) | 1.66x | 32.19 (30.70–35.81) | 1.16x | 81.00 (76.42–100.06) | 2.93x |
| vararg | 34.35 (31.60–35.66) | 70.05 (68.49–74.99) | 2.04x | 66.35 (65.86–89.19) | 1.93x | 245.61 (227.86–310.80) | 7.15x |
| Geometric mean | | | 1.46x | | 1.28x | | 3.21x |
| Worst | | | 2.31x | | 2.05x | | 7.51x |


At the start of the milestone the same suite gave 1.96x on the checked loop, 1.83x on the fast
loop and 4.46x while the GC was marking, with a worst case of 9.2x (the sieve).

## What changed

Found by profiling the suite, in the order of their effect:

- **Tables sized as C Lua sizes them.** Integer keys moved to the array part only when appended
  at its end, so `t[2..n]` lived in the hash part. A full hash part now triggers C Lua's rehash,
  and the array part doubles when it fills. Sieve: 9.2x to 1.7x.
- **Hints learned on writes.** SETFIELD and SETGLOBAL sites never learned a hint, so every write
  to a field other than the first slot left the hot loop. n-body: 2.2x to 1.07x on the fast loop.
- **Library callbacks climbed the stack.** Each call from Go, such as sort's comparator, left
  `ex.top` above its results, so 5,000 comparisons grew the stack to 131,072 slots.
- **Integers compared inline.** EQKJMP sent every compare of two different integers to the
  driver.
- **FASTCALL** for `math.abs`, `floor`, `ceil`, `sqrt`, `max`, `min`, `type`, `string.len` and
  `string.byte`, also through locals bound once to them. The hot loop checks the function's
  built-in id; the driver computes the result without the Go call path.
- **The cached `__index` hop**, constant keys looked up with their compile-time hash, `next` and
  `ipairs` steps without a Go call, LEN and integer keys in the hash part inline, and fewer
  allocations and less bookkeeping in library calls.

## Assessment

- **GC idle: misses.** 1.28x on the fast loop and 1.46x on the checked loop, against 1.2x. Nine
  scripts are within 1.2x on the fast loop; the worst is policy at 2.05x.
- **Worst case: misses.** policy, vararg, sieve, parse, coroutine, sort and oop are above 1.5x on both
  loops.
- **The checked loop costs 14%** across the suite, and up to 40% in tight arithmetic loops
  (floatloop, fib).
- **GC marking: 3.2x.** Write barriers dominate: Lua's calling convention stores the function and
  then its result in one register, two pointer barriers per call, and every Value carries a
  pointer, scalars included.

What the remaining gaps are made of:

- **Calls into Go** leave the hot loop, go through the driver and a call record, and enter the
  loop again: about 60 to 90 ns, against tens of nanoseconds in C Lua. vararg (`select`),
  coroutine, oop (`setmetatable`) and parts of policy and parse are dominated by this.
- **Dynamic string keys** are hashed on every lookup, because strings carry no cached hash.
  C Lua interns them. policy is dominated by this.
- **Inline paths cost registers.** An inline `__index` hop, inline built-ins and inline string
  hashing each made the compiler spill registers in the dispatch head or in CALL, which slows
  every instruction, so they stay in the driver.
