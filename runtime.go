package glua

import (
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/folbricht/glua/internal/syntax"
)

// Runtime is the host side of the sandbox: libraries, globals, modules, bound Go types and
// limits. It never changes after New, so it is safe to share between goroutines.
type Runtime struct {
	libs    Lib
	run     limits // for calls
	init    limits // for the top level, in Load and NewVM
	layer   *Table // frozen: the libraries, host globals and Go modules every environment starts with
	modules *Table // frozen: the Go modules, by name, for package.loaded
	sources map[string]*sourceModule
	loader  func(name string) (string, error)

	reserved map[string]bool // names a script cannot assign
	strMeta  *Table          // the metatable all strings share
	out      *syncWriter     // nil discards
	randSeed *uint64
	clock    func() time.Time
	observe  func(CallStats)
	autoBind bool
	strict   bool
	recover  bool
	hardened bool
	checked  bool

	custom     map[uint64]*typeInfo  // plans customised by Type, by type ID
	registered map[reflect.Type]bool // the types Type registered
	planCache  atomic.Pointer[planSet]
	planMu     sync.Mutex

	pool sync.Pool // executors; they hold no script state, so one pool serves every Script and Rule
}

// sourceModule is a module written in Lua, compiled once by New and run by require in each
// environment that loads it.
type sourceModule struct {
	p    *Proto
	info *syntax.Info
}

// limits are resolved Limits: no zero means default, and no NoLimit.
type limits struct {
	steps     int64
	alloc     int64
	callDepth int
	hostDepth int
}

// Lib options are OR-ed together. With none given, LibSafe is used.
type Lib uint

const (
	LibBase Lib = 1 << iota
	LibString
	LibTable
	LibMath
	LibUTF8
	LibCoroutine
	LibOS   // clock, time, date, difftime
	LibIO   // trusted scripts only
	LibLoad // load(): compile text at run time

	LibNone Lib = 1 << 31 // an empty environment; with other flags, only those
	LibSafe     = LibBase | LibString | LibTable | LibMath | LibUTF8
)

func (l Lib) apply(o *options) { o.libs |= l; o.libsSet = true }

// Limits apply to one entry from Go, including everything it re-enters. A zero field takes
// the default. NoLimit switches a limit off.
type Limits struct {
	Steps          int64 // default 10M. Back-edges, calls, and library work in proportion to its size
	AllocBytes     int64 // default 64 MiB. Charged before each allocation made for the script
	CallDepth      int   // default 10,000 Lua frames. Catchable "stack overflow"
	HostDepth      int   // default 100 nested Go re-entries
	InitSteps      int64 // defaults to Steps. Used by Load and NewVM for the top level
	InitAllocBytes int64 // defaults to AllocBytes
}

func (l Limits) apply(o *options) { o.limits = l }

// NoLimit switches a limit off. The fixed caps still hold.
const NoLimit = -1

// Caps that hold even with NoLimit.
const (
	maxCallFrames = 1 << 20 // frames; the register stack's cap is reached first
	maxHostDepth  = 10000   // nested Go re-entries: each one uses Go stack
)

// Option is implemented by Lib, Limits and the results of the functions below.
type Option interface{ apply(*options) }

type optionFunc func(*options)

func (f optionFunc) apply(o *options) { f(o) }

// options is what the options of New collect.
type options struct {
	libs     Lib
	libsSet  bool
	limits   Limits
	globals  []namedValue
	modules  []namedValue
	sources  []namedValue
	loader   func(string) (string, error)
	output   io.Writer
	randSeed *uint64
	clock    func() time.Time
	observe  func(CallStats)
	autoBind *bool
	strict   bool
	recover  bool
	hardened bool
	checked  *bool // tests choose the loop
	converts []convertReg
	types    []*typeOptions
}

type namedValue struct {
	name string
	v    any
}

// Members holds the contents of a module: GoFuncs, scalars, strings, nested Members,
// pointers to bound types.
type Members map[string]any

