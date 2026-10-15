package glua

import (
	"errors"
	"math"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
	"unsafe"
)

type convPoint struct {
	X, Y int
	Name string `lua:"label"`
	Skip int    `lua:"-"`
}

type convConfig struct {
	Port    int
	Hosts   []string
	Timeout time.Duration
	Labels  map[string]string
	Origin  *convPoint
	Extra   any
	Ratio   float32
	Small   int8
	Flag    bool
}

type userID int64

func TestValueOf(t *testing.T) {
	for _, tc := range []struct {
		v    Value
		want string
	}{
		{ValueOf(42), "42"},
		{ValueOf(int8(-3)), "-3"},
		{ValueOf(uint16(7)), "7"},
		{ValueOf(1.5), "1.5"},
		{ValueOf(float32(0.25)), "0.25"},
		{ValueOf("s"), "s"},
		{ValueOf(true), "true"},
		{ValueOf(userID(9)), "9"},
		{ValueOf(1500 * time.Millisecond), "1.5"},
		{ValueOf(time.Unix(100, 0)), "100"},
		{ValueOf(time.Unix(100, 5e8)), "100.5"},
		{ValueOf(time.Date(2300, 1, 1, 0, 0, 0, 5e8, time.UTC)), "10413792000.5"}, // past UnixNano's range
		{ValueOf(time.Date(1600, 1, 1, 0, 0, 0, 25e7, time.UTC)), "-11676095999.75"},
		{ValueOf([]byte("raw")), "raw"},
		{ValueOf(Int(3)), "3"},
		{ValueOf[*Table](nil), "nil"},
		{ValueOf[any](nil), "nil"},
		{ValueOf[*convPoint](nil), "nil"},
	} {
		if got := tc.v.String(); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
	tab := ValueOf([]any{1, "a", []int{2}})
	if tab.Kind() != KindTable {
		t.Fatal("slice is not a table")
	}
	tt := (*Table)(tab.p)
	if tt.Len() != 3 || tt.Get(Int(2)).String() != "a" || (*Table)(tt.Get(Int(3)).p).Get(Int(1)).String() != "2" {
		t.Error("slice contents")
	}
	m := (*Table)(ValueOf(map[string]int{"a": 1}).p)
	if m.Get(String("a")).String() != "1" {
		t.Error("map contents")
	}
	p := &convPoint{X: 1}
	u := ValueOf(p)
	if u.Kind() != KindUserdata || u.p != unsafe.Pointer(p) {
		t.Error("a pointer to a struct is not the pointer itself")
	}
	byVal := ValueOf(convPoint{X: 2})
	if byVal.Kind() != KindUserdata || (*convPoint)(byVal.p).X != 2 {
		t.Error("a struct by value is not a heap copy")
	}
	for _, bad := range []func(){
		func() { ValueOf(make(chan int)) },
		func() { ValueOf(strings.ToUpper) },
		func() { ValueOf(uint64(math.MaxUint64)) },
		func() { ValueOf(map[float64]int{math.NaN(): 1}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("ValueOf did not panic")
				}
			}()
			bad()
		}()
	}
}

func TestAs(t *testing.T) {
	if i, err := Float(3).As[int](); err != nil || i != 3 {
		t.Errorf("3.0 as int: %v %v", i, err)
	}
	if _, err := Float(3.5).As[int](); err == nil {
		t.Error("3.5 as int did not fail")
	}
	if _, err := String("3").As[int64](); err == nil || !strings.Contains(err.Error(), "cannot convert string to int64") {
		t.Errorf("'3' as int64: %v", err)
	}
	if _, err := Int(300).As[int8](); err == nil || !strings.Contains(err.Error(), "300 is out of range") {
		t.Errorf("300 as int8: %v", err)
	}
	if _, err := Int(-1).As[uint](); err == nil {
		t.Error("-1 as uint did not fail")
	}
	if u, err := Int(5).As[userID](); err != nil || u != 5 {
		t.Errorf("as userID: %v %v", u, err)
	}
	if b, _ := (Value{}).As[bool](); b {
		t.Error("nil as bool is true")
	}
	if b, _ := Int(0).As[bool](); !b {
		t.Error("0 as bool is false: bool uses truthiness")
	}
	if d, err := Float(0.5).As[time.Duration](); err != nil || d != 500*time.Millisecond {
		t.Errorf("as Duration: %v %v", d, err)
	}
	if tm, err := String("2026-10-14T10:00:00Z").As[time.Time](); err != nil || tm.Unix() != 1791972000 {
		t.Errorf("as Time: %v %v", tm, err)
	}
	if s, err := Int(1).As[string](); err == nil {
		t.Errorf("1 as string: %q", s)
	}

	res, err := runSource("cfg", `return {port = 8080, hosts = {"a", "b"}, timeout = 1.5, labels = {env = "prod"},
		origin = {x = 1, y = 2, label = "o"}, extra = {1, 2, {k = true}}, ratio = 0.5, small = -2, flag = 1}`)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := res[0].As[convConfig]()
	if err != nil {
		t.Fatal(err)
	}
	want := convConfig{
		Port: 8080, Hosts: []string{"a", "b"}, Timeout: 1500 * time.Millisecond, Labels: map[string]string{"env": "prod"},
		Origin: &convPoint{X: 1, Y: 2, Name: "o"}, Extra: []any{int64(1), int64(2), map[string]any{"k": true}},
		Ratio: 0.5, Small: -2, Flag: true,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("got  %+v\nwant %+v", cfg, want)
	}
	for _, tc := range []struct{ src, want string }{
		{"return {prot = 1}", "no field 'prot' in glua.convConfig"},
		{"return {origin = {z = 1}}", "origin: no field 'z' in glua.convPoint"},
		{"return {hosts = {1}}", "hosts[1]: cannot convert number to string"},
		{"return {small = 1000}", "small: cannot convert number to int8: 1000 is out of range"},
		{"return {[1] = 2}", "no field 1 in glua.convConfig"},
		{"return {origin = {skip = 1}}", "no field 'skip'"},
		{"return 'x'", "cannot convert string to glua.convConfig"},
	} {
		res, err := runSource("cfg", tc.src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res[0].As[convConfig](); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.src, err, tc.want)
		}
	}
	p := &convPoint{X: 7}
	if q, err := ValueOf(p).As[*convPoint](); err != nil || q != p {
		t.Errorf("userdata as pointer: %v %v", q, err)
	}
	if q, err := ValueOf(p).As[convPoint](); err != nil || q.X != 7 {
		t.Errorf("userdata as struct: %v %v", q, err)
	}
	if _, err := ValueOf(p).As[*convConfig](); err == nil || !strings.Contains(err.Error(), "another type") {
		t.Errorf("userdata as another type: %v", err)
	}
	if x, err := ValueOf(p).As[any](); err != nil || x != any(p) {
		t.Errorf("userdata as any: %v %v", x, err)
	}
}

