package glua

import (
	"context"
	"reflect"
	"sync"
	"unsafe"
)

// engine stands in for the Runtime until milestone 2: it owns the executor pool. Executors
// hold no script state, so one pool serves every function and rule.
type engine struct {
	pool sync.Pool
}

func newEngine() *engine {
	e := &engine{}
	e.pool.New = func() any { return newExecutor() }
	return e
}

// encoderFor picks how an argument of type T becomes a Value. It runs once per argument
// type, when a function is bound, and reports a type that cannot be converted. byValue
// reports a struct passed by value, which goes through a slot of the executor instead.
func encoderFor[T any]() (enc func(T) (Value, error), byValue bool, err error) {
	var z T
	switch any(z).(type) { // built-in types: a concrete function, no reflection
	case int64:
		return any(encInt).(func(T) (Value, error)), false, nil
	case int:
		return any(encIntN).(func(T) (Value, error)), false, nil
	case float64:
		return any(encFloat).(func(T) (Value, error)), false, nil
	case string:
		return any(encString).(func(T) (Value, error)), false, nil
	case bool:
		return any(encBool).(func(T) (Value, error)), false, nil
	case Value:
		return any(encValue).(func(T) (Value, error)), false, nil
	}
	rt := reflect.TypeFor[T]()
	if converterFor(rt) != nil || rt == durationType || rt == timeType {
		return encodeAny[T], false, nil
	}
	switch rt.Kind() { // named types such as `type UserID int64` miss the switch above: go by kind
	case reflect.Int64:
		return func(v T) (Value, error) { return Int(*(*int64)(unsafe.Pointer(&v))), nil }, false, nil
	case reflect.Int:
		return func(v T) (Value, error) { return Int(int64(*(*int)(unsafe.Pointer(&v)))), nil }, false, nil
	case reflect.Float64:
		return func(v T) (Value, error) { return Float(*(*float64)(unsafe.Pointer(&v))), nil }, false, nil
	case reflect.String:
		return func(v T) (Value, error) { return String(*(*string)(unsafe.Pointer(&v))), nil }, false, nil
	case reflect.Bool:
		return func(v T) (Value, error) { return Bool(*(*bool)(unsafe.Pointer(&v))), nil }, false, nil
	case reflect.Struct:
		bindType(rt)
		return nil, true, nil
	case reflect.Pointer:
		if rt.Elem().Kind() == reflect.Struct && converterFor(rt.Elem()) == nil {
			bits := uint64(kindUser)<<56 | bindType(rt.Elem()) // plans the struct now, not on first use
			return func(v T) (Value, error) {
				p := *(*unsafe.Pointer)(unsafe.Pointer(&v))
				if p == nil {
					return Value{}, nil // invariant 1: p == nil exactly when the Value is {nil, 0}
				}
				return Value{p: p, n: bits}, nil
			}, false, nil
		}
	}
	if err := checkEncodable(rt, 0, map[reflect.Type]bool{}); err != nil {
		return nil, false, &BindError{Type: rt, Reason: err.Error()}
	}
	return encodeAny[T], false, nil
}

func encInt(v int64) (Value, error)     { return Int(v), nil }
func encIntN(v int) (Value, error)      { return Int(int64(v)), nil }
func encFloat(v float64) (Value, error) { return Float(v), nil }
func encString(v string) (Value, error) { return String(v), nil }
func encBool(v bool) (Value, error)     { return Bool(v), nil }
func encValue(v Value) (Value, error)   { return v, nil }

// encodeAny converts v by reflection.
func encodeAny[T any](v T) (Value, error) {
	val, err := encodeReflect(reflect.ValueOf(&v).Elem(), 0)
	if err != nil {
		return Value{}, &convErr{msg: err.Error()}
	}
	return val, nil
}

// decoderFor picks how a Value becomes an R, once, when a function is bound, and reports a
// type that no value can convert to.
func decoderFor[R any]() (func(Value) (R, error), error) {
	var z R
	switch any(z).(type) {
	case int64:
		return any(decInt).(func(Value) (R, error)), nil
	case int:
		return any(decIntN).(func(Value) (R, error)), nil
	case float64:
		return any(decFloat).(func(Value) (R, error)), nil
	case string:
		return any(decString).(func(Value) (R, error)), nil
	case bool:
		return any(decBool).(func(Value) (R, error)), nil
	case Value:
		return any(encValue).(func(Value) (R, error)), nil
	case struct{}:
		return func(Value) (R, error) { return z, nil }, nil
	}
	rt := reflect.TypeFor[R]()
	if err := checkDecodable(rt, 0, map[reflect.Type]bool{}); err != nil {
		return nil, &BindError{Type: rt, Reason: err.Error()}
	}
	return func(v Value) (r R, err error) {
		err = decodeReflect(v, reflect.ValueOf(&r).Elem(), "", 0)
		return r, err
	}, nil
}

