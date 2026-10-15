# GopherLua and glua side by side

[GopherLua](https://github.com/yuin/gopher-lua) is a widely used Lua VM written in Go. This
document puts common tasks side by side: how they are done with GopherLua, and what the same
looks like with glua. The two libraries make different choices about the language version,
how state is held and shared, and how Go and Lua exchange values, so the code is often shaped
differently rather than translated one to one.

The GopherLua examples follow its README and API as of v1.1.2.

## Language version

GopherLua implements Lua 5.1, plus `goto` from 5.2. glua implements Lua 5.4. Code that moves
between them meets the usual differences between those versions:

| | Lua 5.1 (GopherLua) | Lua 5.4 (glua) |
| --- | --- | --- |
| Numbers | One type, float64 | Integers (int64) and floats, with `//` and `math.type` |
| Bitwise operators | None in the language | `&`, `\|`, `~`, `<<`, `>>`, unary `~` |
| Environments | `setfenv`, `getfenv` | `_ENV` |
| Variables | | `<const>` and `<close>` |
| Modules | `module()` | Tables returned by the chunk |
| `utf8` library | No | Yes |
| `string.pack` | No | Yes |
| `table.unpack` | `unpack` | `table.unpack` |

GopherLua also adds a `channel` type and library for Go channels, and `os.setenv`. glua has
neither.

## Holding state

GopherLua's `*LState` is a complete Lua state: a stack, a global table and everything the
scripts in it have built. It is not safe for concurrent use; its README recommends one LState
per goroutine, for example from a pool, with each LState set up by running the scripts it
needs.

```go
L := lua.NewState()
defer L.Close()
if err := L.DoFile("policy.lua"); err != nil {
	return err
}
```

glua separates configuration, loaded code and execution. A `Runtime` holds the libraries,
host functions and limits. `Runtime.Load` compiles a script, runs its top level once, and
freezes what it built, giving a `Script` that any number of goroutines can call at the same
time. Execution state comes from a pool inside the Runtime.

```go
rt := lua.MustNew()
script, err := rt.Load(ctx, "policy.lua", src)
```

Because a loaded Script is frozen, its globals and upvalues cannot change after loading. Where
a script must keep state between calls, `script.NewVM(ctx)` gives a private, mutable instance
that one goroutine at a time may use, which is closer to an LState.

| Task | GopherLua | glua |
| --- | --- | --- |
| Set up | `lua.NewState(opts)` | `lua.New(opts...)` |
| Run a chunk | `L.DoString(src)`, `L.DoFile(path)` | `rt.Load(ctx, name, src)`; `lua.Eval[R](ctx, src)` for one-off use |
| Compile once, run many times | `L.LoadString`, then call the `*LFunction` | `rt.Load`, then call the Script's functions |
| Release | `L.Close()` | Nothing to close |
| Concurrent use | One LState per goroutine | One Script for all goroutines; one VM per goroutine at a time |

## Calling Lua from Go

GopherLua calls through the Lua stack: push the function and arguments, call, then read and
pop the results.

```go
err := L.CallByParam(lua.P{
	Fn:      L.GetGlobal("double"),
	NRet:    1,
	Protect: true,
}, lua.LNumber(10))
ret := L.Get(-1)
L.Pop(1)
```

glua binds a Lua function to a typed Go function once, checking the types then, and calls it
like any Go function:

```go
double, err := script.Func1[int64, int64]("double")
n, err := double(ctx, 10)
```

The name can be a global, a dotted path such as `"handlers.allow"`, or `""` for a function the
chunk returned. For calls with Values, `script.Call(ctx, fn, args...)` returns the first
result and `script.CallN` all of them.

## Values

GopherLua's `LValue` is an interface implemented by `LNil`, `LBool`, `LNumber`, `LString`,
`*LTable`, `*LFunction`, `*LUserData`, `*LState` and `LChannel`. Code tests a value with a type
assertion or its `Type()`.

```go
if s, ok := lv.(lua.LString); ok {
	fmt.Println(string(s))
}
```

glua's `Value` is a 16-byte struct, passed by value. It is tested with `Kind()` and read with
accessors that report whether they apply, or converted with `As`:

```go
if s, ok := v.Str(); ok {
	fmt.Println(s)
}
cfg, err := v.As[Config]() // a table decoded into a Go struct
```

| | GopherLua | glua |
| --- | --- | --- |
| Make a value | `lua.LString("x")`, `lua.LNumber(1)`, `lua.LTrue` | `lua.String("x")`, `lua.Int(1)`, `lua.Bool(true)`, `lua.ValueOf(x)` |
| nil | `lua.LNil`, compared with `==` | the zero `Value`; `v.IsNil()` |
| Equality | `==` on the interface, or `L.Equal` | `v.Equal(w)`; `==` does not compile |
| Truthiness | `lua.LVAsBool(v)` | `v.Truthy()` |
| New table | `L.NewTable()`, `L.CreateTable(a, h)` | `lua.NewTable(narr, nhash)` |
| Table access | `tb.RawGetString("k")`, `L.GetField(tb, "k")` | `t.Get(lua.String("k"))`, `t.Field[int]("k")` |
| Iterate | `tb.ForEach(func(k, v lua.LValue) {...})` | `for k, v := range t.All()`, `t.Items()` |

## Calling Go from Lua

A GopherLua `LGFunction` takes its arguments from the stack, by 1-based index, and returns the
number of results it pushed:

```go
func clamp(L *lua.LState) int {
	x, lo := L.CheckNumber(1), L.CheckNumber(2)
	hi := L.OptNumber(3, 1)
	L.Push(lua.LNumber(min(max(x, lo), hi)))
	return 1
}
L.SetGlobal("clamp", L.NewFunction(clamp))
```

A glua `GoFunc` takes a `*Call`, with 0-based argument indices, and sets its results with
`Return`:

```go
clamp := lua.GoFunc(func(c *lua.Call) error {
	x, lo := c.Arg[float64](0), c.Arg[float64](1)
	hi := c.Opt[float64](2, 1)
	return c.Return(min(max(x, lo), hi))
})
rt := lua.MustNew(lua.Global("clamp", clamp))
```

Plain Go functions can be wrapped without writing a GoFunc: `lua.Fn1(strings.ToUpper)`, or
`lua.Fn2E(lookup)` for a `func(context.Context, string) (string, error)`, where a non-nil error
becomes a Lua error. `lua.Reflect(f)` wraps any function by reflection.

| Task | GopherLua | glua |
| --- | --- | --- |
| Register a global function | `L.SetGlobal(name, L.NewFunction(f))`, `L.Register(name, f)` | `lua.Global(name, f)` when making the Runtime |
| Required argument | `L.CheckString(n)`, `L.CheckInt(n)` | `c.Arg[string](i)`, `c.Arg[int](i)` |
| Optional argument | `L.OptString(n, def)` | `c.Opt[string](i, def)` |
| Raw argument | `L.Get(n)` | `c.Value(i)` |
| Number of arguments | `L.GetTop()` | `c.Len()` |
| Return results | `L.Push(v)` …; `return n` | `c.Return(v)`, `c.Return2(a, b)`, `c.ReturnValues(vs...)` |
| Raise an error | `L.RaiseError(format, ...)`, `L.ArgError(n, msg)` | return an `error`; `c.Errorf(format, ...)`; `lua.Terminate(err)` to end the call |
| Call back into Lua | `L.CallByParam(...)` | `c.Invoke(fn, args...)`, `c.InvokeN` |
| Context | `L.Context()` | `c.Context()` |

## Modules

| Task | GopherLua | glua |
| --- | --- | --- |
| A module written in Go | `L.PreloadModule(name, loader)` with a loader that pushes a table; or `L.RegisterModule(name, funcs)` | `lua.Module(name, lua.Members{...})`; the table is also a global |
| A module written in Lua | Files found on `package.path` | `lua.SourceModule(name, src)`, or a `lua.Loader(func(name) (src, error))` that supplies source |
| When `require` can load | At any time | While a Script loads, or at any time in a private VM |

## Go types in Lua

GopherLua wraps a Go value in an `*LUserData` and gives it a metatable, often a named type
metatable whose `__index` holds the methods. Fields are reached through those methods.

```go
mt := L.NewTypeMetatable("person")
L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), map[string]lua.LGFunction{
	"name": personName,
}))
ud := L.NewUserData()
ud.Value = &Person{Name: "Ann"}
L.SetMetatable(ud, L.GetTypeMetatable("person"))
```

In glua, a pointer to a struct is itself the userdata. Lua reads its exported fields directly,
by `snake_case` name or by a `lua` struct tag; fields are read-only unless tagged `rw`. Methods
and metamethods are registered per type:

```go
rt := lua.MustNew(lua.Type[Person](
	lua.Method("greet", lua.Fn1((*Person).Greet)),
	lua.Writable("Name"),
))
// in Lua: p.name, p.name = "Bo", p:greet()
```

Nested structs, slices and maps are live views of the Go value. `lua.Convert[T]` gives a type
a scalar or string form instead of a userdata.

## Libraries and sandboxing

GopherLua opens all standard libraries unless `Options.SkipOpenLibs` is set, after which the
wanted ones are opened by calling their `Open*` functions. glua opens a small default set
(`base`, `string`, `table`, `math`, `utf8`); `coroutine`, `load`, `os` and `io` are opt-in
flags, and `os` and `io` are subsets. `lua.Hardened()` is a profile for untrusted scripts. The
[sandbox document](sandbox.md) lists what each allows.

## Limits and cancellation

| | GopherLua | glua |
| --- | --- | --- |
| Cancellation | `L.SetContext(ctx)` | A `ctx` on every call |
| Running time | The context | `Limits.Steps`, a step budget per call, plus the context |
| Memory | `L.SetMx(mb)`, a monitor on the whole process's heap that exits the process at the limit | `Limits.AllocBytes`, bytes allocated per call, ending that call |
| Stack sizes | `Options.CallStackSize`, `RegistrySize`, `RegistryMaxSize` | `Limits.CallDepth`, `Limits.HostDepth` |

In glua, running out of a limit and cancellation end the whole call: `pcall` inside the script
does not catch them. A host function ends the call the same way by returning an error made
with `lua.Terminate`.

## Errors

GopherLua returns an `*ApiError` whose `Type` says what failed (run, syntax, file, panic),
with the Lua error `Object` and a stack trace. glua returns an `*Error` for runtime errors,
whose `Error()` is one line, `Traceback()` gives the frames and `Unwrap` the Go error behind
it, so `errors.Is` and `errors.As` work; a `*SyntaxError` for code that does not compile; and a
`*BindError` for a binding that cannot work, from `New`, `Load`, `Expr` and `FuncN`.

## Coroutines

GopherLua exposes threads to Go: `L.NewThread()` and `L.Resume(th, fn, args...)`, along with the
`coroutine` library. glua has the `coroutine` library, behind `LibCoroutine`, and no Go API for
resuming coroutines.

## Debugging

GopherLua provides much of the `debug` library, including `getinfo`, `getlocal` and
`getupvalue`, and the Go methods `GetStack` and `GetInfo`. glua provides only
`debug.traceback`; a Go caller gets the frames of an error from `(*Error).Traceback()`.
