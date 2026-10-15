package luajson_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	lua "github.com/folbricht/glua"
	"github.com/folbricht/glua/luajson"
)

// eval runs src on a Runtime with the json module and returns its first result.
func eval(t *testing.T, src string, opts ...lua.Option) (lua.Value, error) {
	t.Helper()
	opts = append([]lua.Option{luajson.Module("json")}, opts...)
	rt := lua.MustNew(opts...)
	s, err := rt.Load(context.Background(), "t.lua", "function f() "+src+" end")
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.Func0[lua.Value]("f")
	if err != nil {
		t.Fatal(err)
	}
	return f(context.Background())
}

func TestEncode(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`return json.encode({1, 2, "three", true, false})`, `[1,2,"three",true,false]`},
		{`return json.encode({b = 1, a = {x = 1.5}, c = "é"})`, `{"a":{"x":1.5},"b":1,"c":"é"}`},
		{`return json.encode({})`, `{}`},
		{`return json.encode({[1] = "a", [3] = "c"})`, `{"1":"a","3":"c"}`},
		{`return json.encode({1, 2, x = 3})`, `{"1":1,"2":2,"x":3}`},
		{`return json.encode("a\"b\n")`, `"a\"b\n"`},
		{`return json.encode(nil)`, `null`},
		{`return json.encode(9007199254740993)`, `9007199254740993`},
		{`return json.encode(1e300)`, `1e+300`},
		{`return json.encode(-0.0)`, `0`},
		{`return json.encode({-0.0, json.decode("-0")})`, `[0,0]`},
		{`return json.encode(json.decode("-0.0"))`, `0`},
		{`return json.encode({a = {1, 2}}, "  ")`, "{\n  \"a\": [\n    1,\n    2\n  ]\n}"},
	} {
		v, err := eval(t, tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.src, err)
			continue
		}
		if got, _ := v.Str(); got != tc.want {
			t.Errorf("%s:\n got %s\nwant %s", tc.src, got, tc.want)
		}
	}
}

func TestEncodeErrors(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`return json.encode(0/0)`, "NaN cannot be encoded"},
		{`return json.encode({x = 1/0})`, "x: +Inf cannot be encoded"},
		{`return json.encode(print)`, "a function value cannot be encoded"},
		{`local t = {} t.self = t return json.encode(t)`, "self: the table contains itself"},
		{`return json.encode({[true] = 1})`, "a boolean key cannot be a JSON object name"},
		{`return json.encode({[1.5] = 1})`, "a float key cannot be a JSON object name"},
		{`return json.encode("\255")`, "string is not valid UTF-8"},
		{`local t = {} for i = 1, 70 do t = {t} end return json.encode(t)`, "nested more than 64 levels deep"},
		{`return json.encode({[1] = 1, ["1"] = 2})`, `both name the member "1"`},
	} {
		_, err := eval(t, "local ok, e = pcall(json.encode, nil) "+tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.src, err, tc.want)
		}
	}
	// a table reached twice without a cycle is fine
	if v, err := eval(t, `local s = {1} return json.encode({s, s})`); err != nil || mustStr(v) != `[[1],[1]]` {
		t.Errorf("shared table: %v %v", v, err)
	}
}

func mustStr(v lua.Value) string {
	s, _ := v.Str()
	return s
}

