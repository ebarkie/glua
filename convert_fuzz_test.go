package glua

import (
	"bytes"
	"math"
	"reflect"
	"testing"
	"time"
)

type fuzzRecord struct {
	Name  string
	Count int16
	Ratio float64
	Tags  []string
	Seen  map[string]uint8
	Next  *fuzzRecord
}

// roundTrip converts x to a Value and back, and reports what came back, or why it failed.
func roundTrip[T any](x T) (T, error) {
	v, err := toValue(x)
	if err != nil {
		var zero T
		return zero, err
	}
	return v.As[T]()
}

// FuzzConvert checks the conversions between Go and Lua values: Go values must come back from
// Lua as they went in, or within a float's precision for durations and times; and converting
// any Lua number to a Go type must give an in-range value or an error, never a wrapped one.
func FuzzConvert(f *testing.F) {
	f.Add(int64(1), 1.5, "a", true, []byte("x"))
	f.Add(int64(math.MinInt64), math.Inf(1), "", false, []byte{})
	f.Add(int64(-1), 1e300, "héllo", true, []byte{0, 255})
	f.Add(int64(1), 1e-9, "x", false, []byte(nil))
	f.Fuzz(func(t *testing.T, i int64, x float64, s string, b bool, raw []byte) {
		check := func(name string, ok bool, got, want any) {
			t.Helper()
			if !ok {
				t.Errorf("%s: got %v, want %v", name, got, want)
			}
		}
		// integers of every size: back as they went in
		if got, err := roundTrip(i); err != nil || got != i {
			check("int64", false, got, i)
		}
		if got, err := roundTrip(int8(i)); err != nil || got != int8(i) {
			check("int8", false, got, int8(i))
		}
		if got, err := roundTrip(uint32(i)); err != nil || got != uint32(i) {
			check("uint32", false, got, uint32(i))
		}
		if u := uint64(i); u > math.MaxInt64 {
			if _, err := toValue(u); err == nil {
				t.Errorf("uint64 %d converted, but no Lua integer holds it", u)
			}
		} else if got, err := roundTrip(u); err != nil || got != u {
			check("uint64", false, got, u)
		}
		// floats: the same bits, or NaN
		if got, err := roundTrip(x); err != nil || math.Float64bits(got) != math.Float64bits(x) && !(math.IsNaN(got) && math.IsNaN(x)) {
			check("float64", false, got, x)
		}
		if got, err := roundTrip(float32(x)); err != nil || got != float32(x) && !math.IsNaN(float64(x)) {
			check("float32", false, got, float32(x))
		}
		// strings and bytes, whatever they hold
		if got, err := roundTrip(s); err != nil || got != s {
			check("string", false, got, s)
		}
		if got, err := roundTrip(raw); err != nil || !bytes.Equal(got, raw) {
			check("[]byte", false, got, raw)
		}
		if got, err := roundTrip(b); err != nil || got != b {
			check("bool", false, got, b)
		}
		// durations travel as float seconds: back within the float's precision
		d := time.Duration(i)
		if got, err := roundTrip(d); err != nil || math.Abs(float64(got-d)) > math.Max(1, math.Abs(float64(d))*0x1p-52) {
			check("Duration", false, got, d)
		}
		// times too, as Unix seconds; whole seconds exactly
		for _, tm := range []time.Time{time.Unix(i%(1<<40), 0), time.Unix(i%(1<<33), int64(x*1e9)%1e9)} {
			got, err := roundTrip(tm)
			limit := math.Max(1, math.Abs(float64(tm.Unix()))*0x1p-52*1e9)
			if err != nil || math.Abs(float64(got.Sub(tm))) > limit {
				check("Time", false, got, tm)
			}
		}
		// structures with nested slices, maps and pointers
		rec := fuzzRecord{Name: s, Count: int16(i), Ratio: x, Tags: []string{s, string(raw)}, Seen: map[string]uint8{s: uint8(i)},
			Next: &fuzzRecord{Name: string(raw), Count: int16(i >> 16)}}
		if math.IsNaN(x) {
			rec.Ratio = 0
		}
		if got, err := roundTrip(rec); err != nil || !reflect.DeepEqual(got, rec) {
			check("struct", false, got, rec)
		}
		// any Lua number to any Go number: in range, or an error
		for _, v := range []Value{Int(i), Float(x)} {
			if n, err := v.As[int8](); err == nil {
				if f, isNum := v.Float(); !isNum || f != float64(n) {
					t.Errorf("%v as int8 gave %d", v, n)
				}
			}
			if n, err := v.As[uint16](); err == nil {
				if f, isNum := v.Float(); !isNum || f != float64(n) {
					t.Errorf("%v as uint16 gave %d", v, n)
				}
			}
			if n, err := v.As[float32](); err == nil {
				if f, _ := v.Float(); !math.IsInf(f, 0) && math.IsInf(float64(n), 0) {
					t.Errorf("%v as float32 overflowed to %v", v, n)
				}
			}
			if d, err := v.As[time.Duration](); err == nil {
				if f, _ := v.Float(); math.Abs(float64(d)-f*1e9) > math.Max(1, math.Abs(f*1e9)*0x1p-52) {
					t.Errorf("%v as Duration gave %v", v, d)
				}
			}
			if tm, err := v.As[time.Time](); err == nil {
				if f, _ := v.Float(); math.Abs(float64(tm.Unix())-f) > math.Max(1, math.Abs(f)*0x1p-52) {
					t.Errorf("%v as Time gave %v (Unix %d)", v, tm, tm.Unix())
				}
			}
		}
	})
}
