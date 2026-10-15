package glua

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unsafe"

	"github.com/folbricht/glua/internal/syntax"
)

// Script is a loaded script: verified code plus the frozen state its top level built.
// Share freely.
type Script struct {
	rt     *Runtime
	name   string
	env    *Table  // the frozen globals
	result []Value // what the chunk returned, frozen
	lim    *limits // for calls
	main   *proto  // for NewVM, which runs the top level again
	info   *syntax.Info
}

// Load compiles src, runs its top level once under the init limits, then freezes everything
// it built: the globals, what the chunk returned, and the modules it required.
func (rt *Runtime) Load(ctx context.Context, name, src string) (*Script, error) {
	p, info, err := compileChunk(chunkID("@"+name), src)
	if err != nil {
		return nil, syntaxError(err)
	}
	setChunkName(p, "@"+name)
	ld := newEnvLoader(rt)
	env := rt.newEnv(ld)
	cl := newMainClosure(p, info, env)
	res, err := rt.runTop(ctx, cl, name)
	if err != nil {
		return nil, err
	}
	if err := freezeEnv(rt, env, res); err != nil {
		return nil, fmt.Errorf("glua: %s: cannot freeze: %w", name, err)
	}
	return &Script{rt: rt, name: name, env: env, result: res, lim: &rt.run, main: p, info: info}, nil
}

// runTop runs a top level under the init limits and returns its results.
func (rt *Runtime) runTop(ctx context.Context, cl *closure, name string) ([]Value, error) {
	ex := rt.borrow(&rt.init)
	err := ex.call(ctx, cl, 0, 0)
	var res []Value
	if err == nil {
		res = append(res, ex.stack[:ex.top]...)
	}
	rt.observeEntry(ex, name, "", err)
	rt.giveBack(ex)
	return res, err
}

// freezeEnv freezes the globals of an environment and the results of its top level, naming
// what cannot be frozen.
func freezeEnv(rt *Runtime, env *Table, res []Value) error {
	f := newFreezer()
	f.plans = rt.plans()
	for k, v := range env.All() {
		if k.isString() {
			f.add(v, "global '"+k.str()+"'")
		} else {
			f.add(v, "global "+k.String())
		}
	}
	for i, v := range res {
		f.add(v, fmt.Sprintf("result %d", i+1))
	}
	f.add(tableValue(env), "the globals")
	if err := f.walk(); err != nil {
		return err
	}
	f.freeze()
	return nil
}

// borrow takes an executor from the pool, under the limits l.
func (rt *Runtime) borrow(l *limits) *executor {
	ex := rt.pool.Get().(*executor)
	ex.setLimits(l)
	return ex
}

// giveBack clears an executor and returns it to the pool, unless it ended abnormally or has
// grown large.
func (rt *Runtime) giveBack(ex *executor) {
	ex.reset()
	release(&rt.pool, ex)
}

// observeEntry reports what an entry used, if the Runtime has an Observe function.
func (rt *Runtime) observeEntry(ex *executor, script, fn string, err error) {
	if rt.observe != nil {
		rt.report(ex, script, fn, err)
	}
}

func (rt *Runtime) report(ex *executor, script, fn string, err error) {
	rt.observe(CallStats{
		Script:     script,
		Func:       fn,
		Steps:      ex.maxSteps - ex.steps - int64(max(ex.st.ticks, 0)),
		AllocBytes: ex.maxAlloc - ex.allocLeft,
		Err:        err,
	})
}

// lookup finds a global, a dotted path of raw table fields, or for "" the chunk's first
// result.
func lookup(env *Table, result []Value, name string) (Value, error) {
	if name == "" {
		if len(result) == 0 || result[0].IsNil() {
			return Value{}, fmt.Errorf("%w: the chunk returned nothing", ErrUndefined)
		}
		return result[0], nil
	}
	v := tableValue(env)
	for part := range strings.SplitSeq(name, ".") {
		if !v.isObj(kindTable) {
			return Value{}, fmt.Errorf("%w: '%s'", ErrUndefined, name)
		}
		v = (*Table)(v.p).Get(String(part))
	}
	if v.IsNil() {
		return Value{}, fmt.Errorf("%w: '%s'", ErrUndefined, name)
	}
	return v, nil
}

