// Package glua here is the API sketch from the design's appendix, with every body
// replaced by a panic. It exists to prove that the public API, generic methods
// included, compiles on Go 1.27 and that the design's examples type-check against it.
// Nothing imports it outside its own tests, and it goes away once the real API lands.
package glua

import (
	"context"
	"errors"
	"io"
	"iter"
	"reflect"
	"time"
)

const sketch = "glua: API sketch only"

// Runtime is the host side of the sandbox: libraries, globals, modules, bound Go types, limits.
// It never changes after New, so it is safe to share between goroutines.
type Runtime struct{}

// New reports bad options: a value that cannot be converted, a duplicate name, a syntax error in a source module.
func New(opts ...Option) (*Runtime, error) { panic(sketch) }

// MustNew is New for package-level variables. It panics on error.
func MustNew(opts ...Option) *Runtime { panic(sketch) }

// Load compiles src, runs its top level once under the init limits, then freezes everything it built.
func (rt *Runtime) Load(ctx context.Context, name, src string) (*Script, error) { panic(sketch) }

// Expr compiles an expression, or a chunk, whose free names are the fields of E and then the Runtime's globals.
func (rt *Runtime) Expr[E, R any](name, src string) (*Rule[E, R], error) { panic(sketch) }

// Eval loads src on a default Runtime (LibSafe, default limits) and decodes the chunk's first result.
func Eval[R any](ctx context.Context, src string) (R, error) { panic(sketch) }

// SetHashSeed fixes the process-wide hash seed, and with it the order of pairs, for tests and replay.
func SetHashSeed(seed uint64) { panic(sketch) }

// Lib options are OR-ed together. With none given, LibSafe is used.
type Lib uint

const (
	LibBase Lib = 1 << iota
	LibString
	LibTable
	LibMath
	LibUTF8
	LibCoroutine
	LibOS       // clock, time, date, difftime
	LibIO       // trusted scripts only
	LibLoad     // load(): compile text at run time
	LibNone Lib = 1 << 31
	LibSafe     = LibBase | LibString | LibTable | LibMath | LibUTF8
)

func (Lib) option() {}

// Limits apply to one entry from Go, including everything it re-enters.
// A zero field takes the default. NoLimit switches a limit off.
type Limits struct {
	Steps          int64
	AllocBytes     int64
	CallDepth      int
	HostDepth      int
	InitSteps      int64
	InitAllocBytes int64
}

func (Limits) option() {}

const NoLimit = -1

// Option is implemented by Lib, Limits and the results of the functions below.
type Option interface{ option() }

// Members holds the contents of a module: GoFuncs, scalars, strings, nested Members, pointers to bound types.
type Members map[string]any

func Global(name string, v any) Option                             { panic(sketch) }
func Module(name string, members Members) Option                   { panic(sketch) }
func SourceModule(name, src string) Option                         { panic(sketch) }
func Loader(find func(name string) (src string, err error)) Option { panic(sketch) }
func Output(w io.Writer) Option                                    { panic(sketch) }
func RandSeed(seed uint64) Option                                  { panic(sketch) }
func Clock(now func() time.Time) Option                            { panic(sketch) }
func Observe(f func(CallStats)) Option                             { panic(sketch) }
func AutoBind(on bool) Option                                      { panic(sketch) }
func StrictGlobals() Option                                        { panic(sketch) }
func RecoverPanics() Option                                        { panic(sketch) }
func Hardened() Option                                             { panic(sketch) }

type CallStats struct {
	Script, Func      string
	Steps, AllocBytes int64
	Err               error
}

// Type customises how *T appears in Lua.
func Type[T any](opts ...TypeOption) Option { panic(sketch) }

// Convert gives T a scalar or string form instead of a userdata.
func Convert[T any](enc func(T) Value, dec func(Value) (T, error)) Option { panic(sketch) }

type TypeOption interface{ typeOption() }

func Method(name string, f GoFunc) TypeOption { panic(sketch) }
func Meta(event string, f GoFunc) TypeOption  { panic(sketch) }
func Writable(fields ...string) TypeOption    { panic(sketch) }
func Hide(fields ...string) TypeOption        { panic(sketch) }
func Rename(field, luaName string) TypeOption { panic(sketch) }

