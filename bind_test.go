package glua

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type bindAddr struct {
	City string
	Zip  int32 `lua:"zip,rw"`
}

type bindBase struct {
	ID   int64
	Kind string
}

type bindUser struct {
	bindBase
	Name    string
	Age     uint8
	Score   float32
	Home    bindAddr
	Work    *bindAddr
	Tags    []string       `lua:"tags,rw"`
	Attrs   map[string]int `lua:"attrs,rw"`
	Frozen  []int
	Timeout time.Duration
	Born    time.Time
	Extra   any
	Secret  string
	Big     uint64
	Mail    bindAddr `lua:",rw"`
	hidden  int
}

func newBindUser() *bindUser {
	return &bindUser{
		bindBase: bindBase{ID: 7, Kind: "admin"},
		Name:     "ann", Age: 30, Score: 1.5,
		Home:    bindAddr{City: "Oslo", Zip: 150},
		Work:    &bindAddr{City: "Bergen", Zip: 5003},
		Tags:    []string{"a", "b"},
		Attrs:   map[string]int{"x": 1, "y": 2},
		Frozen:  []int{1, 2, 3},
		Timeout: 2500 * time.Millisecond,
		Born:    time.Unix(1000, 0),
		Extra:   []any{"e"},
		Big:     1 << 63,
		hidden:  1,
	}
}

// evalUser runs src as the body of function(u) on a Script of rt and returns its first
// result as a string, or the error.
func evalUser(t *testing.T, rt *Runtime, u *bindUser, src string) string {
	t.Helper()
	s, err := rt.Load(context.Background(), "b.lua", "function f(u) "+src+" end")
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.Func1[*bindUser, Value]("f")
	if err != nil {
		t.Fatal(err)
	}
	v, err := f(context.Background(), u)
	if err != nil {
		return "error: " + err.Error()
	}
	return v.String()
}

func TestStructFields(t *testing.T) {
	rt := MustNew()
	u := newBindUser()
	for _, tc := range []struct{ src, want string }{
		{"return u.name .. u.age .. u.score", "ann301.5"},
		{"return u.id .. u.kind", "7admin"},
		{"return u.home.city .. u.home.zip", "Oslo150"},
		{"return u.work.city", "Bergen"},
		{"return #u.tags .. u.tags[1] .. u.tags[2] .. tostring(u.tags[3])", "2abnil"},
		{"local s = '' for i, v in ipairs(u.tags) do s = s .. i .. v end return s", "1a2b"},
		{"local s = '' for k, v in pairs(u.attrs) do s = s .. k .. v end return s", "x1y2"},
		{"return u.attrs.x + u.attrs['y'] .. tostring(u.attrs.z)", "3nil"},
		{"return #u.attrs", "2"},
		{"return u.timeout", "2.5"},
		{"return u.born", "1000"},
		{"return u.extra[1]", "e"},
		{"return u.big", "9.2233720368548e+18"},
		{"return u.nope", "error: b.lua:1: no field 'nope' on bindUser"},
		{"return u.hidden", "error: b.lua:1: no field 'hidden' on bindUser"},
		{"return u.home == u.home, u.work == u.work", "true"},
		{"return type(u.home)", "userdata"},
		{"local n = 0 for i = 1, 100 do n = n + u.age end return n", "3000"},
	} {
		if got := evalUser(t, rt, u, tc.src); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.src, got, tc.want)
		}
	}
}

