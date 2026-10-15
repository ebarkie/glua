# glua

[![Go Reference](https://pkg.go.dev/badge/github.com/folbricht/glua.svg)](https://pkg.go.dev/github.com/folbricht/glua)

A pure-Go Lua 5.4 for embedding small scripts in Go programs: rules, policies, handlers and
configuration. Scripts are loaded once, frozen, and then called from any goroutine as typed Go
functions, under limits on steps, memory and nesting.

## Examples

```go
import lua "github.com/folbricht/glua"
```

### Run Lua from a string

`Eval` runs a chunk once and converts its result:

```go
n, err := lua.Eval[int64](ctx, `return 6 * 7`) // 42
```

To call into a script more than once, load it into a `Runtime` and bind its functions to Go
functions:

```go
rt := lua.MustNew()
script, err := rt.Load(ctx, "greet", `
	function greet(name)
		return "hello, " .. name
	end`)
greet, err := script.Func1[string, string]("greet")

s, err := greet(ctx, "world") // "hello, world"
```

### Load a script from a file

`Load` takes the script's source and a name that error messages use, here the file's:

```go
src, err := os.ReadFile("policy.lua")
rt := lua.MustNew(lua.Limits{Steps: 100_000, AllocBytes: 1 << 20})
script, err := rt.Load(ctx, "policy.lua", string(src))
allow, err := script.Func2[*Request, int64, bool]("allow")

ok, err := allow(ctx, req, 10) // a plain Go function, safe from any goroutine
```

```lua
-- policy.lua
function allow(req, limit)
  return req.method == "GET" and req.cost <= limit
end
```

`Load` runs the script's top level once and freezes what it built, so one `Script` serves
every goroutine. The fields of `*Request` are visible to Lua by their snake_case names.

### Call Go from Lua

Go functions are registered as globals when the `Runtime` is made. Typed adapters such as
`lua.Fn1` wrap plain Go functions; a `lua.GoFunc` reads its arguments and sets its results
itself:

```go
clamp := lua.GoFunc(func(c *lua.Call) error {
	x, lo, hi := c.Arg[float64](0), c.Arg[float64](1), c.Arg[float64](2)
	return c.Return(min(max(x, lo), hi))
})

rt := lua.MustNew(
	lua.Output(os.Stdout), // where print writes; the default discards
	lua.Global("upper", lua.Fn1(strings.ToUpper)),
	lua.Global("clamp", clamp),
)
script, err := rt.Load(ctx, "main.lua", `
	print(upper("hello"), clamp(15, 0, 10)) -- HELLO   10.0
	function shout(s) return upper(s) .. "!" end`)

shout, err := script.Func1[string, string]("shout")
out, err := shout(ctx, "hi") // "HI!"
```

A Go error returned by a host function becomes a Lua error, which the script can catch with
`pcall`.

### Rules over a Go struct

Expressions over a Go struct compile into rules, with misspelt names caught when the rule is
made:

```go
rule, err := rt.Expr[Order, bool]("gold", `total > 100 and customer.tier == "gold"`)
ok, err := rule.Eval(ctx, &order)
```

Requirements: Go 1.27 or later, a 64-bit platform. No cgo.

Status: feature-complete and tested against the Lua 5.4.9 test suite and against C Lua by
differential fuzzing. The faster, unchecked interpreter loop becomes the default after a week
of continuous fuzzing; until then every Runtime runs the bounds-checked loop.

## Documentation

- [Package documentation](https://pkg.go.dev/github.com/folbricht/glua): the API, with runnable
  examples.
- [luajson](https://pkg.go.dev/github.com/folbricht/glua/luajson): converting between Lua
  values and JSON, and a `json` module for scripts.
- [The language](docs/language.md): how glua differs from Lua 5.4, and which libraries it has.
- [Sandbox and limits](docs/sandbox.md): what a script can and cannot do, the limits and the
  fixed caps.
- [GopherLua and glua side by side](docs/gopherlua.md): how common tasks look in each.
- [Benchmarks](docs/benchmarks.md): small scripts in C Lua, glua and other Lua implementations
  for Go. Results by milestone are in [bench](bench).

## License

BSD 3-Clause, see [LICENSE](LICENSE).
