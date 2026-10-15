package glua

import (
	"fmt"
)

// Helpers for the standard library, which reports errors as C Lua's lauxlib does: with the
// position of the Lua code that called the function, and luaL_argerror's wording.

// libErrorf raises an error with the caller's position, as luaL_error does.
func (c *Call) libErrorf(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if pos := c.ex.whereLevel(c, 1); pos != "" {
		msg = pos + ": " + msg
	}
	return &Error{Value: String(msg)}
}

// argError raises "bad argument #n to 'f' (msg)", counting arguments from 1, as
// luaL_argerror does: the function is named as the call site names it, a method call does
// not count self, and without a call-site name the library's qualified name is used.
func (c *Call) argError(n int, fname, msg string) error {
	name, method := c.callSiteName()
	if method {
		n--
		if n == 0 {
			return c.libErrorf("calling '%s' on bad self (%s)", name, msg)
		}
	}
	if name == "" {
		name = fname
		if c.fn != nil && c.fn.qualname != "" {
			name = c.fn.qualname
		}
	}
	return c.libErrorf("bad argument #%d to '%s' (%s)", n, name, msg)
}

// callSiteName returns the name the calling Lua code used for the function, and whether
// it was a method call. It is empty when Go code, such as pcall, made the call, directly or
// through the trampoline.
func (c *Call) callSiteName() (string, bool) {
	ex := c.ex
	if !c.fromLua || ex.st.cl == nil || ex.st.cl.p == trampoline.p || ex.st.pc < 1 {
		return "", false
	}
	i := ex.instr(ex.st.pc - 1)
	switch uint8(i) {
	case opCall, opTailCall:
	default:
		return "", false
	}
	kind, name := ex.st.cl.p.objName(ex.st.pc-1, decA(i))
	switch kind {
	case "global", "field", "local", "upvalue":
		return name, false
	case "method":
		return name, true
	}
	return "", false
}

// typeArgError raises "bad argument #n to 'fname' (T expected, got U)".
func (c *Call) typeArgError(i int, fname, want string) error {
	got := "no value"
	if i < c.Len() {
		got = typeName(c.Value(i))
	}
	return c.argError(i+1, fname, want+" expected, got "+got)
}

// checkAny requires argument i to be present.
func (c *Call) checkAny(i int, fname string) error {
	if i >= c.Len() {
		return c.argError(i+1, fname, "value expected")
	}
	return nil
}

// checkTable returns argument i, which must be a table.
func (c *Call) checkTable(i int, fname string) (*Table, error) {
	v := c.Value(i)
	if !v.isObj(kindTable) {
		return nil, c.typeArgError(i, fname, "table")
	}
	return (*Table)(v.p), nil
}

// checkInt returns argument i as an integer: an integer, a float with an integral value,
// or a string that converts to one, as luaL_checkinteger accepts.
func (c *Call) checkInt(i int, fname string) (int64, error) {
	v := c.Value(i)
	if n, ok := toNumber(v); ok {
		if x, ok := toIntExact(n); ok {
			return x, nil
		}
		return 0, c.argError(i+1, fname, "number has no integer representation")
	}
	return 0, c.typeArgError(i, fname, "number")
}

// optInt is checkInt with a default for a missing or nil argument.
func (c *Call) optInt(i int, fname string, def int64) (int64, error) {
	if c.Value(i).IsNil() {
		return def, nil
	}
	return c.checkInt(i, fname)
}

// checkNumber returns argument i as a number, converting strings.
func (c *Call) checkNumber(i int, fname string) (Value, error) {
	if n, ok := toNumber(c.Value(i)); ok {
		return n, nil
	}
	return Value{}, c.typeArgError(i, fname, "number")
}

// checkFloat returns argument i as a float.
func (c *Call) checkFloat(i int, fname string) (float64, error) {
	n, err := c.checkNumber(i, fname)
	if err != nil {
		return 0, err
	}
	f, _ := n.Float()
	return f, nil
}

// checkString returns argument i as a string; numbers are converted, as luaL_checklstring does.
func (c *Call) checkString(i int, fname string) (string, error) {
	v := c.Value(i)
	switch {
	case v.isString():
		return v.str(), nil
	case v.isNumber():
		return v.String(), nil
	}
	return "", c.typeArgError(i, fname, "string")
}

// optString is checkString with a default for a missing or nil argument.
func (c *Call) optString(i int, fname, def string) (string, error) {
	if c.Value(i).IsNil() {
		return def, nil
	}
	return c.checkString(i, fname)
}

// call calls f with args from library code and returns all its results. It runs in a
// nested run, so it cannot yield.
func (c *Call) call(f Value, args ...Value) ([]Value, error) {
	return c.ex.callSync(f, args)
}

// call1 is call for the first result only, without allocating.
func (c *Call) call1(f Value, args ...Value) (Value, error) {
	return c.ex.callSync1(f, args)
}

// index returns obj[key], with __index, from library code.
func (c *Call) index(obj, key Value) (Value, error) {
	ex := c.ex
	for hops := 0; hops < maxIndexHops; hops++ {
		var h Value
		if obj.isObj(kindTable) {
			t := (*Table)(obj.p)
			v := t.Get(key)
			if !v.IsNil() || t.meta == nil {
				return v, nil
			}
			h = t.meta.metaGet("__index")
		} else if obj.isObj(kindUser) {
			v, slot, err := ex.indexUser(obj, key)
			if slot != -2 {
				return v, err
			}
			h = v
		} else {
			h = ex.metamethod(obj, "__index")
		}
		if h.IsNil() {
			if obj.isObj(kindTable) {
				return Value{}, nil
			}
			return Value{}, c.libErrorf("attempt to index a %s value", typeName(obj))
		}
		if h.isObj(kindLuaFn) || h.isObj(kindGoFn) {
			res, err := c.call(h, obj, key)
			if err != nil || len(res) == 0 {
				return Value{}, err
			}
			return res[0], nil
		}
		obj = h
	}
	return Value{}, c.libErrorf("'__index' chain too long; possibly a loop")
}

// tostring converts v as Lua's tostring does: __tostring, then __name, then the default.
func (c *Call) tostring(v Value) (string, error) {
	if h := c.ex.metamethod(v, "__tostring"); !h.IsNil() {
		res, err := c.call(h, v)
		if err != nil {
			return "", err
		}
		if len(res) == 0 || !res[0].isString() {
			if len(res) > 0 && res[0].isNumber() {
				return res[0].String(), nil
			}
			return "", c.libErrorf("'__tostring' must return a string")
		}
		return res[0].str(), nil
	}
	if v.isObj(kindTable) {
		if mt := (*Table)(v.p).meta; mt != nil {
			if n := mt.Get(String("__name")); n.isString() {
				return n.str() + ": " + hidePointer(v.p), nil
			}
		}
	}
	return v.String(), nil
}

// newLib builds the library table lib from Go functions.
func newLib(lib string, funcs map[string]GoFunc) *Table {
	t := NewTable(0, len(funcs))
	for name, f := range funcs {
		qual := lib + "." + name
		t.Set(String(name), goFnValue(&goFunc{name: name, qualname: qual, f: f, fast: fastIDs[qual]}))
	}
	return t
}

// intOrFloat returns f as an integer if it has an integral value that fits, else as a float,
// as several math functions do.
func intOrFloat(f float64) Value {
	if i, ok := floatToInt(f); ok { // -0.0 too: C's lua_numbertointeger makes it 0
		return Int(i)
	}
	return Float(f)
}
