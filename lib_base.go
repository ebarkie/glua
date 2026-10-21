package glua

import (
	"fmt"
	"strings"
)

// The base library: C Lua's lbaselib without dofile, loadfile and load (load is LibLoad).
// print writes to the executor's output, which discards by default.

// openBase installs the base library in the globals table g.
func openBase(g *Table) {
	for name, f := range map[string]GoFunc{
		"assert": baseAssert, "collectgarbage": baseCollectGarbage, "getmetatable": baseGetMetatable,
		"ipairs": baseIpairs, "next": baseNext, "print": basePrint,
		"rawequal": baseRawEqual, "rawget": baseRawGet, "rawlen": baseRawLen, "rawset": baseRawSet,
		"select": baseSelect, "setmetatable": baseSetMetatable, "tonumber": baseToNumber,
		"tostring": baseToString, "type": baseType, "warn": baseWarn,
	} {
		g.Set(String(name), goFnValue(&goFunc{name: name, qualname: name, f: f, fast: fastIDs[name]}))
	}
	g.Set(String("pairs"), goFnValue(builtinPairs))
	g.Set(String("pcall"), goFnValue(builtinPcall))
	g.Set(String("xpcall"), goFnValue(builtinXpcall))
	g.Set(String("error"), goFnValue(builtinError))
	g.Set(String("_G"), tableValue(g))
	g.Set(String("_VERSION"), String("Lua 5.4"))
	g.Set(String("debug"), tableValue(newLib("debug", map[string]GoFunc{"traceback": debugTraceback})))
}

func baseAssert(c *Call) error {
	if c.Len() == 0 {
		return c.argError(1, "assert", "value expected")
	}
	if c.Value(0).Truthy() {
		return c.returnArgs(0)
	}
	if c.Len() >= 2 {
		return &Error{Value: c.Value(1)} // the message as it is, without a position
	}
	return c.libErrorf("assertion failed!")
}

func baseCollectGarbage(c *Call) error {
	opt, err := c.optString(0, "collectgarbage", "collect")
	if err != nil {
		return err
	}
	// a script can never trigger a collection for the whole process: collect and step measure
	// what the entry keeps and let go of its dead stack, count reports that measure plus what
	// was allocated since, and the other options are no-ops that return their neutral value
	switch opt {
	case "count":
		ex := c.ex
		if est := ex.liveBase + ex.allocated() - ex.liveAt; !ex.liveKnown || est > 2*ex.liveBase+64<<10 {
			// measured again once allocation could have doubled it, as a collector's cycle
			// would come round: the walks cost a bounded share of what is allocated
			if err := c.measureLive(); err != nil {
				return err
			}
		}
		return c.ReturnValues(Float(float64(ex.liveBase+ex.allocated()-ex.liveAt) / 1024))
	case "collect", "step":
		c.clearDead()
		if err := c.measureLive(); err != nil {
			return err
		}
		if opt == "step" {
			return c.ReturnValues(Bool(false)) // as C Lua answers a step on a small heap
		}
		return c.ReturnValues(Int(0))
	case "isrunning":
		return c.ReturnValues(Bool(true))
	case "incremental", "generational":
		return c.ReturnValues(String("incremental"))
	case "stop", "restart", "setpause", "setstepmul":
		return c.ReturnValues(Int(0))
	}
	return c.argError(1, "collectgarbage", fmt.Sprintf("invalid option '%s'", opt))
}

func baseGetMetatable(c *Call) error {
	if err := c.checkAny(0, "getmetatable"); err != nil {
		return err
	}
	mt := c.ex.metatable(c.Value(0))
	if mt == nil {
		return c.ReturnValues(Value{})
	}
	if protected := mt.Get(String("__metatable")); !protected.IsNil() {
		return c.ReturnValues(protected)
	}
	return c.ReturnValues(tableValue(mt))
}