// Global defines a global that every script of the Runtime sees, under a reserved name.
// Tables given here are frozen.
func Global(name string, v any) Option {
	return optionFunc(func(o *options) { o.globals = append(o.globals, namedValue{name, v}) })
}

// Module defines a module of Go members. It is a global under its name, and require returns
// the same table.
func Module(name string, members Members) Option {
	return optionFunc(func(o *options) { o.modules = append(o.modules, namedValue{name, members}) })
}

// SourceModule defines a module written in Lua, which require runs in the environment that
// loads it.
func SourceModule(name, src string) Option {
	return optionFunc(func(o *options) { o.sources = append(o.sources, namedValue{name, src}) })
}

// Loader gives require the source of modules not defined by Module or SourceModule. It runs
// only while a script loads.
func Loader(find func(name string) (src string, err error)) Option {
	return optionFunc(func(o *options) { o.loader = find })
}

// Output is where print and warn write; the default discards. Writes are serialised, so w
// need not be safe for concurrent use.
func Output(w io.Writer) Option { return optionFunc(func(o *options) { o.output = w }) }

// RandSeed seeds math.random at the start of every entry, for reproducible runs.
func RandSeed(seed uint64) Option { return optionFunc(func(o *options) { o.randSeed = &seed }) }

// Clock is the time source of the os library.
func Clock(now func() time.Time) Option { return optionFunc(func(o *options) { o.clock = now }) }

// Observe is called after every entry from Go with what it used.
func Observe(f func(CallStats)) Option { return optionFunc(func(o *options) { o.observe = f }) }

// AutoBind chooses whether Go types statically reachable from a binding are bound with the
// default rules. Without it, only types registered with Type are. It is on by default, and
// off under Hardened.
func AutoBind(on bool) Option { return optionFunc(func(o *options) { o.autoBind = &on }) }

// StrictGlobals makes reading an undefined global an error, instead of nil.
func StrictGlobals() Option { return optionFunc(func(o *options) { o.strict = true }) }

// RecoverPanics turns a panic in a host function into a *PanicError returned by the entry,
// instead of letting it continue.
func RecoverPanics() Option { return optionFunc(func(o *options) { o.recover = true }) }

// Hardened is the profile for untrusted scripts: the bounds-checked loop, AutoBind(false), no
// LibLoad, LibCoroutine, LibOS or LibIO, no NoLimit, and maps and slices passed by copy.
func Hardened() Option { return optionFunc(func(o *options) { o.hardened = true }) }

// withLoop chooses the loop, for tests: checked or fast.
func withLoop(checked bool) Option { return optionFunc(func(o *options) { o.checked = &checked }) }

// CallStats is what one entry from Go used, as Observe reports it.
type CallStats struct {
	Script, Func      string
	Steps, AllocBytes int64
	Err               error
}

// New makes a Runtime. It reports bad options: a value that cannot be converted, a duplicate
// name, a syntax error in a source module.
func New(opts ...Option) (*Runtime, error) {
	used.Store(true)
	var o options
	for _, opt := range opts {
		opt.apply(&o)
	}
	rt := &Runtime{
		libs:     LibSafe,
		loader:   o.loader,
		randSeed: o.randSeed,
		clock:    o.clock,
		observe:  o.observe,
		autoBind: !o.hardened,
		strict:   o.strict,
		recover:  o.recover,
		hardened: o.hardened,
		checked:  defaultChecked || o.hardened,
		reserved: map[string]bool{},
		sources:  map[string]*sourceModule{},
	}
	if o.libsSet {
		rt.libs = o.libs &^ LibNone
	}
	if o.autoBind != nil {
		rt.autoBind = *o.autoBind
	}
	if o.checked != nil && !o.hardened {
		rt.checked = *o.checked
	}
	if o.output != nil {
		rt.out = &syncWriter{w: o.output}
	}
	if rt.clock == nil {
		rt.clock = time.Now
	}
	if o.hardened {
		if bad := rt.libs & (LibLoad | LibCoroutine | LibOS | LibIO); bad != 0 {
			return nil, fmt.Errorf("glua: the hardened profile refuses %s", bad)
		}
	}
	if bad := rt.libs & (LibOS | LibIO); bad != 0 {
		return nil, fmt.Errorf("glua: %s is not available yet", bad)
	}
	for _, c := range o.converts {
		if err := c.register(); err != nil {
			return nil, err
		}
	}
	if err := rt.registerTypes(o.types); err != nil {
		return nil, err
	}
	var err error
	if rt.run, rt.init, err = resolveLimits(o.limits, o.hardened); err != nil {
		return nil, err
	}
	if err := rt.buildLayer(&o); err != nil {
		return nil, err
	}
	rt.pool.New = func() any { return rt.newExecutor() }
	return rt, nil
}

