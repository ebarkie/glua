package syntax

import (
	"cmp"
	"fmt"
	"slices"
)

const (
	maxVars   = 200 // active locals per function, as in C Lua
	maxUpvals = 255 // upvalues per function
)

// Var is a local variable: a parameter, a loop variable or a local declaration.
type Var struct {
	Name     string
	Attrib   string // "", "const" or "close"
	Func     *FuncInfo
	Line     int
	Captured bool // an inner function refers to it
	Assigned bool // it is assigned somewhere other than its declaration
}

// Cell reports whether the variable needs a cell: inner functions see it, and it changes.
// A captured variable that never changes is copied into the closures instead, which is
// Luau's rule. A fresh cell each time the declaration runs gives Lua's per-iteration
// semantics.
func (v *Var) Cell() bool { return v.Captured && v.Assigned }

// FuncInfo is what resolution found out about one function.
type FuncInfo struct {
	Node   *FunctionExpr // nil for the pseudo function that holds the main chunk's _ENV
	Parent *FuncInfo
	Upvals []*Upval // in order of first use
}

// Upval is one captured variable of a function: a local of the enclosing function, or one
// of its upvalues.
type Upval struct {
	Name   string
	Var    *Var
	Parent *Upval // nil when Var is a local of the immediately enclosing function
	Index  int
}

// RefKind says what a name refers to.
type RefKind uint8

const (
	RefLocal  RefKind = iota // a local of the current function
	RefUpval                 // an upvalue of the current function
	RefGlobal                // a free name: a field of _ENV
)

// Ref is the binding of one NameExpr.
type Ref struct {
	Kind  RefKind
	Var   *Var   // RefLocal, RefUpval
	Upval *Upval // RefUpval
	// Env, for a RefGlobal in a chunk that declares or assigns _ENV, is the binding of the
	// _ENV that is visible there; the name is a field of it. It is nil in the common case,
	// where free names go straight to the closure's globals table.
	Env *Ref
	// Field, in an expression chunk, is the field of the environment parameter a free name
	// names; Var and Upval then bind that parameter.
	Field string
}

// ExprEnv makes a chunk an expression over a Go struct: its main function takes the struct
// as the parameter Param, and a free name that is one of Fields is a field of it. Any other
// free name must be Known. Fields maps each name to whether it may be assigned.
type ExprEnv struct {
	Param  string
	Fields map[string]bool
	Known  func(name string) bool
}

// Info is the result of resolution.
type Info struct {
	Main  *FuncInfo
	Funcs map[*FunctionExpr]*FuncInfo
	Names map[*NameExpr]Ref
	// Decls holds the variables each declaration introduces, in order: for a LocalStmt,
	// LocalFunctionStmt, NumericForStmt, GenericForStmt, or a FunctionExpr's parameters.
	Decls map[any][]*Var
	// ExplicitEnv is set when the chunk declares or assigns _ENV. Free names are then
	// fields of whichever _ENV is visible, and EnvVar is the main chunk's own _ENV.
	ExplicitEnv bool
	EnvVar      *Var
}

// Resolve binds every name in the chunk fn and checks what C Lua checks while parsing:
// gotos and labels, break outside a loop, assignments to const variables, and the limits
// on locals and upvalues.
func Resolve(source string, fn *FunctionExpr) (info *Info, err error) {
	return ResolveExpr(source, fn, nil)
}

// ResolveExpr is Resolve for a chunk whose free names env resolves. fn must take env.Param
// as its first parameter. A name neither a field nor known, an assignment to a read-only
// field and an assignment to a global are errors.
func ResolveExpr(source string, fn *FunctionExpr, xenv *ExprEnv) (info *Info, err error) {
	r := &resolver{
		source:  source,
		exprEnv: xenv,
		info: &Info{
			Funcs: map[*FunctionExpr]*FuncInfo{},
			Names: map[*NameExpr]Ref{},
			Decls: map[any][]*Var{},
		},
	}
	r.info.ExplicitEnv = usesEnv(fn)
	// The main chunk's _ENV is a local of a function around it, so the main chunk sees it
	// as upvalue 0, as in C Lua.
	outer := &FuncInfo{}
	env := &Var{Name: "_ENV", Func: outer}
	r.info.EnvVar = env
	top := &funcState{info: outer}
	top.block = &blockState{fs: top, vars: []*Var{env}}
	r.fs = top

	main := r.openFunc(fn)
	if r.info.ExplicitEnv {
		r.upval(r.fs, env) // upvalue 0
	}
	r.info.Main = main.info
	r.block(fn.Body, false, false)
	r.closeFunc()
	if len(r.errs) > 0 {
		// C Lua stops at the first error its single pass detects; report the one it would see first.
		e := slices.MinFunc(r.errs, func(a, b *Error) int { return cmp.Compare(a.Line, b.Line) })
		return nil, e
	}
	return r.info, nil
}