func baseSetMetatable(c *Call) error {
	t, err := c.checkTable(0, "setmetatable")
	if err != nil {
		return err
	}
	mtv := c.Value(1)
	if c.Len() < 2 || !mtv.IsNil() && !mtv.isObj(kindTable) { // as C Lua's luaL_argexpected: no value is not nil
		return c.typeArgError(1, "setmetatable", "nil or table")
	}
	if t.meta != nil && !t.meta.Get(String("__metatable")).IsNil() {
		return c.libErrorf("cannot change a protected metatable")
	}
	if t.Frozen() {
		return c.libErrorf("attempt to modify a frozen table")
	}
	if mtv.IsNil() {
		t.meta = nil
		return c.ReturnValues(c.Value(0))
	}
	mt := (*Table)(mtv.p)
	if !mt.metaOK {
		// decision 8: weak tables and finalisers are not supported. __gc is accepted beside
		// __close, a common fallback idiom, and refused otherwise.
		if !mt.Get(String("__mode")).IsNil() {
			return c.libErrorf("weak tables are not supported ('__mode')")
		}
		if !mt.Get(String("__gc")).IsNil() && mt.Get(String("__close")).IsNil() {
			return c.libErrorf("finalizers are not supported ('__gc')")
		}
		if !mt.Frozen() { // a frozen table is shared: Freeze sets the mark instead
			mt.metaOK = true
		}
	}
	t.meta = mt
	return c.ReturnValues(c.Value(0))
}

func baseNext(c *Call) error {
	t, err := c.checkTable(0, "next")
	if err != nil {
		return err
	}
	k, v, ok := t.Next(c.Value(1))
	if !ok {
		return c.libErrorf("invalid key to 'next'")
	}
	if k.IsNil() {
		return c.ReturnValues(Value{})
	}
	return c.ReturnValues(k, v)
}

var (
	nextG  = &goFunc{name: "next", qualname: "next"}
	nextFn = goFnValue(nextG)
)

// builtinPairs is pairs. When Lua calls it for a value with __pairs, the VM calls
// pairsMeta instead, so that __pairs can yield.
var builtinPairs = &goFunc{name: "pairs", qualname: "pairs"}

func init() {
	builtinPairs.f = basePairs
	nextG.f = baseNext // set here: the VM refers to these functions, which refer back to it
	ipairsAuxG.f = ipairsAux
}

// pairsMeta is function(mm, t) local f, s, c = mm(t) return f, s, c end: pairs through
// __pairs, which C Lua truncates to three results.
var pairsMeta = func() *closure {
	p, _, err := compileChunk("=pairs", "return function(mm, t) local f, s, c = mm(t) return f, s, c end")
	if err != nil {
		panic(err)
	}
	return &closure{p: p.protos[0]}
}()

func basePairs(c *Call) error {
	if err := c.checkAny(0, "pairs"); err != nil {
		return err
	}
	v := c.Value(0)
	if h := c.ex.metamethod(v, "__pairs"); !h.IsNil() {
		res, err := c.call(h, v)
		if err != nil {
			return err
		}
		for len(res) < 3 {
			res = append(res, Value{})
		}
		return c.ReturnValues(res[:3]...)
	}
	return c.ReturnValues(nextFn, v, Value{})
}

// ipairsAux is the iterator ipairs returns: t[i+1] with __index, until it is nil.
func ipairsAux(c *Call) error {
	i, err := c.checkInt(1, "ipairs")
	if err != nil {
		return err
	}
	i++
	v, err := c.index(c.Value(0), Int(i))
	if err != nil {
		return err
	}
	if v.IsNil() {
		return c.ReturnValues(Value{})
	}
	return c.ReturnValues(Int(i), v)
}

var (
	ipairsAuxG  = &goFunc{name: "ipairs_aux"}
	ipairsAuxFn = goFnValue(ipairsAuxG)
)

func baseIpairs(c *Call) error {
	if err := c.checkAny(0, "ipairs"); err != nil {
		return err
	}
	return c.ReturnValues(ipairsAuxFn, c.Value(0), Int(0))
}

func basePrint(c *Call) error {
	var sb strings.Builder
	for i := range c.Len() {
		if i > 0 {
			sb.WriteByte('\t')
		}
		s, err := c.tostring(c.Value(i))
		if err != nil {
			return err
		}
		sb.WriteString(s)
	}
	sb.WriteByte('\n')
	return c.ex.write(sb.String())
}

func baseWarn(c *Call) error {
	var sb strings.Builder
	for i := range c.Len() {
		s, err := c.checkString(i, "warn")
		if err != nil {
			return err
		}
		sb.WriteString(s)
	}
	msg := sb.String()
	switch {
	case msg == "@on":
		c.ex.warnOn = true
	case msg == "@off":
		c.ex.warnOn = false
	case strings.HasPrefix(msg, "@"):
	case c.ex.warnOn:
		return c.ex.write("Lua warning: " + msg + "\n")
	}
	return nil
}