// MustNew is New for package-level variables. It panics on error.
func MustNew(opts ...Option) *Runtime {
	rt, err := New(opts...)
	if err != nil {
		panic(err)
	}
	return rt
}

// String returns the names of the flags in l, such as "LibLoad|LibCoroutine".
func (l Lib) String() string {
	names := []string{"LibBase", "LibString", "LibTable", "LibMath", "LibUTF8", "LibCoroutine", "LibOS", "LibIO", "LibLoad"}
	s := ""
	for i, name := range names {
		if l&(1<<i) != 0 {
			if s != "" {
				s += "|"
			}
			s += name
		}
	}
	if l&LibNone != 0 {
		if s != "" {
			s += "|"
		}
		s += "LibNone"
	}
	if s == "" {
		return "Lib(0)"
	}
	return s
}

// resolveLimits fills in defaults and checks the limits against the profile.
func resolveLimits(l Limits, hardened bool) (run, init limits, err error) {
	check := func(name string, v int64) error {
		if v < NoLimit {
			return fmt.Errorf("glua: Limits.%s is negative: %d", name, v)
		}
		if v == NoLimit && hardened {
			return fmt.Errorf("glua: the hardened profile refuses NoLimit for Limits.%s", name)
		}
		return nil
	}
	for _, f := range []struct {
		name string
		v    int64
	}{
		{"Steps", l.Steps}, {"AllocBytes", l.AllocBytes}, {"CallDepth", int64(l.CallDepth)},
		{"HostDepth", int64(l.HostDepth)}, {"InitSteps", l.InitSteps}, {"InitAllocBytes", l.InitAllocBytes},
	} {
		if err := check(f.name, f.v); err != nil {
			return run, init, err
		}
	}
	pick := func(v, def, unlimited int64) int64 {
		switch v {
		case 0:
			return def
		case NoLimit:
			return unlimited
		}
		return v
	}
	run = limits{
		steps:     pick(l.Steps, defaultSteps, math.MaxInt64),
		alloc:     pick(l.AllocBytes, defaultAlloc, math.MaxInt64),
		callDepth: int(min(pick(int64(l.CallDepth), defaultDepth, maxCallFrames), maxCallFrames)),
		hostDepth: int(min(pick(int64(l.HostDepth), defaultHost, maxHostDepth), maxHostDepth)),
	}
	init = run
	init.steps = pick(l.InitSteps, run.steps, math.MaxInt64)
	init.alloc = pick(l.InitAllocBytes, run.alloc, math.MaxInt64)
	return run, init, nil
}