type resolver struct {
	source  string
	info    *Info
	fs      *funcState
	errs    []*Error
	exprEnv *ExprEnv
}

type funcState struct {
	info    *FuncInfo
	parent  *funcState
	block   *blockState
	nactive int // active locals
	upvals  map[*Var]*Upval
	node    *FunctionExpr
	gotos   []*pendingGoto // unresolved, reported when the function closes
}

type blockState struct {
	fs     *funcState
	parent *blockState
	vars   []*Var
	loop   bool
	repeat bool // a repeat body: its locals stay in scope for the until condition
	labels []*labelInfo
	stmts  []Stmt
	cur    int // index of the statement being resolved
}

type labelInfo struct {
	name      string
	line      int
	afterLine int
	idx       int // statement index in its block
	nvars     int // the block's locals active at the label
	last      bool
}

type pendingGoto struct {
	label string
	line  int
	brk   bool
}

func (r *resolver) errorf(line int, format string, args ...any) {
	r.errs = append(r.errs, &Error{Source: r.source, Line: line, Msg: fmt.Sprintf(format, args...)})
}

func (r *resolver) openFunc(fn *FunctionExpr) *funcState {
	fs := &funcState{
		info:   &FuncInfo{Node: fn, Parent: r.fs.info},
		parent: r.fs,
		upvals: map[*Var]*Upval{},
		node:   fn,
	}
	r.info.Funcs[fn] = fs.info
	r.fs = fs
	fs.block = &blockState{fs: fs}
	var params []*Var
	for _, name := range fn.Params {
		params = append(params, r.declare(name, "", fn.Line))
	}
	r.info.Decls[fn] = params
	return fs
}

func (r *resolver) closeFunc() {
	fs := r.fs
	for _, g := range fs.gotos {
		if g.brk {
			r.errorf(fs.node.CloseLine, "break outside loop at line %d", g.line)
		} else {
			r.errorf(fs.node.CloseLine, "no visible label '%s' for <goto> at line %d", g.label, g.line)
		}
	}
	r.fs = fs.parent
}

func (r *resolver) where(fs *funcState) string {
	if fs.node == nil || fs.node.Line == 0 {
		return "main function"
	}
	return fmt.Sprintf("function at line %d", fs.node.Line)
}

// declare adds a local to the current block.
func (r *resolver) declare(name, attrib string, line int) *Var {
	fs := r.fs
	v := &Var{Name: name, Attrib: attrib, Func: fs.info, Line: line}
	fs.block.vars = append(fs.block.vars, v)
	fs.nactive++
	if fs.nactive > maxVars {
		r.errorf(line, "too many local variables (limit is %d) in %s", maxVars, r.where(fs))
	}
	return v
}

// lookup finds the variable a name refers to, innermost first, across function boundaries.
func lookup(fs *funcState, name string) *Var {
	for ; fs != nil; fs = fs.parent {
		for b := fs.block; b != nil; b = b.parent {
			for i := len(b.vars) - 1; i >= 0; i-- {
				if b.vars[i].Name == name {
					return b.vars[i]
				}
			}
		}
	}
	return nil
}

// upval returns fs's upvalue for v, a local of an enclosing function, creating the chain
// of upvalues through the functions in between.
func (r *resolver) upval(fs *funcState, v *Var) *Upval {
	if u, ok := fs.upvals[v]; ok {
		return u
	}
	u := &Upval{Name: v.Name, Var: v, Index: len(fs.info.Upvals)}
	if v.Func != fs.parent.info {
		u.Parent = r.upval(fs.parent, v)
	}
	v.Captured = true
	fs.upvals[v] = u
	fs.info.Upvals = append(fs.info.Upvals, u)
	if len(fs.info.Upvals) > maxUpvals {
		line := 0
		if fs.node != nil {
			line = fs.node.Line
		}
		r.errorf(line, "too many upvalues (limit is %d) in %s", maxUpvals, r.where(fs))
	}
	return u
}

