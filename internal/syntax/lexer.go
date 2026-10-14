package syntax

import (
	"math"
	"strconv"
	"strings"
)

// Other is the kind of a byte that starts no token, such as '@'. C Lua hands such bytes to
// the parser, which reports "unexpected symbol near '@'".
const Other Kind = 255

// Lexer splits Lua source into tokens. It reports errors by panicking with *Error, which the
// parser recovers.
type Lexer struct {
	source string // chunk name for messages
	src    string
	pos    int
	line   int
	lineAt int // offset of the first byte of the current line
	buf    strings.Builder
}

// NewLexer returns a lexer for src. A first line starting with '#' is skipped, as C Lua's
// loaders do for files, when skipComment is set.
func NewLexer(source, src string, skipComment bool) *Lexer {
	l := &Lexer{source: source, src: src, line: 1}
	if skipComment && strings.HasPrefix(src, "#") {
		for l.pos < len(src) && !isNewline(src[l.pos]) {
			l.pos++
		}
	}
	return l
}

func isNewline(c byte) bool { return c == '\n' || c == '\r' }
func isAlpha(c byte) bool   { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' }
func isDigit(c byte) bool   { return c >= '0' && c <= '9' }
func isAlnum(c byte) bool   { return isAlpha(c) || isDigit(c) }
func isXDigit(c byte) bool  { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
func isSpace(c byte) bool   { return c == ' ' || c == '\t' || c == '\v' || c == '\f' || isNewline(c) }

func hexValue(c byte) int {
	switch {
	case isDigit(c):
		return int(c - '0')
	case c >= 'a':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}

// cur returns the current byte, or -1 at the end of the input.
func (l *Lexer) cur() int {
	if l.pos < len(l.src) {
		return int(l.src[l.pos])
	}
	return -1
}

func (l *Lexer) peek(n int) int {
	if l.pos+n < len(l.src) {
		return int(l.src[l.pos+n])
	}
	return -1
}

// newline skips '\n', '\r', "\n\r" or "\r\n" and counts one line.
func (l *Lexer) newline() {
	c := l.src[l.pos]
	l.pos++
	if l.pos < len(l.src) && isNewline(l.src[l.pos]) && l.src[l.pos] != c {
		l.pos++
	}
	l.line++
	l.lineAt = l.pos
}

// fail reports an error at the current line. near is the text after "near", or "" for none.
func (l *Lexer) fail(msg, near string) {
	if near != "" {
		msg += " near " + near
	}
	panic(&Error{Source: l.source, Line: l.line, Col: l.pos - l.lineAt + 1, Msg: msg})
}

// Next returns the next token.
func (l *Lexer) Next() Token {
	for {
		c := l.cur()
		start := l.pos
		tok := Token{Line: l.line, Col: l.pos - l.lineAt + 1}
		switch {
		case c < 0:
			tok.Kind = EOF
			return tok
		case c == '\n' || c == '\r':
			l.newline()
			continue
		case c == ' ' || c == '\t' || c == '\v' || c == '\f':
			l.pos++
			continue
		case c == '-':
			if l.peek(1) != '-' {
				l.pos++
				tok.Kind = Minus
				return tok
			}
			l.pos += 2
			if l.cur() == '[' {
				if sep := l.skipSep(); sep >= 2 {
					l.pos++ // the second bracket
					l.longString(sep, true, tok.Line)
					continue
				}
			}
			for l.pos < len(l.src) && !isNewline(l.src[l.pos]) {
				l.pos++
			}
			continue
		case c == '[':
			sep := l.skipSep()
			if sep >= 2 {
				l.pos++ // the second bracket
				l.longString(sep, false, tok.Line)
				tok.Kind, tok.Str, tok.raw = String, l.buf.String(), l.src[start:l.pos]
				return tok
			}
			if sep == 0 {
				l.fail("invalid long string delimiter", "'"+l.src[start:l.pos]+"'")
			}
			tok.Kind = LBracket
			return tok
		case c == '"' || c == '\'':
			raw := l.shortString(byte(c))
			tok.Kind, tok.Str, tok.raw = String, l.buf.String(), raw
			return tok
		case c == '.':
			if l.peek(1) == '.' {
				if l.peek(2) == '.' {
					l.pos += 3
					tok.Kind = Dots
				} else {
					l.pos += 2
					tok.Kind = Concat
				}
				return tok
			}
			if !isDigit(byte(l.peek(1))) {
				l.pos++
				tok.Kind = Dot
				return tok
			}
			return l.numeral(tok)
		case isDigit(byte(c)):
			return l.numeral(tok)
		case isAlpha(byte(c)):
			for l.pos < len(l.src) && isAlnum(l.src[l.pos]) {
				l.pos++
			}
			word := l.src[start:l.pos]
			if k, ok := keywords[word]; ok {
				tok.Kind = k
			} else {
				tok.Kind, tok.Str, tok.raw = Name, word, word
			}
			return tok
		}
		// operators and single bytes
		l.pos++
		two := func(next byte, k2, k1 Kind) Kind {
			if l.cur() == int(next) {
				l.pos++
				return k2
			}
			return k1
		}
		switch c {
		case '+':
			tok.Kind = Plus
		case '*':
			tok.Kind = Star
		case '/':
			tok.Kind = two('/', DSlash, Slash)
		case '%':
			tok.Kind = Percent
		case '^':
			tok.Kind = Caret
		case '#':
			tok.Kind = Hash
		case '&':
			tok.Kind = Amp
		case '~':
			tok.Kind = two('=', Ne, Tilde)
		case '|':
			tok.Kind = Pipe
		case '<':
			if l.cur() == '<' {
				l.pos++
				tok.Kind = Shl
			} else {
				tok.Kind = two('=', Le, Lt)
			}
		case '>':
			if l.cur() == '>' {
				l.pos++
				tok.Kind = Shr
			} else {
				tok.Kind = two('=', Ge, Gt)
			}
		case '=':
			tok.Kind = two('=', Eq, Assign)
		case '(':
			tok.Kind = LParen
		case ')':
			tok.Kind = RParen
		case '{':
			tok.Kind = LBrace
		case '}':
			tok.Kind = RBrace
		case ']':
			tok.Kind = RBracket
		case ':':
			tok.Kind = two(':', DColon, Colon)
		case ';':
			tok.Kind = Semi
		case ',':
			tok.Kind = Comma
		default:
			tok.Kind = Other
			if c >= 32 && c < 127 {
				tok.raw = "'" + string(rune(c)) + "'"
			} else {
				tok.raw = "'<\\" + strconv.Itoa(c) + ">'"
			}
		}
		return tok
	}
}

// skipSep reads the '[' or ']' at pos and the '=' signs after it, as C Lua's skip_sep
// does. It returns their count plus 2 if the same bracket follows, 1 for a lone bracket,
// and 0 for '=' signs without a second bracket. It never consumes the second bracket.
func (l *Lexer) skipSep() int {
	b := l.src[l.pos]
	l.pos++
	n := 0
	for l.cur() == '=' {
		l.pos++
		n++
	}
	if l.cur() == int(b) {
		return n + 2
	}
	if n == 0 {
		return 1
	}
	return 0
}

// longString reads the body of a long string or comment whose opening bracket with sep-2
// '=' signs has been read. The contents go to l.buf, with every newline sequence as "\n".
func (l *Lexer) longString(sep int, comment bool, startLine int) {
	l.buf.Reset()
	if l.pos < len(l.src) && isNewline(l.src[l.pos]) {
		l.newline() // a newline right after the opening bracket is not part of the string
	}
	for {
		c := l.cur()
		switch {
		case c < 0:
			what := "string"
			if comment {
				what = "comment"
			}
			l.fail("unfinished long "+what+" (starting at line "+strconv.Itoa(startLine)+")", "<eof>")
		case c == ']':
			save := l.pos
			if l.skipSep() == sep {
				l.pos++ // the second bracket
				return
			}
			if !comment {
				l.buf.WriteString(l.src[save:l.pos])
			}
		case isNewline(byte(c)):
			l.newline()
			if !comment {
				l.buf.WriteByte('\n')
			}
		default:
			if !comment {
				l.buf.WriteByte(byte(c))
			}
			l.pos++
		}
	}
}

// shortString reads a quoted string. Its contents, with escapes applied, go to l.buf; it
// returns the text C Lua shows after "near": the quotes around the decoded contents.
func (l *Lexer) shortString(q byte) string {
	l.buf.Reset()
	l.pos++ // opening quote
	near := func() string { return "'" + string(q) + l.buf.String() }
	for {
		c := l.cur()
		switch {
		case c < 0:
			l.fail("unfinished string", "<eof>")
		case isNewline(byte(c)):
			l.fail("unfinished string", near()+"'")
		case c == int(q):
			l.pos++
			return string(q) + l.buf.String() + string(q)
		case c == '\\':
			l.escape(near)
		default:
			l.buf.WriteByte(byte(c))
			l.pos++
		}
	}
}

// escape reads an escape sequence at pos and writes what it stands for to l.buf. On an
// error, the text after "near" holds the string so far, the raw escape and the offending
// byte, as in C Lua.
func (l *Lexer) escape(near func() string) {
	start := l.pos
	fail := func(msg string) {
		end := l.pos
		if end < len(l.src) {
			end++
		}
		l.fail(msg, near()+l.src[start:end]+"'")
	}
	l.pos++ // the backslash
	c := l.cur()
	switch {
	case c < 0:
		return // the caller reports the unfinished string
	case simpleEscapes[c] != 0:
		l.buf.WriteByte(simpleEscapes[c])
		l.pos++
	case isNewline(byte(c)):
		l.newline()
		l.buf.WriteByte('\n')
	case c == 'x':
		r := 0
		for range 2 {
			l.pos++
			if !isXDigit(byte(l.cur())) {
				fail("hexadecimal digit expected")
			}
			r = r*16 + hexValue(l.src[l.pos])
		}
		l.pos++
		l.buf.WriteByte(byte(r))
	case c == 'z':
		l.pos++
		for l.pos < len(l.src) && isSpace(l.src[l.pos]) {
			if isNewline(l.src[l.pos]) {
				l.newline()
			} else {
				l.pos++
			}
		}
	case c == 'u':
		l.pos++
		if l.cur() != '{' {
			fail("missing '{'")
		}
		l.pos++
		if !isXDigit(byte(l.cur())) {
			fail("hexadecimal digit expected")
		}
		r := uint32(0)
		for isXDigit(byte(l.cur())) {
			if r > 0x7FFFFFFF>>4 {
				fail("UTF-8 value too large")
			}
			r = r*16 + uint32(hexValue(l.src[l.pos]))
			l.pos++
		}
		if l.cur() != '}' {
			fail("missing '}'")
		}
		l.pos++
		l.buf.WriteString(utf8Esc(r))
	case isDigit(byte(c)):
		r := 0
		for i := 0; i < 3 && isDigit(byte(l.cur())); i++ {
			r = r*10 + int(l.src[l.pos]-'0')
			l.pos++
		}
		if r > 255 {
			fail("decimal escape too large")
		}
		l.buf.WriteByte(byte(r))
	default:
		fail("invalid escape sequence")
	}
}

var simpleEscapes = [256]byte{'a': '\a', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t', 'v': '\v', '\\': '\\', '"': '"', '\'': '\''}

// utf8Esc encodes x as C Lua's luaO_utf8esc does: standard UTF-8 up to 0x10FFFF, and the
// original 5- and 6-byte forms beyond, up to 0x7FFFFFFF. Surrogates are encoded as is.
func utf8Esc(x uint32) string {
	if x < 0x80 {
		return string([]byte{byte(x)})
	}
	var buf [8]byte
	n := 1
	mfb := uint32(0x3f) // maximum that fits in the first byte
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
	return string(buf[8-n:])
}

// numeral reads a number. Like C Lua, it takes hex digits, dots and exponents greedily, and
// one letter touching the end, so that "3x" is one malformed number rather than two tokens.
func (l *Lexer) numeral(tok Token) Token {
	start := l.pos
	expo := "Ee"
	if l.cur() == '0' && (l.peek(1) == 'x' || l.peek(1) == 'X') {
		l.pos += 2
		expo = "Pp"
	}
	for {
		c := l.cur()
		if c >= 0 && strings.IndexByte(expo, byte(c)) >= 0 {
			l.pos++
			if c := l.cur(); c == '+' || c == '-' {
				l.pos++
			}
		} else if c >= 0 && (isXDigit(byte(c)) || c == '.') {
			l.pos++
		} else {
			break
		}
	}
	if c := l.cur(); c >= 0 && isAlpha(byte(c)) {
		l.pos++
	}
	text := l.src[start:l.pos]
	tok.raw = text
	i, f, isInt, ok := ParseNumber(text)
	if !ok {
		l.fail("malformed number", "'"+text+"'")
	}
	if isInt {
		tok.Kind, tok.Int = Int, i
	} else {
		tok.Kind, tok.Num = Float, f
	}
	return tok
}

// ParseNumber converts a Lua numeral, without sign or surrounding space, as C Lua's
// luaO_str2num does: a decimal integer that fits in 64 bits, a hexadecimal integer
// (wrapping around), or else a float, decimal or hexadecimal.
func ParseNumber(s string) (i int64, f float64, isInt, ok bool) {
	if i, ok := parseInt(s); ok {
		return i, 0, true, true
	}
	if f, ok := parseFloat(s); ok {
		return 0, f, false, true
	}
	return 0, 0, false, false
}

func parseInt(s string) (int64, bool) {
	if len(s) > 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		var a uint64
		for i := 2; i < len(s); i++ {
			if !isXDigit(s[i]) {
				return 0, false
			}
			a = a*16 + uint64(hexValue(s[i])) // wraps around, as C Lua does
		}
		return int64(a), true
	}
	if s == "" {
		return 0, false
	}
	var a uint64
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return 0, false
		}
		d := uint64(s[i] - '0')
		if a > (math.MaxInt64-d)/10 {
			return 0, false // overflow: C Lua reads it as a float
		}
		a = a*10 + d
	}
	return int64(a), true
}