func TestDecode(t *testing.T) {
	v, err := eval(t, `
		local d = json.decode('{"a": [1, 2.5, "x", true, null, {"b": false}], "n": -7, "big": 1e400, "i": 9223372036854775808}')
		return string.format("%s %s %s %s %s %s %s %s %s %s", math.type(d.a[1]), d.a[2], d.a[3], d.a[4], tostring(d.a[5]),
			tostring(d.a[6].b), d.n, d.big, math.type(d.i), #d.a)`)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustStr(v); got != "integer 2.5 x true nil false -7 inf float 6" {
		t.Errorf("got %q", got)
	}
	for _, tc := range []struct{ src, want string }{
		{`return json.decode('{"a": ')`, "unexpected EOF"},
		{`return json.decode('[1,]')`, "invalid character"},
		{`return json.decode('{"a": 1, "a": 2}')`, "duplicate"},
		{`return json.decode('1 2')`, "data after the JSON value"},
		{`return json.decode(string.rep("[", 70) .. string.rep("]", 70))`, "nested more than 64 levels deep"},
		{`return json.decode()`, "bad argument #1 to 'decode'"},
	} {
		_, err := eval(t, tc.src)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.src, err, tc.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	in := `{"cfg":{"hosts":["a","b"],"port":8080,"ratio":0.25,"tls":true},"name":"svc"}`
	v, err := luajson.Unmarshal([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	out, err := luajson.Marshal(v)
	if err != nil || string(out) != in {
		t.Errorf("got %s, %v", out, err)
	}
	tbl, _ := v.As[*lua.Table]()
	type cfg struct {
		Hosts []string
		Port  int
		Ratio float64
		TLS   bool `lua:"tls"`
	}
	c, err := tbl.Field[cfg]("cfg")
	if err != nil || c.Port != 8080 || len(c.Hosts) != 2 || !c.TLS {
		t.Errorf("decoded into a struct: %+v %v", c, err)
	}
	if _, err := luajson.Unmarshal([]byte(`{"a":`)); err == nil || !strings.HasPrefix(err.Error(), "luajson: ") {
		t.Errorf("syntax error: %v", err)
	}
}

func TestDecodeChargesBudget(t *testing.T) {
	big := `"[" .. string.rep("[1,2,3],", 20000) .. "[]]"`
	_, err := eval(t, "local s = "+big+" for i = 1, 20 do json.decode(s) end", lua.Limits{AllocBytes: 4 << 20})
	if !errors.Is(err, lua.ErrAlloc) {
		t.Errorf("decode was not charged: %v", err)
	}
	_, err = eval(t, `local t = {} for i = 1, 20000 do t[i] = "abcdefgh" end for i = 1, 50 do json.encode(t) end`,
		lua.Limits{AllocBytes: 4 << 20})
	if !errors.Is(err, lua.ErrAlloc) {
		t.Errorf("encode was not charged: %v", err)
	}
}

// An indent jsontext cannot take is an error, not a panic that takes down the host.
func TestEncodeBadIndent(t *testing.T) {
	v, err := eval(t, `local ok, e = pcall(json.encode, {1}, "xx") return tostring(ok) .. " " .. e`)
	if err != nil || mustStr(v) != "false the indent may hold only spaces and tabs" {
		t.Errorf("got %v, %v", v, err)
	}
	if _, err := luajson.Marshal(lua.Int(1), luajson.Options{Indent: "-"}); err == nil {
		t.Error("Marshal accepted a bad indent")
	}
}

// A table is an array only when its keys are exactly 1 to n: holes with other keys that make
// up the count must not drop values.
func TestEncodeHoles(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`return json.encode({1, nil, 3, x = "a"})`, `{"1":1,"3":3,"x":"a"}`},
		{`local t = {1, 2, 3} t[5] = 5 t[2] = nil return json.encode(t)`, `{"1":1,"3":3,"5":5}`},
		{`return json.encode({[1] = "a", [2] = "b", [2.5] = "c"})`, "error"},
	} {
		v, err := eval(t, tc.src)
		got := mustStr(v)
		if err != nil {
			got = "error"
		}
		if got != tc.want {
			t.Errorf("%s: got %q (%v), want %s", tc.src, got, err, tc.want)
		}
	}
}

// What encode and decode make is charged once: they charge as they work, so the call path
// does not charge their results again.
func TestChargedOnce(t *testing.T) {
	var used int64
	rt := lua.MustNew(luajson.Module("json"), lua.Observe(func(s lua.CallStats) { used = s.AllocBytes }))
	s, err := rt.Load(context.Background(), "t.lua", `
		local t = {} for i = 1, 2000 do t[i] = "abcdefghij" end
		function enc() return #json.encode(t) end
		function base() return 0 end`)
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := s.Func0[int64]("enc")
	n, err := enc(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if used < n || used > n+n/2 {
		t.Errorf("encoding %d bytes charged %d", n, used)
	}
}
