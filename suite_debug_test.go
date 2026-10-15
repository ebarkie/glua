package glua

// Test-only parts of the debug library that the Lua test suite needs. They approximate C
// Lua: Go functions have no frames of their own, so a Go function shows up as a C level
// only where the frames record one (a protected call, or a nested run started by Go).

// infoLevel is one level of the call stack, as debug.getinfo numbers them.
type infoLevel struct {
	cl       *closure // nil for a Go function
	fn       Value
	pc       int
	name     string
	namewhat string
}

// stackLevels lists the levels of the call stack, level 0 being the Go function c.
func stackLevels(c *Call) []infoLevel {
	ex := c.ex
	st := &ex.st
	levels := []infoLevel{{fn: goFnValue(c.fn)}}
	if st.cl == nil {
		return levels
	}
	cl, base, pc, nf := st.cl, st.base, st.pc, st.nf
	for {
		lv := infoLevel{cl: cl, fn: ex.stack[base-1], pc: pc}
		levels = append(levels, lv)
		if nf == 0 {
			break
		}
		fr := ex.pframes[nf-1]
		base -= int(fr >> 32 & 0xffffff)
		pc = int(uint32(fr))
		nf--
		if fr&(frGoEntry|frCatch) != 0 {
			levels = append(levels, infoLevel{}) // the Go function that called it
			if fr&frGoEntry != 0 && nf == 0 {
				break
			}
		} else if i := ex.instrAt(ex.stack[base-1], pc-1); uint8(i) == opCall || uint8(i) == opTailCall {
			callerCl := (*closure)(ex.stack[base-1].p)
			kind, name := callerCl.p.objName(pc-1, decA(i))
			switch kind {
			case "global", "field", "local", "upvalue", "method":
				levels[len(levels)-1].name, levels[len(levels)-1].namewhat = name, kind
			}
		}
		v := ex.stack[base-1]
		if !v.isObj(kindLuaFn) {
			break
		}
		cl = (*closure)(v.p)
	}
	return levels
}

// instrAt returns instruction pc of the Lua function f, or 0.
func (ex *executor) instrAt(f Value, pc int) uint32 {
	if !f.isObj(kindLuaFn) || pc < 0 {
		return 0
	}
	code := (*closure)(f.p).p.code
	if pc >= len(code) {
		return 0
	}
	return code[pc]
}

func debugGetinfo(c *Call) error {
	arg := 0
	if c.Value(0).isObj(kindThread) {
		if (*coroutine)(c.Value(0).p) != c.ex.co {
			return c.ReturnValues() // the frames of another coroutine are not kept in a form to walk
		}
		arg = 1
	}
	t := NewTable(0, 16)
	var lv infoLevel
	if f := c.Value(arg); f.isObj(kindLuaFn) || f.isObj(kindGoFn) {
		lv.fn = f
		if f.isObj(kindLuaFn) {
			lv.cl = (*closure)(f.p)
		}
	} else {
		level, err := c.checkInt(arg, "getinfo")
		if err != nil {
			return err
		}
		levels := stackLevels(c)
		if level < 0 || level >= int64(len(levels)) {
			return c.ReturnValues()
		}
		lv = levels[level]
		t.Set(String("istailcall"), Bool(false))
		if lv.cl != nil {
			t.Set(String("currentline"), Int(int64(lv.cl.p.lineAt(lv.pc-1))))
		} else {
			t.Set(String("currentline"), Int(-1))
		}
		if lv.name != "" {
			t.Set(String("name"), String(lv.name))
			t.Set(String("namewhat"), String(lv.namewhat))
		} else {
			t.Set(String("namewhat"), String(""))
		}
	}
	if !lv.fn.IsNil() {
		t.Set(String("func"), lv.fn)
	}
	if lv.cl == nil {
		t.Set(String("what"), String("C"))
		t.Set(String("source"), String("=[C]"))
		t.Set(String("short_src"), String("[C]"))
		t.Set(String("linedefined"), Int(-1))
		t.Set(String("lastlinedefined"), Int(-1))
		t.Set(String("nups"), Int(0))
		t.Set(String("nparams"), Int(0))
		t.Set(String("isvararg"), Bool(true))
		return c.ReturnValues(tableValue(t))
	}
	p := lv.cl.p
	what := "Lua"
	if p.name == "main chunk" {
		what = "main"
	}
	source := p.chunk
	if source == "" {
		source = "=" + p.source
	}
	t.Set(String("what"), String(what))
	t.Set(String("source"), String(source))
	t.Set(String("short_src"), String(p.source))
	t.Set(String("linedefined"), Int(int64(p.linedefined)))
	t.Set(String("lastlinedefined"), Int(int64(p.lastline)))
	t.Set(String("nups"), Int(int64(len(p.upvals))))
	t.Set(String("nparams"), Int(int64(p.nparams)))
	t.Set(String("isvararg"), Bool(p.vararg))
	lines := NewTable(0, 0)
	for _, l := range p.lines {
		if l > 0 {
			lines.Set(Int(int64(l)), Bool(true))
		}
	}
	t.Set(String("activelines"), tableValue(lines))
	return c.ReturnValues(tableValue(t))
}