func TestStructWrites(t *testing.T) {
	rt := MustNew()
	u := newBindUser()
	for _, tc := range []struct{ src, want string }{
		{"u.name = 'x'", "error: b.lua:1: field 'name' of bindUser is read-only"},
		{"u.home.zip = 1", "error: b.lua:1: attempt to modify a read-only bindAddr"},
		{"u.mail.zip = 99 return u.mail.zip", "99"},
		{"u.mail.city = 'x'", "error: b.lua:1: field 'city' of bindAddr is read-only"},
		{"u.work.zip = 42 return u.work.zip", "42"},
		{"u.tags[1] = 'z' u.tags[3] = 'c' return table.concat({u.tags[1], u.tags[2], u.tags[3]})", "zbc"},
		{"u.tags[9] = 'c'", "error: b.lua:1: index 9 out of range for []string of length 3"},
		{"u.tags[1] = 5", "error: b.lua:1: bad value for []string (cannot convert number to string)"},
		{"u.frozen[1] = 5", "error: b.lua:1: attempt to modify a read-only []int"},
		{"u.attrs.z = 3 u.attrs.x = nil return u.attrs.z", "3"},
		{"u.mail = {city = 'Rome', zip = 1} return u.mail.city", "Rome"},
		{"u.mail = {town = 'x'}", "error: b.lua:1: bad value for field 'mail' of bindUser (no field 'town' in glua.bindAddr)"},
		{"u.nope = 1", "error: b.lua:1: no field 'nope' on bindUser"},
	} {
		if got := evalUser(t, rt, u, tc.src); got != tc.want {
			t.Errorf("%s:\n got %q\nwant %q", tc.src, got, tc.want)
		}
	}
	if u.Mail != (bindAddr{City: "Rome", Zip: 1}) || u.Work.Zip != 42 || strings.Join(u.Tags, "") != "zbc" {
		t.Errorf("writes did not reach Go: %+v %+v %v", u.Mail, u.Work, u.Tags)
	}
	if _, ok := u.Attrs["x"]; ok || u.Attrs["z"] != 3 {
		t.Errorf("map writes: %v", u.Attrs)
	}
}

type bindWritable struct {
	N int `lua:"n,rw"`
}

func TestStructByValueReadonly(t *testing.T) {
	s := mustLoad(t, MustNew(), "function f(w) w.n = 1 end function g(w) return w.n end")
	f, _ := s.Func1[bindWritable, struct{}]("f")
	if _, err := f(context.Background(), bindWritable{}); err == nil || !strings.Contains(err.Error(), "attempt to modify a read-only bindWritable") {
		t.Errorf("by value: %v", err)
	}
	g, _ := s.Func1[*bindWritable, int]("g")
	if n, err := g(context.Background(), &bindWritable{N: 4}); err != nil || n != 4 {
		t.Errorf("by pointer: %v %v", n, err)
	}
	// a shared script cannot hold a Go value with writable fields
	if _, err := New(Global("w", &bindWritable{})); err == nil || !strings.Contains(err.Error(), "writable fields cannot be shared") {
		t.Errorf("rw global: %v", err)
	}
}

func TestHardenedCopies(t *testing.T) {
	rt := MustNew(Hardened(), Type[bindUser](), Type[bindAddr](), Type[bindBase]())
	u := newBindUser()
	// each read is a fresh copy, so the write is lost
	if got := evalUser(t, rt, u, "u.tags[1] = 'z' return u.tags[1] .. type(u.tags)"); got != "atable" {
		t.Errorf("hardened slice: %q", got)
	}
	if u.Tags[0] != "a" {
		t.Error("a hardened script wrote through a slice")
	}
}

type bindReq struct {
	Method  string
	Headers map[string]string
	Cost    int
}

