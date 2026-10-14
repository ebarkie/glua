package glua

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"unsafe"

	"github.com/folbricht/glua/internal/syntax"
)

// envParam is the name of an expression's environment parameter. It is not a Lua name, so
// a script cannot refer to it.
const envParam = "(env)"

// Rule is a compiled Expr. It is a concrete generic type with a method, so a call to Eval is
// static and escape analysis can see that env does not leak: a func value would force every
// pointer argument to the heap. Share freely.
type Rule[E, R any] struct {
	rt       *Runtime
	pool     *sync.Pool
	lim      *limits
	cl       *Closure
	id       uint64
	dec      resultDecoder[R]
	copyBack bool // E has writable fields
	name     string
}

// Expr compiles an expression, or a chunk, whose free names are the fields of E and then the
// Runtime's globals. The source is parsed as `return <src>` first, then as a chunk; in a
// chunk, fields that are writable can be assigned. Unknown names, assignments to read-only
// fields or to globals, and a field that shadows a global are errors here, not at run time.
func (rt *Runtime) Expr[E, R any](name, src string) (*Rule[E, R], error) {
	t := reflect.TypeFor[E]()
	if t.Kind() != reflect.Struct {
		return nil, &BindError{Name: name, Type: t, Reason: "the environment of an Expr must be a struct"}
	}
	if err := rt.checkType(t); err != nil {
		return nil, bindName(name, err)
	}
	id := bindType(t)
	ti := (*rt.plans())[id]
	ld := newEnvLoader(rt)
	env := rt.newEnv(ld)
	fields := map[string]bool{}
	copyBack := false
	for _, f := range ti.fields {
		if !env.Get(f.key).IsNil() {
			return nil, &BindError{Name: name, Type: t, Reason: fmt.Sprintf("field %s shadows the global '%s'", f.goName, f.name)}
		}
		fields[f.name] = f.rw
		copyBack = copyBack || f.rw
	}
	xenv := &syntax.ExprEnv{
		Param:  envParam,
		Fields: fields,
		Known:  func(n string) bool { return !env.Get(String(n)).IsNil() },
	}
	p, _, err := compileExpr(chunkID("="+name), src, xenv)
	if err != nil {
		return nil, syntaxError(err)
	}
	setChunkName(p, "="+name)
	if err := freezeEnv(rt, env, nil); err != nil {
		return nil, err
	}
	dec, err := checkedResultDecoder[R](rt)
	if err != nil {
		return nil, bindName(name, err)
	}
	return &Rule[E, R]{rt: rt, pool: &rt.pool, lim: &rt.run, cl: &Closure{p: p, env: env}, id: id,
		dec: dec, copyBack: copyBack, name: name}, nil
}

// Eval copies *env into a slot of the borrowed executor, runs, and on success copies it
// back, which carries the writable fields out: the others are read-only to the script. env
// itself does not escape, so a caller-local struct stays on the stack. What its pointers and
// strings refer to is copied to the heap slot and must live on the heap.
func (ru *Rule[E, R]) Eval(ctx context.Context, env *E) (r R, err error) {
	ex := ru.pool.Get().(*executor)
	if ru.lim != nil {
		ex.setLimits(ru.lim)
	}
	slot := slotFor[E](ex, ru.id, 0)
	*slot = *env // copy in
	ex.stack[1] = Value{p: unsafe.Pointer(slot), n: kindUser<<56 | flagCallscoped | ru.id}
	ex.st.hi = 2
	if err = ex.call(ctx, ru.cl, 0, 1); err == nil {
		res := ex.stack[:ex.top]
		escapeResults(res)
		r, err = ru.dec.decode(res)
	}
	if err == nil && ru.copyBack {
		*env = *slot // copy out
	}
	if ru.rt != nil {
		ru.rt.observeEntry(ex, ru.name, "", err)
	}
	ex.reset()
	clear(unsafe.Slice(slot, 1)) // nothing of this call stays reachable from the pool
	release(ru.pool, ex)
	return r, err
}

// newRule makes a Rule from a hand-assembled closure, for the tests of the executor.
func newRule[E, R any](e *engine, cl *Closure) *Rule[E, R] {
	dec, _ := resultDecoderFor[R]()
	t := reflect.TypeFor[E]()
	return &Rule[E, R]{pool: &e.pool, cl: cl, id: bindType(t), dec: dec, copyBack: true}
}
