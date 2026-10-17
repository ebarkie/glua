package glua

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sync/atomic"
	"unsafe"
)

// Host functions see their arguments the Lua way, as C Lua's lauxlib does: a numeric string
// is a number, and a number is a string. Results going back to Go are strict.

// argPanic is what Arg panics with; the host call that received the Call recovers it.
type argPanic struct{}

// Arg returns argument i, counting from 0, converted to T. A missing or unconvertible
// argument raises "bad argument #1 to 'f' (number expected, got nil)", which pcall can
// catch. It unwinds by panic, so it must run on the goroutine that received c.
// c.Value(i).As[T]() is the form that returns an error instead.
func (c *Call) Arg[T any](i int) T {
	v, err := argValue[T](c, i)
	if err != nil {
		c.argErr, c.unwinding = err, true
		panic(argPanic{})
	}
	return v
}

// Opt returns argument i converted to T, or def if it is nil or missing.
func (c *Call) Opt[T any](i int, def T) T {
	if c.Value(i).IsNil() {
		return def
	}
	return c.Arg[T](i)
}

// Return sets the function's single result. The last call wins. It reports only a value
// that cannot be converted, so `return c.Return(x)` reads well.
func (c *Call) Return[T any](v T) error {
	x, err := resultValue(v)
	if err != nil {
		return err
	}
	c.return1(x)
	return nil
}

// Return2 sets two results.
func (c *Call) Return2[T, U any](v T, u U) error {
	x, err := resultValue(v)
	if err != nil {
		return err
	}
	y, err := resultValue(u)
	if err != nil {
		return err
	}
	c.res = append(c.res[:0], x, y)
	return nil
}

// resultValue converts a host function's result.
func resultValue[T any](v T) (Value, error) {
	// a switch on a nil *T boxes nothing; the cast is safe because the case fixes T
	p := unsafe.Pointer(&v)
	switch any((*T)(nil)).(type) {
	case *int64:
		return Int(*(*int64)(p)), nil
	case *int:
		return Int(int64(*(*int)(p))), nil
	case *float64:
		return Float(*(*float64)(p)), nil
	case *string:
		return String(*(*string)(p)), nil
	case *bool:
		return Bool(*(*bool)(p)), nil
	case *Value:
		return *(*Value)(p), nil
	}
	return encodeAny(v)
}

// argValue converts argument i to T, the Lua way.
func argValue[T any](c *Call, i int) (T, error) {
	v := c.Value(i)
	var r T
	p := unsafe.Pointer(&r)
	switch any((*T)(nil)).(type) { // a nil *T boxes nothing; each case fixes T for the cast
	case *int64:
		n, err := argInt(c, i, v, math.MinInt64, math.MaxInt64)
		*(*int64)(p) = n
		return r, err
	case *int:
		n, err := argInt(c, i, v, math.MinInt, math.MaxInt)
		*(*int)(p) = int(n)
		return r, err
	case *float64:
		f, err := argFloat(c, i, v)
		*(*float64)(p) = f
		return r, err
	case *string:
		s, err := argString(c, i, v)
		*(*string)(p) = s
		return r, err
	case *bool:
		*(*bool)(p) = v.truthy()
		return r, nil
	case *Value:
		*(*Value)(p) = v
		return r, nil
	}
	return argReflect[T](c, i, v)
}

// argReflect converts an argument by reflection. It is apart from argValue, whose result
// would otherwise escape to the heap on every call.
func argReflect[T any](c *Call, i int, v Value) (T, error) {
	var r T
	err := decodeArg(c, i, v, reflect.ValueOf(&r).Elem())
	return r, err
}

// argInt converts an argument to an integer in [lo, hi].
func argInt(c *Call, i int, v Value, lo, hi int64) (int64, error) {
	if v.isString() {
		if n, ok := strToNumber(v.str()); ok {
			v = n
		}
	}
	if n, ok := v.Int(); ok {
		if n < lo || n > hi {
			return 0, c.argError(i+1, c.fnName(), "number out of range")
		}
		return n, nil
	}
	if v.isNumber() {
		return 0, c.argError(i+1, c.fnName(), "number has no integer representation")
	}
	return 0, c.typeArgError(i, c.fnName(), "number")
}

func argFloat(c *Call, i int, v Value) (float64, error) {
	if v.isString() {
		if n, ok := strToNumber(v.str()); ok {
			v = n
		}
	}
	if f, ok := v.Float(); ok {
		return f, nil
	}
	return 0, c.typeArgError(i, c.fnName(), "number")
}