// buildLayer builds the frozen table of what every environment starts with: the libraries,
// the host globals and the Go modules. Their names are reserved.
func (rt *Runtime) buildLayer(o *options) error {
	g := NewTable(0, 32)
	if rt.libs&LibBase != 0 {
		openBase(g)
	}
	if rt.libs&LibString != 0 {
		rt.strMeta = openString(g)
	}
	if rt.libs&LibTable != 0 {
		openTable(g)
	}
	if rt.libs&LibMath != 0 {
		openMath(g)
	}
	if rt.libs&LibUTF8 != 0 {
		openUTF8(g)
	}
	if rt.libs&LibCoroutine != 0 {
		g.Set(String("coroutine"), tableValue(coroutineLib()))
	}
	g.Set(String("_G"), Value{}) // per environment
	for k := range g.All() {
		rt.reserved[k.str()] = true
	}
	for _, name := range []string{"_G", "package", "require", "load"} {
		rt.reserved[name] = true
	}
	define := func(name string, v Value) error {
		if v.n>>56 == kindUser && !isScalar(v.p) {
			if ti := userType(v); ti != nil {
				if err := rt.checkType(ti.rt); err != nil {
					return bindName(name, err)
				}
			}
		}
		if !isName(name) {
			return &BindError{Name: name, Reason: "not a Lua name"}
		}
		if !g.Get(String(name)).IsNil() || rt.reserved[name] {
			return &BindError{Name: name, Reason: "the name is already defined"}
		}
		g.Set(String(name), v)
		rt.reserved[name] = true
		return nil
	}
	for _, nv := range o.globals {
		v, err := hostValue(nv.name, nv.v)
		if err != nil {
			return err
		}
		if err := define(nv.name, v); err != nil {
			return err
		}
	}
	rt.modules = NewTable(0, len(o.modules))
	for _, nv := range o.modules {
		v, err := hostValue(nv.name, nv.v)
		if err != nil {
			return err
		}
		if err := define(nv.name, v); err != nil {
			return err
		}
		rt.modules.Set(String(nv.name), v)
	}
	for _, nv := range o.sources {
		name, src := nv.name, nv.v.(string)
		if _, ok := rt.sources[name]; ok || !rt.modules.Get(String(name)).IsNil() {
			return &BindError{Name: name, Reason: "the module is already defined"}
		}
		m, err := compileModule(name, src)
		if err != nil {
			return err
		}
		rt.sources[name] = m
	}
	fr := newFreezer()
	fr.plans, fr.intern = rt.plans(), true
	fr.add(tableValue(g), "globals")
	if err := fr.walk(); err != nil {
		return &BindError{Reason: err.Error()}
	}
	fr.freeze()
	if err := freezeValue(tableValue(rt.modules), "package.loaded"); err != nil { // the modules are frozen already
		return &BindError{Reason: err.Error()}
	}
	if rt.strMeta != nil {
		rt.strMeta.freeze()
	}
	rt.layer = g
	return nil
}

// compileModule compiles the source of a module.
func compileModule(name, src string) (*sourceModule, error) {
	p, info, err := compileChunk(chunkID("="+name), src)
	if err != nil {
		return nil, syntaxError(err)
	}
	setChunkName(p, "="+name)
	return &sourceModule{p: p, info: info}, nil
}

// newExecutor makes an executor for this Runtime's pool.
func (rt *Runtime) newExecutor() *executor {
	ex := newExecutor()
	ex.rt = rt
	ex.strMeta = rt.strMeta
	ex.randSeed = rt.randSeed
	ex.checked = rt.checked
	if rt.out != nil {
		ex.output = rt.out
	}
	return ex
}

// setLimits gives the executor the limits of the entry about to start.
func (ex *executor) setLimits(l *limits) {
	ex.maxSteps, ex.maxAlloc = l.steps, l.alloc
	ex.maxCallDepth, ex.maxHostDepth = l.callDepth, l.hostDepth
}

// syncWriter serialises writes to the Output writer.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(b)
}

// isName reports whether s is a Lua name: letters, digits and underscores, not starting
// with a digit, and not a reserved word.
func isName(s string) bool {
	if s == "" || syntax.IsReserved(s) {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || i > 0 && '0' <= c && c <= '9') {
			return false
		}
	}
	return true
}

