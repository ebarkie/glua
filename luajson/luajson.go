// Package luajson converts between glua values and JSON, straight from tables to JSON text
// and back without an intermediate tree, and gives scripts a json module.
//
// A table that is a non-empty sequence, keys 1 to n and no others, becomes a JSON array.
// Any other table becomes an object, with its keys in sorted order so that the output is
// deterministic; integer keys become their decimal strings. Strings must be valid UTF-8.
// Functions, userdata, coroutines, NaN and infinities cannot be encoded. Tables nested more
// than 64 deep, and tables that contain themselves, are errors.
//
// Decoding gives tables, strings, booleans and numbers: an integer for a number written
// without a fraction or exponent that fits in 64 bits, a float otherwise; -0 is the float -0.0,
// as only a float keeps its sign. A JSON null is nil, so a null in an array leaves a hole and a
// null member is absent.
package luajson

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"

	"encoding/json/jsontext"

	lua "github.com/folbricht/glua"
)

// maxDepth is the deepest nesting of tables and of JSON arrays and objects.
const maxDepth = 64

// Options for Marshal.
type Options struct {
	Indent string // when set, one member or element per line, indented by this: spaces and tabs only
}

// Marshal encodes v as JSON.
func Marshal(v lua.Value, opts ...Options) ([]byte, error) {
	var b bytes.Buffer
	if err := MarshalWrite(&b, v, opts...); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

// MarshalWrite encodes v as JSON to w.
func MarshalWrite(w io.Writer, v lua.Value, opts ...Options) error {
	var jopts []jsontext.Options
	if len(opts) > 0 && opts[0].Indent != "" {
		if strings.Trim(opts[0].Indent, " \t") != "" { // jsontext panics on anything else
			return errors.New("luajson: the indent may hold only spaces and tabs")
		}
		jopts = append(jopts, jsontext.Multiline(true), jsontext.WithIndent(opts[0].Indent))
	}
	e := &encoder{enc: jsontext.NewEncoder(w, jopts...), seen: map[*lua.Table]bool{}}
	return e.value(v, 0, "")
}

type encoder struct {
	enc  *jsontext.Encoder
	seen map[*lua.Table]bool // tables on the current path
}

func (e *encoder) value(v lua.Value, depth int, path string) error {
	switch v.Kind() {
	case lua.KindNil:
		return e.enc.WriteToken(jsontext.Null)
	case lua.KindBool:
		return e.enc.WriteToken(jsontext.Bool(v.Truthy()))
	case lua.KindInt:
		i, _ := v.Int()
		return e.enc.WriteToken(jsontext.Int(i))
	case lua.KindFloat:
		f, _ := v.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return pathError(path, "%v cannot be encoded as JSON", f)
		}
		return e.enc.WriteToken(jsontext.Float(f))
	case lua.KindString:
		s, _ := v.Str()
		if err := e.enc.WriteToken(jsontext.String(s)); err != nil {
			return pathError(path, "string is not valid UTF-8")
		}
		return nil
	case lua.KindTable:
		t, _ := v.As[*lua.Table]()
		return e.table(t, depth, path)
	}
	return pathError(path, "a %s value cannot be encoded as JSON", v.Kind())
}