func argString(c *Call, i int, v Value) (string, error) {
	if s, ok := v.Str(); ok {
		return s, nil
	}
	if v.isNumber() {
		return v.String(), nil
	}
	return "", c.typeArgError(i, c.fnName(), "string")
}

// decodeArg converts argument i into rv, the Lua way for scalars, by the general rules
// otherwise.
func decodeArg(c *Call, i int, v Value, rv reflect.Value) error {
	t := rv.Type()
	if converterFor(t) == nil && t != durationType {
		switch t.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n, err := argInt(c, i, v, math.MinInt64, math.MaxInt64)
			if err != nil {
				return err
			}
			if rv.OverflowInt(n) {
				return c.argError(i+1, c.fnName(), "number out of range")
			}
			rv.SetInt(n)
			return nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			n, err := argInt(c, i, v, 0, math.MaxInt64)
			if err != nil {
				return err
			}
			if rv.OverflowUint(uint64(n)) {
				return c.argError(i+1, c.fnName(), "number out of range")
			}
			rv.SetUint(uint64(n))
			return nil
		case reflect.Float32, reflect.Float64:
			f, err := argFloat(c, i, v)
			if err != nil {
				return err
			}
			rv.SetFloat(f)
			return nil
		case reflect.String:
			s, err := argString(c, i, v)
			if err != nil {
				return err
			}
			rv.SetString(s)
			return nil
		case reflect.Bool:
			rv.SetBool(v.truthy())
			return nil
		}
	}
	if err := decodeReflect(v, rv, "", 0); err != nil {
		if v.IsNil() && i >= c.Len() || wrongKind(v, t) {
			return c.typeArgError(i, c.fnName(), luaTypeName(t))
		}
		return c.argError(i+1, c.fnName(), convMsg(err))
	}
	return nil
}

// wrongKind reports whether v is of no Lua type that t takes, so that the error names the
// type wanted, as C Lua's luaL_checktype and luaL_checkudata do: a table for *Table, and for
// a pointer to a named struct that is bound as a userdata, a userdata of that type or a table
// to fill one. A type with a conversion of its own, such as time.Time, keeps its messages.
func wrongKind(v Value, t reflect.Type) bool {
	switch {
	case t == tableType:
		return !v.isObj(kindTable)
	case t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct && t.Elem().Name() != "" &&
		t.Elem() != timeType && converterFor(t) == nil && converterFor(t.Elem()) == nil:
		return !v.isObj(kindTable) && !(v.isObj(kindUser) && v.n&typeIDMask == bindType(t.Elem()))
	}
	return false
}

// luaTypeName names what a Go type expects, for "X expected, got nil".
func luaTypeName(t reflect.Type) string {
	switch {
	case t == tableType:
		return "table"
	case t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct:
		return t.Elem().Name()
	case t.Kind() == reflect.Slice, t.Kind() == reflect.Map, t.Kind() == reflect.Struct:
		return "table"
	}
	return t.String()
}

// fnName is the name of the function being called, for messages.
func (c *Call) fnName() string {
	if c.fn == nil {
		return "?"
	}
	return c.fn.name
}

// InvokeN calls fn with args from inside a host function and appends its results to dst[:0].
// It shares the budgets of the outermost entry.
func (c *Call) InvokeN(dst []Value, fn Value, args ...Value) ([]Value, error) {
	return c.ex.callSyncInto(dst[:0], fn, args)
}

// argDec decodes one parameter of a typed adapter. A context.Context parameter receives
// the call's context and takes no Lua argument.
type argDec[T any] struct {
	ctx  bool
	plan *atomic.Pointer[argPlan]
}

// argPlan is how an argDec decodes without reflection: a pointer to a bound struct, a struct,
// or a named number, string or bool, which argValue's fast paths miss. It is chosen when the
// adapter is made, and again after a conversion is registered, which changes how a type
// converts. Anything it does not take goes to argValue.
type argPlan struct {
	gen  uint64 // convGen when chosen
	kind argKind
	id   uint64 // the type ID, for akPtr and akStruct
}

type argKind uint8

const (
	akOther  argKind = iota // argValue
	akPtr                   // a userdata of the struct's type, not callscoped
	akStruct                // a userdata of the type, copied
	akInt64                 // int too: glua needs a 64-bit platform
	akFloat64
	akString
	akBool
)

