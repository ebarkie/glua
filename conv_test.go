package glua

import (
	"math"
	"testing"
)

func TestStrToNumber(t *testing.T) {
	tests := []struct {
		s    string
		want Value
		ok   bool
	}{
		{"10", Int(10), true}, {"  10  ", Int(10), true}, {"-10", Int(-10), true}, {"+10", Int(10), true},
		{"0x10", Int(16), true}, {"-0x10", Int(-16), true}, {"0xffffffffffffffff", Int(-1), true},
		{"1e2", Float(100), true}, {".5", Float(0.5), true}, {"0x1p4", Float(16), true}, {"3.", Float(3), true},
		{"9223372036854775808", Float(9223372036854775808), true}, {"-9223372036854775808", Float(-9223372036854775808), true},
		{"", Value{}, false}, {" ", Value{}, false}, {"1e", Value{}, false}, {"0x", Value{}, false},
		{"inf", Value{}, false}, {"nan", Value{}, false}, {"1 2", Value{}, false}, {"- 1", Value{}, false},
		{"1\x00", Value{}, false}, {"++1", Value{}, false}, {"1_000", Value{}, false}, {"0b101", Value{}, false},
	}
	for _, tt := range tests {
		got, ok := strToNumber(tt.s)
		if ok != tt.ok || (ok && !same(&got, &tt.want)) {
			t.Errorf("%q: got %v (%v) %v, want %v (%v) %v", tt.s, got, got.Kind(), ok, tt.want, tt.want.Kind(), tt.ok)
		}
	}
	// "-9223372036854775808" is a float in C Lua too: the digits overflow before the sign applies
	if v, _ := strToNumber("-9223372036854775808"); v.Kind() != KindFloat {
		t.Error("expected a float")
	}
}

func TestStrToIntBase(t *testing.T) {
	tests := []struct {
		s    string
		base int
		want int64
		ok   bool
	}{
		{"ff", 16, 255, true}, {"FF", 16, 255, true}, {"  -1010 ", 2, -10, true}, {"zz", 36, 1295, true},
		{"10", 8, 8, true}, {"8", 8, 0, false}, {"1.5", 10, 0, false}, {"", 10, 0, false}, {"-", 10, 0, false},
		{"ffffffffffffffff", 16, -1, true}, {"7fffffffffffffff", 16, math.MaxInt64, true},
	}
	for _, tt := range tests {
		got, ok := strToIntBase(tt.s, tt.base)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("%q base %d: got %d %v, want %d %v", tt.s, tt.base, got, ok, tt.want, tt.ok)
		}
	}
}
