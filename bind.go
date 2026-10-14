package glua

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unique"
	"unsafe"
)

// Struct binding. Each bound Go type gets a plan, built once by reflection: for a struct,
// the offset and kind of each visible field; for a slice or a map, how its elements load.
// A pointer to a struct is a userdata whose p is the Go pointer itself and whose n carries
// the type ID. Nested structs, slices and maps are userdata too, pointing at the field, so
// they are live and cost nothing.
//
// Plans and type IDs are process-wide: an ID means the same type in every Runtime. What a
// Runtime adds with Type, Method, Meta, Writable, Hide and Rename lives in a copy of the
// plan that only its executors see.

type fieldKind uint8

// Field kinds. The first five are loaded inline by the hot loop; the rest go to the driver.
const (
	fInt64 fieldKind = iota
	fInt
	fFloat64
	fString
	fBool
	fInt32
	fInt16
	fInt8
	fUint64
	fUint
	fUint32
	fUint16
	fUint8
	fFloat32
	fStruct // a struct by value: a userdata pointing into the parent
	fPtr    // a pointer to a struct: a userdata of that pointer
	fSlice  // a live proxy, or a copy under Hardened
	fMap    // a live proxy, or a copy under Hardened
	fOther  // converted by the general rules, such as time.Time or a Convert type
)

// field is one Go struct field, or the element of a slice or map, as Lua sees it.
type field struct {
	key    Value     // the canonical Lua name
	name   string    // the same, as a Go string
	goName string    // the Go field name, which the TypeOptions use
	off    uintptr   // offset inside the Go struct
	kind   fieldKind // how to load it
	rw     bool      // tagged rw, or listed in Writable: a script may assign it
	typ    reflect.Type
	sub    uint64 // the plan of a struct, pointer, slice or map field
}

type planKind uint8

const (
	planStruct planKind = iota
	planSlice
	planMap
	planOpaque // a userdata without fields, such as a type AutoBind(false) did not register
)

// typeInfo is the plan of a bound type.
type typeInfo struct {
	rt      reflect.Type // the struct type, or the slice or map type
	id      uint64
	kind    planKind
	fields  []field
	byName  map[string]int   // Lua name to index in fields
	methods map[string]Value // Method, per Runtime
	meta    *Table           // the metatable: Meta, per Runtime; __len and __pairs for proxies
	elem    field            // planSlice and planMap: how an element loads
	err     string           // why the type cannot be bound, reported at bind time
	handles []unique.Handle[string]
}

var (
	typeMu  sync.Mutex
	typeIDs = map[reflect.Type]uint64{} // process-wide: an ID means the same type in every Runtime
	types   atomic.Pointer[[]*typeInfo] // copy on write, so the loop reads it without a lock
)

func init() {
	types.Store(&[]*typeInfo{})
}

// bindType builds the plan of struct, slice or map type rt once, with those of the types its
// fields reach, and returns its type ID.
func bindType(rt reflect.Type) uint64 {
	typeMu.Lock()
	defer typeMu.Unlock()
	var added []*typeInfo
	id := bindTypeLocked(rt, &added)
	if len(added) > 0 {
		cur := *types.Load()
		next := make([]*typeInfo, len(cur), len(cur)+len(added))
		copy(next, cur)
		for _, ti := range added {
			for uint64(len(next)) <= ti.id {
				next = append(next, nil)
			}
			next[ti.id] = ti
		}
		types.Store(&next)
	}
	return id
}

// nextTypeID is the next ID to hand out.
var nextTypeID uint64

func bindTypeLocked(rt reflect.Type, added *[]*typeInfo) uint64 {
	if id, ok := typeIDs[rt]; ok {
		return id
	}
	id := nextTypeID
	nextTypeID++
	if id >= 1<<32 {
		panic("glua: too many bound types") // the loop reads the ID as 32 bits
	}
	typeIDs[rt] = id // before the fields, so a type can refer to itself
	ti := &typeInfo{rt: rt, id: id, byName: map[string]int{}}
	*added = append(*added, ti)
	switch rt.Kind() {
	case reflect.Slice:
		ti.kind = planSlice
		ti.elem = elemField(rt.Elem(), added)
		ti.meta = proxyMeta
	case reflect.Map:
		ti.kind = planMap
		ti.elem = elemField(rt.Elem(), added)
		ti.meta = proxyMeta
	case reflect.Struct:
		planStructFields(ti, added)
	default:
		ti.kind = planOpaque
	}
	return id
}