// planArg chooses and stores the plan for a parameter of type T.
func planArg[T any](slot *atomic.Pointer[argPlan]) *argPlan {
	pl := &argPlan{gen: convGen.Load()}
	t := reflect.TypeFor[T]()
	switch {
	case t == valueType || t == tableType || t == durationType || t == timeType || converterFor(t) != nil:
	case t.Kind() == reflect.Pointer:
		if e := t.Elem(); e.Kind() == reflect.Struct && e != timeType && converterFor(e) == nil {
			pl.kind, pl.id = akPtr, bindType(e)
		}
	case t.Kind() == reflect.Struct:
		pl.kind, pl.id = akStruct, bindType(t)
	case t.Kind() == reflect.Int64 || t.Kind() == reflect.Int:
		pl.kind = akInt64
	case t.Kind() == reflect.Float64:
		pl.kind = akFloat64
	case t.Kind() == reflect.String:
		pl.kind = akString
	case t.Kind() == reflect.Bool:
		pl.kind = akBool
	}
	slot.Store(pl)
	return pl
}

var contextType = reflect.TypeFor[context.Context]()

func newArgDec[T any]() argDec[T] {
	t := reflect.TypeFor[T]()
	if t == contextType {
		return argDec[T]{ctx: true}
	}
	if t.Kind() == reflect.Func {
		panic("glua: a function parameter of type " + t.String() + " is not supported")
	}
	if err := checkDecodable(t, 0, map[reflect.Type]bool{}); err != nil {
		panic("glua: parameter of type " + t.String() + ": " + err.Error())
	}
	d := argDec[T]{plan: new(atomic.Pointer[argPlan])}
	planArg[T](d.plan)
	return d
}

// get decodes the next parameter, from argument *i.
func (d argDec[T]) get(c *Call, i *int) (T, error) {
	if d.ctx {
		return any(c.Context()).(T), nil
	}
	n := *i
	*i++
	pl := d.plan.Load()
	if pl.gen != convGen.Load() {
		pl = planArg[T](d.plan)
	}
	v := c.Value(n)
	var r T
	p := unsafe.Pointer(&r)
	switch pl.kind {
	case akPtr:
		if v.isObj(kindUser) && v.n&(typeIDMask|flagCallscoped) == pl.id {
			*(*unsafe.Pointer)(p) = v.p
			return r, nil
		}
	case akStruct:
		if v.isObj(kindUser) && v.n&typeIDMask == pl.id {
			return *(*T)(v.p), nil
		}
	case akInt64:
		x, err := argInt(c, n, v, math.MinInt64, math.MaxInt64)
		*(*int64)(p) = x
		return r, err
	case akFloat64:
		f, err := argFloat(c, n, v)
		*(*float64)(p) = f
		return r, err
	case akString:
		s, err := argString(c, n, v)
		*(*string)(p) = s
		return r, err
	case akBool:
		*(*bool)(p) = v.truthy()
		return r, nil
	}
	return argValue[T](c, n) // what the plan does not take, with its errors
}

// resEnc encodes the result of a typed adapter, by an encoder chosen when the adapter is
// made, and again after a conversion is registered.
type resEnc[R any] struct {
	plan *atomic.Pointer[resPlan[R]]
}

type resPlan[R any] struct {
	gen uint64 // convGen when chosen
	enc func(R) (Value, error)
}

func newResEnc[R any]() resEnc[R] {
	if _, _, err := encoderFor[R](); err != nil {
		panic("glua: result of type " + reflect.TypeFor[R]().String() + ": " + err.Error())
	}
	e := resEnc[R]{plan: new(atomic.Pointer[resPlan[R]])}
	planRes[R](e.plan)
	return e
}

// planRes chooses and stores the encoder for a result of type R.
func planRes[R any](slot *atomic.Pointer[resPlan[R]]) *resPlan[R] {
	pl := &resPlan[R]{gen: convGen.Load()}
	enc, byValue, err := encoderFor[R]()
	switch {
	case err != nil:
		enc = encodeAny[R] // reports the error at each call
	case byValue:
		bits := uint64(kindUser)<<56 | bindType(reflect.TypeFor[R]())
		enc = func(v R) (Value, error) {
			p := new(R) // a struct result is copied to the heap: the script may keep it
			*p = v
			return Value{p: unsafe.Pointer(p), n: bits}, nil
		}
	}
	pl.enc = enc
	slot.Store(pl)
	return pl
}

func (e resEnc[R]) ret(c *Call, r R) error {
	pl := e.plan.Load()
	if pl.gen != convGen.Load() {
		pl = planRes[R](e.plan)
	}
	v, err := pl.enc(r)
	if err != nil {
		return err
	}
	c.return1(v)
	return nil
}