// lookupFunc finds a function for FuncN.
func lookupFunc(env *Table, result []Value, name string) (Value, error) {
	v, err := lookup(env, result, name)
	if err != nil {
		return Value{}, err
	}
	if !v.isObj(kindLuaFn) && !v.isObj(kindGoFn) {
		return Value{}, fmt.Errorf("%w: '%s' is a %s", ErrNotFunction, name, typeName(v))
	}
	return v, nil
}

// Globals returns the frozen globals table.
func (s *Script) Globals() *Table { return s.env }

// Result returns the first value the chunk returned.
func (s *Script) Result() Value {
	if len(s.result) == 0 {
		return Value{}
	}
	return s.result[0]
}

// Get reads a global, or a dotted path, from the frozen globals and converts it to T. It
// runs nothing.
func (s *Script) Get[T any](name string) (T, error) {
	var r T
	v, err := lookup(s.env, s.result, name)
	if err != nil {
		return r, err
	}
	return v.As[T]()
}

// WithLimits returns the same code and state under other limits. It fails if the profile
// refuses them.
func (s *Script) WithLimits(l Limits) (*Script, error) {
	run, _, err := resolveLimits(l, s.rt.hardened)
	if err != nil {
		return nil, err
	}
	c := *s
	c.lim = &run
	return &c, nil
}

// Call calls fn, which must be a function of the same Runtime, and returns its first result.
func (s *Script) Call(ctx context.Context, fn Value, args ...Value) (Value, error) {
	var buf [1]Value
	res, err := s.rt.callN(ctx, s.lim, s.name, buf[:0], fn, args)
	if err != nil || len(res) == 0 {
		return Value{}, err
	}
	return res[0], nil
}

// CallN calls fn and appends its results to dst[:0].
func (s *Script) CallN(ctx context.Context, dst []Value, fn Value, args ...Value) ([]Value, error) {
	return s.rt.callN(ctx, s.lim, s.name, dst, fn, args)
}

// callN calls fn with args on a borrowed executor.
func (rt *Runtime) callN(ctx context.Context, l *limits, script string, dst []Value, fn Value, args []Value) ([]Value, error) {
	cl, base, err := entryFunc(fn)
	if err != nil {
		return dst[:0], err
	}
	ex := rt.borrow(l)
	if err = ex.ensure(base + len(args) + 1); err == nil {
		ex.stack[1] = fn
		copy(ex.stack[base:], args)
		ex.st.hi = base + len(args)
		err = ex.call(ctx, cl, 0, base-1+len(args))
	}
	dst = dst[:0]
	if err == nil {
		dst = append(dst, ex.stack[:ex.top]...)
	}
	rt.observeEntry(ex, script, "", err)
	rt.giveBack(ex)
	return dst, err
}

// entryFunc returns the closure an entry runs for fn, and the stack index of its first
// argument: a Go function runs from the trampoline, which takes it as its first argument.
func entryFunc(fn Value) (*closure, int, error) {
	switch {
	case fn.isObj(kindLuaFn):
		return (*closure)(fn.p), 1, nil
	case fn.isObj(kindGoFn):
		return trampoline, 2, nil
	}
	return nil, 0, fmt.Errorf("%w: a %s value", ErrNotFunction, typeName(fn))
}

// boundFunc is what a typed function needs: the function, where its arguments go, and the
// Runtime, limits and names of its entry.
type boundFunc struct {
	rt     *Runtime
	lim    *limits
	fn     Value
	cl     *closure
	base   int // stack index of the first argument
	script string
	name   string
	vm     *VM // nil for a Script: an executor from the pool
}