// planStructFields fills in the visible fields of a struct plan, embedded structs' promoted
// fields included. Two fields that map to one name, or a name that is a Lua keyword, make
// the type unbindable.
func planStructFields(ti *typeInfo, added *[]*typeInfo) {
	rt := ti.rt
	for _, sf := range reflect.VisibleFields(rt) {
		if !sf.IsExported() || sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			continue
		}
		off, ok := fieldOffset(rt, sf.Index)
		if !ok {
			continue // promoted through an embedded pointer
		}
		name, rw, skip := fieldName(sf)
		if skip {
			continue
		}
		f := elemField(sf.Type, added)
		f.goName, f.off, f.rw = sf.Name, off, rw
		ti.addField(f, name)
	}
}

// addField adds f under the Lua name name.
func (ti *typeInfo) addField(f field, name string) {
	if !isName(name) {
		ti.err = fmt.Sprintf("field %s maps to '%s', which is not a Lua name", f.goName, name)
		return
	}
	if j, dup := ti.byName[name]; dup {
		ti.err = fmt.Sprintf("fields %s and %s both map to '%s'", ti.fields[j].goName, f.goName, name)
		return
	}
	h := unique.Make(name)
	ti.handles = append(ti.handles, h)
	f.name = name
	f.key = String(h.Value()) // canonical, so a constant of the same name has the same pointer
	ti.byName[name] = len(ti.fields)
	ti.fields = append(ti.fields, f)
}

// fieldOffset sums the offsets along an index path, unless it goes through a pointer.
func fieldOffset(rt reflect.Type, index []int) (uintptr, bool) {
	var off uintptr
	t := rt
	for i, x := range index {
		sf := t.Field(x)
		off += sf.Offset
		t = sf.Type
		if i < len(index)-1 && t.Kind() != reflect.Struct {
			return 0, false
		}
	}
	return off, true
}

// elemField returns how a value of type t, stored in a field or an element, loads.
func elemField(t reflect.Type, added *[]*typeInfo) field {
	f := field{typ: t, kind: fOther}
	if converterFor(t) != nil || t == timeType || t == durationType {
		return f
	}
	switch t.Kind() {
	case reflect.Int64:
		f.kind = fInt64
	case reflect.Int:
		f.kind = fInt
	case reflect.Int32:
		f.kind = fInt32
	case reflect.Int16:
		f.kind = fInt16
	case reflect.Int8:
		f.kind = fInt8
	case reflect.Uint64, reflect.Uintptr:
		f.kind = fUint64
	case reflect.Uint:
		f.kind = fUint
	case reflect.Uint32:
		f.kind = fUint32
	case reflect.Uint16:
		f.kind = fUint16
	case reflect.Uint8:
		f.kind = fUint8
	case reflect.Float64:
		f.kind = fFloat64
	case reflect.Float32:
		f.kind = fFloat32
	case reflect.String:
		f.kind = fString
	case reflect.Bool:
		f.kind = fBool
	case reflect.Struct:
		f.kind, f.sub = fStruct, bindTypeLocked(t, added)
	case reflect.Pointer:
		if t.Elem().Kind() == reflect.Struct && converterFor(t.Elem()) == nil {
			f.kind, f.sub = fPtr, bindTypeLocked(t.Elem(), added)
		}
	case reflect.Slice:
		if t != bytesType {
			f.kind, f.sub = fSlice, bindTypeLocked(t, added)
		}
	case reflect.Map:
		f.kind, f.sub = fMap, bindTypeLocked(t, added)
	}
	return f
}

