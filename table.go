package glua

import "unsafe"

// node is one slot of a table's hash part. A deleted key stays with a nil value, so
// assigning nil during a traversal is safe; a rehash drops it.
type node struct {
	k, v Value
}

// Table is a Lua table: an array part for the keys 1..n and an open-addressing hash part
// of our own. Go maps are not used: they rehash the key on every access and cannot
// support next() without side tables. Table methods are raw: they never run metamethods.
type Table struct {
	arr   []Value
	nodes []node // length is zero or a power of two; linear probing from hash & mask
	used  int    // nodes holding a key, deleted ones included
	meta  *Table
	flags uint8
}

const (
	tabFrozen = 1 << iota
)

// NewTable returns a table with room for narr array items and nhash other keys.
func NewTable(narr, nhash int) *Table {
	t := &Table{}
	if narr > 0 {
		t.arr = make([]Value, 0, narr)
	}
	if nhash > 0 {
		t.nodes = make([]node, nodeSize(nhash))
	}
	return t
}

// nodeSize returns the power of two that holds n keys at a load factor of 3/4.
func nodeSize(n int) int {
	size := 4
	for size*3 < n*4 {
		size *= 2
	}
	return size
}

// keyEq is key equality for normalised keys: the same two words, or two strings with the
// same bytes behind different pointers.
func keyEq(a, b *Value) bool {
	if same(a, b) {
		return true
	}
	return a.n == b.n && a.n>>56 == kindString && !isScalar(a.p) && !isScalar(b.p) && a.str() == b.str()
}

// findNode returns the index of key in the hash part, or -1.
func (t *Table) findNode(key *Value, h uint64) int {
	if len(t.nodes) == 0 {
		return -1
	}
	mask := uint64(len(t.nodes) - 1)
	for j := uint64(0); j <= mask; j++ {
		idx := (h + j) & mask
		nd := &t.nodes[idx]
		if nd.k.p == nil {
			return -1
		}
		if keyEq(&nd.k, key) {
			return int(idx)
		}
	}
	return -1
}

// Get returns t[key] without metamethods. A nil or NaN key reads as nil.
func (t *Table) Get(key Value) Value {
	v, _ := t.getSlot(key)
	return v
}

// getSlot returns t[key] and, for a key found in the hash part, its node index (else -1).
func (t *Table) getSlot(key Value) (Value, int) {
	key, ok := normKey(key)
	if !ok {
		return Value{}, -1
	}
	if key.p == tInt() {
		if i := int64(key.n) - 1; uint64(i) < uint64(len(t.arr)) {
			return t.arr[i], -1
		}
	}
	if i := t.findNode(&key, hashValue(key)); i >= 0 {
		return t.nodes[i].v, i
	}
	return Value{}, -1
}

// Set assigns t[key] = val without metamethods. It panics if t is frozen, or if key is nil or NaN.
func (t *Table) Set(key, val Value) {
	if t.flags&tabFrozen != 0 {
		panic("glua: attempt to modify a frozen table")
	}
	key, ok := normKey(key)
	if !ok {
		if key.p == nil {
			panic("glua: table index is nil")
		}
		panic("glua: table index is NaN")
	}
	t.set(key, val)
}

// set assigns t[key] = val for a normalised key.
func (t *Table) set(key, val Value) {
	if key.p == tInt() {
		i := int64(key.n) - 1
		if uint64(i) < uint64(len(t.arr)) {
			t.arr[i] = val
			return
		}
		if i == int64(len(t.arr)) && !val.IsNil() {
			t.appendArr(val)
			return
		}
	}
	h := hashValue(key)
	if idx := t.findNode(&key, h); idx >= 0 {
		t.nodes[idx].v = val
		return
	}
	if val.IsNil() {
		return
	}
	if (t.used+1)*4 > len(t.nodes)*3 {
		t.rehash(1)
	}
	t.insert(key, val, h)
}

// appendArr adds val at the end of the array part, then moves any following integer
// keys over from the hash part.
func (t *Table) appendArr(val Value) {
	t.arr = append(t.arr, val)
	for len(t.nodes) > 0 {
		next := Int(int64(len(t.arr)) + 1)
		idx := t.findNode(&next, hashValue(next))
		if idx < 0 || t.nodes[idx].v.IsNil() {
			return
		}
		t.arr = append(t.arr, t.nodes[idx].v)
		t.nodes[idx].v = Value{} // leaves a deleted key behind; the next rehash drops it
	}
}

// insert places a new key; the caller has made room.
func (t *Table) insert(key, val Value, h uint64) {
	mask := uint64(len(t.nodes) - 1)
	for j := h; ; j++ {
		nd := &t.nodes[j&mask]
		if nd.k.p == nil {
			nd.k, nd.v = key, val
			t.used++
			return
		}
	}
}

// rehash rebuilds the hash part for its live keys plus extra, dropping deleted keys.
func (t *Table) rehash(extra int) {
	live := 0
	for i := range t.nodes {
		if !t.nodes[i].v.IsNil() {
			live++
		}
	}
	old := t.nodes
	t.nodes = make([]node, nodeSize(live+extra))
	t.used = 0
	for i := range old {
		if nd := &old[i]; !nd.v.IsNil() {
			t.insert(nd.k, nd.v, hashValue(nd.k))
		}
	}
}

// Len returns the length of t's sequence part, without metamethods.
func (t *Table) Len() int {
	n := len(t.arr)
	for n > 0 && t.arr[n-1].IsNil() {
		n--
	}
	return n
}

// Frozen reports whether t is read-only.
func (t *Table) Frozen() bool { return t.flags&tabFrozen != 0 }

// tableValue returns t as a Value.
func tableValue(t *Table) Value { return Value{p: unsafe.Pointer(t), n: kindTable << 56} }