// Script is a loaded script: verified code plus the frozen state its top level built. Share freely.
type Script struct{}

func (s *Script) Func0[R any](name string) (func(context.Context) (R, error), error) { panic(sketch) }
func (s *Script) Func1[A, R any](name string) (func(context.Context, A) (R, error), error) {
	panic(sketch)
}
func (s *Script) Func2[A, B, R any](name string) (func(context.Context, A, B) (R, error), error) {
	panic(sketch)
}
func (s *Script) Func3[A, B, C, R any](name string) (func(context.Context, A, B, C) (R, error), error) {
	panic(sketch)
}

func (s *Script) Get[T any](name string) (T, error)                                { panic(sketch) }
func (s *Script) Globals() *Table                                                  { panic(sketch) }
func (s *Script) Result() Value                                                    { panic(sketch) }
func (s *Script) Call(ctx context.Context, fn Value, args ...Value) (Value, error) { panic(sketch) }
func (s *Script) CallN(ctx context.Context, dst []Value, fn Value, args ...Value) ([]Value, error) {
	panic(sketch)
}
func (s *Script) WithLimits(l Limits) (*Script, error)   { panic(sketch) }
func (s *Script) NewVM(ctx context.Context) (*VM, error) { panic(sketch) }

// Rule is a compiled Expr.
type Rule[E, R any] struct{}

func (r *Rule[E, R]) Eval(ctx context.Context, env *E) (R, error) { panic(sketch) }

// VM is a private, mutable instance of a Script. One goroutine at a time.
type VM struct{}

func (vm *VM) Get[T any](name string) (T, error)                                  { panic(sketch) }
func (vm *VM) Set[T any](name string, v T) error                                  { panic(sketch) }
func (vm *VM) Globals() *Table                                                    { panic(sketch) }
func (vm *VM) Func0[R any](name string) (func(context.Context) (R, error), error) { panic(sketch) }
func (vm *VM) Func1[A, R any](name string) (func(context.Context, A) (R, error), error) {
	panic(sketch)
}
func (vm *VM) Func2[A, B, R any](name string) (func(context.Context, A, B) (R, error), error) {
	panic(sketch)
}
func (vm *VM) Func3[A, B, C, R any](name string) (func(context.Context, A, B, C) (R, error), error) {
	panic(sketch)
}
func (vm *VM) Call(ctx context.Context, fn Value, args ...Value) (Value, error) { panic(sketch) }
func (vm *VM) CallN(ctx context.Context, dst []Value, fn Value, args ...Value) ([]Value, error) {
	panic(sketch)
}

type Kind uint8

const (
	KindNil Kind = iota
	KindBool
	KindInt
	KindFloat
	KindString
	KindTable
	KindFunction
	KindUserdata
	KindThread
)

// Value is any Lua value. The zero Value is nil. It cannot be compared with ==; use Equal.
type Value struct {
	_ [0]func()
}

func Bool(b bool) Value        { panic(sketch) }
func Int(i int64) Value        { panic(sketch) }
func Float(f float64) Value    { panic(sketch) }
func String(s string) Value    { panic(sketch) }
func ValueOf[T any](v T) Value { panic(sketch) }

func (v Value) Kind() Kind             { panic(sketch) }
func (v Value) IsNil() bool            { panic(sketch) }
func (v Value) Truthy() bool           { panic(sketch) }
func (v Value) Equal(w Value) bool     { panic(sketch) }
func (v Value) Int() (int64, bool)     { panic(sketch) }
func (v Value) Float() (float64, bool) { panic(sketch) }
func (v Value) Str() (string, bool)    { panic(sketch) }
func (v Value) As[T any]() (T, error)  { panic(sketch) }
func (v Value) String() string         { panic(sketch) }

// Pair as a result type decodes the first two results.
type Pair[A, B any] struct {
	First  A
	Second B
}

// Table methods are raw: they never run metamethods.
type Table struct{}

func NewTable(narr, nhash int) *Table { panic(sketch) }

