package glua

import (
	"strings"

	"github.com/folbricht/glua/internal/syntax"
)

const idSize = 60 // LUA_IDSIZE: the room a chunk name gets in messages

// chunkID formats a chunk name for messages, as C Lua's luaO_chunkid does: "=name" is
// name, "@file" is the file name (shortened from the left), and anything else is source
// text, shown as [string "first line..."].
func chunkID(name string) string {
	switch {
	case strings.HasPrefix(name, "="):
		if len(name) <= idSize {
			return name[1:]
		}
		return name[1:idSize]
	case strings.HasPrefix(name, "@"):
		if len(name) <= idSize {
			return name[1:]
		}
		return "..." + name[len(name)-(idSize-3)+1:]
	}
	const pre, rets, pos = `[string "`, "...", `"]`
	room := idSize - len(pre+rets+pos) - 1
	nl := strings.IndexByte(name, '\n')
	if len(name) < room && nl < 0 {
		return pre + name + pos
	}
	if nl >= 0 {
		name = name[:nl]
	}
	if len(name) > room {
		name = name[:room]
	}
	return pre + name + rets + pos
}

// setChunkName records the chunk name as given in p and the functions inside it.
func setChunkName(p *proto, name string) {
	p.chunk = name
	for _, q := range p.protos {
		setChunkName(q, name)
	}
}

// newMainClosure makes the closure of a chunk's main function under the globals table env.
// A chunk that names _ENV reads it through upvalue 0, which then holds env, in a cell when
// the chunk assigns it.
func newMainClosure(p *proto, info *syntax.Info, env *Table) *closure {
	cl := &closure{p: p, env: env}
	if len(p.upvals) > 0 {
		v := Value{}
		if env != nil {
			v = tableValue(env)
		}
		if info.EnvVar.Cell() {
			v = cellValue(&cell{v: v})
		}
		cl.up = []Value{v}
	}
	return cl
}

// openLoad installs load, the LibLoad library, in the globals table g, which is the
// default environment of the chunks it loads.
func openLoad(g *Table) {
	g.Set(String("load"), goFnValue(&goFunc{name: "load", f: func(c *Call) error { return baseLoad(c, g) }}))
}

// baseLoad is load(chunk [, chunkname [, mode [, env]]]). Only text is accepted. Source
// bytes are charged to the allocation budget, and tokens, roughly, to the step budget.
func baseLoad(c *Call, globals *Table) error {
	ex := c.ex
	chunk := c.Value(0)
	var src string
	switch {
	case chunk.isString():
		src = chunk.str()
	case chunk.isObj(kindLuaFn) || chunk.isObj(kindGoFn):
		var b strings.Builder
		for {
			if err := ex.tick(); err != nil { // a reader that never ends runs out of steps
				return err
			}
			res, err := c.call(chunk)
			if le, ok := err.(*Error); ok && le.terminal {
				return err
			}
			if err != nil {
				return c.ReturnValues(Value{}, errValue(err))
			}
			if len(res) == 0 || res[0].IsNil() || (res[0].isString() && res[0].n&lenMask == 0) {
				break
			}
			if res[0].isNumber() {
				res[0] = String(res[0].String())
			} else if !res[0].isString() {
				return c.ReturnValues(Value{}, errValue(c.libErrorf("reader function must return a string")))
			}
			if err := ex.produce(int(res[0].n & lenMask)); err != nil { // the source grows as it is read
				return err
			}
			b.WriteString(res[0].str())
			if b.Len() > maxAlloc {
				return c.ReturnValues(Value{}, String("chunk too large"))
			}
		}
		src = b.String()
	default:
		return c.typeArgError(0, "load", "string")
	}
	name := "=(load)"
	if chunk.isString() {
		name = src
	}
	name, err := c.optString(1, "load", name)
	if err != nil {
		return err
	}
	mode, err := c.optString(2, "load", "bt")
	if err != nil {
		return err
	}
	if strings.HasPrefix(src, "\x1bLua") {
		if !strings.Contains(mode, "b") {
			return c.ReturnValues(Value{}, String("attempt to load a binary chunk (mode is '"+mode+"')"))
		}
		return c.ReturnValues(Value{}, String("binary chunks are not supported"))
	}
	if !strings.Contains(mode, "t") {
		return c.ReturnValues(Value{}, String("attempt to load a text chunk (mode is '"+mode+"')"))
	}
	if err := ex.charge(len(src)); err != nil {
		return err
	}
	for i := 0; i < len(src); i += 8 * 1024 { // one step per 8 bytes, a token's worth
		for range min(1024, (len(src)-i+7)/8) {
			if err := ex.tick(); err != nil {
				return err
			}
		}
	}
	p, info, err := compileChunk(chunkID(name), src)
	if err != nil {
		if se, ok := err.(*syntax.Error); ok {
			return c.ReturnValues(Value{}, String(se.Error()))
		}
		return err
	}
	setChunkName(p, name)
	env := globals
	if c.Len() >= 4 {
		env = nil
		if e := c.Value(3); e.isObj(kindTable) {
			env = (*Table)(e.p)
		}
	}
	return c.ReturnValues(luaFnValue(newMainClosure(p, info, env)))
}

// errValue is the Lua value of an error.
func errValue(err error) Value {
	if le, ok := err.(*Error); ok {
		return le.Value
	}
	return String(err.Error())
}
