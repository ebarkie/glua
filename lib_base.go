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
		g.Set(String(name), goFnValue(&goFunc{name: name, f: f}))
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
		return c.ReturnValues(callArgs(c, 0)...)
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
	// a script can never trigger a collection for the whole process: every option is a no-op
	// that returns its neutral value, except count
	switch opt {
	case "count":
		used := float64(c.ex.maxAlloc-c.ex.allocLeft) / 1024
		return c.ReturnValues(Float(used))
	case "collect", "step":
		if opt == "step" {
			return c.ReturnValues(Bool(false))
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
	if !mtv.IsNil() && !mtv.isObj(kindTable) {
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
	// decision 8: weak tables and finalisers are not supported. __gc is accepted beside
	// __close, a common fallback idiom, and refused otherwise.
	if !mt.Get(String("__mode")).IsNil() {
		return c.libErrorf("weak tables are not supported ('__mode')")
	}
	if !mt.Get(String("__gc")).IsNil() && mt.Get(String("__close")).IsNil() {
		return c.libErrorf("finalizers are not supported ('__gc')")
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

var nextFn = goFnValue(&goFunc{name: "next", f: baseNext})

// builtinPairs is pairs. When Lua calls it for a value with __pairs, the VM calls
// pairsMeta instead, so that __pairs can yield.
var builtinPairs = &goFunc{name: "pairs"}

func init() { builtinPairs.f = basePairs }

// pairsMeta is function(mm, t) local f, s, c = mm(t) return f, s, c end: pairs through
// __pairs, which C Lua truncates to three results.
var pairsMeta = func() *Closure {
	p, _, err := compileChunk("=pairs", "return function(mm, t) local f, s, c = mm(t) return f, s, c end")
	if err != nil {
		panic(err)
	}
	return &Closure{p: p.protos[0]}
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

var ipairsAuxFn = goFnValue(&goFunc{name: "ipairs_aux", f: ipairsAux})

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
	k, ok := normKey(c.Value(1))
	if !ok {
		if c.Value(1).IsNil() {
			return c.libErrorf("table index is nil")
		}
		return c.libErrorf("table index is NaN")
	}
	t.set(k, c.Value(2))
	return c.ReturnValues(c.Value(0))
}

func baseSelect(c *Call) error {
	n := c.Len()
	if s := c.Value(0); s.isString() && s.str() == "#" {
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
	return c.ReturnValues(callArgs(c, int(i))...)
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

// debugTraceback is debug.traceback([message [, level]]): the message, then the frames of
// the calling Lua code. A message that is not a string is returned as it is.
func debugTraceback(c *Call) error {
	msg := c.Value(0)
	if !msg.IsNil() && !msg.isString() && !msg.isNumber() {
		return c.ReturnValues(msg)
	}
	var sb strings.Builder
	if !msg.IsNil() {
		sb.WriteString(msg.String())
		sb.WriteByte('\n')
	}
	sb.WriteString("stack traceback:\n\t[C]: in function 'debug.traceback'")
	le := &Error{}
	ex := c.ex
	ex.captureTrace(le, ex.runBase())
	for _, f := range le.Traceback() {
		if f.Source == "" {
			sb.WriteString("\n\t[C]: in ?")
			continue
		}
		fmt.Fprintf(&sb, "\n\t%s:%d: in %s", f.Source, f.Line, f.Function)
	}
	sb.WriteString("\n\t[C]: in ?")
	return c.ReturnValues(String(sb.String()))
}