// bindFunc finds the function name of a Script for FuncN.
func (s *Script) bindFunc(name string) (*boundFunc, error) {
	fn, err := lookupFunc(s.env, s.result, name)
	if err != nil {
		return nil, err
	}
	cl, base, _ := entryFunc(fn)
	return &boundFunc{rt: s.rt, lim: s.lim, fn: fn, cl: cl, base: base, script: s.name, name: name}, nil
}

// begin borrows an executor and places the function for a call.
func (b *boundFunc) begin(nargs int) *executor {
	var ex *executor
	if b.vm != nil {
		ex = b.vm.enter()
	} else {
		ex = b.rt.borrow(b.lim)
	}
	ex.stack[1] = b.fn
	ex.st.hi = b.base + nargs
	return ex
}

// run runs the call whose arguments are in place.
func (b *boundFunc) run(ctx context.Context, ex *executor, nargs int) error {
	return ex.call(ctx, b.cl, 0, b.base-1+nargs)
}

// end reports the entry and returns the executor.
func (b *boundFunc) end(ex *executor, err error) {
	b.rt.observeEntry(ex, b.script, b.name, err)
	if b.vm != nil {
		b.vm.leave(ex)
		return
	}
	b.rt.giveBack(ex)
}

// argEnc places an argument of type T: encoded, or, for a struct by value, copied into a
// slot of the executor.
type argEnc[T any] struct {
	enc     func(T) (Value, error)
	byValue bool
	id      uint64
	scalar  scalarKind // a built-in scalar, encoded inline
}

// scalarKind names the built-in scalar types the boundary converts inline, without an
// indirect call.
type scalarKind uint8

const (
	notScalar scalarKind = iota
	scalarInt64
	scalarFloat64
	scalarString
	scalarBool
)

func scalarOf[T any]() scalarKind {
	switch any((*T)(nil)).(type) {
	case *int64:
		return scalarInt64
	case *float64:
		return scalarFloat64
	case *string:
		return scalarString
	case *bool:
		return scalarBool
	}
	return notScalar
}

func newArgEnc[T any](rt *Runtime) (argEnc[T], error) {
	if err := rt.checkType(reflect.TypeFor[T]()); err != nil {
		return argEnc[T]{}, err
	}
	enc, byValue, err := encoderFor[T]()
	a := argEnc[T]{enc: enc, byValue: byValue, scalar: scalarOf[T]()}
	if byValue {
		a.id = bindType(reflect.TypeFor[T]())
	}
	return a, err
}

// put stores v in stack[i]. pos is the argument position, which picks the slot. A private
// VM has state that outlives the call, so it copies a struct to the heap instead.
func (a *argEnc[T]) put(ex *executor, i, pos int, v T, private bool) (slot *T, err error) {
	p := unsafe.Pointer(&v)
	switch a.scalar { // the scalar kind fixes T, so the casts are exact
	case scalarInt64:
		ex.stack[i] = Int(*(*int64)(p))
		return nil, nil
	case scalarFloat64:
		ex.stack[i] = Float(*(*float64)(p))
		return nil, nil
	case scalarString:
		ex.stack[i] = String(*(*string)(p))
		return nil, nil
	case scalarBool:
		ex.stack[i] = Bool(*(*bool)(p))
		return nil, nil
	}
	if !a.byValue {
		ex.stack[i], err = a.enc(v)
		return nil, err
	}
	if private {
		p := new(T)
		*p = v
		ex.stack[i] = Value{p: unsafe.Pointer(p), n: kindUser<<56 | a.id}
		return nil, nil
	}
	slot = slotFor[T](ex, a.id, pos)
	*slot = v // copy in: the script sees the slot, flagged readonly and callscoped
	ex.stack[i] = Value{p: unsafe.Pointer(slot), n: kindUser<<56 | flagReadonly | flagCallscoped | a.id}
	return slot, nil
}

// clearSlot zeroes a slot after the call, so nothing of it stays reachable from the pool.
func clearSlot[T any](slot *T) {
	if slot != nil {
		clear(unsafe.Slice(slot, 1))
	}
}