// bind resolves a name in the current function.
func (r *resolver) bind(name string) Ref {
	v := lookup(r.fs, name)
	switch {
	case v == nil && r.exprEnv != nil && name != r.exprEnv.Param:
		if _, ok := r.exprEnv.Fields[name]; ok {
			ref := r.bind(r.exprEnv.Param)
			ref.Field = name
			return ref
		}
		fallthrough
	case v == nil:
		ref := Ref{Kind: RefGlobal}
		if r.info.ExplicitEnv {
			env := r.bind("_ENV") // always found: the main chunk's _ENV at least
			ref.Env = &env
		}
		return ref
	case v.Func == r.fs.info:
		return Ref{Kind: RefLocal, Var: v}
	default:
		return Ref{Kind: RefUpval, Var: v, Upval: r.upval(r.fs, v)}
	}
}

// block resolves a block in a new scope.
func (r *resolver) block(b *Block, loop, repeat bool, cond ...Expr) {
	fs := r.fs
	bs := &blockState{fs: fs, parent: fs.block, loop: loop, repeat: repeat, stmts: b.Stmts}
	fs.block = bs
	saved := fs.nactive
	for i, s := range b.Stmts {
		bs.cur = i
		r.stmt(s, i)
	}
	for _, c := range cond { // repeat's until condition sees the body's locals
		r.expr(c)
	}
	fs.block = bs.parent
	fs.nactive = saved
}

func (r *resolver) stmt(s Stmt, idx int) {
	switch s := s.(type) {
	case *AssignStmt:
		for _, v := range s.Values {
			r.expr(v)
		}
		for i, t := range s.Targets {
			if n, ok := t.(*NameExpr); ok {
				ref := r.bind(n.Name)
				r.info.Names[n] = ref
				if r.exprEnv != nil {
					switch {
					case ref.Field != "" && !r.exprEnv.Fields[ref.Field]:
						r.errorf(s.TargetLines[i], "cannot assign to read-only field '%s'", n.Name)
					case ref.Kind == RefGlobal && r.exprEnv.Known(n.Name):
						r.errorf(s.TargetLines[i], "cannot assign to global '%s'", n.Name)
					case ref.Kind == RefGlobal:
						r.errorf(s.TargetLines[i], "unknown name '%s'", n.Name)
					}
					if ref.Field != "" {
						continue
					}
				}
				if ref.Var != nil {
					if ref.Var.Attrib != "" {
						r.errorf(s.TargetLines[i], "attempt to assign to const variable '%s'", n.Name)
					}
					ref.Var.Assigned = true
				}
			} else {
				r.expr(t)
			}
		}
	case *CallStmt:
		r.expr(s.Call)
	case *DoStmt:
		r.block(s.Body, false, false)
	case *WhileStmt:
		r.expr(s.Cond)
		r.block(s.Body, true, false)
	case *RepeatStmt:
		r.block(s.Body, true, true, s.Cond)
	case *IfStmt:
		for i, c := range s.Conds {
			r.expr(c)
			r.block(s.Blocks[i], false, false)
		}
		if s.Else != nil {
			r.block(s.Else, false, false)
		}
	case *NumericForStmt:
		r.expr(s.Start)
		r.expr(s.Limit)
		if s.Step != nil {
			r.expr(s.Step)
		}
		r.forBody(s, []string{s.Var}, s.Body, s.Line)
	case *GenericForStmt:
		for _, e := range s.Exprs {
			r.expr(e)
		}
		r.forBody(s, s.Names, s.Body, s.Line)
	case *FunctionStmt:
		if n, ok := s.Target.(*NameExpr); ok {
			ref := r.bind(n.Name)
			r.info.Names[n] = ref
			if ref.Var != nil {
				if ref.Var.Attrib != "" {
					r.errorf(s.Line, "attempt to assign to const variable '%s'", n.Name)
				}
				ref.Var.Assigned = true
			}
		} else {
			r.expr(s.Target)
		}
		r.function(s.Func)
	case *LocalFunctionStmt:
		v := r.declare(s.Name, "", s.Line) // in scope in its own body
		r.info.Decls[s] = []*Var{v}
		r.function(s.Func)
	case *LocalStmt:
		for _, e := range s.Values {
			r.expr(e)
		}
		var vars []*Var
		for i, name := range s.Names {
			vars = append(vars, r.declare(name, s.Attribs[i], s.Line))
		}
		r.info.Decls[s] = vars
	case *ReturnStmt:
		for _, e := range s.Values {
			r.expr(e)
		}
	case *BreakStmt:
		r.gotoStmt("break", s.Line, true, idx)
	case *GotoStmt:
		r.gotoStmt(s.Label, s.Line, false, idx)
	case *LabelStmt:
		r.label(s, idx)
	}
}

