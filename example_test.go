package glua_test

// The six examples of the design, run with their output checked.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"

	lua "github.com/folbricht/glua"
)

var ctx = context.Background()

// 1. A one-liner.
func Example_oneLiner() {
	n, err := lua.Eval[int64](ctx, `return 6 * 7`)
	fmt.Println(n, err)
	// Output: 42 <nil>
}

// 2. A rule against a Go struct.

type Customer struct {
	Tier string
}

type Order struct {
	Total    float64
	Items    int
	Customer *Customer
	Discount float64 `lua:"discount,rw"`
}

func Example_rule() {
	rt := lua.MustNew()
	rule, err := rt.Expr[Order, bool]("gold-discount", `total > 100 and customer.tier == "gold"`)
	if err != nil {
		panic(err)
	}
	order := Order{Total: 120, Customer: &Customer{Tier: "gold"}}
	ok, err := rule.Eval(ctx, &order) // safe from any goroutine; order stays on the caller's stack
	fmt.Println(ok, err)

	// a typo fails when the rule is made, not in production
	_, err = rt.Expr[Order, bool]("gold-discount", `totl > 100`)
	fmt.Println(err)

	// in a chunk, a field tagged rw is an output
	tiers, _ := rt.Expr[Order, struct{}]("tiers", `
		if total > 100 then discount = 0.1 end
		if items >= 10 then discount = discount + 0.05 end`)
	order = Order{Total: 150, Items: 12}
	tiers.Eval(ctx, &order)
	fmt.Printf("%.2f\n", order.Discount)
	// Output:
	// true <nil>
	// gold-discount:1: unknown name 'totl'
	// 0.15
}

// 3. Named handlers, called from many goroutines.

type Request struct {
	Method string
	Path   string
	hdr    map[string]string
}

func (r *Request) Header(name string) string { return r.hdr[name] }

const policySrc = `
local prefix = "/api/"
function allow(req, limit)
  if req.method ~= "GET" then return false end
  local cost = tonumber(req:header("x-cost")) or 0
  return req.path:sub(1, #prefix) == prefix and cost <= limit
end
`

func Example_handlers() {
	rt, err := lua.New(
		lua.Limits{Steps: 100_000, AllocBytes: 1 << 20},
		lua.Global("env", "prod"),
		lua.Global("upper", lua.Fn1(strings.ToUpper)),
		lua.Type[Request](lua.Method("header", lua.Fn2((*Request).Header))),
	)
	if err != nil {
		panic(err)
	}
	script, err := rt.Load(ctx, "policy.lua", policySrc) // compiles, runs the top level once, freezes the result
	if err != nil {
		panic(err)
	}
	allow, err := script.Func2[*Request, int64, bool]("allow")
	if err != nil {
		panic(err)
	}

	reqs := []*Request{
		{Method: "GET", Path: "/api/users", hdr: map[string]string{"x-cost": "3"}},
		{Method: "GET", Path: "/api/report", hdr: map[string]string{"x-cost": "50"}},
		{Method: "POST", Path: "/api/users"},
		{Method: "GET", Path: "/static/app.js"},
	}
	results := make([]bool, len(reqs))
	var wg sync.WaitGroup
	for i, req := range reqs {
		wg.Go(func() {
			results[i], _ = allow(ctx, req, 10) // a plain func(context.Context, *Request, int64) (bool, error)
		})
	}
	wg.Wait()
	fmt.Println(results)

	// optional hooks can be probed
	if _, err := script.Func1[*Request, struct{}]("on_deny"); errors.Is(err, lua.ErrUndefined) {
		fmt.Println("no on_deny hook")
	}
	// Output:
	// [true false false false]
	// no on_deny hook
}

// 4. Host functions.

var store = map[string]string{"user:42": "ann"}

func lookup(ctx context.Context, key string) (string, error) {
	if v, ok := store[key]; ok {
		return v, nil
	}
	return "", errors.New("not found")
}