func TestTableFields(t *testing.T) {
	tab := NewTable(0, 4)
	tab.SetField("port", 8080)
	tab.SetField("name", "srv")
	tab.SetField("hosts", []string{"a"})
	if p, err := tab.Field[int]("port"); err != nil || p != 8080 {
		t.Errorf("port: %v %v", p, err)
	}
	if h, err := tab.Field[[]string]("hosts"); err != nil || len(h) != 1 || h[0] != "a" {
		t.Errorf("hosts: %v %v", h, err)
	}
	if _, err := tab.Field[int]("name"); err == nil || !strings.Contains(err.Error(), "field 'name': cannot convert string to int") {
		t.Errorf("name as int: %v", err)
	}
	if m, err := tab.Field[*Table]("missing"); err != nil || m != nil {
		t.Errorf("missing as *Table: %v %v", m, err)
	}
	hosts, _ := tab.Field[*Table]("hosts")
	var items []string
	for _, v := range hosts.Items() {
		items = append(items, v.String())
	}
	if strings.Join(items, ",") != "a" {
		t.Errorf("items %v", items)
	}
}

func TestConvert(t *testing.T) {
	_, err := New(Convert(
		func(a netip.Addr) Value { return String(a.String()) },
		func(v Value) (netip.Addr, error) {
			s, ok := v.Str()
			if !ok {
				return netip.Addr{}, errors.New("not a string")
			}
			return netip.ParseAddr(s)
		}))
	if err != nil {
		t.Fatal(err)
	}
	addr := netip.MustParseAddr("10.0.0.1")
	if v := ValueOf(addr); v.String() != "10.0.0.1" {
		t.Errorf("ValueOf(addr) = %v", v)
	}
	if a, err := String("::1").As[netip.Addr](); err != nil || a.String() != "::1" {
		t.Errorf("As[netip.Addr]: %v %v", a, err)
	}
	if _, err := String("nope").As[netip.Addr](); err == nil || !strings.Contains(err.Error(), "cannot convert string to netip.Addr") {
		t.Errorf("bad address: %v", err)
	}
	if v := ValueOf([]netip.Addr{addr}); (*Table)(v.p).Get(Int(1)).String() != "10.0.0.1" {
		t.Error("conversion inside a slice")
	}
	if _, err := New(Convert(func(netip.Addr) Value { return Value{} }, func(Value) (netip.Addr, error) { return netip.Addr{}, nil })); err == nil {
		t.Error("a second conversion for the same type was accepted")
	}
}

func TestBindTimeChecks(t *testing.T) {
	if _, _, err := encoderFor[chan int](); err == nil {
		t.Error("encoder for chan int")
	}
	if _, _, err := encoderFor[[]func()](); err == nil || !strings.Contains(err.Error(), "needs an adapter") {
		t.Errorf("encoder for []func(): %v", err)
	}
	if _, err := decoderFor[map[string]chan int](); err == nil {
		t.Error("decoder for map[string]chan int")
	}
	var be *BindError
	if _, err := decoderFor[func()](); !errors.As(err, &be) || be.Type != reflect.TypeFor[func()]() {
		t.Errorf("decoder for func(): %v", err)
	}
	for _, f := range []func() error{
		func() error { _, _, err := encoderFor[convConfig](); return err },
		func() error { _, _, err := encoderFor[*convConfig](); return err },
		func() error { _, _, err := encoderFor[map[string][]int](); return err },
		func() error { _, err := decoderFor[convConfig](); return err },
		func() error { _, err := decoderFor[Pair[int, string]](); return err },
	} {
		if err := f(); err != nil {
			t.Error(err)
		}
	}
	d, err := resultDecoderFor[Pair[*Table, string]]()
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.decode([]Value{{}, String("not found")})
	if err != nil || p.First != nil || p.Second != "not found" {
		t.Errorf("pair: %+v %v", p, err)
	}
	d2, _ := resultDecoderFor[int64]()
	if _, err := d2.decode(nil); err == nil {
		t.Error("no results decoded into int64")
	}
	d3, _ := resultDecoderFor[struct{}]()
	if _, err := d3.decode([]Value{String("x")}); err != nil {
		t.Error(err)
	}
}
