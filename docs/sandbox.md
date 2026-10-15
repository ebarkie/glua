# Sandbox and limits

glua is built to run scripts the host does not fully trust: rules written by customers,
policies, handlers. This document says what the sandbox promises, what the host controls, and
what remains the host's job.

## What a script cannot do

Whatever a script does, it cannot:

- reach files, processes, the network, the environment or the clock, unless the host grants
  it with a library or a host function;
- run without bound, or allocate without bound;
- crash the process: exhaust the Go stack, run it out of memory with one allocation, or hit a
  fault that `recover` cannot stop;
- see data from another call, because a shared script's state is frozen;
- learn heap addresses, because `tostring` and `%p` show a keyed hash.

The sandbox does not cover what host functions do, data races the host creates on Go values it
passes in, or timing side channels.

## Two profiles

| | Default | `lua.Hardened()` |
| --- | --- | --- |
| Libraries | `LibSafe`; more on request | `LibSafe` only: `New` fails if `LibLoad`, `LibCoroutine`, `LibOS` or `LibIO` is added |
| Go types | Bound when a binding reaches them | Only types registered with `lua.Type[T]` |
| Slices and maps in bound structs | Live views of the Go value | Copies |
| Limits | Defaults; `NoLimit` allowed | Defaults; `NoLimit` refused |

`LibSafe` is the base library, `string`, `table`, `math` and `utf8`. The `io` library gives a
script the file system, and is meant for trusted scripts only.

## Limits

Limits apply to one entry from Go: a call through a function from `FuncN`, `Call` or
`Rule.Eval`, or the top level that `Load` and `NewVM` run. They cover everything the entry
runs, nested calls included. A zero field takes the default, so a `Runtime` made with no
options is bounded; `lua.NoLimit` switches a limit off.

| Limit | Default | What it counts | When it runs out |
| --- | --- | --- | --- |
| `Steps` | 10 million | Backward jumps, calls, and library work in proportion to its size: a step per 64 bytes a library function produces, per comparison in `table.sort`, per 8 values copied for varargs | `ErrSteps`, terminal |
| `AllocBytes` | 64 MiB | Bytes allocated for the script: tables and their growth, closures, strings, coroutines, stack growth, host functions' results, `load`'s source. It counts what is allocated, not what is live | `ErrAlloc`, terminal |
| `CallDepth` | 10,000 | Lua frames | `stack overflow`, which `pcall` can catch |
| `HostDepth` | 100 | Nesting of Go and Lua: host functions that call back into Lua, library callbacks such as sort's comparator, scripts called from host functions | `ErrHostDepth`, terminal |
| `InitSteps`, `InitAllocBytes` | `Steps`, `AllocBytes` | The same, for the top level in `Load` and `NewVM` | As above |
| context | none | Checked when the entry starts, and again every 1,024 steps | The context's cause, terminal |

`Script.WithLimits` gives the same code and state other limits, for example per tenant. The
`Observe` option reports what each entry used.

### Terminal errors

Running out of steps or bytes, nesting too deep, and a cancelled context end the entry:

- `pcall`, `xpcall`, message handlers and `coroutine.resume` never see them.
- After a host function returns, the error stands, whatever the function did with it.
- `__close` handlers written in Lua still run, sharing a grace budget of 1,024 steps and 4 KB.
- The entry returns an `*lua.Error` that unwraps to `ErrSteps`, `ErrAlloc`, `ErrHostDepth` or
  the context's cause, with the script's position and traceback.

### Budgets across scripts

A host function that calls another script with `c.Context()` shares the entry's budgets: the
nested call can use at most what the entry has left, and what it uses is charged back. A
nested call made with an unrelated context gets budgets of its own.

## Fixed caps

These hold even with `NoLimit`, because what they prevent would end the process.

| Cap | Value | What happens |
| --- | --- | --- |
| Syntax nesting in the parser | 200 levels | `chunk has too many syntax levels` |
| Depth of the syntax tree, chains such as `a.b.c` and `1+2+3` included | 2,000 | `chunk has too many syntax levels` |
| Pattern matching recursion | 200 levels; 32 captures | `pattern too complex`, `too many captures` |
| `__index`, `__newindex` and `__call` chains | 2,000 | `'__index' chain too long; possibly a loop` |
| Register stack | 1 million slots | `stack overflow` |
| Lua frames with `CallDepth: NoLimit` | about 1 million | `stack overflow` |
| Nesting of Go and Lua with `HostDepth: NoLimit` | 10,000 | `ErrHostDepth` |
| Any single allocation | 1 GiB, checked before the allocation | `ErrAlloc` |
| `string.format` width and precision | two digits, as in C Lua | `invalid conversion` |
| Go–Lua conversion | 64 levels deep, so cycles end too | A conversion error |

## Panics

A panic inside the VM itself, which the bounds-checked interpreter loop turns any VM bug into,
is recovered at the entry and returned as a `*lua.PanicError`. A panic in a host function is
the host's: it passes through to the caller, unless the `RecoverPanics` option turns it into a
`*lua.PanicError` too. Either way the executor involved is discarded. `runtime.Goexit` passes
through.

## The interpreter loop

glua has two interpreter loops generated from one source. The checked loop indexes everything
through bounds-checked slices, so even a VM bug is a recoverable panic. The fast loop reads
through raw pointers that the bytecode verifier vouches for. The checked loop is the default
until a week of continuous fuzzing of both loops has run clean; after that the fast loop
becomes the default, and `Hardened` keeps the checked one.

## What remains the host's job

- **Host functions must be safe for concurrent use.** A shared script calls them from every
  goroutine that calls the script.
- **Go values passed in are shared, not copied**, except structs passed by value and, under
  `Hardened`, slices and maps. No goroutine may write a value while a script can read it. Such
  a race can tear a string header or hit `concurrent map read and map write`, which no
  `recover` stops.
- **Go values and functions given to `Global` and `Module`** are reachable from every script
  of the Runtime.
- **A script's output** goes to the `Output` writer, serialised by the Runtime, or to the
  `OutputFunc` function with the entry's context, unserialised. Errors carry script text and
  values: decide what reaches your logs and users.