func baseRawEqual(c *Call) error {
	if err := c.checkAny(0, "rawequal"); err != nil {
		return err
	}
	if err := c.checkAny(1, "rawequal"); err != nil {
		return err
	}
	return c.ReturnValues(Bool(rawEqual(c.Value(0), c.Value(1))))
}

func baseRawLen(c *Call) error {
	v := c.Value(0)
	switch {
	case v.isObj(kindTable):
		return c.ReturnValues(Int(int64((*Table)(v.p).Len())))
	case v.isString():
		return c.ReturnValues(Int(int64(v.n & lenMask)))
	}
	return c.typeArgError(0, "rawlen", "table or string")
}

func baseRawGet(c *Call) error {
	t, err := c.checkTable(0, "rawget")
	if err != nil {
		return err
	}
	if err := c.checkAny(1, "rawget"); err != nil {
		return err
	}
	return c.ReturnValues(t.Get(c.Value(1)))
}

func baseRawSet(c *Call) error {
	t, err := c.checkTable(0, "rawset")
	if err != nil {
		return err
	}
	if err := c.checkAny(1, "rawset"); err != nil {
		return err
	}
	if err := c.checkAny(2, "rawset"); err != nil {
		return err
	}
	if t.Frozen() {
		return c.libErrorf("attempt to modify a frozen table")
	}
	if t.flags&tabGuarded != 0 && c.ex.reservedKey(c.Value(1)) {
		return c.libErrorf("attempt to assign to reserved name '%s'", c.Value(1).str())
	}
	k, ok := normKey(c.Value(1))
	if !ok {
		if c.Value(1).IsNil() {
			return c.libErrorf("table index is nil")
		}
		return c.libErrorf("table index is NaN")
	}
	if err := c.ex.tableSet(t, k, c.Value(2)); err != nil {
		return err
	}
	return c.ReturnValues(c.Value(0))
}

func baseSelect(c *Call) error {
	n := c.Len()
	if s := c.Value(0); s.isString() && strings.HasPrefix(s.str(), "#") { // as C Lua: only the first byte counts
		return c.ReturnValues(Int(int64(n - 1)))
	}
	i, err := c.checkInt(0, "select")
	if err != nil {
		return err
	}
	switch {
	case i < 0:
		i = int64(n) + i
	case i > int64(n):
		i = int64(n)
	}
	if i < 1 {
		return c.argError(1, "select", "index out of range")
	}
	return c.returnArgs(int(i))
}

func baseToNumber(c *Call) error {
	if c.Value(1).IsNil() { // no base: standard conversion
		if err := c.checkAny(0, "tonumber"); err != nil {
			return err
		}
		if n, ok := toNumber(c.Value(0)); ok {
			return c.ReturnValues(n)
		}
		return c.ReturnValues(Value{})
	}
	base, err := c.checkInt(1, "tonumber")
	if err != nil {
		return err
	}
	v := c.Value(0)
	if !v.isString() {
		return c.typeArgError(0, "tonumber", "string")
	}
	if base < 2 || base > 36 {
		return c.argError(2, "tonumber", "base out of range")
	}
	if n, ok := strToIntBase(v.str(), int(base)); ok {
		return c.ReturnValues(Int(n))
	}
	return c.ReturnValues(Value{})
}

func baseToString(c *Call) error {
	if err := c.checkAny(0, "tostring"); err != nil {
		return err
	}
	s, err := c.tostring(c.Value(0))
	if err != nil {
		return err
	}
	return c.ReturnValues(String(s))
}

func baseType(c *Call) error {
	if err := c.checkAny(0, "type"); err != nil {
		return err
	}
	var name string
	switch c.Value(0).Kind() {
	case KindInt, KindFloat:
		name = "number"
	default:
		name = c.Value(0).Kind().String()
	}
	return c.ReturnValues(String(name))
}

// waitingIn names the Go function a thread that resumed another is waiting in: the callee of
// its current CALL, as C Lua names a library function, or "?".
func waitingIn(ts *threadState) string {
	return calleeName(ts.st.cl, ts.st.base, ts.st.pc, ts.stack)
}

