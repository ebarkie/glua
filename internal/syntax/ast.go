package syntax

// Expr is an expression node. Line is where the node's code should be reported: the start of
// a call's prefix, the operator of a binary expression, the first token otherwise.
type Expr interface {
	exprNode()
	Pos() int
}

// Stmt is a statement node.
type Stmt interface {
	stmtNode()
	Pos() int
}

type (
	NilExpr    struct{ Line int }
	TrueExpr   struct{ Line int }
	FalseExpr  struct{ Line int }
	VarargExpr struct{ Line int }

	IntExpr struct {
		Line  int
		Value int64
	}

	FloatExpr struct {
		Line  int
		Value float64
	}

	StringExpr struct {
		Line  int
		Value string
	}

	// NameExpr is a free or local name; scope resolution decides which.
	NameExpr struct {
		Line int
		Name string
	}

	// IndexExpr is Obj[Key]. Obj.name has a StringExpr key.
	IndexExpr struct {
		Line     int
		Obj, Key Expr
	}

	CallExpr struct {
		Line int
		Fn   Expr
		Args []Expr
	}

	// MethodCallExpr is Obj:Name(Args).
	MethodCallExpr struct {
		Line int
		Obj  Expr
		Name string
		Args []Expr
	}

	FunctionExpr struct {
		Line, EndLine int
		CloseLine     int      // line of the token after the closing 'end', where C Lua reports pending gotos
		Params        []string // a method's "self" comes first
		IsVararg      bool
		Body          *Block
		Name          string // for messages: "f", "a.b:c", "<anonymous>"
	}

	// BinaryExpr is L Op R. Op is one of the operator kinds, And or Or.
	BinaryExpr struct {
		Line int
		Op   Kind
		L, R Expr
	}

	// UnaryExpr is Op X. Op is Minus, Not, Hash or Tilde.
	UnaryExpr struct {
		Line int
		Op   Kind
		X    Expr
	}

	TableExpr struct {
		Line, EndLine int
		Fields        []Field
	}

	// ParenExpr is (X). It cuts a call or '...' to one value and makes X no longer assignable.
	ParenExpr struct {
		Line int
		X    Expr
	}
)

// Field is one entry of a table constructor. Key is nil for a positional entry.
type Field struct {
	Key, Value Expr
}

type (
	// AssignStmt is Targets = Values. Each target is a NameExpr or IndexExpr. TargetLines
	// has, per target, the line of the ',' or '=' after it, where C Lua reports an
	// assignment to a const variable.
	AssignStmt struct {
		Line            int
		Targets, Values []Expr
		TargetLines     []int
	}

	// CallStmt is a call used as a statement.
	CallStmt struct {
		Line int
		Call Expr // CallExpr or MethodCallExpr
	}

	DoStmt struct {
		Line int
		Body *Block
	}

	WhileStmt struct {
		Line int
		Cond Expr
		Body *Block
	}

	RepeatStmt struct {
		Line int
		Body *Block
		Cond Expr // in the scope of Body's locals
	}

	// IfStmt has one condition and block per if and elseif. Else may be nil.
	IfStmt struct {
		Line   int
		Conds  []Expr
		Blocks []*Block
		Else   *Block
	}

	NumericForStmt struct {
		Line               int
		Var                string
		Start, Limit, Step Expr // Step may be nil
		Body               *Block
	}

	GenericForStmt struct {
		Line  int
		Names []string
		Exprs []Expr
		Body  *Block
	}

	// FunctionStmt is "function a.b.c() end" or "function a.b:c() end": Target = Func.
	// Target is a NameExpr or IndexExpr; for a method, Func's first parameter is self.
	FunctionStmt struct {
		Line   int
		Target Expr
		Func   *FunctionExpr
	}

	// LocalFunctionStmt is "local function name() end"; the name is in scope in the body.
	LocalFunctionStmt struct {
		Line int
		Name string
		Func *FunctionExpr
	}

	// LocalStmt is "local a <const>, b = ...". Attribs has "", "const" or "close" per name.
	LocalStmt struct {
		Line    int
		Names   []string
		Attribs []string
		Values  []Expr
	}

	ReturnStmt struct {
		Line   int
		Values []Expr
	}

	BreakStmt struct{ Line int }

	GotoStmt struct {
		Line  int
		Label string
	}

	// LabelStmt is ::Name::. AfterLine is the line of the token after it, where C Lua
	// reports a duplicate label or a goto that jumps into a local's scope.
	LabelStmt struct {
		Line      int
		Name      string
		AfterLine int
	}
)