func TestTypeOptions(t *testing.T) {
	header := GoFunc(func(c *Call) error {
		r, err := c.Value(0).As[*bindReq]()
		if err != nil {
			return err
		}
		k, _ := c.Value(1).Str()
		return c.ReturnValues(String(r.Headers[k]))
	})
	rt := MustNew(Type[bindReq](
		Method("header", header),
		Meta("__tostring", func(c *Call) error {
			r, _ := c.Value(0).As[*bindReq]()
			return c.ReturnValues(String("req " + r.Method))
		}),
		Meta("__eq", func(c *Call) error {
			a, _ := c.Value(0).As[*bindReq]()
			b, _ := c.Value(1).As[*bindReq]()
			return c.ReturnValues(Bool(a.Method == b.Method))
		}),
		Meta("__index", func(c *Call) error { return c.ReturnValues(String("default")) }),
		Rename("Method", "verb"),
		Hide("Headers"),
		Writable("Cost"),
	))
	s := mustLoad(t, rt, `
		function f(r, o) r.cost = 9 return r:header("x-a") .. "|" .. tostring(r) .. "|" .. r.verb .. "|" .. tostring(r == o) .. "|" .. r.other .. "|" .. tostring(rawequal(r, o)) end
		function g(r) return r.method end`)
	f, err := s.Func2[*bindReq, *bindReq, string]("f")
	if err != nil {
		t.Fatal(err)
	}
	r := &bindReq{Method: "GET", Headers: map[string]string{"x-a": "1"}}
	got, err := f(context.Background(), r, &bindReq{Method: "GET"})
	if err != nil || got != "1|req GET|GET|true|default|false" {
		t.Errorf("got %q, %v", got, err)
	}
	if r.Cost != 9 {
		t.Error("Writable did not make cost writable")
	}
	g, _ := s.Func1[*bindReq, string]("g")
	if v, err := g(context.Background(), r); err != nil || v != "default" {
		t.Errorf("renamed field through __index: %q %v", v, err)
	}
	// another Runtime sees the type with the default rules
	if got := evalUserReq(t, MustNew(), r, "return r.method"); got != "GET" {
		t.Errorf("default rules in another Runtime: %q", got)
	}
	for _, tc := range []struct {
		opts []TypeOption
		want string
	}{
		{[]TypeOption{Hide("Nope")}, "no visible field Nope"},
		{[]TypeOption{Writable("Nope")}, "no visible field Nope"},
		{[]TypeOption{Rename("Nope", "x")}, "no visible field Nope"},
		{[]TypeOption{Rename("Cost", "method")}, "fields Method and Cost both map to 'method'"},
		{[]TypeOption{Rename("Cost", "end")}, "not a Lua name"},
		{[]TypeOption{Method("cost", header)}, "method cost has the name of a field"},
		{[]TypeOption{Meta("eq", header)}, "does not start with __"},
	} {
		if _, err := New(Type[bindReq](tc.opts...)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("got %v, want %q", err, tc.want)
		}
	}
	if _, err := New(Type[int]()); err == nil {
		t.Error("Type[int] accepted")
	}
	if _, err := New(Type[bindReq](), Type[*bindReq]()); err == nil {
		t.Error("a type registered twice")
	}
}

func evalUserReq(t *testing.T, rt *Runtime, r *bindReq, src string) string {
	t.Helper()
	s := mustLoad(t, rt, "function f(r) "+src+" end")
	f, err := s.Func1[*bindReq, string]("f")
	if err != nil {
		t.Fatal(err)
	}
	v, err := f(context.Background(), r)
	if err != nil {
		return "error: " + err.Error()
	}
	return v
}

type bindEmpty struct{ x int }

type bindDup struct {
	A int `lua:"x"`
	B int `lua:"x"`
}

type bindOuter struct {
	In *bindReq `lua:"inner"`
}

func TestBindErrors(t *testing.T) {
	s := mustLoad(t, MustNew(), "function f() end")
	var be *BindError
	if _, err := s.Func1[*bindEmpty, int]("f"); !errors.As(err, &be) || !strings.Contains(be.Reason, "nothing is visible") {
		t.Errorf("empty: %v", err)
	}
	if _, err := s.Func1[bindDup, int]("f"); err == nil || !strings.Contains(err.Error(), "fields A and B both map to 'x'") {
		t.Errorf("duplicate: %v", err)
	}
	strict := mustLoad(t, MustNew(AutoBind(false), Type[bindOuter]()), "function f(o) return o.inner end function g(o) return o.inner.method end")
	if _, err := strict.Func1[*bindReq, int]("f"); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Errorf("AutoBind(false): %v", err)
	}
	g, err := strict.Func1[*bindOuter, string]("g")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g(context.Background(), &bindOuter{In: &bindReq{Method: "GET"}}); err == nil || !strings.Contains(err.Error(), "no field 'method' on bindReq") {
		t.Errorf("an unregistered type reached through a field is opaque: %v", err)
	}
}
