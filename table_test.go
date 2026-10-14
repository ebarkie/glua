package glua

import (
	"math"
	"math/rand/v2"
	"testing"
)

// A table must behave like a map from normalised keys, whatever mix of operations it sees.
func TestTableAgainstModel(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for round := range 200 {
		tbl := NewTable(r.IntN(4), r.IntN(4))
		model := map[any]int64{}
		key := func() (Value, any) {
			switch r.IntN(4) {
			case 0:
				i := int64(r.IntN(40) - 5)
				return Int(i), i
			case 1:
				i := int64(r.IntN(40))
				return Float(float64(i)), i // integral floats are the same key as the integer
			case 2:
				f := float64(r.IntN(10)) + 0.5
				return Float(f), f
			default:
				s := string(rune('a' + r.IntN(20)))
				return String(s), s
			}
		}
		for range 300 {
			k, mk := key()
			if r.IntN(3) == 0 {
				tbl.Set(k, Value{})
				delete(model, mk)
			} else {
				v := r.Int64N(1000) + 1
				tbl.Set(k, Int(v))
				model[mk] = v
			}
		}
		for mk, want := range model {
			var k Value
			switch mk := mk.(type) {
			case int64:
				k = Int(mk)
			case float64:
				k = Float(mk)
			case string:
				k = String(mk)
			}
			if got, _ := tbl.Get(k).Int(); got != want {
				t.Fatalf("round %d: t[%v] = %v, want %d", round, k, tbl.Get(k), want)
			}
		}
		n := 0
		for k, v := range tbl.All() {
			n++
			if v.IsNil() || k.IsNil() {
				t.Fatalf("round %d: traversal yields %v = %v", round, k, v)
			}
		}
		if n != len(model) {
			t.Fatalf("round %d: traversal yields %d pairs, want %d", round, n, len(model))
		}
		checkBorder(t, tbl)
	}
}

func checkBorder(t *testing.T, tbl *Table) {
	t.Helper()
	n := tbl.Len()
	if n < 0 || (n > 0 && tbl.Get(Int(int64(n))).IsNil()) || !tbl.Get(Int(int64(n)+1)).IsNil() {
		t.Fatalf("Len() = %d is not a border", n)
	}
}

func TestTableBorders(t *testing.T) {
	tbl := NewTable(0, 0)
	if tbl.Len() != 0 {
		t.Fatal("empty table")
	}
	for i := 1; i <= 100; i++ {
		tbl.Set(Int(int64(i)), Bool(true))
	}
	if tbl.Len() != 100 {
		t.Fatalf("Len() = %d", tbl.Len())
	}
	tbl.Set(Int(100), Value{})
	tbl.Set(Int(50), Value{})
	checkBorder(t, tbl)
	// a sequence that lives in the hash part: filled backwards
	h := NewTable(0, 0)
	for i := 1000; i >= 2; i-- {
		h.Set(Int(int64(i)), Int(int64(i)))
	}
	checkBorder(t, h) // 0 or 1000: with t[1] nil, both are borders
	h.Set(Int(1), Int(1))
	if h.Len() != 1000 {
		t.Fatalf("Len() = %d, want 1000", h.Len())
	}
	if len(h.arr) < 1000 {
		t.Errorf("array part has %d items after t[1] was set; the hash keys should have moved", len(h.arr))
	}
	for _, nd := range h.nodes {
		if !nd.v.IsNil() {
			t.Errorf("key %v stayed in the hash part", nd.k)
		}
	}
}

// Setting fields to nil during a traversal is allowed and must not disturb it.
func TestNextWithDeletion(t *testing.T) {
	tbl := NewTable(0, 0)
	for i := range 50 {
		tbl.Set(String(string(rune('A'+i))), Int(int64(i)))
		tbl.Set(Int(int64(i+1)), Int(int64(i)))
	}
	seen := 0
	k := Value{}
	for {
		nk, _, ok := tbl.Next(k)
		if !ok {
			t.Fatalf("Next(%v) failed", k)
		}
		if nk.IsNil() {
			break
		}
		seen++
		tbl.Set(nk, Value{}) // delete as we go
		k = nk
	}
	if seen != 100 {
		t.Fatalf("saw %d keys, want 100", seen)
	}
	if k, _, _ := tbl.Next(Value{}); !k.IsNil() {
		t.Fatalf("table not empty: %v", k)
	}
	if _, _, ok := tbl.Next(String("missing")); ok {
		t.Fatal("Next accepted a key that is not in the table")
	}
}

func TestTableKeys(t *testing.T) {
	tbl := NewTable(0, 0)
	tbl.Set(Float(2), String("two"))
	if s, _ := tbl.Get(Int(2)).Str(); s != "two" {
		t.Fatal("2.0 and 2 are different keys")
	}
	tbl.Set(Float(math.Copysign(0, -1)), String("zero"))
	if s, _ := tbl.Get(Int(0)).Str(); s != "zero" {
		t.Fatal("-0.0 and 0 are different keys")
	}
	if !tbl.Get(Float(math.NaN())).IsNil() || !tbl.Get(Value{}).IsNil() {
		t.Fatal("NaN or nil key reads a value")
	}
	for _, k := range []Value{{}, Float(math.NaN())} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Set(%v) did not panic", k)
				}
			}()
			tbl.Set(k, Int(1))
		}()
	}
	// same string content behind different pointers is the same key
	a := []byte("key")
	tbl.Set(String(string(a)), Int(7))
	if v, _ := tbl.Get(String("key")).Int(); v != 7 {
		t.Fatal("string keys compare by pointer")
	}
}

// Deleted keys stay as tombstones until a rehash, which drops them.
func TestTombstones(t *testing.T) {
	tbl := NewTable(0, 4)
	for i := range 1000 {
		k := String(string(rune(0x100 + i)))
		tbl.Set(k, Int(1))
		tbl.Set(k, Value{})
	}
	if len(tbl.nodes) > 64 {
		t.Fatalf("hash part grew to %d nodes for a table that never holds more than one key", len(tbl.nodes))
	}
}

func TestMetamethodCache(t *testing.T) {
	mt := NewTable(0, 4)
	if !mt.metaGet("__index").IsNil() || mt.flags&tabNoIndex == 0 {
		t.Fatal("absence of __index not cached")
	}
	mt.Set(String("__index"), Int(1))
	if mt.flags&tabAbsent != 0 {
		t.Fatal("writing a __ key did not clear the cache")
	}
	if v, _ := mt.metaGet("__index").Int(); v != 1 {
		t.Fatal("__index not found after it was set")
	}
	frozen := NewTable(0, 1)
	frozen.flags |= tabFrozen
	frozen.metaGet("__len")
	if frozen.flags&tabAbsent != 0 {
		t.Fatal("reading a frozen metatable wrote its flags")
	}
}