// load reads the scalar field of the struct at base, for the five inline kinds and the other
// numbers. ok is false for the kinds that need the executor.
func (f *field) load(base unsafe.Pointer) (v Value, ok bool) {
	p := unsafe.Add(base, f.off)
	switch f.kind {
	case fInt64:
		return Int(*(*int64)(p)), true
	case fInt:
		return Int(int64(*(*int)(p))), true
	case fInt32:
		return Int(int64(*(*int32)(p))), true
	case fInt16:
		return Int(int64(*(*int16)(p))), true
	case fInt8:
		return Int(int64(*(*int8)(p))), true
	case fUint:
		return uintValue(uint64(*(*uint)(p))), true
	case fUint64:
		return uintValue(*(*uint64)(p)), true
	case fUint32:
		return Int(int64(*(*uint32)(p))), true
	case fUint16:
		return Int(int64(*(*uint16)(p))), true
	case fUint8:
		return Int(int64(*(*uint8)(p))), true
	case fFloat64:
		return Float(*(*float64)(p)), true
	case fFloat32:
		return Float(float64(*(*float32)(p))), true
	case fBool:
		return Bool(*(*bool)(p)), true
	case fString:
		return String(*(*string)(p)), true
	}
	return Value{}, false
}

// uintValue is an unsigned integer as Lua sees it: past the int64 range, a float.
func uintValue(u uint64) Value {
	if u > 1<<63-1 {
		return Float(float64(u))
	}
	return Int(int64(u))
}

// loadField reads field f of the struct at base, which the userdata obj holds or reaches.
// Struct, slice and map fields are userdata pointing at the field; they inherit obj's
// callscoped flag, and are read-only unless f is writable and obj is.
func (ex *executor) loadField(obj Value, base unsafe.Pointer, f *field) (Value, error) {
	if v, ok := f.load(base); ok {
		return v, nil
	}
	p := unsafe.Add(base, f.off)
	flags := obj.n & (flagCallscoped | flagReadonly)
	if !f.rw {
		flags |= flagReadonly
	}
	switch f.kind {
	case fStruct:
		return Value{p: p, n: kindUser<<56 | flags | f.sub}, nil
	case fPtr:
		q := *(*unsafe.Pointer)(p)
		if q == nil {
			return Value{}, nil
		}
		return Value{p: q, n: kindUser<<56 | obj.n&flagReadonly | f.sub}, nil
	case fSlice, fMap:
		if ex.rt != nil && ex.rt.hardened {
			return ex.copyOut(reflect.NewAt(f.typ, p).Elem())
		}
		if f.kind == fMap && *(*unsafe.Pointer)(p) == nil || f.kind == fSlice && (*sliceHeader)(p).data == nil {
			return Value{}, nil // a nil map or slice is nil
		}
		return Value{p: p, n: kindUser<<56 | flags | f.sub}, nil
	}
	return ex.copyOut(reflect.NewAt(f.typ, p).Elem())
}

// copyOut converts a Go value by the general rules, charging what it allocates.
func (ex *executor) copyOut(rv reflect.Value) (Value, error) {
	v, err := encodeReflect(rv, 0)
	if err != nil {
		return Value{}, ex.errorf("%s", err.Error())
	}
	if v.isObj(kindTable) {
		t := (*Table)(v.p)
		if err := ex.charge(64 + 16*len(t.arr) + 32*len(t.nodes)); err != nil {
			return Value{}, err
		}
	}
	return v, nil
}

// storeField assigns val to field f of the struct at base.
func (ex *executor) storeField(ti *typeInfo, base unsafe.Pointer, f *field, val Value) error {
	p := unsafe.Add(base, f.off)
	if err := decodeReflect(val, reflect.NewAt(f.typ, p).Elem(), "", 0); err != nil {
		msg := err.Error()
		if ce, ok := err.(*convErr); ok {
			msg = ce.msg
		}
		return ex.errorf("bad value for field '%s' of %s (%s)", f.name, ti.rt.Name(), msg)
	}
	return nil
}

// plan returns the plan of a userdata, as this executor's Runtime sees it.
func (ex *executor) plan(v Value) *typeInfo {
	id := v.n & typeIDMask
	if id >= uint64(len(*ex.types)) {
		ex.types = ex.planSet() // a type bound during this entry
	}
	if id < uint64(len(*ex.types)) {
		return (*ex.types)[id]
	}
	return nil
}

// planSet returns the plans this executor's entries see.
func (ex *executor) planSet() *[]*typeInfo {
	if ex.rt != nil {
		return ex.rt.plans()
	}
	return types.Load()
}

