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
//
// It reads through offsets that stay inside the string: advancing p past the last block
// would make a pointer one past the allocation, which invariant 3 forbids. A string's last
// partial block is read as the 8 bytes that end the string, overlapping the block before it,
// and a short string as two overlapping 4-byte words or three bytes, so that no byte loop runs.
func hashString(p unsafe.Pointer, n int) uint64 {
	h := hashSeed ^ uint64(n)*m1
	var a uint64
	switch {
	case n > 8:
		for i := 0; i+8 < n; i += 8 {
			h = mix(h^*(*uint64)(unsafe.Add(p, i)), m2)
		}
		a = *(*uint64)(unsafe.Add(p, n-8))
	case n == 8:
		a = *(*uint64)(p)
	case n >= 4:
		a = uint64(*(*uint32)(p))<<32 | uint64(*(*uint32)(unsafe.Add(p, n-4)))
	case n > 0:
		a = uint64(*(*byte)(p))<<16 | uint64(*(*byte)(unsafe.Add(p, n/2)))<<8 | uint64(*(*byte)(unsafe.Add(p, n-1)))
	}
	return mix(h^a, m1^hashSeed)
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