// resultDecoder decodes the results of a call into R: struct{} discards them, Pair takes the
// first two, and anything else the first.
type resultDecoder[R any] struct {
	one    func(Value) (R, error)
	pair   func(a, b Value) (any, error)
	scalar scalarKind // a built-in scalar, decoded inline
}

func resultDecoderFor[R any]() (resultDecoder[R], error) {
	if p, ok := any(*new(R)).(pairType); ok {
		dec, err := p.pairDecoder()
		return resultDecoder[R]{pair: dec}, err
	}
	one, err := decoderFor[R]()
	return resultDecoder[R]{one: one, scalar: scalarOf[R]()}, err
}

// decode decodes the results res.
func (d *resultDecoder[R]) decode(res []Value) (r R, err error) {
	var a, b Value
	if len(res) > 0 {
		a = res[0]
	}
	p := unsafe.Pointer(&r)
	switch d.scalar { // the scalar kind fixes R, so the casts are exact
	case scalarInt64:
		if i, ok := a.Int(); ok {
			*(*int64)(p) = i
			return r, nil
		}
	case scalarBool:
		*(*bool)(p) = a.truthy()
		return r, nil
	case scalarString:
		if a.isString() {
			*(*string)(p) = a.str()
			return r, nil
		}
	case scalarFloat64:
		if f, ok := a.Float(); ok {
			*(*float64)(p) = f
			return r, nil
		}
	}
	if d.pair != nil {
		if len(res) > 1 {
			b = res[1]
		}
		p, err := d.pair(a, b)
		if err != nil {
			var r R
			return r, err
		}
		return p.(R), nil
	}
	return d.one(a)
}

// Pair as a result type decodes the first two results, as in the idiom return nil, "message".
type Pair[A, B any] struct {
	First  A
	Second B
}

// pairType is implemented by every Pair.
type pairType interface {
	pairDecoder() (func(a, b Value) (any, error), error)
}

func (Pair[A, B]) pairDecoder() (func(a, b Value) (any, error), error) {
	da, err := decoderFor[A]()
	if err != nil {
		return nil, err
	}
	db, err := decoderFor[B]()
	if err != nil {
		return nil, err
	}
	return func(a, b Value) (any, error) {
		var p Pair[A, B]
		var err error
		if p.First, err = da(a); err != nil {
			return nil, err
		}
		if p.Second, err = db(b); err != nil {
			return nil, err
		}
		return p, nil
	}, nil
}

func decInt(v Value) (int64, error) {
	if i, ok := v.Int(); ok {
		return i, nil
	}
	return 0, &convErr{msg: "cannot convert " + typeName(v) + " to int64"}
}

func decIntN(v Value) (int, error) {
	i, err := decInt(v)
	return int(i), err
}

func decFloat(v Value) (float64, error) {
	if f, ok := v.Float(); ok {
		return f, nil
	}
	return 0, &convErr{msg: "cannot convert " + typeName(v) + " to float64"}
}

func decString(v Value) (string, error) {
	if s, ok := v.Str(); ok {
		return s, nil
	}
	return "", &convErr{msg: "cannot convert " + typeName(v) + " to string"}
}

func decBool(v Value) (bool, error) { return v.truthy(), nil }

// bindFunc1 returns a typed Go function backed by cl, run on executors from e's pool.
// It does the five steps of a typed call: borrow, encode, run, decode, clear.
func bindFunc1[A, R any](e *engine, cl *Closure) func(context.Context, A) (R, error) {
	enc, byValue, _ := encoderFor[A]() // chosen once, at bind time
	dec, _ := decoderFor[R]()
	if byValue {
		id := bindType(reflect.TypeFor[A]())
		return func(ctx context.Context, a A) (r R, err error) {
			ex := e.pool.Get().(*executor)
			slot := slotFor[A](ex, id, 0)
			*slot = a // copy in: the script sees the slot, flagged readonly and callscoped
			ex.stack[1] = Value{p: unsafe.Pointer(slot), n: kindUser<<56 | flagReadonly | flagCallscoped | id}
			if err = ex.call(ctx, cl, 0, 1); err == nil {
				r, err = dec(ex.stack[0])
			}
			ex.reset()
			clear(unsafe.Slice(slot, 1)) // nothing of this call stays reachable from the pool
			release(&e.pool, ex)
			return r, err
		}
	}
	return func(ctx context.Context, a A) (r R, err error) {
		ex := e.pool.Get().(*executor) // borrow
		ex.stack[1], err = enc(a)      // encode
		if err == nil {
			err = ex.call(ctx, cl, 0, 1) // run
		}
		if err == nil {
			r, err = dec(ex.stack[0]) // decode
		}
		ex.reset() // clear
		release(&e.pool, ex)
		return r, err
	}
}
