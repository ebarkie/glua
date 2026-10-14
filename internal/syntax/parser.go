package syntax

import "fmt"

const (
	// maxLevels caps the parser's recursion, as LUAI_MAXCCALLS does in C Lua.
	maxLevels = 200
	// maxDepth caps the depth of the tree, which also counts the links of left-leaning
	// chains such as a.b.c or 1+2+3 that the parser builds without recursing. The passes
	// over the tree recurse, so this bounds their stack.
	maxDepth = 2000
)

// Parse parses a chunk into the body of its main function, which takes '...'.
func Parse(source, src string) (fn *FunctionExpr, err error) {
	return parse(source, src, false)
}

// ParseFile is Parse for the contents of a file: a first line starting with '#' is skipped.
func ParseFile(source, src string) (fn *FunctionExpr, err error) {
	return parse(source, src, true)
}

func parse(source, src string, skipComment bool) (fn *FunctionExpr, err error) {
	p := &parser{lex: NewLexer(source, src, skipComment), source: source}
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*Error)
			if !ok {
				panic(r)
			}
			err = e
		}
	}()
	p.next()
	fn = &FunctionExpr{Line: 0, IsVararg: true, Name: "main chunk"}
	p.funcs = append(p.funcs, fn)
	fn.Body = p.block()
	p.check(EOF)
	fn.EndLine, fn.CloseLine = p.tok.EndLine, p.tok.EndLine
	return fn, nil
}

type parser struct {
	lex    *Lexer
	source string
	tok    Token
	ahead  Token
	peeked bool
	funcs  []*FunctionExpr // enclosing functions, innermost last
	level  int             // recursion depth
	chain  int             // links of left-leaning chains on the current path
}

func (p *parser) next() {
	if p.peeked {
		p.tok, p.peeked = p.ahead, false
		return
	}
	p.tok = p.lex.Next()
}

func (p *parser) lookahead() Kind {
	if !p.peeked {
		p.ahead, p.peeked = p.lex.Next(), true
	}
	return p.ahead.Kind
}

// syntaxError reports msg near the current token, as luaX_syntaxerror does.
func (p *parser) syntaxError(msg string) {
	panic(&Error{Source: p.source, Line: p.errLine(), Col: p.tok.Col, Msg: msg + " near " + p.tok.Near()})
}

// semError reports msg without "near", as luaK_semerror does.
func (p *parser) semError(msg string) {
	panic(&Error{Source: p.source, Line: p.errLine(), Col: p.tok.Col, Msg: msg})
}

func (p *parser) errLine() int {
	if p.peeked {
		return p.ahead.EndLine
	}
	return p.tok.EndLine
}

func (p *parser) errorExpected(k Kind) {
	p.syntaxError(k.quoted() + " expected")
}

func (p *parser) test(k Kind) bool {
	if p.tok.Kind == k {
		p.next()
		return true
	}
	return false
}

func (p *parser) check(k Kind) {
	if p.tok.Kind != k {
		p.errorExpected(k)
	}
}

func (p *parser) checkNext(k Kind) {
	p.check(k)
	p.next()
}

// checkMatch expects what to close who, opened on line.
func (p *parser) checkMatch(what, who Kind, line int) {
	if !p.test(what) {
		if line == p.tok.EndLine {
			p.errorExpected(what)
		}
		p.syntaxError(fmt.Sprintf("%s expected (to close %s at line %d)", what.quoted(), who.quoted(), line))
	}
}

func (p *parser) name() string {
	p.check(Name)
	s := p.tok.Str
	p.next()
	return s
}

func (p *parser) enter() {
	p.level++
	if p.level > maxLevels || p.level+p.chain > maxDepth {
		p.semError("chunk has too many syntax levels")
	}
}

func (p *parser) leave() { p.level-- }

// link counts one more link of a left-leaning chain on the current path.
func (p *parser) link() {
	p.chain++
	if p.level+p.chain > maxDepth {
		p.semError("chunk has too many syntax levels")
	}
}

// blockFollow reports whether the current token ends a block.
func (p *parser) blockFollow(withUntil bool) bool {
	switch p.tok.Kind {
	case Else, Elseif, End, EOF:
		return true
	case Until:
		return withUntil
	}
	return false
}