func (e *encoder) table(t *lua.Table, depth int, path string) error {
	if depth >= maxDepth {
		return pathError(path, "nested more than %d levels deep", maxDepth)
	}
	if e.seen[t] {
		return pathError(path, "the table contains itself")
	}
	e.seen[t] = true
	defer delete(e.seen, t)
	n, count, seq := t.Len(), 0, t.Len() > 0
	for k := range t.All() {
		count++
		if i, ok := k.Int(); !ok || k.Kind() != lua.KindInt || i < 1 || i > int64(n) {
			seq = false
		}
	}
	if seq && count == n { // keys 1 to n and no others: an array
		if err := e.enc.WriteToken(jsontext.BeginArray); err != nil {
			return err
		}
		for i, v := range t.Items() {
			if err := e.value(v, depth+1, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return e.enc.WriteToken(jsontext.EndArray)
	}
	type member struct {
		name string
		v    lua.Value
	}
	members := make([]member, 0, count)
	for k, v := range t.All() {
		var name string
		switch k.Kind() {
		case lua.KindString:
			name, _ = k.Str()
		case lua.KindInt:
			i, _ := k.Int()
			name = strconv.FormatInt(i, 10)
		default:
			return pathError(path, "a %s key cannot be a JSON object name", k.Kind())
		}
		members = append(members, member{name, v})
	}
	slices.SortFunc(members, func(a, b member) int { return strings.Compare(a.name, b.name) })
	if err := e.enc.WriteToken(jsontext.BeginObject); err != nil {
		return err
	}
	for i, m := range members {
		if i > 0 && m.name == members[i-1].name {
			return pathError(path, "keys 1 and \"1\" both name the member %q", m.name)
		}
		if err := e.enc.WriteToken(jsontext.String(m.name)); err != nil {
			return pathError(path, "key is not valid UTF-8")
		}
		if err := e.value(m.v, depth+1, path+"."+m.name); err != nil {
			return err
		}
	}
	return e.enc.WriteToken(jsontext.EndObject)
}

func pathError(path, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if path != "" {
		msg = strings.TrimPrefix(path, ".") + ": " + msg
	}
	return errors.New("luajson: " + msg)
}

// Unmarshal decodes one JSON value into tables, strings, numbers and booleans.
func Unmarshal(data []byte) (lua.Value, error) {
	return UnmarshalRead(bytes.NewReader(data))
}

// UnmarshalRead decodes one JSON value from r.
func UnmarshalRead(r io.Reader) (lua.Value, error) {
	d := &decoder{dec: jsontext.NewDecoder(r)}
	v, err := d.value(0)
	if err != nil {
		return lua.Value{}, d.wrap(err)
	}
	if _, err := d.dec.ReadToken(); err != io.EOF {
		return lua.Value{}, errors.New("luajson: data after the JSON value")
	}
	return v, nil
}

type decoder struct {
	dec    *jsontext.Decoder
	charge func(n int) error // charges what decoding makes, when a script decodes
}

func (d *decoder) wrap(err error) error {
	if strings.HasPrefix(err.Error(), "luajson: ") {
		return err
	}
	var se *jsontext.SyntacticError
	if errors.As(err, &se) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("luajson: %w", err)
	}
	return err
}

func (d *decoder) value(depth int) (lua.Value, error) {
	tok, err := d.dec.ReadToken()
	if err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return lua.Value{}, err
	}
	switch tok.Kind() {
	case 'n':
		return lua.Value{}, nil
	case 't', 'f':
		return lua.Bool(tok.Bool()), nil
	case '"':
		s := tok.String()
		if err := d.spend(len(s)); err != nil {
			return lua.Value{}, err
		}
		return lua.String(s), nil
	case '0':
		raw := tok.String()
		if !strings.ContainsAny(raw, ".eE") && raw != "-0" {
			if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
				return lua.Int(i), nil
			}
		}
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return lua.Value{}, fmt.Errorf("luajson: number %s: %w", raw, err)
		}
		return lua.Float(f), nil
	case '[':
		if depth >= maxDepth {
			return lua.Value{}, fmt.Errorf("luajson: nested more than %d levels deep", maxDepth)
		}
		t := lua.NewTable(0, 0)
		if err := d.spend(64); err != nil {
			return lua.Value{}, err
		}
		for i := int64(1); d.dec.PeekKind() != ']'; i++ {
			v, err := d.value(depth + 1)
			if err != nil {
				return lua.Value{}, err
			}
			if err := d.spend(16); err != nil {
				return lua.Value{}, err
			}
			if !v.IsNil() {
				t.Set(lua.Int(i), v)
			}
		}
		if _, err := d.dec.ReadToken(); err != nil {
			return lua.Value{}, err
		}
		return lua.ValueOf(t), nil
	case '{':
		if depth >= maxDepth {
			return lua.Value{}, fmt.Errorf("luajson: nested more than %d levels deep", maxDepth)
		}
		t := lua.NewTable(0, 0)
		if err := d.spend(64); err != nil {
			return lua.Value{}, err
		}
		for d.dec.PeekKind() != '}' {
			name, err := d.dec.ReadToken()
			if err != nil {
				return lua.Value{}, err
			}
			k := name.String()
			v, err := d.value(depth + 1)
			if err != nil {
				return lua.Value{}, err
			}
			if err := d.spend(32 + len(k)); err != nil {
				return lua.Value{}, err
			}
			if !v.IsNil() {
				t.Set(lua.String(k), v)
			}
		}
		if _, err := d.dec.ReadToken(); err != nil {
			return lua.Value{}, err
		}
		return lua.ValueOf(t), nil
	}
	return lua.Value{}, fmt.Errorf("luajson: unexpected %v", tok)
}

func (d *decoder) spend(n int) error {
	if d.charge == nil {
		return nil
	}
	return d.charge(n)
}

// Module returns an option that defines a module of the given name, usually "json", with
// encode(value [, indent]) and decode(text). What decode makes, and the text encode makes,
// count toward the calling script's budgets.
//
//	rt := lua.MustNew(luajson.Module("json"))
//	-- in Lua: local doc = json.decode(text); local text = json.encode({ok = true})
func Module(name string) lua.Option {
	return lua.Module(name, lua.Members{
		"encode": lua.GoFunc(encode),
		"decode": lua.GoFunc(decode),
	})
}

func encode(c *lua.Call) error {
	var opts Options
	if s, ok := c.Value(1).Str(); ok {
		opts.Indent = s
	}
	var b bytes.Buffer
	w := &chargedWriter{w: &b, c: c}
	if err := MarshalWrite(w, c.Value(0), opts); err != nil {
		if w.err != nil {
			return w.err
		}
		return c.Errorf("%s", strings.TrimPrefix(err.Error(), "luajson: "))
	}
	return c.Return(strings.TrimSuffix(b.String(), "\n"))
}

func decode(c *lua.Call) error {
	text := c.Arg[string](0)
	d := &decoder{dec: jsontext.NewDecoder(strings.NewReader(text)), charge: c.Charge}
	v, err := d.value(0)
	if err == nil {
		if _, terr := d.dec.ReadToken(); terr != io.EOF {
			err = errors.New("luajson: data after the JSON value")
		}
	}
	if err != nil {
		var le *lua.Error
		if errors.As(err, &le) {
			return err // a limit ran out
		}
		return c.Errorf("%s", strings.TrimPrefix(d.wrap(err).Error(), "luajson: "))
	}
	return c.ReturnValues(v)
}

// chargedWriter charges what encode writes before writing it.
type chargedWriter struct {
	w   io.Writer
	c   *lua.Call
	err error
}

func (w *chargedWriter) Write(p []byte) (int, error) {
	if w.err = w.c.Charge(len(p)); w.err != nil {
		return 0, w.err
	}
	return w.w.Write(p)
}
