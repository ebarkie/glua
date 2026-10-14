package glua

// Bounds-checked accessors for the checked loop, which genloop generates from fast.go into
// checked_gen.go. In the checked loop, a compiler or verifier bug becomes a Go panic, which
// the entry recovers and returns as a PanicError, instead of memory corruption.

func kAtChecked(k []Value, i uint32) *Value    { return &k[i] }
func hintAtChecked(p *Proto, i uint32) *uint32 { return &p.hints[i] }
func upAtChecked(cl *Closure, i uint8) *Value  { return &cl.up[i] }
func khashAtChecked(p *Proto, i uint32) uint64 { return p.khash[i] }

// checkedCaller returns the closure in a caller's function slot, asserting what the fast loop
// trusts.
func checkedCaller(f *Value) *Closure {
	if f.n != kindLuaFn<<56 || isScalar(f.p) {
		panic("glua: checked loop: no Lua function in the caller's function slot")
	}
	return (*Closure)(f.p)
}