// forBody resolves a loop body with its control variables. The variables live in a scope of
// their own around the body, as in C Lua.
func (r *resolver) forBody(node Stmt, names []string, body *Block, line int) {
	fs := r.fs
	outer := &blockState{fs: fs, parent: fs.block}
	fs.block = outer
	saved := fs.nactive
	var vars []*Var
	for _, n := range names {
		vars = append(vars, r.declare(n, "", line))
	}
	r.info.Decls[node] = vars
	r.block(body, true, false)
	fs.block = outer.parent
	fs.nactive = saved
}

func (r *resolver) function(fn *FunctionExpr) {
	r.openFunc(fn)
	r.block(fn.Body, false, false)
	r.closeFunc()
}

// label declares a label after checking that no visible label has its name.
func (r *resolver) label(s *LabelStmt, idx int) {
	bs := r.fs.block
	for b := bs; b != nil; b = b.parent {
		for _, l := range b.labels {
			if l.name == s.Name {
				r.errorf(s.AfterLine, "label '%s' already defined on line %d", s.Name, l.line)
				return
			}
		}
	}
	// A label followed only by other labels is at the end of its block: as far as gotos are
	// concerned, it is outside the scope of the block's locals. Not in a repeat body, whose
	// locals the until condition still sees.
	last := !bs.repeat
	for _, next := range bs.stmts[idx+1:] {
		if _, ok := next.(*LabelStmt); !ok {
			last = false
			break
		}
	}
	bs.labels = append(bs.labels, &labelInfo{
		name: s.Name, line: s.Line, afterLine: s.AfterLine, idx: idx, nvars: len(bs.vars), last: last,
	})
}

// gotoStmt checks a goto, or a break as a goto to the end of the innermost loop. The label
// may come later in the source, so the check runs over the whole statement lists of the
// enclosing blocks, which the tree has in full.
func (r *resolver) gotoStmt(name string, line int, brk bool, idx int) {
	fs := r.fs
	if brk {
		for b := fs.block; b != nil; b = b.parent {
			if b.loop {
				return
			}
		}
		fs.gotos = append(fs.gotos, &pendingGoto{label: name, line: line, brk: true})
		return
	}
	// pos is the goto's statement index in block b, and nvars the locals of b active there.
	pos, nvars := idx, len(fs.block.vars)
	for b := fs.block; b != nil; b = b.parent {
		for j, s := range b.stmts {
			l, ok := s.(*LabelStmt)
			if !ok || l.Name != name {
				continue
			}
			if j > pos {
				// forward: the label is declared later, so count the locals declared up to it
				n := countLocals(b, j)
				if !r.labelIsLast(b, j) && n > nvars {
					r.errorf(l.AfterLine, "<goto %s> at line %d jumps into the scope of local '%s'",
						name, line, localName(b, nvars, j))
				}
			}
			return
		}
		if b.parent == nil {
			break
		}
		// continue in the enclosing block, from the statement that contains this block
		pos = b.parent.cur // the statement that contains block b
		nvars = countLocals(b.parent, pos)
	}
	fs.gotos = append(fs.gotos, &pendingGoto{label: name, line: line})
}

// The scope stack only holds what has been resolved so far, so for a forward label these
// helpers look at the block's statements directly.

func (r *resolver) labelIsLast(b *blockState, j int) bool {
	if b.repeat {
		return false
	}
	for _, next := range b.stmts[j+1:] {
		if _, ok := next.(*LabelStmt); !ok {
			return false
		}
	}
	return true
}

// countLocals returns the number of locals the statements of b before index j declare.
func countLocals(b *blockState, j int) int {
	n := 0
	for _, s := range b.stmts[:min(j, len(b.stmts))] {
		switch s := s.(type) {
		case *LocalStmt:
			n += len(s.Names)
		case *LocalFunctionStmt:
			n++
		}
	}
	return n
}

