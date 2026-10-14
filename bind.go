package glua

import (
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
	"unique"
	"unsafe"
)

type fieldKind uint8

const (
	fInt64 fieldKind = iota
	fInt
	fFloat64
	fString
	fBool
)

// field is one Go struct field as Lua sees it.
type field struct {
	key  Value     // the canonical Lua name
	name string    // the same, as a Go string
	off  uintptr   // offset inside the Go struct
	kind fieldKind // how to load it
	rw   bool      // tagged rw: a script may assign it
}

// load reads the field of the struct at base.
func (f *field) load(base unsafe.Pointer) Value {
	p := unsafe.Add(base, f.off)
	switch f.kind {
	case fInt64:
		return Int(*(*int64)(p))
	case fInt:
		return Int(int64(*(*int)(p)))
	case fFloat64:
		return Float(*(*float64)(p))
	case fBool:
		return Bool(*(*bool)(p))
	default:
		return String(*(*string)(p))
	}
}

// typeInfo is the layout plan of a struct type, built once by reflection.
type typeInfo struct {
	rt      reflect.Type
	fields  []field
	handles []unique.Handle[string] // keep the canonical field names alive
}

var (
	typeMu  sync.Mutex
	typeIDs = map[reflect.Type]uint64{} // process-wide: an ID means the same type in every Runtime
	types   atomic.Pointer[[]*typeInfo] // copy on write, so the loop reads it without a lock
)

func init() {
	types.Store(&[]*typeInfo{})
}

// bindType builds the field plan of struct type rt once and returns its type ID. The spike
// binds exported fields of five kinds; milestone 2 adds the rest.
func bindType(rt reflect.Type) uint64 {
	typeMu.Lock()
	defer typeMu.Unlock()
	if id, ok := typeIDs[rt]; ok {
		return id
	}
	ti := &typeInfo{rt: rt}
	for i := range rt.NumField() {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		var k fieldKind
		switch sf.Type.Kind() {
		case reflect.Int64:
			k = fInt64
		case reflect.Int:
			k = fInt
		case reflect.Float64:
			k = fFloat64
		case reflect.String:
			k = fString
		case reflect.Bool:
			k = fBool
		default:
			continue
		}
		name, rw := snakeCase(sf.Name), false
		if tag, ok := sf.Tag.Lookup("lua"); ok {
			n, opt, _ := strings.Cut(tag, ",")
			if n == "-" {
				continue
			}
			if n != "" {
				name = n
			}
			rw = opt == "rw"
		}
		h := unique.Make(name)
		ti.handles = append(ti.handles, h)
		key := String(h.Value()) // canonical, so a constant of the same name has the same pointer
		ti.fields = append(ti.fields, field{key: key, name: name, off: sf.Offset, kind: k, rw: rw})
	}
	cur := *types.Load()
	next := append(append(make([]*typeInfo, 0, len(cur)+1), cur...), ti)
	types.Store(&next)
	id := uint64(len(next) - 1)
	typeIDs[rt] = id
	return id
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