// indexUser does obj[key] for a userdata: a field, a method, an element of a proxy, or the
// type's __index.
func (ex *executor) indexUser(obj, key Value) (v Value, learnSlot int, err error) {
	ti := ex.plan(obj)
	learnSlot = -1
	if ti == nil {
		return Value{}, -1, ex.errorf("attempt to index a userdata value")
	}
	switch ti.kind {
	case planStruct:
		if name, ok := key.Str(); ok {
			if j, ok := ti.byName[name]; ok {
				v, err = ex.loadField(obj, obj.p, &ti.fields[j])
				if ti.fields[j].kind <= fBool {
					learnSlot = j
				}
				return v, learnSlot, err
			}
			if m, ok := ti.methods[name]; ok {
				return m, -1, nil
			}
		}
	case planSlice:
		hdr := (*sliceHeader)(obj.p)
		if i, ok := key.Int(); ok {
			if i < 1 || uint64(i) > uint64(hdr.len) {
				return Value{}, -1, nil
			}
			elem := unsafe.Add(hdr.data, uintptr(i-1)*ti.rt.Elem().Size())
			return ex.loadElem(obj, elem, &ti.elem)
		}
	case planMap:
		m := reflect.NewAt(ti.rt, obj.p).Elem()
		k := reflect.New(ti.rt.Key()).Elem()
		if decodeReflect(key, k, "", 0) != nil {
			return Value{}, -1, nil // a key the map cannot hold is absent
		}
		x := m.MapIndex(k)
		if !x.IsValid() {
			return Value{}, -1, nil
		}
		v, err = ex.copyOut(x)
		if v.n>>56 == kindUser && !isScalar(v.p) {
			v.n |= flagReadonly // a copy of a struct in the map: writing it would change nothing
		}
		return v, -1, err
	}
	if ti.meta != nil {
		if h := ti.meta.metaGet("__index"); !h.IsNil() && ti.kind == planStruct {
			return h, -2, nil // the caller calls or indexes it
		}
	}
	if name, ok := key.Str(); ok {
		return Value{}, -1, ex.errorf("no field '%s' on %s", name, ti.rt.Name())
	}
	return Value{}, -1, ex.errorf("no field %s on %s", key.String(), ti.rt.Name())
}

// loadElem reads the element at p of a slice proxy obj, as a field at offset 0.
func (ex *executor) loadElem(obj Value, p unsafe.Pointer, f *field) (Value, int, error) {
	v, err := ex.loadField(obj, p, f)
	return v, -1, err
}

// newindexUser does obj[key] = val for a userdata.
func (ex *executor) newindexUser(obj, key, val Value) error {
	ti := ex.plan(obj)
	if ti == nil {
		return ex.errorf("attempt to index a userdata value")
	}
	readonly := obj.n&flagReadonly != 0
	switch ti.kind {
	case planStruct:
		name, ok := key.Str()
		j, found := ti.byName[name]
		if !ok || !found {
			if ti.meta != nil {
				if h := ti.meta.metaGet("__newindex"); !h.IsNil() {
					return ex.callMeta("__newindex", h, obj, key, val)
				}
			}
			if ok {
				return ex.errorf("no field '%s' on %s", name, ti.rt.Name())
			}
			return ex.errorf("no field %s on %s", key.String(), ti.rt.Name())
		}
		f := &ti.fields[j]
		if !f.rw {
			return ex.errorf("field '%s' of %s is read-only", name, ti.rt.Name())
		}
		if readonly {
			return ex.errorf("attempt to modify a read-only %s", ti.rt.Name())
		}
		return ex.storeField(ti, obj.p, f, val)
	case planSlice:
		if readonly {
			return ex.errorf("attempt to modify a read-only %s", ti.rt)
		}
		hdr := (*sliceHeader)(obj.p)
		i, ok := key.Int()
		if !ok || i < 1 || uint64(i) > uint64(hdr.len)+1 {
			return ex.errorf("index %s out of range for %s of length %d", key.String(), ti.rt, hdr.len)
		}
		s := reflect.NewAt(ti.rt, obj.p).Elem()
		if uint64(i) == uint64(hdr.len)+1 {
			if err := ex.charge(int(ti.rt.Elem().Size())); err != nil {
				return err
			}
			s.Set(reflect.Append(s, reflect.Zero(ti.rt.Elem())))
		}
		if err := decodeReflect(val, s.Index(int(i-1)), "", 0); err != nil {
			return ex.errorf("bad value for %s (%s)", ti.rt, convMsg(err))
		}
		return nil
	case planMap:
		if readonly {
			return ex.errorf("attempt to modify a read-only %s", ti.rt)
		}
		m := reflect.NewAt(ti.rt, obj.p).Elem()
		k := reflect.New(ti.rt.Key()).Elem()
		if err := decodeReflect(key, k, "", 0); err != nil {
			return ex.errorf("bad key for %s (%s)", ti.rt, convMsg(err))
		}
		if val.IsNil() {
			m.SetMapIndex(k, reflect.Value{})
			return nil
		}
		x := reflect.New(ti.rt.Elem()).Elem()
		if err := decodeReflect(val, x, "", 0); err != nil {
			return ex.errorf("bad value for %s (%s)", ti.rt, convMsg(err))
		}
		if err := ex.charge(int(ti.rt.Key().Size() + ti.rt.Elem().Size())); err != nil {
			return err
		}
		m.SetMapIndex(k, x)
		return nil
	}
	return ex.errorf("attempt to modify a %s", ti.rt)
}