// Typed adapters: no reflection when called. A first parameter of type context.Context
// receives c.Context() and takes no Lua argument. Fn returns one result; FnE also an error,
// which becomes a Lua error; Do returns nothing; DoE only an error. They panic when made
// with a parameter or result type that cannot be converted.

// Fn0 adapts func() R.
func Fn0[R any](f func() R) GoFunc {
	er := newResEnc[R]()
	return func(c *Call) error { return er.ret(c, f()) }
}

// Fn1 adapts func(A) R.
func Fn1[A, R any](f func(A) R) GoFunc {
	da, er := newArgDec[A](), newResEnc[R]()
	return func(c *Call) error {
		i := 0
		a, err := da.get(c, &i)
		if err != nil {
			return err
		}
		return er.ret(c, f(a))
	}
}

// Fn2 adapts func(A, B) R, such as a method expression (*T).M.
func Fn2[A, B, R any](f func(A, B) R) GoFunc {
	da, db, er := newArgDec[A](), newArgDec[B](), newResEnc[R]()
	return func(c *Call) error {
		i := 0
		a, err := da.get(c, &i)
		if err != nil {
			return err
		}
		b, err := db.get(c, &i)
		if err != nil {
			return err
		}
		return er.ret(c, f(a, b))
	}
}

// Fn3 adapts func(A, B, C) R.
func Fn3[A, B, C, R any](f func(A, B, C) R) GoFunc {
	da, db, dc, er := newArgDec[A](), newArgDec[B](), newArgDec[C](), newResEnc[R]()
	return func(c *Call) error {
		i := 0
		a, err := da.get(c, &i)
		if err != nil {
			return err
		}
		b, err := db.get(c, &i)
		if err != nil {
			return err
		}
		x, err := dc.get(c, &i)
		if err != nil {
			return err
		}
		return er.ret(c, f(a, b, x))
	}
}

// Fn0E adapts func() (R, error); a non-nil error becomes a Lua error.
func Fn0E[R any](f func() (R, error)) GoFunc {
	er := newResEnc[R]()
	return func(c *Call) error {
		r, err := f()
		if err != nil {
			return err
		}
		return er.ret(c, r)
	}
}

// Fn1E adapts func(A) (R, error).
func Fn1E[A, R any](f func(A) (R, error)) GoFunc {
	da, er := newArgDec[A](), newResEnc[R]()
	return func(c *Call) error {
		i := 0
		a, err := da.get(c, &i)
		if err != nil {
			return err
		}
		r, err := f(a)
		if err != nil {
			return err
		}
		return er.ret(c, r)
	}
}

// Fn2E adapts func(A, B) (R, error).
func Fn2E[A, B, R any](f func(A, B) (R, error)) GoFunc {
	da, db, er := newArgDec[A](), newArgDec[B](), newResEnc[R]()
	return func(c *Call) error {
		i := 0
		a, err := da.get(c, &i)
		if err != nil {
			return err
		}
		b, err := db.get(c, &i)
		if err != nil {
			return err
		}
		r, err := f(a, b)
		if err != nil {
			return err
		}
		return er.ret(c, r)
	}
}

// Fn3E adapts func(A, B, C) (R, error).
func Fn3E[A, B, C, R any](f func(A, B, C) (R, error)) GoFunc {
	da, db, dc, er := newArgDec[A](), newArgDec[B](), newArgDec[C](), newResEnc[R]()
	return func(c *Call) error {
		i := 0
		a, err := da.get(c, &i)
		if err != nil {
			return err
		}
		b, err := db.get(c, &i)
		if err != nil {
			return err
		}
		x, err := dc.get(c, &i)
		if err != nil {
			return err
		}
		r, err := f(a, b, x)
		if err != nil {
			return err
		}
		return er.ret(c, r)
	}
}

// Do0 adapts func(), which returns nothing to Lua.
func Do0(f func()) GoFunc {
	return func(c *Call) error { f(); return nil }
}

// Do1 adapts func(A).
func Do1[A any](f func(A)) GoFunc {
	da := newArgDec[A]()
	return func(c *Call) error {
		i := 0
		a, err := da.get(c, &i)
		if err != nil {
			return err
		}
		f(a)
		return nil
	}
}

// Do2 adapts func(A, B).
func Do2[A, B any](f func(A, B)) GoFunc {
	da, db := newArgDec[A](), newArgDec[B]()
	return func(c *Call) error {
		i := 0
		a, err := da.get(c, &i)
		if err != nil {
			return err
		}
		b, err := db.get(c, &i)
		if err != nil {
			return err
		}
		f(a, b)
		return nil
	}
}