func (p *parser) block() *Block {
	b := &Block{}
	for !p.blockFollow(true) {
		if p.tok.Kind == Return {
			b.Stmts = append(b.Stmts, p.statement())
			break
		}
		if s := p.statement(); s != nil {
			b.Stmts = append(b.Stmts, s)
		}
	}
	b.EndLine = p.tok.Line
	return b
}

func (p *parser) statement() Stmt {
	line := p.tok.Line
	p.enter()
	defer p.leave()
	switch p.tok.Kind {
	case Semi:
		p.next()
		return nil
	case If:
		return p.ifStat(line)
	case While:
		p.next()
		cond := p.expr()
		p.checkNext(Do)
		body := p.block()
		p.checkMatch(End, While, line)
		return &WhileStmt{Line: line, Cond: cond, Body: body}
	case Do:
		p.next()
		body := p.block()
		p.checkMatch(End, Do, line)
		return &DoStmt{Line: line, Body: body}
	case For:
		return p.forStat(line)
	case Repeat:
		p.next()
		body := p.block()
		p.checkMatch(Until, Repeat, line)
		return &RepeatStmt{Line: line, Body: body, Cond: p.expr()}
	case Function:
		return p.funcStat(line)
	case Local:
		p.next()
		if p.test(Function) {
			name := p.name()
			fn := p.body(false, line)
			fn.Name = name
			return &LocalFunctionStmt{Line: line, Name: name, Func: fn}
		}
		return p.localStat(line)
	case DColon:
		p.next()
		name := p.name()
		p.checkNext(DColon)
		return &LabelStmt{Line: line, Name: name, AfterLine: p.tok.EndLine}
	case Return:
		p.next()
		var vals []Expr
		if !p.blockFollow(true) && p.tok.Kind != Semi {
			vals = p.exprList()
		}
		p.test(Semi)
		return &ReturnStmt{Line: line, Values: vals}
	case Break:
		p.next()
		return &BreakStmt{Line: line}
	case Goto:
		p.next()
		return &GotoStmt{Line: line, Label: p.name()}
	}
	return p.exprStat(line)
}

func (p *parser) ifStat(line int) Stmt {
	s := &IfStmt{Line: line}
	for {
		p.next() // if or elseif
		s.Conds = append(s.Conds, p.expr())
		p.checkNext(Then)
		s.Blocks = append(s.Blocks, p.block())
		if p.tok.Kind != Elseif {
			break
		}
	}
	if p.test(Else) {
		s.Else = p.block()
	}
	p.checkMatch(End, If, line)
	return s
}

func (p *parser) forStat(line int) Stmt {
	p.next()
	v := p.name()
	switch p.tok.Kind {
	case Assign:
		p.next()
		s := &NumericForStmt{Line: line, Var: v, Start: p.expr()}
		p.checkNext(Comma)
		s.Limit = p.expr()
		if p.test(Comma) {
			s.Step = p.expr()
		}
		p.checkNext(Do)
		s.Body = p.block()
		p.checkMatch(End, For, line)
		return s
	case Comma, In:
		s := &GenericForStmt{Line: line, Names: []string{v}}
		for p.test(Comma) {
			s.Names = append(s.Names, p.name())
		}
		p.checkNext(In)
		s.ExprLine = p.tok.Line
		s.Exprs = p.exprList()
		p.checkNext(Do)
		s.Body = p.block()
		p.checkMatch(End, For, line)
		return s
	}
	p.syntaxError("'=' or 'in' expected")
	return nil
}

func (p *parser) funcStat(line int) Stmt {
	p.next()
	nameLine := p.tok.Line
	name := p.name()
	var target Expr = &NameExpr{Line: nameLine, Name: name}
	full := name
	method := false
	for p.tok.Kind == Dot || p.tok.Kind == Colon {
		method = p.tok.Kind == Colon
		p.next()
		kl := p.tok.Line
		key := p.name()
		target = &IndexExpr{Line: kl, Obj: target, Key: &StringExpr{Line: kl, Value: key}}
		if method {
			full += ":" + key
			break
		}
		full += "." + key
	}
	fn := p.body(method, line)
	fn.Name = full
	return &FunctionStmt{Line: line, Target: target, Func: fn}
}