// sliceHeader is the layout of a Go slice.
type sliceHeader struct {
	data     unsafe.Pointer
	len, cap int
}

// convMsg is the message of a conversion error, without the package prefix.
func convMsg(err error) string {
	if ce, ok := err.(*convErr); ok {
		return ce.msg
	}
	return err.Error()
}

// userLen is #obj for a slice or map proxy, or -1.
func (ex *executor) userLen(obj Value) int {
	ti := ex.plan(obj)
	if ti == nil {
		return -1
	}
	switch ti.kind {
	case planSlice:
		return (*sliceHeader)(obj.p).len
	case planMap:
		return reflect.NewAt(ti.rt, obj.p).Elem().Len()
	}
	return -1
}

// proxyMeta is the metatable of slice and map proxies: __len, and __pairs over the
// elements, in key order for maps.
var proxyMeta *Table

func init() {
	mt := NewTable(0, 4)
	mt.Set(String("__len"), goFnValue(&goFunc{name: "__len", f: func(c *Call) error {
		return c.ReturnValues(Int(int64(c.ex.userLen(c.Value(0)))))
	}}))
	mt.Set(String("__pairs"), goFnValue(&goFunc{name: "__pairs", f: proxyPairs}))
	mt.freeze()
	proxyMeta = mt
}

// proxyPairs returns the iterator over a slice or map proxy.
func proxyPairs(c *Call) error {
	obj := c.Value(0)
	ti := c.ex.plan(obj)
	switch ti.kind {
	case planSlice:
		return c.ReturnValues(ipairsAuxFn, obj, Int(0))
	case planMap:
		keys := reflect.NewAt(ti.rt, obj.p).Elem().MapKeys()
		lk := make([]Value, 0, len(keys))
		for _, k := range keys {
			v, err := c.ex.copyOut(k)
			if err != nil {
				return err
			}
			lk = append(lk, v)
		}
		sort.Slice(lk, func(i, j int) bool { return lessKey(lk[i], lk[j]) })
		i := 0
		it := &goFunc{name: "map iterator", stateful: true, f: func(c *Call) error {
			for i < len(lk) {
				k := lk[i]
				i++
				v, _, err := c.ex.indexUser(obj, k)
				if err != nil {
					return err
				}
				if !v.IsNil() {
					return c.ReturnValues(k, v)
				}
			}
			return c.ReturnValues(Value{})
		}}
		return c.ReturnValues(goFnValue(it), obj, Value{})
	}
	return c.libErrorf("bad argument to 'pairs'")
}

// lessKey orders map keys for iteration: numbers, then strings, then the rest.
func lessKey(a, b Value) bool {
	switch {
	case a.isNumber() && b.isNumber():
		x, _ := a.Float()
		y, _ := b.Float()
		return x < y
	case a.isString() && b.isString():
		return a.str() < b.str()
	}
	return a.isNumber() || a.isString() && !b.isNumber()
}