// hostValue converts a value given to Global or Module. Functions must be GoFuncs: a bare
// Go function needs an explicit adapter.
func hostValue(name string, v any) (Value, error) {
	switch x := v.(type) {
	case GoFunc:
		return goFnValue(&goFunc{name: name, f: x, host: true}), nil
	case func(*Call) error:
		return goFnValue(&goFunc{name: name, f: x, host: true}), nil
	case Members:
		t := NewTable(0, len(x))
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			mv, err := hostValue(k, x[k])
			if err != nil {
				var be *BindError
				if errors.As(err, &be) {
					be.Name = name + "." + be.Name
				}
				return Value{}, err
			}
			t.Set(String(k), mv)
		}
		return tableValue(t), nil
	}
	if v != nil && reflect.TypeOf(v).Kind() == reflect.Func {
		return Value{}, &BindError{Name: name, Type: reflect.TypeOf(v), Reason: "a Go function needs an adapter, such as lua.Fn1 or lua.Reflect"}
	}
	val, err := toValue(v)
	if err != nil {
		return Value{}, &BindError{Name: name, Type: reflect.TypeOf(v), Reason: err.Error()}
	}
	return val, nil
}

// newEnv builds the globals table of a new environment: the Runtime's layer, plus _G,
// package, require and, with LibLoad, load, which belong to the environment.
func (rt *Runtime) newEnv(ld *envLoader) *Table {
	env := NewTable(0, rt.layer.used+8)
	for k, v := range rt.layer.All() {
		env.set(k, v)
	}
	env.set(String("_G"), tableValue(env))
	if rt.libs&LibLoad != 0 {
		env.set(String("load"), goFnValue(&goFunc{name: "load", f: func(c *Call) error { return baseLoad(c, env) }}))
	}
	if rt.libs&LibBase != 0 {
		pkg := NewTable(0, 2)
		pkg.set(String("loaded"), tableValue(ld.loaded))
		env.set(String("package"), tableValue(pkg))
		env.set(String("require"), goFnValue(&goFunc{name: "require", f: ld.require}))
	}
	if rt.strict {
		mt := NewTable(0, 1)
		mt.set(String("__index"), goFnValue(strictIndex))
		env.meta = mt
	}
	env.flags |= tabGuarded
	ld.env = env
	return env
}

// strictIndex is the __index of an environment under StrictGlobals.
var strictIndex = &goFunc{name: "strict", f: func(c *Call) error {
	k := c.Value(1)
	if k.isString() {
		return c.libErrorf("undefined global '%s'", k.str())
	}
	return c.libErrorf("undefined global %s", k.String())
}}

// envLoader is the record of the modules an environment loaded. require can load a module
// for the first time only while the environment is being built, or in a private VM.
type envLoader struct {
	rt     *Runtime
	env    *Table
	loaded *Table // package.loaded of the environment
}

func newEnvLoader(rt *Runtime) *envLoader {
	ld := &envLoader{rt: rt, loaded: NewTable(0, rt.modules.used+4)}
	for k, v := range rt.modules.All() {
		ld.loaded.set(k, v)
	}
	return ld
}

// require is require(name): the module from package.loaded, or else a source module or
// one the Loader finds, run now and recorded.
func (ld *envLoader) require(c *Call) error {
	name, err := c.checkString(0, "require")
	if err != nil {
		return err
	}
	if v := ld.loaded.Get(String(name)); !v.IsNil() {
		return c.ReturnValues(v)
	}
	if ld.loaded.Frozen() {
		return c.libErrorf("module '%s' not found: modules can be loaded only while the script loads", name)
	}
	m := ld.rt.sources[name]
	if m == nil && ld.rt.loader != nil {
		src, err := ld.rt.loader(name)
		if err != nil {
			return c.libErrorf("module '%s' not found: %s", name, err.Error())
		}
		if err := c.ex.charge(len(src)); err != nil {
			return err
		}
		if m, err = compileModule(name, src); err != nil {
			return c.libErrorf("error loading module '%s': %s", name, err.Error())
		}
	}
	if m == nil {
		return c.libErrorf("module '%s' not found", name)
	}
	res, err := c.call(luaFnValue(newMainClosure(m.p, m.info, ld.env)), String(name))
	if err != nil {
		return err
	}
	v := Bool(true)
	if len(res) > 0 && !res[0].IsNil() {
		v = res[0]
	}
	if got := ld.loaded.Get(String(name)); !got.IsNil() {
		v = got // the module set package.loaded itself
	}
	ld.loaded.Set(String(name), v)
	return c.ReturnValues(v)
}
