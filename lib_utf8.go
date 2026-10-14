package glua

// The utf8 library, after C Lua 5.4's lutf8lib.c, with strict decoding (up to U+10FFFF,
// no surrogates) unless lax is asked for (up to 0x7FFFFFFF).

const (
	maxUnicode = 0x10FFFF
	maxUTF     = 0x7FFFFFFF
	msgInvalid = "invalid UTF-8 code"
)

func openUTF8(g *Table) {
	lib := newLib("utf8", map[string]GoFunc{
		"char": utf8Char, "codes": utf8Codes, "codepoint": utf8Codepoint, "len": utf8Len, "offset": utf8Offset,
	})
	lib.Set(String("charpattern"), String("[\x00-\x7F\xC2-\xFD][\x80-\xBF]*"))
	g.Set(String("utf8"), tableValue(lib))
}

func isCont(s string, i int) bool { return i < len(s) && s[i]&0xC0 == 0x80 }

// utf8Decode decodes the sequence at s[i] and returns its code and the index after it, or
// -1 for an invalid sequence. Past the end reads as a NUL byte, as in C.
func utf8Decode(s string, i int, strict bool) (rune, int) {
	limits := [...]uint32{^uint32(0), 0x80, 0x800, 0x10000, 0x200000, 0x4000000}
	byteAt := func(k int) uint32 {
		if k < len(s) {
			return uint32(s[k])
		}
		return 0
	}
	c := byteAt(i)
	var res uint32
	if c < 0x80 {
		res = c
	} else {
		count := 0
		for ; c&0x40 != 0; c <<= 1 {
			count++
			cc := byteAt(i + count)
			if cc&0xC0 != 0x80 {
				return 0, -1
			}
			res = res<<6 | cc&0x3F
		}
		res |= (c & 0x7F) << (count * 5)
		if count > 5 || res > maxUTF || res < limits[count] {
			return 0, -1
		}
		i += count
	}
	if strict && (res > maxUnicode || res >= 0xD800 && res <= 0xDFFF) {
		return 0, -1
	}
	return rune(res), i + 1
}

// uPosRelat is lutf8lib's u_posrelat: a negative position counts from the end.
func uPosRelat(pos int64, n int) int64 {
	switch {
	case pos >= 0:
		return pos
	case uint64(-pos) > uint64(n):
		return 0
	}
	return int64(n) + pos + 1
}

func utf8Len(c *Call) error {
	s, err := c.checkString(0, "len")
	if err != nil {
		return err
	}
	i, err := c.optInt(1, "len", 1)
	if err != nil {
		return err
	}
	j, err := c.optInt(2, "len", -1)
	if err != nil {
		return err
	}
	lax := c.Value(3).Truthy()
	posi, posj := uPosRelat(i, len(s)), uPosRelat(j, len(s))
	if posi < 1 || posi-1 > int64(len(s)) {
		return c.argError(2, "len", "initial position out of bounds")
	}
	posi--
	posj--
	if posj >= int64(len(s)) {
		return c.argError(3, "len", "final position out of bounds")
	}
	n := int64(0)
	for posi <= posj {
		_, next := utf8Decode(s, int(posi), !lax)
		if next < 0 {
			return c.ReturnValues(Value{}, Int(posi+1))
		}
		posi = int64(next)
		n++
	}
	return c.ReturnValues(Int(n))
}

func utf8Codepoint(c *Call) error {
	s, err := c.checkString(0, "codepoint")
	if err != nil {
		return err
	}
	i, err := c.optInt(1, "codepoint", 1)
	if err != nil {
		return err
	}
	posi := uPosRelat(i, len(s))
	j, err := c.optInt(2, "codepoint", posi)
	if err != nil {
		return err
	}
	pose := uPosRelat(j, len(s))
	lax := c.Value(3).Truthy()
	if posi < 1 {
		return c.argError(2, "codepoint", "out of bounds")
	}
	if pose > int64(len(s)) {
		return c.argError(3, "codepoint", "out of bounds")
	}
	if posi > pose {
		return c.ReturnValues()
	}
	var vals []Value
	for k := int(posi - 1); k < int(pose); {
		code, next := utf8Decode(s, k, !lax)
		if next < 0 {
			return c.libErrorf(msgInvalid)
		}
		vals = append(vals, Int(int64(code)))
		k = next
	}
	return c.ReturnValues(vals...)
}

