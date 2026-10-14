package glua

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"unsafe"
)

type ruleCustomer struct {
	Tier string
}

type ruleOrder struct {
	Total    float64
	Items    int
	Customer *ruleCustomer
	Discount float64 `lua:"discount,rw"`
	Note     string  `lua:"note,rw"`
}

func TestExpr(t *testing.T) {
	ctx := context.Background()
	rt := MustNew()
	rule, err := rt.Expr[ruleOrder, bool]("gold-discount", `total > 100 and customer.tier == "gold"`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		o    ruleOrder
		want bool
	}{
		{ruleOrder{Total: 120, Customer: &ruleCustomer{Tier: "gold"}}, true},
		{ruleOrder{Total: 80, Customer: &ruleCustomer{Tier: "gold"}}, false},
		{ruleOrder{Total: 120, Customer: &ruleCustomer{Tier: "silver"}}, false},
	} {
		if got, err := rule.Eval(ctx, &tc.o); err != nil || got != tc.want {
			t.Errorf("%+v: %v %v", tc.o, got, err)
		}
	}
	if _, err := rule.Eval(ctx, &ruleOrder{Total: 120}); err == nil || !strings.Contains(err.Error(), "gold-discount:1: attempt to index a nil value (field 'customer')") {
		t.Errorf("nil customer: %v", err)
	}

	chunk, err := rt.Expr[ruleOrder, int]("tiers", `
		local n = 0
		if total > 100 then discount = 0.1 n = n + 1 end
		if items > 5 then discount = discount + 0.05 note = "bulk" n = n + 1 end
		local function f() return items * 2 end
		return n + f()`)
	if err != nil {
		t.Fatal(err)
	}
	o := ruleOrder{Total: 150, Items: 6}
	if n, err := chunk.Eval(ctx, &o); err != nil || n != 14 {
		t.Errorf("chunk: %v %v", n, err)
	}
	if o.Discount < 0.149 || o.Discount > 0.151 || o.Note != "bulk" || o.Total != 150 {
		t.Errorf("writable fields: %+v", o)
	}
	// a failed run copies nothing back
	failing, _ := rt.Expr[ruleOrder, bool]("fail", `discount = 1 error("no")`)
	o2 := ruleOrder{}
	if _, err := failing.Eval(ctx, &o2); err == nil || o2.Discount != 0 {
		t.Errorf("failed rule: %v %+v", err, o2)
	}
	// a local shadows a field
	shadow, err := rt.Expr[ruleOrder, float64]("shadow", `local total = 1 return total`)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := shadow.Eval(ctx, &ruleOrder{Total: 9}); v != 1 {
		t.Errorf("shadow: %v", v)
	}
	// globals of the Runtime
	glob, err := rt.Expr[ruleOrder, string]("glob", `string.format("%.1f", math.max(total, 1))`)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := glob.Eval(ctx, &ruleOrder{Total: 2.5}); err != nil || v != "2.5" {
		t.Errorf("glob: %v %v", v, err)
	}
}

type ruleShadow struct {
	Type string
}

func TestExprErrors(t *testing.T) {
	rt := MustNew(Global("limit", 10))
	for _, tc := range []struct{ src, want string }{
		{"totl > 100", "gold:1: unknown name 'totl'"},
		{"total = 1", "gold:1: cannot assign to read-only field 'total'"},
		{"limit = 1", "gold:1: cannot assign to global 'limit'"},
		{"x = 1", "gold:1: unknown name 'x'"},
		{"local function f() return custmer end return f()", "gold:1: unknown name 'custmer'"},
		{"total >", "gold:1: syntax error near '>'"},
		{"select('#', ...)", "gold:1: cannot use '...' outside a vararg function near '...'"},
	} {
		_, err := rt.Expr[ruleOrder, bool]("gold", tc.src)
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: got %v, want %q", tc.src, err, tc.want)
		}
	}
	if _, err := rt.Expr[ruleOrder, bool]("ok", "total > limit"); err != nil {
		t.Errorf("a host global: %v", err)
	}
	var be *BindError
	if _, err := rt.Expr[ruleShadow, bool]("s", "true"); !errors.As(err, &be) || !strings.Contains(be.Reason, "shadows the global 'type'") {
		t.Errorf("shadowing: %v", err)
	}
	if _, err := rt.Expr[*ruleOrder, bool]("p", "true"); err == nil {
		t.Error("a pointer environment was accepted")
	}
	var se *SyntaxError
	if _, err := rt.Expr[ruleOrder, bool]("gold", "totl"); !errors.As(err, &se) || se.Line != 1 {
		t.Errorf("not a SyntaxError: %#v", err)
	}
}

func TestExprEscapes(t *testing.T) {
	rt := MustNew()
	keep, err := rt.Expr[ruleOrder, Value]("keep", `(function() return customer end)()`)
	if err != nil {
		t.Fatal(err)
	}
	c := &ruleCustomer{Tier: "gold"}
	v, err := keep.Eval(context.Background(), &ruleOrder{Customer: c})
	if err != nil || v.p != unsafePointerOf(c) {
		t.Errorf("a pointer field is the pointer itself: %v", err)
	}
	// the environment itself is a slot; returning a field proxy copies it out
	type wrap struct {
		Inner ruleCustomer
	}
	inner, err := rt.Expr[wrap, Value]("inner", "inner")
	if err != nil {
		t.Fatal(err)
	}
	v1, _ := inner.Eval(context.Background(), &wrap{Inner: ruleCustomer{Tier: "a"}})
	v2, _ := inner.Eval(context.Background(), &wrap{Inner: ruleCustomer{Tier: "b"}})
	if (*ruleCustomer)(v1.p).Tier != "a" || (*ruleCustomer)(v2.p).Tier != "b" {
		t.Error("an escaped proxy into the slot changed with the next call")
	}
}

func TestExprConcurrentNoAlloc(t *testing.T) {
	rt := MustNew()
	rule, err := rt.Expr[ruleOrder, bool]("r", `total > 100 and customer.tier == "gold" and items < 10`)
	if err != nil {
		t.Fatal(err)
	}
	gold := &ruleCustomer{Tier: "gold"}
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Go(func() {
			for i := range 300 {
				o := ruleOrder{Total: float64(90 + i%30), Items: g, Customer: gold}
				if ok, err := rule.Eval(context.Background(), &o); err != nil || ok != (o.Total > 100) {
					t.Errorf("%+v: %v %v", o, ok, err)
					return
				}
			}
		})
	}
	wg.Wait()
	if raceEnabled || stackStress {
		return
	}
	ctx := context.Background()
	if a := testing.AllocsPerRun(1000, func() {
		o := ruleOrder{Total: 120, Items: 1, Customer: gold}
		rule.Eval(ctx, &o)
	}); a != 0 {
		t.Errorf("Eval allocates %v times per call", a)
	}
}

func unsafePointerOf[T any](p *T) unsafe.Pointer { return unsafe.Pointer(p) }