func parseFloat(s string) (float64, bool) {
	if len(s) > 1 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		return parseHexFloat(s[2:])
	}
	// strconv accepts forms Lua does not: "inf", "nan", underscores. Lua numerals are
	// digits, one dot and an exponent, so check that first.
	digits, dot, exp := 0, false, false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case isDigit(c):
			digits++
		case c == '.' && !dot && !exp:
			dot = true
		case (c == 'e' || c == 'E') && digits > 0 && !exp:
			exp = true
			if i+1 < len(s) && (s[i+1] == '+' || s[i+1] == '-') {
				i++
			}
			if i+1 >= len(s) {
				return 0, false
			}
		default:
			return 0, false
		}
	}
	if digits == 0 {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); !ok || ne.Err != strconv.ErrRange {
			return 0, false
		}
		// out of range: like strtod, take the infinity or zero ParseFloat returned
	}
	return f, true
}

// parseHexFloat reads the part after "0x" as C Lua's lua_strx2number does: hex digits with
// an optional dot, then an optional binary exponent "p[+-]digits".
func parseHexFloat(s string) (float64, bool) {
	const maxSigDig = 30
	var m float64
	e, sig, nonsig, any, dot := 0, 0, 0, false, false
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		if c == '.' {
			if dot {
				return 0, false
			}
			dot = true
			continue
		}
		if !isXDigit(c) {
			break
		}
		any = true
		if sig == 0 && c == '0' {
			nonsig++
		} else if sig++; sig <= maxSigDig {
			m = m*16 + float64(hexValue(c))
		} else {
			e++ // too many digits: ignore the digit, keep its weight
		}
		if dot {
			e--
		}
	}
	if !any {
		return 0, false
	}
	e *= 4
	if i < len(s) {
		if s[i] != 'p' && s[i] != 'P' {
			return 0, false
		}
		i++
		neg := false
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			neg = s[i] == '-'
			i++
		}
		if i >= len(s) {
			return 0, false
		}
		exp := 0
		for ; i < len(s); i++ {
			if !isDigit(s[i]) {
				return 0, false
			}
			if exp < 1<<20 {
				exp = exp*10 + int(s[i]-'0')
			}
		}
		if neg {
			exp = -exp
		}
		e += exp
	}
	return math.Ldexp(m, e), true
}

// Near returns the text C Lua shows after "near" for tok.
func (tok Token) Near() string {
	switch tok.Kind {
	case Name, String, Int, Float, Other:
		if tok.Kind == Other {
			return tok.raw
		}
		return "'" + tok.raw + "'"
	}
	return tok.Kind.quoted()
}