// calleeName names the Go function that the CALL at pc-1 of the frame of cl at base called,
// as C Lua names a library function, or "?".
func calleeName(cl *closure, base, pc int, stack []Value) string {
	if cl == nil || pc < 1 {
		return "?"
	}
	i := cl.p.code[pc-1]
	if op := uint8(i); op != opCall && op != opTailCall {
		return "?"
	}
	if f := stack[base+decA(i)]; f.isObj(kindGoFn) {
		if g := (*goFunc)(f.p); g.qualname != "" && !g.stateful {
			return "function '" + g.qualname + "'"
		}
	}
	return "?"
}

// threadLines appends the traceback lines of a thread's frames, innermost first, through
// every run on its stack. Go functions are [C] lines, as C functions are in C Lua: the pcall
// or xpcall below a protected call, and the Go function below a nested run. The trampoline
// is no line of its own: the Go function it calls is.
func threadLines(lines []string, st *state, pframes []uint64, stack []Value) []string {
	cl, base, pc := st.cl, st.base, st.pc
	if cl == nil {
		return lines // a coroutine with a Go body that has no Lua frame yet
	}
	for f := st.nf - 1; f >= 0; f-- {
		if cl.p != trampoline.p {
			t := traceFrame(traceEntry{p: cl.p, pc: pc})
			lines = append(lines, fmt.Sprintf("%s:%d: in %s", t.Source, t.Line, t.Function))
		}
		fr := pframes[f]
		if fr&frCatch != 0 {
			if fr&frHandler != 0 {
				lines = append(lines, "[C]: in function 'xpcall'")
			} else {
				lines = append(lines, "[C]: in function 'pcall'")
			}
		}
		if f == 0 || uint32(fr) == 0 {
			break // no Lua code below
		}
		base -= int(fr >> 32 & 0xffffff)
		cl = (*closure)(stack[base-1].p)
		pc = int(uint32(fr))
		if fr&frGoEntry != 0 {
			lines = append(lines, "[C]: in "+calleeName(cl, base, pc, stack))
		}
	}
	return lines
}

// debugTraceback is debug.traceback([thread,] [message [, level]]): the message, then the
// frames of the thread, by default the running one. A message that is not a string is
// returned as it is. Level 0 is the function at the top of the thread's stack: traceback
// itself in the running thread, where the default is 1, its caller; in another thread, the
// default, the yield or resume it waits in.
func debugTraceback(c *Call) error {
	ex := c.ex
	arg := 0
	var co *coroutine // the thread to trace, nil for the running one
	if v := c.Value(0); v.isObj(kindThread) {
		arg = 1
		if co = (*coroutine)(v.p); co == ex.co || co.main && ex.co == nil {
			co = nil
		}
	}
	msg := c.Value(arg)
	if !msg.IsNil() && !msg.isString() && !msg.isNumber() {
		return c.ReturnValues(msg)
	}
	def := int64(1)
	if co != nil {
		def = 0
	}
	level, err := c.optInt(arg+1, "traceback", def)
	if err != nil {
		return err
	}

	// the lines of the traceback from level 0; a Go function is [C], as in C Lua
	var lines []string
	goBelow := false // the thread's stack starts in the host, which C Lua shows as "[C]: in ?"
	switch {
	case co == nil:
		lines = append(lines, "[C]: in function 'debug.traceback'")
		lines = threadLines(lines, &ex.st, ex.pframes, ex.stack)
		goBelow = ex.co == nil
	case co.status == coSuspended && co.started:
		lines = append(lines, "[C]: in function 'coroutine.yield'")
		lines = threadLines(lines, &co.st, co.pframes, co.stack)
	case co.status == coNormal:
		ts := &co.threadState // its state waits in it, in resume
		// resume, or a coroutine.wrap function
		lines = append(lines, "[C]: in "+waitingIn(ts))
		lines = threadLines(lines, &ts.st, ts.pframes, ts.stack)
		goBelow = co.main
	case co.status == coDead && co.errVal != nil && co.stack != nil:
		lines = threadLines(lines, &co.st, co.pframes, co.stack) // where it failed
	}
	if goBelow {
		lines = append(lines, "[C]: in ?")
	}

	var sb strings.Builder
	if !msg.IsNil() {
		sb.WriteString(msg.String())
		sb.WriteByte('\n')
	}
	sb.WriteString("stack traceback:")
	if level >= 0 && level < int64(len(lines)) {
		for _, l := range lines[level:] {
			sb.WriteString("\n\t")
			sb.WriteString(l)
		}
	}
	return c.ReturnValues(String(sb.String()))
}