// slotFor returns this executor's slot for type E at argument position pos. It is allocated
// as an E, so the GC sees its pointers.
func slotFor[E any](ex *executor, id uint64, pos int) *E {
	i := id*maxSlotArgs + uint64(pos)
	if i < uint64(len(ex.slots)) && ex.slots[i] != nil {
		return (*E)(ex.slots[i])
	}
	for uint64(len(ex.slots)) <= i {
		ex.slots = append(ex.slots, nil)
	}
	s := new(E)
	ex.slots[i] = unsafe.Pointer(s)
	return s
}

// maxSlotArgs is the number of argument positions that have slots: Func3's three, and the
// Rule's environment at 0.
const maxSlotArgs = 4

// escapeResults makes the results of a call that used slots safe to keep: a callscoped
// userdata among them, or in a table they reach, is replaced by a heap copy of its struct.
func escapeResults(res []Value) {
	scan := false
	for i := range res {
		if v := &res[i]; v.n>>56 == kindUser && v.n&flagCallscoped != 0 || v.n>>56 == kindTable {
			scan = !isScalar(v.p)
		}
		if scan {
			break
		}
	}
	if !scan {
		return // scalars, strings and functions cannot hold a slot
	}
	seen := map[unsafe.Pointer]bool{}
	var work []*Table
	fix := func(v *Value) {
		switch {
		case v.n>>56 == kindUser && v.n&flagCallscoped != 0 && !isScalar(v.p):
			if ti := userType(*v); ti != nil {
				p := reflect.New(ti.rt)
				p.Elem().Set(reflect.NewAt(ti.rt, v.p).Elem())
				*v = Value{p: p.UnsafePointer(), n: v.n &^ (flagCallscoped | flagReadonly)}
			}
		case v.isObj(kindTable) && !seen[v.p]:
			seen[v.p] = true
			if t := (*Table)(v.p); !t.Frozen() {
				work = append(work, t)
			}
		}
	}
	for i := range res {
		fix(&res[i])
	}
	for len(work) > 0 {
		t := work[len(work)-1]
		work = work[:len(work)-1]
		for i := range t.arr {
			fix(&t.arr[i])
		}
		for i := range t.nodes {
			fix(&t.nodes[i].k)
			fix(&t.nodes[i].v)
		}
		if t.meta != nil {
			mv := tableValue(t.meta)
			fix(&mv)
		}
	}
}

// Func0 returns a Go function backed by the Lua function name: a global, a dotted path
// ("handlers.allow"), or "" for the function the chunk returned. Safe for concurrent use.
func (s *Script) Func0[R any](name string) (func(context.Context) (R, error), error) {
	b, err := s.bindFunc(name)
	if err != nil {
		return nil, err
	}
	return func0[R](b)
}

// Func1 is Func0 for a function of one argument.
func (s *Script) Func1[A, R any](name string) (func(context.Context, A) (R, error), error) {
	b, err := s.bindFunc(name)
	if err != nil {
		return nil, err
	}
	return func1[A, R](b)
}

// Func2 is Func0 for a function of two arguments.
func (s *Script) Func2[A, B, R any](name string) (func(context.Context, A, B) (R, error), error) {
	b, err := s.bindFunc(name)
	if err != nil {
		return nil, err
	}
	return func2[A, B, R](b)
}

// Func3 is Func0 for a function of three arguments.
func (s *Script) Func3[A, B, C, R any](name string) (func(context.Context, A, B, C) (R, error), error) {
	b, err := s.bindFunc(name)
	if err != nil {
		return nil, err
	}
	return func3[A, B, C, R](b)
}

// bindName adds the function's name to a bind error.
func bindName(name string, err error) error {
	var be *BindError
	if errors.As(err, &be) && be.Name == "" {
		be.Name = name
	}
	return err
}

func func0[R any](b *boundFunc) (func(context.Context) (R, error), error) {
	dec, err := checkedResultDecoder[R](b.rt)
	if err != nil {
		return nil, bindName(b.name, err)
	}
	return func(ctx context.Context) (r R, err error) {
		ex := b.begin(0)
		if err = b.run(ctx, ex, 0); err == nil {
			r, err = dec.decode(ex.stack[:ex.top])
		}
		b.end(ex, err)
		return r, err
	}, nil
}