// upvalueArg returns the closure and the 0-based upvalue index of debug.getupvalue(f, n)
// and debug.setupvalue(f, n, v), or nil when f has no upvalue n.
func upvalueArg(c *Call, fname string) (*closure, int, error) {
	n, err := c.checkInt(1, fname)
	if err != nil {
		return nil, 0, err
	}
	f := c.Value(0)
	if !f.isObj(kindLuaFn) {
		if f.isObj(kindGoFn) {
			return nil, 0, nil
		}
		return nil, 0, c.typeArgError(0, fname, "function")
	}
	cl := (*closure)(f.p)
	if n == 1 && len(cl.up) == 0 && cl.p.name == "main chunk" {
		return cl, envUpvalue, nil // a chunk that never names _ENV: its globals table
	}
	if n < 1 || n > int64(len(cl.up)) {
		return nil, 0, nil
	}
	return cl, int(n - 1), nil
}

// envUpvalue stands for the _ENV upvalue C Lua gives every main chunk, which glua keeps as
// the closure's globals table when the chunk never names _ENV.
const envUpvalue = -1

func upvalueName(cl *closure, i int) string {
	if i < len(cl.p.upnames) && cl.p.upnames[i] != "" {
		return cl.p.upnames[i]
	}
	return "?"
}

func debugGetupvalue(c *Call) error {
	cl, i, err := upvalueArg(c, "getupvalue")
	if err != nil || cl == nil {
		return err
	}
	if i == envUpvalue {
		return c.ReturnValues(String("_ENV"), envValue(cl))
	}
	v := cl.up[i]
	if v.isObj(kindCell) {
		v = v.cell().v
	}
	return c.ReturnValues(String(upvalueName(cl, i)), v)
}

// debugSetupvalue sets an upvalue. A capture that is never assigned is a copy in each
// closure, so setting it changes this closure only.
func debugSetupvalue(c *Call) error {
	if err := c.checkAny(2, "setupvalue"); err != nil {
		return err
	}
	cl, i, err := upvalueArg(c, "setupvalue")
	if err != nil || cl == nil {
		return err
	}
	if i == envUpvalue {
		cl.env = nil
		if v := c.Value(2); v.isObj(kindTable) {
			cl.env = (*Table)(v.p)
		}
		return c.ReturnValues(String("_ENV"))
	}
	if cl.up[i].isObj(kindCell) {
		cl.up[i].cell().v = c.Value(2)
	} else {
		cl.up[i] = c.Value(2)
	}
	return c.ReturnValues(String(upvalueName(cl, i)))
}

// addSuiteDebug adds the test-only debug functions to the debug table of g.
func addSuiteDebug(g *Table) {
	d := (*Table)(g.Get(String("debug")).p)
	for name, f := range map[string]GoFunc{
		"getinfo": debugGetinfo, "getupvalue": debugGetupvalue, "setupvalue": debugSetupvalue,
	} {
		d.Set(String(name), goFnValue(&goFunc{name: name, qualname: "debug." + name, f: f}))
	}
}
