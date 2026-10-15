# glua

A pure-Go Lua 5.4 for embedding small scripts in Go programs: rules, policies, handlers and
configuration. Scripts are loaded once, frozen, and then called from any goroutine as typed Go
functions, under limits on steps, memory and nesting.

```go
import lua "github.com/folbricht/glua"

rt := lua.MustNew(lua.Limits{Steps: 100_000, AllocBytes: 1 << 20})

script, err := rt.Load(ctx, "policy.lua", `
	function allow(req, limit)
		return req.method == "GET" and req.cost <= limit
	end`)
allow, err := script.Func2[*Request, int64, bool]("allow")

ok, err := allow(ctx, req, 10) // a plain Go function, safe from any goroutine
```

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
- [The language](docs/language.md): how glua differs from Lua 5.4, and which libraries it has.
- [Sandbox and limits](docs/sandbox.md): what a script can and cannot do, the limits and the
  fixed caps.
- Benchmark results by milestone are in [bench](bench).

## License

BSD 3-Clause, see [LICENSE](LICENSE).
