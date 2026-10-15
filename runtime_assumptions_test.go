package glua

import (
	"runtime"
	"testing"
	"unsafe"
)

// The design leans on behaviour of the Go runtime that the spec does not promise. Each test
// here fails loudly if a Go release changes it. Two more are checked elsewhere: that the
// write barrier preserves registers and that a reachable call makes the allocator spill
// show up in the machine code, which bench/asmgate checks in CI; and that pointers are 8
// bytes, which value.go checks at compile time.

// The heap does not move objects: addresses are hashed, and compared with the stack limit
// inside fast.
func TestAssumeNonMovingHeap(t *testing.T) {
	type obj struct{ pad [64]byte }
	objs := make([]*obj, 1000)
	addrs := make([]uintptr, len(objs))
	for i := range objs {
		objs[i] = &obj{}
		addrs[i] = uintptr(unsafe.Pointer(objs[i]))
	}
	keys := NewTable(0, len(objs))
	for i := range objs {
		keys.Set(Value{p: unsafe.Pointer(objs[i]), n: kindUser << 56}, Int(int64(i)))
	}
	for range 5 {
		garbage := make([][]byte, 0, 1000)
		for range 1000 {
			garbage = append(garbage, make([]byte, 512))
		}
		runtime.KeepAlive(garbage)
		runtime.GC()
	}
	for i := range objs {
		if uintptr(unsafe.Pointer(objs[i])) != addrs[i] {
			t.Fatalf("object %d moved: the VM hashes addresses and compares them with the stack limit", i)
		}
		if v := keys.Get(Value{p: unsafe.Pointer(objs[i]), n: kindUser << 56}); v.String() != Int(int64(i)).String() {
			t.Fatalf("a table keyed by address lost object %d after a collection", i)
		}
	}
}

// A string is a pointer and a length: the VM builds string Values from both words, and
// loads string fields of bound structs that way.
func TestAssumeStringLayout(t *testing.T) {
	s := "hello, layout"
	w := (*[2]uintptr)(unsafe.Pointer(&s))
	if unsafe.Sizeof(s) != 16 || w[0] != uintptr(unsafe.Pointer(unsafe.StringData(s))) || w[1] != uintptr(len(s)) {
		t.Fatal("a string header is no longer a pointer and a length")
	}
}

// A slice is a pointer, a length and a capacity: slice proxies and the inline append read
// and write its words.
func TestAssumeSliceLayout(t *testing.T) {
	x := make([]Value, 3, 7)
	h := (*sliceHeader)(unsafe.Pointer(&x))
	if unsafe.Sizeof(x) != 24 || h.data != unsafe.Pointer(unsafe.SliceData(x)) || h.len != 3 || h.cap != 7 {
		t.Fatal("a slice header is no longer a pointer, a length and a capacity")
	}
	// the inline append writes only the length word
	*(*int)(unsafe.Add(unsafe.Pointer(&x), unsafe.Sizeof(uintptr(0)))) = 4
	if len(x) != 4 || cap(x) != 7 {
		t.Fatal("the length is not the second word of a slice")
	}
}

// A map value is one pointer, nil for a nil map: map proxies test it to tell a nil map.
func TestAssumeMapIsAPointer(t *testing.T) {
	var m map[string]int
	if unsafe.Sizeof(m) != unsafe.Sizeof(uintptr(0)) || *(*unsafe.Pointer)(unsafe.Pointer(&m)) != nil {
		t.Fatal("a nil map is no longer a nil pointer")
	}
	m = map[string]int{}
	if *(*unsafe.Pointer)(unsafe.Pointer(&m)) == nil {
		t.Fatal("a map is no longer a pointer")
	}
}

// A bool is one byte holding 0 or 1, and int is 8 bytes: the hot loop loads bound struct
// fields of both kinds directly.
func TestAssumeScalarLayout(t *testing.T) {
	b := true
	if unsafe.Sizeof(b) != 1 || *(*uint8)(unsafe.Pointer(&b)) != 1 {
		t.Fatal("a bool is no longer one byte holding 1 for true")
	}
	if unsafe.Sizeof(int(0)) != 8 {
		t.Fatal("int is not 8 bytes")
	}
}

// An interior pointer keeps its whole object alive: a proxy of a nested struct points into
// its parent, which nothing else may reference.
func TestAssumeInteriorPointersKeepAlive(t *testing.T) {
	type inner struct{ N int64 }
	type outer struct {
		pad   [4096]byte
		Inner inner
	}
	proxies := make([]*inner, 100)
	for i := range proxies {
		o := &outer{Inner: inner{N: int64(i)}}
		proxies[i] = &o.Inner
	}
	for range 3 {
		runtime.GC()
		_ = make([]byte, 1<<20)
	}
	for i, p := range proxies {
		if p.N != int64(i) {
			t.Fatalf("proxy %d reads %d after collections", i, p.N)
		}
	}
}