// Block is a list of statements; a ReturnStmt can only be last. EndLine is the line of the
// token that closes it.
type Block struct {
	Stmts   []Stmt
	EndLine int
}

func (*NilExpr) exprNode()        {}
func (*TrueExpr) exprNode()       {}
func (*FalseExpr) exprNode()      {}
func (*VarargExpr) exprNode()     {}
func (*IntExpr) exprNode()        {}
func (*FloatExpr) exprNode()      {}
func (*StringExpr) exprNode()     {}
func (*NameExpr) exprNode()       {}
func (*IndexExpr) exprNode()      {}
func (*CallExpr) exprNode()       {}
func (*MethodCallExpr) exprNode() {}
func (*FunctionExpr) exprNode()   {}
func (*BinaryExpr) exprNode()     {}
func (*UnaryExpr) exprNode()      {}
func (*TableExpr) exprNode()      {}
func (*ParenExpr) exprNode()      {}

func (e *NilExpr) Pos() int        { return e.Line }
func (e *TrueExpr) Pos() int       { return e.Line }
func (e *FalseExpr) Pos() int      { return e.Line }
func (e *VarargExpr) Pos() int     { return e.Line }
func (e *IntExpr) Pos() int        { return e.Line }
func (e *FloatExpr) Pos() int      { return e.Line }
func (e *StringExpr) Pos() int     { return e.Line }
func (e *NameExpr) Pos() int       { return e.Line }
func (e *IndexExpr) Pos() int      { return e.Line }
func (e *CallExpr) Pos() int       { return e.Line }
func (e *MethodCallExpr) Pos() int { return e.Line }
func (e *FunctionExpr) Pos() int   { return e.Line }
func (e *BinaryExpr) Pos() int     { return e.Line }
func (e *UnaryExpr) Pos() int      { return e.Line }
func (e *TableExpr) Pos() int      { return e.Line }
func (e *ParenExpr) Pos() int      { return e.Line }

func (*AssignStmt) stmtNode()        {}
func (*CallStmt) stmtNode()          {}
func (*DoStmt) stmtNode()            {}
func (*WhileStmt) stmtNode()         {}
func (*RepeatStmt) stmtNode()        {}
func (*IfStmt) stmtNode()            {}
func (*NumericForStmt) stmtNode()    {}
func (*GenericForStmt) stmtNode()    {}
func (*FunctionStmt) stmtNode()      {}
func (*LocalFunctionStmt) stmtNode() {}
func (*LocalStmt) stmtNode()         {}
func (*ReturnStmt) stmtNode()        {}
func (*BreakStmt) stmtNode()         {}
func (*GotoStmt) stmtNode()          {}
func (*LabelStmt) stmtNode()         {}

func (s *AssignStmt) Pos() int        { return s.Line }
func (s *CallStmt) Pos() int          { return s.Line }
func (s *DoStmt) Pos() int            { return s.Line }
func (s *WhileStmt) Pos() int         { return s.Line }
func (s *RepeatStmt) Pos() int        { return s.Line }
func (s *IfStmt) Pos() int            { return s.Line }
func (s *NumericForStmt) Pos() int    { return s.Line }
func (s *GenericForStmt) Pos() int    { return s.Line }
func (s *FunctionStmt) Pos() int      { return s.Line }
func (s *LocalFunctionStmt) Pos() int { return s.Line }
func (s *LocalStmt) Pos() int         { return s.Line }
func (s *ReturnStmt) Pos() int        { return s.Line }
func (s *BreakStmt) Pos() int         { return s.Line }
func (s *GotoStmt) Pos() int          { return s.Line }
func (s *LabelStmt) Pos() int         { return s.Line }

// IsMultiValue reports whether e can produce several values: a call or '...' that is not
// wrapped in parentheses.
func IsMultiValue(e Expr) bool {
	switch e.(type) {
	case *CallExpr, *MethodCallExpr, *VarargExpr:
		return true
	}
	return false
}