// localName returns the name of local number n of block b, counting the declarations of
// its statements before index j.
func localName(b *blockState, n, j int) string {
	for _, s := range b.stmts[:j] {
		switch s := s.(type) {
		case *LocalStmt:
			if n < len(s.Names) {
				return s.Names[n]
			}
			n -= len(s.Names)
		case *LocalFunctionStmt:
			if n == 0 {
				return s.Name
			}
			n--
		}
	}
	return "?"
}

func (r *resolver) expr(e Expr) {
	switch e := e.(type) {
	case *NameExpr:
		ref := r.bind(e.Name)
		r.info.Names[e] = ref
		if r.exprEnv != nil && ref.Kind == RefGlobal && ref.Env == nil && !r.exprEnv.Known(e.Name) {
			r.errorf(e.Line, "unknown name '%s'", e.Name)
		}
	case *IndexExpr:
		r.expr(e.Obj)
		r.expr(e.Key)
	case *CallExpr:
		r.expr(e.Fn)
		for _, a := range e.Args {
			r.expr(a)
		}
	case *MethodCallExpr:
		r.expr(e.Obj)
		for _, a := range e.Args {
			r.expr(a)
		}
	case *FunctionExpr:
		r.function(e)
	case *BinaryExpr:
		r.expr(e.L)
		r.expr(e.R)
	case *UnaryExpr:
		r.expr(e.X)
	case *TableExpr:
		for _, f := range e.Fields {
			if f.Key != nil {
				r.expr(f.Key)
			}
			r.expr(f.Value)
		}
	case *ParenExpr:
		r.expr(e.X)
	}
}

// usesEnv reports whether the chunk declares a variable named _ENV or assigns to _ENV.
func usesEnv(fn *FunctionExpr) bool {
	found := false
	var expr func(Expr)
	var block func(*Block)
	names := func(ns []string) {
		if slices.Contains(ns, "_ENV") {
			found = true
		}
	}
	expr = func(e Expr) {
		switch e := e.(type) {
		case *IndexExpr:
			expr(e.Obj)
			expr(e.Key)
		case *CallExpr:
			expr(e.Fn)
			for _, a := range e.Args {
				expr(a)
			}
		case *MethodCallExpr:
			expr(e.Obj)
			for _, a := range e.Args {
				expr(a)
			}
		case *FunctionExpr:
			names(e.Params)
			block(e.Body)
		case *BinaryExpr:
			expr(e.L)
			expr(e.R)
		case *UnaryExpr:
			expr(e.X)
		case *TableExpr:
			for _, f := range e.Fields {
				if f.Key != nil {
					expr(f.Key)
				}
				expr(f.Value)
			}
		case *ParenExpr:
			expr(e.X)
		}
	}
	block = func(b *Block) {
		for _, s := range b.Stmts {
			switch s := s.(type) {
			case *AssignStmt:
				for _, t := range s.Targets {
					if n, ok := t.(*NameExpr); ok && n.Name == "_ENV" {
						found = true
					}
					expr(t)
				}
				for _, v := range s.Values {
					expr(v)
				}
			case *CallStmt:
				expr(s.Call)
			case *DoStmt:
				block(s.Body)
			case *WhileStmt:
				expr(s.Cond)
				block(s.Body)
			case *RepeatStmt:
				block(s.Body)
				expr(s.Cond)
			case *IfStmt:
				for i, c := range s.Conds {
					expr(c)
					block(s.Blocks[i])
				}
				if s.Else != nil {
					block(s.Else)
				}
			case *NumericForStmt:
				names([]string{s.Var})
				expr(s.Start)
				expr(s.Limit)
				if s.Step != nil {
					expr(s.Step)
				}
				block(s.Body)
			case *GenericForStmt:
				names(s.Names)
				for _, e := range s.Exprs {
					expr(e)
				}
				block(s.Body)
			case *FunctionStmt:
				if n, ok := s.Target.(*NameExpr); ok && n.Name == "_ENV" {
					found = true
				}
				expr(s.Target)
				expr(s.Func)
			case *LocalFunctionStmt:
				names([]string{s.Name})
				expr(s.Func)
			case *LocalStmt:
				names(s.Names)
				for _, v := range s.Values {
					expr(v)
				}
			case *ReturnStmt:
				for _, v := range s.Values {
					expr(v)
				}
			}
		}
	}
	names(fn.Params)
	block(fn.Body)
	return found
}