// snakeCase turns a Go field name into a Lua one: TotalPrice becomes total_price, and
// UserID becomes user_id.
func snakeCase(s string) string {
	r := []rune(s)
	var b strings.Builder
	for i, c := range r {
		if unicode.IsUpper(c) {
			if i > 0 && (unicode.IsLower(r[i-1]) || (i+1 < len(r) && unicode.IsLower(r[i+1]) && unicode.IsUpper(r[i-1]))) {
				b.WriteByte('_')
			}
			b.WriteRune(unicode.ToLower(c))
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// fieldName returns the Lua name of a struct field: snake_case, or the name its lua tag
// gives. rw reports a writable field, and skip one tagged "-".
func fieldName(sf reflect.StructField) (name string, rw, skip bool) {
	name = snakeCase(sf.Name)
	if tag, ok := sf.Tag.Lookup("lua"); ok {
		n, opt, _ := strings.Cut(tag, ",")
		if n == "-" {
			return "", false, true
		}
		if n != "" {
			name = n
		}
		rw = opt == "rw"
	}
	return name, rw, false
}

// TypeOption customises how a Go type appears in Lua, in Type.
type TypeOption interface{ applyType(*typeOptions) }

type typeOptionFunc func(*typeOptions)

func (f typeOptionFunc) applyType(o *typeOptions) { f(o) }

// typeOptions is what Type collects for one type.
type typeOptions struct {
	t        reflect.Type
	methods  []namedFunc
	meta     []namedFunc
	writable []string
	hide     []string
	rename   [][2]string
}

type namedFunc struct {
	name string
	f    GoFunc
}

// Type customises how *T appears in Lua: methods, metamethods, and which fields are visible,
// writable and under what name. With AutoBind(false), only types registered here are bound.
func Type[T any](opts ...TypeOption) Option {
	return optionFunc(func(o *options) {
		to := &typeOptions{t: reflect.TypeFor[T]()}
		for _, opt := range opts {
			opt.applyType(to)
		}
		o.types = append(o.types, to)
	})
}

// Method adds a method: obj:name(...) calls f with obj as the first argument.
func Method(name string, f GoFunc) TypeOption {
	return typeOptionFunc(func(o *typeOptions) { o.methods = append(o.methods, namedFunc{name, f}) })
}

// Meta adds a metamethod, such as "__eq", "__lt", "__tostring" or "__len".
func Meta(event string, f GoFunc) TypeOption {
	return typeOptionFunc(func(o *typeOptions) { o.meta = append(o.meta, namedFunc{event, f}) })
}

// Writable makes the named Go fields writable from Lua.
func Writable(fields ...string) TypeOption {
	return typeOptionFunc(func(o *typeOptions) { o.writable = append(o.writable, fields...) })
}

// Hide makes the named Go fields invisible to Lua.
func Hide(fields ...string) TypeOption {
	return typeOptionFunc(func(o *typeOptions) { o.hide = append(o.hide, fields...) })
}

// Rename gives the Go field a Lua name of its own.
func Rename(field, luaName string) TypeOption {
	return typeOptionFunc(func(o *typeOptions) { o.rename = append(o.rename, [2]string{field, luaName}) })
}

// customize returns the Runtime's copy of a plan with the options applied.
func customize(gp *typeInfo, o *typeOptions) (*typeInfo, error) {
	ti := &typeInfo{rt: gp.rt, id: gp.id, kind: gp.kind, byName: map[string]int{}, elem: gp.elem, meta: gp.meta}
	bad := func(format string, args ...any) error {
		return &BindError{Type: gp.rt, Reason: fmt.Sprintf(format, args...)}
	}
	known := map[string]bool{}
	for _, f := range gp.fields {
		known[f.goName] = true
	}
	for _, list := range [][]string{o.hide, o.writable} {
		for _, name := range list {
			if !known[name] {
				return nil, bad("no visible field %s", name)
			}
		}
	}
	renames := map[string]string{}
	for _, r := range o.rename {
		if !known[r[0]] {
			return nil, bad("no visible field %s", r[0])
		}
		renames[r[0]] = r[1]
	}
	hidden := map[string]bool{}
	for _, name := range o.hide {
		hidden[name] = true
	}
	writable := map[string]bool{}
	for _, name := range o.writable {
		writable[name] = true
	}
	for _, f := range gp.fields {
		if hidden[f.goName] {
			continue
		}
		if writable[f.goName] {
			f.rw = true
		}
		name := f.name
		if n, ok := renames[f.goName]; ok {
			name = n
		}
		ti.addField(f, name)
	}
	if ti.err != "" {
		return nil, bad("%s", ti.err)
	}
	if len(o.methods) > 0 {
		ti.methods = map[string]Value{}
		for _, m := range o.methods {
			if _, ok := ti.byName[m.name]; ok {
				return nil, bad("method %s has the name of a field", m.name)
			}
			if !isName(m.name) {
				return nil, bad("method name '%s' is not a Lua name", m.name)
			}
			ti.methods[m.name] = goFnValue(&goFunc{name: m.name, qualname: gp.rt.Name() + ":" + m.name, f: m.f})
		}
	}
	if len(o.meta) > 0 {
		mt := NewTable(0, len(o.meta)+1)
		mt.Set(String("__name"), String(gp.rt.Name()))
		for _, m := range o.meta {
			if !strings.HasPrefix(m.name, "__") {
				return nil, bad("metamethod name '%s' does not start with __", m.name)
			}
			mt.Set(String(m.name), goFnValue(&goFunc{name: m.name, f: m.f}))
		}
		mt.freeze()
		ti.meta = mt
	}
	return ti, nil
}

// opaque returns a copy of a struct plan without fields, for a type AutoBind(false) did not
// register.
func opaque(gp *typeInfo) *typeInfo {
	return &typeInfo{rt: gp.rt, id: gp.id, kind: planOpaque, byName: map[string]int{}}
}

// planSet is a Runtime's view of the process-wide plans.
type planSet struct {
	base  *[]*typeInfo // the process-wide list it was made from
	plans *[]*typeInfo
}

// plans returns the plans this Runtime's executors see: the process-wide ones, with this
// Runtime's customised and opaque copies in place.
func (rt *Runtime) plans() *[]*typeInfo {
	g := types.Load()
	if len(rt.custom) == 0 && rt.autoBind {
		return g
	}
	if ps := rt.planCache.Load(); ps != nil && ps.base == g {
		return ps.plans
	}
	rt.planMu.Lock()
	defer rt.planMu.Unlock()
	next := make([]*typeInfo, len(*g))
	for i, gp := range *g {
		switch c, ok := rt.custom[uint64(i)]; {
		case ok:
			next[i] = c
		case !rt.autoBind && gp != nil && gp.kind == planStruct:
			next[i] = opaque(gp)
		default:
			next[i] = gp
		}
	}
	rt.planCache.Store(&planSet{base: g, plans: &next})
	return &next
}

// registerTypes applies the Type options of New.
func (rt *Runtime) registerTypes(list []*typeOptions) error {
	rt.custom = map[uint64]*typeInfo{}
	rt.registered = map[reflect.Type]bool{}
	for _, o := range list {
		t := o.t
		if t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			return &BindError{Type: o.t, Reason: "Type needs a struct type"}
		}
		if rt.registered[t] {
			return &BindError{Type: t, Reason: "the type is registered twice"}
		}
		rt.registered[t] = true
		id := bindType(t)
		gp := (*types.Load())[id]
		c, err := customize(gp, o)
		if err != nil {
			return err
		}
		rt.custom[id] = c
	}
	return nil
}

// checkType reports, at bind time, a struct type the Runtime cannot expose: one AutoBind(false)
// did not register, one whose fields clash, or one with nothing visible.
func (rt *Runtime) checkType(t reflect.Type) error {
	if t == tableType {
		return nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || converterFor(t) != nil || t == timeType {
		return nil
	}
	if !rt.autoBind && !rt.registered[t] {
		return &BindError{Type: t, Reason: "the type is not registered with lua.Type, and AutoBind is off"}
	}
	id := bindType(t)
	ti := (*rt.plans())[id]
	if ti.err != "" {
		return &BindError{Type: t, Reason: ti.err}
	}
	if len(ti.fields) == 0 && len(ti.methods) == 0 && ti.meta == nil {
		return &BindError{Type: t, Reason: "nothing is visible: no exported fields, methods or metamethods; use lua.Convert"}
	}
	return nil
}