func Example_hostFunctions() {
	clamp := lua.GoFunc(func(c *lua.Call) error {
		x, lo := c.Arg[float64](0), c.Arg[float64](1)
		hi := c.Opt[float64](2, 1)
		return c.Return(min(max(x, lo), hi))
	})
	split := lua.GoFunc(func(c *lua.Call) error {
		s, sep := c.Arg[string](0), c.Arg[string](1)
		head, tail, _ := strings.Cut(s, sep)
		return c.Return2(head, tail)
	})
	each := lua.GoFunc(func(c *lua.Call) error {
		t := c.Arg[*lua.Table](0)
		for _, v := range t.Items() {
			if _, err := c.Invoke(c.Value(1), v); err != nil {
				return err
			}
		}
		return nil
	})
	rt := lua.MustNew(
		lua.Output(os.Stdout), // where print goes; the default discards it
		lua.Global("upper", lua.Fn1(strings.ToUpper)),
		lua.Global("clamp", clamp),
		lua.Global("log_warn", lua.Do1(func(msg string) { fmt.Println("warning:", msg) })),
		lua.Module("kv", lua.Members{
			"get":     lua.Fn2E(lookup), // func(context.Context, string) (string, error)
			"split":   split,
			"each":    each,
			"version": 3,
		}),
		lua.Type[Request](lua.Method("header", lua.Fn2((*Request).Header))),
	)
	script, err := rt.Load(ctx, "host.lua", `
		function run(req)
			print(upper("hello"))                     -- global
			print(clamp(1.7, 0))                      -- third argument left out, so c.Opt supplies 1
			print(kv.get("user:42"))                  -- module member; the Go side also receives the context
			local head, tail = kv.split("a=b", "=")   -- two results
			print(head, tail)
			print(req:header("x-cost"))               -- method, req goes in as the first argument
			print(pcall(kv.get, ""))                  -- a Go error is a Lua error, a string here
			local total = 0
			kv.each({1, 2, 3}, function(n) total = total + n end)   -- Go calls back into Lua
			print(total)
			print(require("kv") == kv, kv.version)    -- the same table as the global kv
			log_warn("done")
		end`)
	if err != nil {
		panic(err)
	}
	run, _ := script.Func1[*Request, struct{}]("run")
	if _, err := run(ctx, &Request{hdr: map[string]string{"x-cost": "7"}}); err != nil {
		fmt.Println(err)
	}
	// Output:
	// HELLO
	// 1.0
	// ann
	// a	b
	// 7
	// false	not found
	// 6
	// true	3
	// warning: done
}

// 5. Config files, tables and a VM of your own.

type Config struct {
	Port  int
	Hosts []string
}

func Example_config() {
	rt := lua.MustNew()
	script, err := rt.Load(ctx, "config.lua", `config = { port = 8080, hosts = {"a", "b"} }
		count = 0
		function bump() count = count + 1 return count end`)
	if err != nil {
		panic(err)
	}
	cfg, err := script.Get[Config]("config") // decoded from the frozen globals; runs nothing
	fmt.Printf("%+v %v\n", cfg, err)

	t, _ := script.Get[*lua.Table]("config") // or walk it, from any goroutine
	port, _ := t.Field[int]("port")
	fmt.Println(port)
	var keys []string
	for k := range t.All() {
		keys = append(keys, k.String())
	}
	slices.Sort(keys)
	fmt.Println(keys)

	vm, err := script.NewVM(ctx) // runs the top level again in a private, unfrozen environment
	if err != nil {
		panic(err)
	}
	err = vm.Set("verbose", true) // state persists between calls; one goroutine at a time
	bump, _ := vm.Func0[int]("bump")
	bump(ctx)
	n, _ := bump(ctx)
	fmt.Println(err, n)

	shared, _ := script.Func0[int]("bump")
	_, err = shared(ctx)
	fmt.Println(err)
	// Output:
	// {Port:8080 Hosts:[a b]} <nil>
	// 8080
	// [hosts port]
	// <nil> 2
	// config.lua:3: attempt to modify a frozen table
}

// 6. Errors.
func Example_errors() {
	_, err := lua.Eval[int64](ctx, `local function check(n) if n > 1 then error("boom") end end
		check(2)`)
	if le, ok := errors.AsType[*lua.Error](err); ok {
		fmt.Println(le) // one line
		for _, f := range le.Traceback() {
			fmt.Printf("%s:%d: in %s\n", f.Source, f.Line, f.Function)
		}
	}

	rt := lua.MustNew(lua.Limits{Steps: 1000})
	_, err = rt.Load(ctx, "spin.lua", `while true do end`)
	if errors.Is(err, lua.ErrSteps) || errors.Is(err, context.DeadlineExceeded) {
		fmt.Println("ran too long:", err)
	}

	_, err = rt.Load(ctx, "bad.lua", "x = = 1")
	if se, ok := errors.AsType[*lua.SyntaxError](err); ok {
		fmt.Println(se.Line, se.Msg)
	}
	// Output:
	// eval:1: boom
	// eval:1: in function 'check'
	// eval:2: in main chunk
	// ran too long: spin.lua:1: glua: step limit exceeded
	// 1 unexpected symbol near '='
}