func utf8Char(c *Call) error {
	var b []byte
	for i := range c.Len() {
		code, err := c.checkInt(i, "char")
		if err != nil {
			return err
		}
		if uint64(code) > maxUTF {
			return c.argError(i+1, "char", "value out of range")
		}
		b = append(b, utf8EscBytes(uint32(code))...)
	}
	return c.ReturnValues(String(string(b)))
}

// utf8EscBytes encodes x as C Lua's luaO_utf8esc does, with the extended forms beyond U+10FFFF.
func utf8EscBytes(x uint32) []byte {
	if x < 0x80 {
		return []byte{byte(x)}
	}
	var buf [8]byte
	n := 1
	mfb := uint32(0x3f)
	for {
		buf[8-n] = byte(0x80 | x&0x3f)
		n++
		x >>= 6
		mfb >>= 1
		if x <= mfb {
			break
		}
	}
	buf[8-n] = byte((^mfb << 1) | x)
	return append([]byte(nil), buf[8-n:]...)
}

func utf8Offset(c *Call) error {
	s, err := c.checkString(0, "offset")
	if err != nil {
		return err
	}
	n, err := c.checkInt(1, "offset")
	if err != nil {
		return err
	}
	def := int64(1)
	if n < 0 {
		def = int64(len(s)) + 1
	}
	i, err := c.optInt(2, "offset", def)
	if err != nil {
		return err
	}
	posi := uPosRelat(i, len(s))
	if posi < 1 || posi-1 > int64(len(s)) {
		return c.argError(3, "offset", "position out of bounds")
	}
	posi--
	if n == 0 {
		for posi > 0 && isCont(s, int(posi)) { // find the beginning of the current sequence
			posi--
		}
	} else {
		if isCont(s, int(posi)) {
			return c.libErrorf("initial position is a continuation byte")
		}
		if n < 0 {
			for n < 0 && posi > 0 { // move back
				for {
					posi--
					if !(posi > 0 && isCont(s, int(posi))) {
						break
					}
				}
				n++
			}
		} else {
			n-- // do not move for the first character
			for n > 0 && posi < int64(len(s)) {
				for {
					posi++
					if !isCont(s, int(posi)) {
						break
					}
				}
				n--
			}
		}
	}
	if n == 0 {
		return c.ReturnValues(Int(posi + 1))
	}
	return c.ReturnValues(Value{})
}

func utf8Codes(c *Call) error {
	s, err := c.checkString(0, "codes")
	if err != nil {
		return err
	}
	if isCont(s, 0) {
		return c.argError(1, "codes", msgInvalid)
	}
	strict := !c.Value(1).Truthy()
	it := utf8IterStrict
	if !strict {
		it = utf8IterLax
	}
	return c.ReturnValues(it, String(s), Int(0))
}

var (
	utf8IterStrict = goFnValue(&goFunc{name: "for iterator", f: func(c *Call) error { return utf8Iter(c, true) }})
	utf8IterLax    = goFnValue(&goFunc{name: "for iterator", f: func(c *Call) error { return utf8Iter(c, false) }})
)

func utf8Iter(c *Call, strict bool) error {
	s, err := c.checkString(0, "codes")
	if err != nil {
		return err
	}
	n, _ := c.Value(1).Int()
	i := uint64(n)
	if i < uint64(len(s)) {
		for isCont(s, int(i)) { // go to the next character
			i++
		}
	}
	if i >= uint64(len(s)) {
		return c.ReturnValues()
	}
	code, next := utf8Decode(s, int(i), strict)
	if next < 0 || isCont(s, next) {
		return c.libErrorf(msgInvalid)
	}
	return c.ReturnValues(Int(int64(i)+1), Int(int64(code)))
}
