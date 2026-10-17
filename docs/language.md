# The language: glua and Lua 5.4

glua implements Lua 5.4: the syntax, integers and floats, bitwise operators, `<const>` and
`<close>` variables, `goto`, `_ENV`, metatables and coroutines. It is checked against the test
suite of the Lua 5.4.9 release, which is vendored in `testdata/lua-tests`, and against C Lua
5.4.9 itself by differential fuzzing. Error messages follow C Lua's wording, positions
included.

This document lists what is different. Most of it follows from two decisions: scripts are
embedded in a Go program that may run many of them at once, and the host, not the script,
decides what a script can reach.

## The test suite

Of the suite's 33 files, 13 run unmodified, 11 run with marked cuts, and 9 do not run. Each
cut in a patched file carries a `-- glua:` comment with its reason, and `MANIFEST` gives the
reason for every file.

| Status | Files |
| --- | --- |
| Run unmodified | big, bitwise, code, constructs, heavy, literals, math, pm, sort, tpack, utf8, vararg, verybig |
| Run with cuts | attrib, calls, closure, coroutine, cstack, errors, events, goto, locals, nextvar, strings |
| Not run | api and db (the C API and the debug library), files (io and os beyond glua's subset), gc and gengc (the collector), main (the standalone interpreter), all (the driver), and two helper modules |

## Loading and freezing

A script's top level runs once, when the host loads it, and everything it built is then
frozen: the tables reachable from its globals and from what the chunk returned, their
metatables, and the captured variables that closures can assign. Writing one afterwards
raises `attempt to modify a frozen table` or `attempt to assign to frozen upvalue`. Inside a
call, tables and closures that the call creates itself are ordinary and mutable.

So a script loaded for shared use cannot keep state from one call to the next:

```lua
local count = 0
function handle(req)
  count = count + 1 -- attempt to assign to frozen upvalue
end
```

The host can give such a script a private, unfrozen instance instead (`Script.NewVM`), or keep
the state on the Go side.

Loading fails when the frozen state would reach something that cannot be shared: a coroutine,
a library function with hidden state (a `gmatch` iterator, a `coroutine.wrap` function, a
`lines` iterator), an open file, or a Go value a script can change: one whose type has writable
fields or a `__newindex` metamethod, or that the host marked `Mutable`.

## Reserved names

Names defined by the libraries and by the host (`print`, `string`, a host global `upper`, a
host module `kv`) cannot be assigned, by any route: `string = nil`, `_G.print = f` and
`rawset(_G, "kv", 1)` all raise `attempt to assign to reserved name`. A local of the same
name is fine. `_G` is reserved too, and so are `package`, `require` and `load` when their
libraries are open. A name the host leaves out with `Without` is not reserved.

## Libraries

The host chooses the libraries. The default set is the base library, `string`, `table`,
`math` and `utf8`. `Without` leaves out single names, such as `collectgarbage` or
`string.rep`, and the host may define its own global under a global left out.

| Library | In glua |
| --- | --- |
| base | All of it except `dofile` and `loadfile`. `print` and `warn` write to the host's output, which discards by default. `collectgarbage` does nothing: `"count"` reports the kilobytes the current call has allocated, across renewals, and every other option returns its neutral value |
| `require`, `package` | Only modules the host defines, in Lua or in Go, and those its loader function provides. No search path, no C modules, no `package.path`, `cpath`, `searchers` or `preload`. A shared script can require a module for the first time only while it loads |
| `load` | Off by default. Text chunks only: binary chunks are refused. The environment argument must be a table or nil |
| `string` | All of it, `pack`, `unpack` and `packsize` included, except `string.dump` |
| `table`, `math`, `utf8` | All of it. `math.random` starts from a fresh seed on each call into the script, or from the host's fixed seed |
| `coroutine` | Off by default; all of it when on |
| `os` | Off by default. Only `clock`, `time`, `date` and `difftime`, driven by a clock the host can replace. `os.clock` is the time since the host set up the script environment, not processor time, which one script in a shared process cannot have to itself |
| `io` | Off by default, for trusted scripts only. `open`, `lines`, `close`, `read`, `write`, `type`, and `stdin`, `stdout` and `stderr`; `io.write` and `io.stdout` go to the host's output. No `popen`, `tmpfile`, `input` or `output` |
| `debug` | Only `debug.traceback`, for `xpcall(f, debug.traceback)` |

## Not supported

- **Weak tables.** `setmetatable` raises an error for a metatable with `__mode`.
- **Finalizers.** `__gc` raises an error, unless `__close` is also present, a common fallback
  idiom, in which case it is ignored. Memory is the Go collector's business.
- **Binary chunks** and `string.dump`.
- **Metatables for numbers, booleans and nil.** Strings share the string library's metatable,
  as in Lua; there is no `debug.setmetatable`.
- **Light userdata** and user values. Userdata are Go values the host passes in.

## Behaviour that differs

- **Nesting between Go and Lua is bounded and terminal.** C Lua raises a catchable `C stack
  overflow` when Lua and C call each other too deeply. In glua the host's `HostDepth` limit
  ends the whole call, and `pcall` cannot catch it.
- **Terminal errors end the call.** Running out of the step or memory budget, a cancelled
  context, or an error a host function made with `Terminate`, ends the whole call into the
  script: `pcall`, `xpcall` and `coroutine.resume` do not see these errors. `__close` handlers
  still run, within a small grace budget.
- **Closers of an error.** `__close` handlers that run because of an error run on top of the
  failing frames, so a traceback from them shows those frames, and they cannot yield.
- **Errors in message handlers.** An error inside an `xpcall` message handler gives `error in
  error handling` at once; C Lua calls the handler again first.
- **Tracebacks** name functions but not metamethods.
- **Addresses.** `tostring` of a table or function, and `%p`, show a keyed hash of the address,
  never the address itself. Strings are not interned, so two equal strings may show different
  `%p` values.
- **Iteration order.** `pairs` order depends on a per-process hash seed. The host can fix the
  seed for tests and replay.
- **Captured variables.** A local that closures capture but nothing ever assigns after its
  declaration is copied into each closure; one that is assigned is shared through a cell. Lua
  cannot tell the difference without the debug library, which glua does not provide.
- **The limit on locals** (200 per function) is reported after parsing, so a syntax error
  later in the chunk is reported first.

## Go values in Lua

What the host passes in follows Lua's rules where it can:

- Go integers, floats, strings and bools are Lua numbers, strings and booleans.
  `time.Duration` is a number of seconds, and `time.Time` a number of Unix seconds.
- A pointer to a struct is a userdata. Lua reads its exported fields by their `snake_case`
  names (`TotalPrice` is `total_price`), or by the name a `lua:"name"` tag gives. A field is
  read-only unless tagged `lua:",rw"` or opened by the host. A field that does not exist is an
  error, `no field 'pth' on Request`, not nil.
- Nested structs, slices and maps inside a bound struct are live views: Lua sees the Go value
  itself, and writes to writable ones reach Go. Slices are 1-based from Lua. For untrusted
  scripts the host can make them copies instead.
- Methods and metamethods are whatever the host registers for the type.
- A struct passed to a rule or a shared function by value is a read-only view valid during
  the call; if the script returns it, the host receives a copy.