func (p *parser) localStat(line int) Stmt {
	s := &LocalStmt{Line: line}
	closes := 0
	for {
		s.Names = append(s.Names, p.name())
		attr := ""
		if p.test(Lt) {
			attr = p.name()
			p.checkNext(Gt)
			switch attr {
			case "const":
			case "close":
				closes++
				if closes > 1 {
					p.semError("multiple to-be-closed variables in local list")
				}
			default:
				p.semError(fmt.Sprintf("unknown attribute '%s'", attr))
			}
		}
		s.Attribs = append(s.Attribs, attr)
		if !p.test(Comma) {
			break
		}
	}
	if p.test(Assign) {
		s.Values = p.exprList()
	}
	return s
}

func (p *parser) exprStat(line int) Stmt {
	e := p.suffixedExpr()
	if p.tok.Kind == Assign || p.tok.Kind == Comma {
		targets := []Expr{e}
		var lines []int
		for {
			if !assignable(targets[len(targets)-1]) {
				p.syntaxError("syntax error")
			}
			lines = append(lines, p.tok.EndLine)
			if !p.test(Comma) {
				break
			}
			targets = append(targets, p.suffixedExpr())
		}
		p.checkNext(Assign)
		return &AssignStmt{Line: line, Targets: targets, Values: p.exprList(), TargetLines: lines}
	}
	switch e.(type) {
	case *CallExpr, *MethodCallExpr:
		return &CallStmt{Line: line, Call: e}
	}
	p.syntaxError("syntax error")
	return nil
}

func assignable(e Expr) bool {
	switch e.(type) {
	case *NameExpr, *IndexExpr:
		return true
	}
	return false
}

// body parses a function's parameters and block; the "function" keyword and any name have
// been read. A method gets "self" as its first parameter.
func (p *parser) body(method bool, line int) *FunctionExpr {
	fn := &FunctionExpr{Line: line, Name: "<anonymous>"}
	if method {
		fn.Params = append(fn.Params, "self")
	}
	p.checkNext(LParen)
	if p.tok.Kind != RParen {
		for {
			if p.tok.Kind == Name {
				fn.Params = append(fn.Params, p.name())
			} else if p.tok.Kind == Dots {
				p.next()
				fn.IsVararg = true
				break
			} else {
				p.syntaxError("<name> or '...' expected")
			}
			if !p.test(Comma) {
				break
			}
		}
	}
	p.checkNext(RParen)
	p.funcs = append(p.funcs, fn)
	fn.Body = p.block()
	p.funcs = p.funcs[:len(p.funcs)-1]
	fn.EndLine = p.tok.Line
	p.checkMatch(End, Function, line)
	fn.CloseLine = p.errLine()
	return fn
}

func (p *parser) exprList() []Expr {
	list := []Expr{p.expr()}
	for p.test(Comma) {
		list = append(list, p.expr())
	}
	return list
}

func (p *parser) primaryExpr() Expr {
	switch p.tok.Kind {
	case Name:
		e := &NameExpr{Line: p.tok.Line, Name: p.tok.Str}
		p.next()
		return e
	case LParen:
		line := p.tok.Line
		p.next()
		e := p.expr()
		p.checkMatch(RParen, LParen, line)
		return &ParenExpr{Line: line, X: e}
	}
	p.syntaxError("unexpected symbol")
	return nil
}

func (p *parser) suffixedExpr() Expr {
	e := p.primaryExpr()
	saved := p.chain
	defer func() { p.chain = saved }()
	for {
		switch p.tok.Kind {
		case Dot:
			p.next()
			kl := p.tok.Line
			e = &IndexExpr{Line: kl, Obj: e, Key: &StringExpr{Line: kl, Value: p.name()}}
		case LBracket:
			kl := p.tok.Line
			p.next()
			k := p.expr()
			p.checkNext(RBracket)
			e = &IndexExpr{Line: kl, Obj: e, Key: k}
		case Colon:
			p.next()
			name := p.name()
			al := p.tok.Line // a call is on the line of its arguments, as in C Lua 5.4.9
			e = &MethodCallExpr{Line: al, Obj: e, Name: name, Args: p.args(al)}
		case LParen, String, LBrace:
			al := p.tok.Line
			e = &CallExpr{Line: al, Fn: e, Args: p.args(al)}
		default:
			return e
		}
		p.link()
	}
}