func (t *Table) Get(key Value) Value                 { panic(sketch) }
func (t *Table) Set(key, val Value)                  { panic(sketch) }
func (t *Table) Field[T any](name string) (T, error) { panic(sketch) }
func (t *Table) SetField[T any](name string, v T)    { panic(sketch) }
func (t *Table) Len() int                            { panic(sketch) }
func (t *Table) All() iter.Seq2[Value, Value]        { panic(sketch) }
func (t *Table) Items() iter.Seq2[int, Value]        { panic(sketch) }
func (t *Table) Freeze() error                       { panic(sketch) }
func (t *Table) Frozen() bool                        { panic(sketch) }

// GoFunc is the one native signature. Everything else adapts to it.
type GoFunc func(c *Call) error

// Call is the view a host function has of its invocation.
type Call struct{}

func (c *Call) Context() context.Context                                      { panic(sketch) }
func (c *Call) Len() int                                                      { panic(sketch) }
func (c *Call) Value(i int) Value                                             { panic(sketch) }
func (c *Call) Arg[T any](i int) T                                            { panic(sketch) }
func (c *Call) Opt[T any](i int, def T) T                                     { panic(sketch) }
func (c *Call) Return[T any](v T) error                                       { panic(sketch) }
func (c *Call) Return2[T, U any](v T, u U) error                              { panic(sketch) }
func (c *Call) ReturnValues(vs ...Value) error                                { panic(sketch) }
func (c *Call) Invoke(fn Value, args ...Value) (Value, error)                 { panic(sketch) }
func (c *Call) InvokeN(dst []Value, fn Value, args ...Value) ([]Value, error) { panic(sketch) }
func (c *Call) Errorf(format string, args ...any) error                       { panic(sketch) }

func Fn0[R any](f func() R) GoFunc                           { panic(sketch) }
func Fn1[A, R any](f func(A) R) GoFunc                       { panic(sketch) }
func Fn2[A, B, R any](f func(A, B) R) GoFunc                 { panic(sketch) }
func Fn3[A, B, C, R any](f func(A, B, C) R) GoFunc           { panic(sketch) }
func Fn0E[R any](f func() (R, error)) GoFunc                 { panic(sketch) }
func Fn1E[A, R any](f func(A) (R, error)) GoFunc             { panic(sketch) }
func Fn2E[A, B, R any](f func(A, B) (R, error)) GoFunc       { panic(sketch) }
func Fn3E[A, B, C, R any](f func(A, B, C) (R, error)) GoFunc { panic(sketch) }
func Do0(f func()) GoFunc                                    { panic(sketch) }
func Do1[A any](f func(A)) GoFunc                            { panic(sketch) }
func Do2[A, B any](f func(A, B)) GoFunc                      { panic(sketch) }
func Do3[A, B, C any](f func(A, B, C)) GoFunc                { panic(sketch) }
func Do0E(f func() error) GoFunc                             { panic(sketch) }
func Do1E[A any](f func(A) error) GoFunc                     { panic(sketch) }
func Do2E[A, B any](f func(A, B) error) GoFunc               { panic(sketch) }
func Do3E[A, B, C any](f func(A, B, C) error) GoFunc         { panic(sketch) }

// Reflect adapts any Go function. Convenient and slow; it is never applied implicitly.
func Reflect(fn any) GoFunc { panic(sketch) }

// Error is a runtime error raised by a script or by a host function it called.
type Error struct {
	Value Value
}

func (e *Error) Error() string      { panic(sketch) }
func (e *Error) Unwrap() error      { panic(sketch) }
func (e *Error) Traceback() []Frame { panic(sketch) }

type Frame struct {
	Source   string
	Line     int
	Function string
}

type SyntaxError struct {
	Source    string
	Line, Col int
	Msg       string
}

func (e *SyntaxError) Error() string { panic(sketch) }

type BindError struct {
	Name   string
	Type   reflect.Type
	Reason string
}

func (e *BindError) Error() string { panic(sketch) }

type PanicError struct {
	Value any
	Stack []byte
}

func (e *PanicError) Error() string { panic(sketch) }

var (
	ErrSteps       = errors.New("glua: step limit exceeded")
	ErrAlloc       = errors.New("glua: allocation limit exceeded")
	ErrHostDepth   = errors.New("glua: too many nested calls between Go and Lua")
	ErrUndefined   = errors.New("glua: name is not defined")
	ErrNotFunction = errors.New("glua: value is not a function")
)
