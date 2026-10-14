package glua

import (
	"fmt"
	"math"
	"reflect"
)

// maxConvDepth is the fixed cap on the nesting of a Go-Lua conversion.
const maxConvDepth = 64

// toValue converts a Go value to a Lua value by the built-in rules: scalars and strings,
// Values, tables, GoFuncs, pointers to structs as userdata, and slices and maps as new
// tables.
func toValue(v any) (Value, error) {
	return toValueDepth(reflect.ValueOf(v), 0)
}

func toValueDepth(rv reflect.Value, depth int) (Value, error) {
	if depth > maxConvDepth {
		return Value{}, fmt.Errorf("nested more than %d levels deep", maxConvDepth)
	}
	if !rv.IsValid() {
		return Value{}, nil
	}
	if rv.CanInterface() {
		switch x := rv.Interface().(type) {
		case Value:
			return x, nil
		case *Table:
			if x == nil {
				return Value{}, nil
			}
			return tableValue(x), nil
		case GoFunc:
			if x == nil {
				return Value{}, nil
			}
			return goFnValue(&goFunc{name: "?", f: x}), nil
		}
	}
	switch rv.Kind() {
	case reflect.Bool:
		return Bool(rv.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return Int(rv.Int()), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		u := rv.Uint()
		if u > math.MaxInt64 {
			return Value{}, fmt.Errorf("%d does not fit in a Lua integer", u)
		}
		return Int(int64(u)), nil
	case reflect.Float32, reflect.Float64:
		return Float(rv.Float()), nil
	case reflect.String:
		return String(rv.String()), nil
	case reflect.Interface:
		return toValueDepth(rv.Elem(), depth+1)
	case reflect.Pointer:
		if rv.IsNil() {
			return Value{}, nil
		}
		if rv.Type().Elem().Kind() == reflect.Struct {
			id := bindType(rv.Type().Elem())
			return Value{p: rv.UnsafePointer(), n: kindUser<<56 | id}, nil
		}
		return toValueDepth(rv.Elem(), depth+1)
	case reflect.Slice, reflect.Array:
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			return Value{}, nil
		}
		t := NewTable(rv.Len(), 0)
		for i := range rv.Len() {
			x, err := toValueDepth(rv.Index(i), depth+1)
			if err != nil {
				return Value{}, err
			}
			t.arr[i] = x
		}
		return tableValue(t), nil
	case reflect.Map:
		if rv.IsNil() {
			return Value{}, nil
		}
		t := NewTable(0, rv.Len())
		for it := rv.MapRange(); it.Next(); {
			k, err := toValueDepth(it.Key(), depth+1)
			if err != nil {
				return Value{}, err
			}
			x, err := toValueDepth(it.Value(), depth+1)
			if err != nil {
				return Value{}, err
			}
			nk, ok := normKey(k)
			if !ok {
				return Value{}, fmt.Errorf("map key %v is not a valid table key", it.Key())
			}
			t.set(nk, x)
		}
		return tableValue(t), nil
	}
	return Value{}, fmt.Errorf("unsupported type %s", rv.Type())
}