// Do3 adapts func(A, B, C).
func Do3[A, B, C any](f func(A, B, C)) GoFunc {
	da, db, dc := newArgDec[A](), newArgDec[B](), newArgDec[C]()
	return func(c *Call) error {
		i := 0
		a, err := da.get(c, &i)
		if err != nil {
			return err
		}
		b, err := db.get(c, &i)
		if err != nil {
			return err
		}
		x, err := dc.get(c, &i)
		if err != nil {
			return err
		}
		f(a, b, x)
		return nil
	}
}

// Do0E adapts func() error.
func Do0E(f func() error) GoFunc {
	return func(c *Call) error { return f() }
}

// Do1E adapts func(A) error.
func Do1E[A any](f func(A) error) GoFunc {
	da := newArgDec[A]()
	return func(c *Call) error {
		i := 0
		a, err := da.get(c, &i)
		if err != nil {
			return err
		}
		return f(a)
	}
}

// Do2E adapts func(A, B) error.
func Do2E[A, B any](f func(A, B) error) GoFunc {
	da, db := newArgDec[A](), newArgDec[B]()
	return func(c *Call) error {
		i := 0
		a, err := da.get(c, &i)
		if err != nil {
			return err
		}
		b, err := db.get(c, &i)
		if err != nil {
			return err
		}
		return f(a, b)
	}
}

// Do3E adapts func(A, B, C) error.
func Do3E[A, B, C any](f func(A, B, C) error) GoFunc {
	da, db, dc := newArgDec[A](), newArgDec[B](), newArgDec[C]()
	return func(c *Call) error {
		i := 0
		a, err := da.get(c, &i)
		if err != nil {
			return err
		}
		b, err := db.get(c, &i)
		if err != nil {
			return err
		}
		x, err := dc.get(c, &i)
		if err != nil {
			return err
		}
		return f(a, b, x)
	}
}

var errorType = reflect.TypeFor[error]()

// Reflect adapts any Go function by reflection. Convenient and slow; it is never applied
// implicitly. A first context.Context parameter receives the call's context, a final error
// result becomes a Lua error, and a variadic function takes the remaining arguments. It
// panics if fn is not a function or has a type that cannot be converted.
func Reflect(fn any) GoFunc {
	fv := reflect.ValueOf(fn)
	ft := fv.Type()
	if ft.Kind() != reflect.Func || fv.IsNil() {
		panic(fmt.Sprintf("glua: Reflect needs a function, not %T", fn))
	}
	nin := ft.NumIn()
	for j := range nin {
		t := ft.In(j)
		if j == nin-1 && ft.IsVariadic() {
			t = t.Elem()
		}
		if j == 0 && t == contextType {
			continue
		}
		if err := checkDecodable(t, 0, map[reflect.Type]bool{}); err != nil {
			panic("glua: Reflect: parameter " + t.String() + ": " + err.Error())
		}
	}
	nout := ft.NumOut()
	hasErr := nout > 0 && ft.Out(nout-1) == errorType
	if hasErr {
		nout--
	}
	for j := range nout {
		if err := checkEncodable(ft.Out(j), 0, map[reflect.Type]bool{}); err != nil {
			panic("glua: Reflect: result " + ft.Out(j).String() + ": " + err.Error())
		}
	}
	return func(c *Call) error {
		in := make([]reflect.Value, 0, nin)
		i := 0
		for j := range nin {
			t := ft.In(j)
			if j == 0 && t == contextType {
				in = append(in, reflect.ValueOf(c.Context()))
				continue
			}
			if j == nin-1 && ft.IsVariadic() {
				for ; i < c.Len(); i++ {
					x := reflect.New(t.Elem()).Elem()
					if err := decodeArg(c, i, c.Value(i), x); err != nil {
						return err
					}
					in = append(in, x)
				}
				break
			}
			x := reflect.New(t).Elem()
			if err := decodeArg(c, i, c.Value(i), x); err != nil {
				return err
			}
			in = append(in, x)
			i++
		}
		out := fv.Call(in)
		if hasErr {
			if e := out[nout]; !e.IsNil() {
				return e.Interface().(error)
			}
		}
		res := make([]Value, nout)
		for j := range nout {
			v, err := encodeReflect(out[j], 0)
			if err != nil {
				return &convErr{msg: err.Error()}
			}
			res[j] = v
		}
		return c.ReturnValues(res...)
	}
}