func (p *parser) args(line int) []Expr {
	switch p.tok.Kind {
	case String:
		e := &StringExpr{Line: p.tok.Line, Value: p.tok.Str}
		p.next()
		return []Expr{e}
	case LBrace:
		return []Expr{p.table()}
	case LParen:
		p.next()
		var list []Expr
		if p.tok.Kind != RParen {
			list = p.exprList()
		}
		p.checkMatch(RParen, LParen, line)
		return list
	}
	p.syntaxError("function arguments expected")
	return nil
}

func (p *parser) table() *TableExpr {
	line := p.tok.Line
	t := &TableExpr{Line: line}
	p.checkNext(LBrace)
	for p.tok.Kind != RBrace {
		switch {
		case p.tok.Kind == Name && p.lookahead() == Assign:
			kl := p.tok.Line
			k := &StringExpr{Line: kl, Value: p.name()}
			p.next() // '='
			t.Fields = append(t.Fields, Field{Key: k, Value: p.expr()})
		case p.tok.Kind == LBracket:
			p.next()
			k := p.expr()
			p.checkNext(RBracket)
			p.checkNext(Assign)
			t.Fields = append(t.Fields, Field{Key: k, Value: p.expr()})
		default:
			t.Fields = append(t.Fields, Field{Value: p.expr()})
		}
		if !p.test(Comma) && !p.test(Semi) {
			break
		}
	}
	t.EndLine = p.tok.Line
	p.checkMatch(RBrace, LBrace, line)
	return t
}

func (p *parser) simpleExpr() Expr {
	line := p.tok.Line
	var e Expr
	switch p.tok.Kind {
	case Float:
		e = &FloatExpr{Line: line, Value: p.tok.Num}
	case Int:
		e = &IntExpr{Line: line, Value: p.tok.Int}
	case String:
		e = &StringExpr{Line: line, Value: p.tok.Str}
	case Nil:
		e = &NilExpr{Line: line}
	case True:
		e = &TrueExpr{Line: line}
	case False:
		e = &FalseExpr{Line: line}
	case Dots:
		if !p.funcs[len(p.funcs)-1].IsVararg {
			p.syntaxError("cannot use '...' outside a vararg function")
		}
		e = &VarargExpr{Line: line}
	case LBrace:
		return p.table()
	case Function:
		p.next()
		return p.body(false, line)
	default:
		return p.suffixedExpr()
	}
	p.next()
	return e
}

// Operator priorities, from C Lua's lparser.c: left and right binding power.
var binaryPriority = map[Kind][2]int{
	Plus: {10, 10}, Minus: {10, 10},
	Star: {11, 11}, Percent: {11, 11}, Slash: {11, 11}, DSlash: {11, 11},
	Caret: {14, 13}, // right associative
	Amp:   {6, 6}, Pipe: {4, 4}, Tilde: {5, 5}, Shl: {7, 7}, Shr: {7, 7},
	Concat: {9, 8}, // right associative
	Eq:     {3, 3}, Lt: {3, 3}, Le: {3, 3}, Ne: {3, 3}, Gt: {3, 3}, Ge: {3, 3},
	And: {2, 2}, Or: {1, 1},
}

const unaryPriority = 12

func (p *parser) expr() Expr { return p.subExpr(0) }

// subExpr parses an expression whose operators bind tighter than limit.
func (p *parser) subExpr(limit int) Expr {
	p.enter()
	defer p.leave()
	var e Expr
	switch op := p.tok.Kind; op {
	case Not, Minus, Hash, Tilde:
		line := p.tok.Line
		p.next()
		e = &UnaryExpr{Line: line, Op: op, X: p.subExpr(unaryPriority)}
	default:
		e = p.simpleExpr()
	}
	saved := p.chain
	defer func() { p.chain = saved }()
	for {
		op := p.tok.Kind
		prio, ok := binaryPriority[op]
		if !ok || prio[0] <= limit {
			return e
		}
		line := p.tok.Line
		p.next()
		e = &BinaryExpr{Line: line, Op: op, L: e, R: p.subExpr(prio[1])}
		p.link()
	}
}
