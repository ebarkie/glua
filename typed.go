package glua

import (
	"context"
	"fmt"
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
// type, when a function is bound. byValue reports a struct passed by value, which goes
// through a slot of the executor instead.
func encoderFor[T any]() (enc func(T) Value, byValue bool) {
	var z T
	switch any(z).(type) { // built-in types: hand back the concrete constructor itself
	case int64:
		return any(Int).(func(T) Value), false
	case float64:
		return any(Float).(func(T) Value), false
	case string:
		return any(String).(func(T) Value), false
	case bool:
		return any(Bool).(func(T) Value), false
	}
	rt := reflect.TypeFor[T]()
	switch rt.Kind() { // named types such as `type UserID int64` miss the switch above: go by kind
	case reflect.Int64:
		return func(v T) Value { return Int(*(*int64)(unsafe.Pointer(&v))) }, false
	case reflect.String:
		return func(v T) Value { return String(*(*string)(unsafe.Pointer(&v))) }, false
	case reflect.Struct:
		bindType(rt)
		return nil, true
	case reflect.Pointer:
		if rt.Elem().Kind() == reflect.Struct {
			bits := uint64(kindUser)<<56 | bindType(rt.Elem()) // plans the struct now, not on first use
			return func(v T) Value {
				p := *(*unsafe.Pointer)(unsafe.Pointer(&v))
				if p == nil {
					return Value{} // invariant 1: p == nil exactly when the Value is {nil, 0}
				}
				return Value{p: p, n: bits}
			}, false
		}
	}
	panic("glua: unsupported argument type " + rt.String()) // a *BindError in milestone 2
}

// decoderFor picks how a result Value becomes an R, once, when a function is bound.
func decoderFor[R any]() func(Value) (R, error) {
	var z R
	switch any(z).(type) {
	case int64:
		return any(decInt).(func(Value) (R, error))
	case float64:
		return any(decFloat).(func(Value) (R, error))
	case string:
		return any(decString).(func(Value) (R, error))
	case bool:
		return any(decBool).(func(Value) (R, error))
	case struct{}:
		return func(Value) (R, error) { return z, nil }
	}
	panic("glua: unsupported result type " + reflect.TypeFor[R]().String())
}

func decInt(v Value) (int64, error) {
	if i, ok := v.Int(); ok {
		return i, nil
	}
	return 0, fmt.Errorf("glua: cannot convert %s to int64", typeName(v))
}

func decFloat(v Value) (float64, error) {
	if f, ok := v.Float(); ok {
		return f, nil
	}
	return 0, fmt.Errorf("glua: cannot convert %s to float64", typeName(v))
}

func decString(v Value) (string, error) {
	if s, ok := v.Str(); ok {
		return s, nil
	}
	return "", fmt.Errorf("glua: cannot convert %s to string", typeName(v))
}

func decBool(v Value) (bool, error) { return v.truthy(), nil }

// bindFunc1 returns a typed Go function backed by cl, run on executors from e's pool.
// It does the five steps of a typed call: borrow, encode, run, decode, clear.
func bindFunc1[A, R any](e *engine, cl *Closure) func(context.Context, A) (R, error) {
	enc, byValue := encoderFor[A]() // chosen once, at bind time
	dec := decoderFor[R]()
	if byValue {
		id := bindType(reflect.TypeFor[A]())
		return func(ctx context.Context, a A) (r R, err error) {
			ex := e.pool.Get().(*executor)
			slot := slotFor[A](ex, id)
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
		ex := e.pool.Get().(*executor)                // borrow
		ex.stack[1] = enc(a)                          // encode
		if err = ex.call(ctx, cl, 0, 1); err == nil { // run
			r, err = dec(ex.stack[0]) // decode
		}
		ex.reset() // clear
		release(&e.pool, ex)
		return r, err
	}
}

// Rule is a compiled expression over a struct E. It is a concrete generic type with a
// method, so a call to Eval is static and escape analysis can see that env does not leak.
// A func value would force every pointer argument to the heap.
type Rule[E, R any] struct {
	pool *sync.Pool
	cl   *Closure
	id   uint64
	dec  func(Value) (R, error)
}

func newRule[E, R any](e *engine, cl *Closure) *Rule[E, R] {
	return &Rule[E, R]{pool: &e.pool, cl: cl, id: bindType(reflect.TypeFor[E]()), dec: decoderFor[R]()}
}

// Eval copies *env into a slot of the borrowed executor, runs, and copies it back. env
// itself does not escape, so a caller-local struct stays on the stack.
func (ru *Rule[E, R]) Eval(ctx context.Context, env *E) (r R, err error) {
	ex := ru.pool.Get().(*executor)
	slot := slotFor[E](ex, ru.id)
	*slot = *env // copy in
	ex.stack[1] = Value{p: unsafe.Pointer(slot), n: kindUser<<56 | flagCallscoped | ru.id}
	if err = ex.call(ctx, ru.cl, 0, 1); err == nil {
		r, err = ru.dec(ex.stack[0])
	}
	ex.reset()
	*env = *slot                 // copy out; milestone 2 copies only the rw fields
	clear(unsafe.Slice(slot, 1)) // nothing of this call stays reachable from the pool
	release(ru.pool, ex)
	return r, err
}

// slotFor returns this executor's slot for type E. It is allocated as an E, so the GC sees its pointers.
func slotFor[E any](ex *executor, id uint64) *E {
	if id < uint64(len(ex.slots)) && ex.slots[id] != nil {
		return (*E)(ex.slots[id])
	}
	for uint64(len(ex.slots)) <= id {
		ex.slots = append(ex.slots, nil)
	}
	s := new(E)
	ex.slots[id] = unsafe.Pointer(s)
	return s
}