func func1[A, R any](b *boundFunc) (func(context.Context, A) (R, error), error) {
	ea, err := newArgEnc[A](b.rt)
	if err != nil {
		return nil, bindName(b.name, err)
	}
	dec, err := checkedResultDecoder[R](b.rt)
	if err != nil {
		return nil, bindName(b.name, err)
	}
	private := b.vm != nil
	return func(ctx context.Context, a A) (r R, err error) {
		ex := b.begin(1)
		sa, err := ea.put(ex, b.base, 0, a, private)
		if err == nil {
			err = b.run(ctx, ex, 1)
		}
		if err == nil {
			res := ex.stack[:ex.top]
			if sa != nil {
				escapeResults(res)
			}
			r, err = dec.decode(res)
		}
		b.end(ex, err)
		clearSlot(sa)
		return r, err
	}, nil
}

func func2[A, B, R any](b *boundFunc) (func(context.Context, A, B) (R, error), error) {
	ea, err := newArgEnc[A](b.rt)
	if err != nil {
		return nil, bindName(b.name, err)
	}
	eb, err := newArgEnc[B](b.rt)
	if err != nil {
		return nil, bindName(b.name, err)
	}
	dec, err := checkedResultDecoder[R](b.rt)
	if err != nil {
		return nil, bindName(b.name, err)
	}
	private := b.vm != nil
	return func(ctx context.Context, a A, bv B) (r R, err error) {
		ex := b.begin(2)
		sa, err := ea.put(ex, b.base, 0, a, private)
		var sb *B
		if err == nil {
			sb, err = eb.put(ex, b.base+1, 1, bv, private)
		}
		if err == nil {
			err = b.run(ctx, ex, 2)
		}
		if err == nil {
			res := ex.stack[:ex.top]
			if sa != nil || sb != nil {
				escapeResults(res)
			}
			r, err = dec.decode(res)
		}
		b.end(ex, err)
		clearSlot(sa)
		clearSlot(sb)
		return r, err
	}, nil
}

func func3[A, B, C, R any](b *boundFunc) (func(context.Context, A, B, C) (R, error), error) {
	ea, err := newArgEnc[A](b.rt)
	if err != nil {
		return nil, bindName(b.name, err)
	}
	eb, err := newArgEnc[B](b.rt)
	if err != nil {
		return nil, bindName(b.name, err)
	}
	ec, err := newArgEnc[C](b.rt)
	if err != nil {
		return nil, bindName(b.name, err)
	}
	dec, err := checkedResultDecoder[R](b.rt)
	if err != nil {
		return nil, bindName(b.name, err)
	}
	private := b.vm != nil
	return func(ctx context.Context, a A, bv B, c C) (r R, err error) {
		ex := b.begin(3)
		sa, err := ea.put(ex, b.base, 0, a, private)
		var sb *B
		var sc *C
		if err == nil {
			sb, err = eb.put(ex, b.base+1, 1, bv, private)
		}
		if err == nil {
			sc, err = ec.put(ex, b.base+2, 2, c, private)
		}
		if err == nil {
			err = b.run(ctx, ex, 3)
		}
		if err == nil {
			res := ex.stack[:ex.top]
			if sa != nil || sb != nil || sc != nil {
				escapeResults(res)
			}
			r, err = dec.decode(res)
		}
		b.end(ex, err)
		clearSlot(sa)
		clearSlot(sb)
		clearSlot(sc)
		return r, err
	}, nil
}

// checkedResultDecoder is resultDecoderFor, after checking that the Runtime exposes a pointer
// result's struct type.
func checkedResultDecoder[R any](rt *Runtime) (resultDecoder[R], error) {
	if t := reflect.TypeFor[R](); t.Kind() == reflect.Pointer {
		if err := rt.checkType(t); err != nil {
			return resultDecoder[R]{}, err
		}
	}
	return resultDecoderFor[R]()
}
