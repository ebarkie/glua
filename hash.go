package glua

import (
	"math"
	"math/bits"
	"math/rand/v2"
	"unsafe"
)

// hashSeed keys every table hash. It is random per process, so pairs order differs between
// runs and chosen keys cannot force collisions. SetHashSeed will fix it for tests and replay.
var (
	hashSeed = rand.Uint64()
	hashMul  = rand.Uint64() | 1 // odd multiplier for integers, floats and addresses
)

const (
	m1 = 0xa0761d6478bd642f
	m2 = 0xe7037ed1a0b428db
)

// mix is a 64x64->128 multiply folded to 64 bits.
func mix(a, b uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	return hi ^ lo
}

// hashString is a seeded hash of the bytes of s. hash/maphash is not used because its seed
// cannot be set, and SetHashSeed needs one that can.
func hashString(p unsafe.Pointer, n int) uint64 {
	h := hashSeed ^ uint64(n)*m1
	for ; n >= 8; n -= 8 {
		h = mix(h^*(*uint64)(p), m2)
		p = unsafe.Add(p, 8)
	}
	var tail uint64
	for i := range n {
		tail |= uint64(*(*byte)(unsafe.Add(p, i))) << (8 * i)
	}
	return mix(h^tail, m1^hashSeed)
}

// hashWord hashes an integer, float bits or an address.
func hashWord(x uint64) uint64 {
	return mix(x*hashMul, m2^hashSeed)
}

// hashValue hashes a normalised key. Strings hash by content, everything else by its bits
// or address, so the hash agrees with rawEqual for keys.
func hashValue(v Value) uint64 {
	switch {
	case v.p == tInt(), v.p == tFloat(), v.p == tBool():
		return hashWord(v.n ^ uint64(uintptr(v.p)-uintptr(unsafe.Pointer(&tags[0])))<<62)
	case v.isString():
		return hashString(v.p, int(v.n&lenMask))
	}
	return hashWord(uint64(uintptr(v.p)))
}

// normKey turns a float with an exact integer value into an integer, so that a two-word
// compare is valid for keys. It reports false for nil and NaN, which cannot be keys.
func normKey(k Value) (Value, bool) {
	switch k.p {
	case nil:
		return k, false
	case tFloat():
		f := math.Float64frombits(k.n)
		if f != f {
			return k, false
		}
		if i, ok := floatToInt(f); ok {
			return Int(i), true
		}
	}
	return k, true
}
