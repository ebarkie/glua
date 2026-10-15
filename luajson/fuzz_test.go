package luajson_test

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"errors"
	"math"
	"strings"
	"testing"

	lua "github.com/folbricht/glua"
	"github.com/folbricht/glua/luajson"
)

// FuzzDecode decodes any input. It must not panic; what decodes must encode again, unless a
// number was too large for a float; and once encoded, decoding and encoding again must give the
// same text. A script decoding the same input under tight limits must not panic either.
func FuzzDecode(f *testing.F) {
	for _, s := range []string{`{"a":[1,2.5,"x",true,null,{"b":false}],"n":-7}`, `[]`, `{}`, `"é😀"`,
		`1e400`, `-0`, `9223372036854775808`, `[[[[[[[[1]]]]]]]]`, `{"a":1,"a":2}`, `[1,]`, `"\x00"`, `{"1":1,"2":2}`} {
		f.Add([]byte(s))
	}
	rt := lua.MustNew(luajson.Module("json"), lua.Limits{Steps: 100_000, AllocBytes: 1 << 20})
	s, err := rt.Load(context.Background(), "f.lua", `function f(s) local ok, v = pcall(json.decode, s) if ok then return (pcall(json.encode, v)) end end`)
	if err != nil {
		f.Fatal(err)
	}
	decode, err := s.Func1[string, lua.Value]("f")
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if _, err := decode(context.Background(), string(data)); err != nil {
			if _, ok := errors.AsType[*lua.PanicError](err); ok {
				t.Fatalf("the script panicked: %v", err)
			}
		}
		v, err := luajson.Unmarshal(data)
		if err != nil {
			return
		}
		out, err := luajson.Marshal(v)
		if err != nil {
			if !strings.Contains(err.Error(), "Inf cannot be encoded") {
				t.Fatalf("decoded %q but cannot encode it: %v", data, err)
			}
			return
		}
		if !jsontext.Value(out).IsValid() {
			t.Fatalf("encoded invalid JSON %q", out)
		}
		v2, err := luajson.Unmarshal(out)
		if err != nil {
			t.Fatalf("cannot decode what was encoded, %q: %v", out, err)
		}
		out2, err := luajson.Marshal(v2)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("not stable:\n%s\n%s (%v)", out, out2, err)
		}
	})
}

// FuzzEncode builds a table from the input, with keys and values of every type, holes, nesting
// and shared subtables, and encodes it. Encoding must not panic, and what it gives must be
// valid JSON that decodes to the same structure, encoded the same way again.
func FuzzEncode(f *testing.F) {
	for _, s := range []string{"\x01\x02\x03", "\x10a\x11\x12b", "\x20\x01\x21\x02\x30", "\x05\x05\x05\x40\x41"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		root := lua.NewTable(0, 0)
		stack := []*lua.Table{root}
		next := 1
		val := func(b byte, i int) lua.Value {
			switch b % 9 {
			case 0:
				return lua.Int(int64(b) - 128)
			case 1:
				return lua.Float(float64(b) / 7)
			case 2:
				return lua.Float([]float64{math.Inf(1), math.NaN(), math.Copysign(0, -1), 1e300, 5e-324}[int(b)%5])
			case 3:
				return lua.String(string(data[i:min(i+int(b%5), len(data))]))
			case 4:
				return lua.Bool(b&1 == 1)
			case 5:
				return lua.Int(math.MinInt64 + int64(b))
			case 6:
				return lua.ValueOf(stack[int(b)%len(stack)]) // a table on the path: a cycle
			case 7:
				return lua.String("é€😀")
			}
			return lua.Value{}
		}
		for i := 0; i < len(data) && len(stack) < 80; i++ {
			b := data[i]
			top := stack[len(stack)-1]
			switch b >> 5 {
			case 0, 1: // append
				top.Set(lua.Int(int64(next)), val(b, i))
				next++
			case 2: // a key past the end: a hole
				top.Set(lua.Int(int64(next+1+int(b%3))), val(b, i))
			case 3: // a string key
				top.Set(lua.String(string(rune('a'+b%6))), val(b, i))
			case 4: // another key type
				k := []lua.Value{lua.Float(1.5), lua.Bool(true), lua.Int(-1), lua.Int(0), lua.String("1"), lua.Float(2.0)}[b%6]
				top.Set(k, val(b, i))
			case 5: // open a nested table
				nt := lua.NewTable(0, 0)
				top.Set(lua.Int(int64(next)), lua.ValueOf(nt))
				next = 1
				stack = append(stack, nt)
			case 6: // close it
				if len(stack) > 1 {
					stack = stack[:len(stack)-1]
					next = stack[len(stack)-1].Len() + 1
				}
			default: // delete a key
				top.Set(lua.Int(int64(b%4+1)), lua.Value{})
			}
		}
		for _, indent := range []string{"", "  "} {
			out, err := luajson.Marshal(lua.ValueOf(root), luajson.Options{Indent: indent})
			if err != nil {
				continue
			}
			if !jsontext.Value(out).IsValid() {
				t.Fatalf("encoded invalid JSON %q", out)
			}
			v, err := luajson.Unmarshal(out)
			if err != nil {
				t.Fatalf("cannot decode %q: %v", out, err)
			}
			out2, err := luajson.Marshal(v, luajson.Options{Indent: indent})
			if err != nil || !bytes.Equal(out, out2) {
				t.Fatalf("does not round-trip:\n%s\n%s (%v)", out, out2, err)
			}
		}
	})
}
