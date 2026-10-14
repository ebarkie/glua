package glua_test

// These are the six examples of the design, written against the API sketch. They have no
// Output comments, so go test compiles them but never runs them: the sketch only panics.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	glua "github.com/folbricht/glua/internal/apisketch"
)

var ctx = context.Background()

// 1. One-liner
func Example_oneLiner() {
	n, err := glua.Eval[int64](ctx, `return 6 * 7`)
	fmt.Println(n, err)
}

// 2. A rule against a Go struct

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
	rt := glua.MustNew()
	rule, err := rt.Expr[Order, bool]("gold-discount", `total > 100 and customer.tier == "gold"`)
	if err != nil {
		panic(err)
	}

	order := Order{Total: 120, Customer: &Customer{Tier: "gold"}}
	ok, err := rule.Eval(ctx, &order)
	fmt.Println(ok, err)
}

// 3. Named handlers, called from many goroutines

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
	rt, err := glua.New(
		glua.Limits{Steps: 100_000, AllocBytes: 1 << 20},
		glua.Global("env", "prod"),
		glua.Global("upper", glua.Fn1(strings.ToUpper)),
		glua.Type[Request](glua.Method("header", glua.Fn2((*Request).Header))),
	)
	if err != nil {
		panic(err)
	}

	script, err := rt.Load(ctx, "policy.lua", policySrc)
	if err != nil {
		panic(err)
	}
	allow, err := script.Func2[*Request, int64, bool]("allow")
	if err != nil {
		panic(err)
	}

	req := &Request{Method: "GET", Path: "/api/x"}
	ok, err := allow(ctx, req, 10) // a plain func(context.Context, *Request, int64) (bool, error)
	fmt.Println(ok, err)

	if _, err := script.Func1[*Request, struct{}]("on_deny"); errors.Is(err, glua.ErrUndefined) {
		fmt.Println("no on_deny hook")
	}
	_, _ = script.Func0[glua.Pair[string, string]]("")
}

// 4. Host functions

func lookup(ctx context.Context, key string) (string, error) {
	return "", errors.New("not found")
}

func Example_hostFunctions() {
	clamp := glua.GoFunc(func(c *glua.Call) error {
		x, lo := c.Arg[float64](0), c.Arg[float64](1)
		hi := c.Opt[float64](2, 1)
		return c.Return(min(max(x, lo), hi))
	})
	split := glua.GoFunc(func(c *glua.Call) error {
		s, sep := c.Arg[string](0), c.Arg[string](1)
		head, tail, _ := strings.Cut(s, sep)
		return c.Return2(head, tail)
	})
	each := glua.GoFunc(func(c *glua.Call) error {
		t, err := c.Value(0).As[*glua.Table]()
		if err != nil {
			return err
		}
		for _, v := range t.Items() {
			if _, err := c.Invoke(c.Value(1), v); err != nil {
				return err
			}
		}
		return nil
	})

	rt := glua.MustNew(
		glua.Output(os.Stdout),
		glua.Global("upper", glua.Fn1(strings.ToUpper)),
		glua.Global("clamp", clamp),
		glua.Global("log_warn", glua.Do1(func(msg string) { slog.Warn(msg) })),
		glua.Module("kv", glua.Members{
			"get":     glua.Fn2E(lookup),
			"split":   split,
			"each":    each,
			"version": 3,
		}),
		glua.Type[Request](glua.Method("header", glua.Fn2((*Request).Header))),
	)
	_ = rt
}

// 5. Config files, tables and a VM of your own

type Config struct {
	Port  int
	Hosts []string
}

func Example_config() {
	rt := glua.MustNew()
	script, err := rt.Load(ctx, "config.lua", `config = { port = 8080, hosts = {"a", "b"} }`)
	if err != nil {
		panic(err)
	}

	cfg, err := script.Get[Config]("config")
	fmt.Println(cfg, err)

	t, _ := script.Get[*glua.Table]("config")
	port, _ := t.Field[int]("port")
	fmt.Println(port)
	for k, v := range t.All() {
		fmt.Println(k, v)
	}

	vm, err := script.NewVM(ctx)
	if err != nil {
		panic(err)
	}
	err = vm.Set("verbose", true)
	fmt.Println(err)
}

// 6. Errors

func Example_errors() {
	_, err := glua.Eval[int64](ctx, `error("boom")`)

	if le, ok := errors.AsType[*glua.Error](err); ok {
		fmt.Println(le.Value, le.Traceback())
	}
	if errors.Is(err, glua.ErrSteps) || errors.Is(err, context.DeadlineExceeded) {
		fmt.Println("ran too long")
	}
	if se, ok := errors.AsType[*glua.SyntaxError](err); ok {
		fmt.Println(se.Line, se.Col, se.Msg)
	}
}

// Generic methods cannot satisfy interfaces and reflection cannot see them. The design
// relies on neither, but method values of generic methods must work for adapters.
func Example_methodValues() {
	var v glua.Value
	asInt := v.As[int64]
	_, _ = asInt()

	t := glua.NewTable(0, 2)
	t.SetField("port", 8080) // type inferred
	_, _ = t.Field[int]("port")
}
