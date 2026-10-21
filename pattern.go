package glua

import "strings"

// Lua patterns, ported from C Lua 5.4's lstrlib.c: a direct interpreter over the pattern
// text, with no compile step. Backtracking, scanning sets and comparing back-references
// charge steps, and recursion is capped at 200 levels ("pattern too complex").

const (
	maxMatchDepth = 200
	maxCaptures   = 32
	capUnfinished = -1
	capPosition   = -2
	luaEsc        = '%'
	specials      = "^$*+?.([%-"
)

type capture struct {
	init int // start in src
	len  int // length, or capUnfinished, or capPosition
}

type matchState struct {
	src     string
	pat     string
	level   int
	capture [maxCaptures]capture
	depth   int
	ticks   int
	c       *Call
	err     error
	caps    []Value // gsub's captures for a function, reused from match to match
}

// fail records the first error; matching then unwinds.
func (ms *matchState) fail(format string, args ...any) int {
	if ms.err == nil {
		ms.err = ms.c.libErrorf(format, args...)
	}
	return -1
}

// step charges the budget for one backtracking iteration.
func (ms *matchState) step() bool {
	if ms.ticks++; ms.ticks >= 64 {
		return ms.spend()
	}
	return ms.err == nil
}

// work charges the budget for n bytes of a class tested, a set scanned or a back-reference
// compared, which cost the same as n iterations.
func (ms *matchState) work(n int) bool {
	if ms.ticks += n; ms.ticks >= 64 {
		return ms.spend()
	}
	return ms.err == nil
}

// spend takes a step per 64 iterations counted.
func (ms *matchState) spend() bool {
	for ; ms.ticks >= 64; ms.ticks -= 64 {
		if err := ms.c.ex.tick(); err != nil {
			if ms.err == nil {
				ms.err = err
			}
			ms.ticks = 0
			return false
		}
	}
	return ms.err == nil
}

func (ms *matchState) checkCapture(c byte) int {
	l := int(c) - '1'
	if l < 0 || l >= ms.level || ms.capture[l].len == capUnfinished {
		return ms.fail("invalid capture index %%%d", l+1)
	}
	return l
}

func (ms *matchState) captureToClose() int {
	level := ms.level - 1
	for ; level >= 0; level-- {
		if ms.capture[level].len == capUnfinished {
			return level
		}
	}
	return ms.fail("invalid pattern capture")
}

// classEnd returns the index just past the single-character class at p.
func (ms *matchState) classEnd(p int) int {
	pat := ms.pat
	c := pat[p]
	p++
	if c == luaEsc {
		if p >= len(pat) {
			return ms.fail("malformed pattern (ends with '%%')")
		}
		return p + 1
	}
	if c == '[' {
		end := setEnd(pat, p)
		if end < 0 {
			return ms.fail("malformed pattern (missing ']')")
		}
		if !ms.work(end - p - 1) { // the set's length
			return -1
		}
		return end
	}
	return p
}

// setEnd returns the index just past the ']' of the set whose '[' is at p-1 of pat, or -1
// when none closes it.
func setEnd(pat string, p int) int {
	if p < len(pat) && pat[p] == '^' {
		p++
	}
	for { // look for a ']'
		if p >= len(pat) {
			return -1
		}
		cc := pat[p]
		p++
		if cc == luaEsc && p < len(pat) {
			p++ // skip escapes, such as '%]'
		}
		if p < len(pat) && pat[p] == ']' {
			return p + 1
		}
		if p >= len(pat) {
			return -1
		}
	}
}

func isAlpha(c byte) bool  { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigitB(c byte) bool { return c >= '0' && c <= '9' }
func isLower(c byte) bool  { return c >= 'a' && c <= 'z' }
func isUpper(c byte) bool  { return c >= 'A' && c <= 'Z' }
func isSpaceB(c byte) bool { return c == ' ' || c >= '\t' && c <= '\r' }
func isCntrl(c byte) bool  { return c < 32 || c == 127 }
func isPunct(c byte) bool  { return c > 32 && c < 127 && !isAlpha(c) && !isDigitB(c) }
func isXDigitB(c byte) bool {
	return isDigitB(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// singleClass reports whether c matches class cl, such as 'a' for %a; uppercase classes
// are complements; any other cl matches itself.
func singleClass(c, cl byte) bool {
	var res bool
	switch cl | 0x20 {
	case 'a':
		res = isAlpha(c)
	case 'c':
		res = isCntrl(c)
	case 'd':
		res = isDigitB(c)
	case 'g':
		res = c > 32 && c < 127
	case 'l':
		res = isLower(c)
	case 'p':
		res = isPunct(c)
	case 's':
		res = isSpaceB(c)
	case 'u':
		res = isUpper(c)
	case 'w':
		res = isAlpha(c) || isDigitB(c)
	case 'x':
		res = isXDigitB(c)
	case 'z':
		res = c == 0 // deprecated, but C Lua 5.4 still has it
	default:
		return cl == c
	}
	if isUpper(cl) {
		return !res
	}
	return res
}

// matchClass reports whether c matches the set [ ... ] whose '[' is at p and whose ']' is
// at ec-1. Its callers charge for the scan.
func (ms *matchState) matchClass(c byte, p, ec int) bool {
	pat := ms.pat
	sig := true
	p++
	if pat[p] == '^' {
		sig = false
		p++
	}
	for ec--; p < ec; p++ {
		switch {
		case pat[p] == luaEsc:
			p++
			if singleClass(c, pat[p]) {
				return sig
			}
		case p+2 < ec && pat[p+1] == '-':
			if pat[p] <= c && c <= pat[p+2] {
				return sig
			}
			p += 2
		case pat[p] == c:
			return sig
		}
	}
	return !sig
}

// singleMatch reports whether src[s] matches the class from p to ep.
func (ms *matchState) singleMatch(s, p, ep int) bool {
	if s >= len(ms.src) {
		return false
	}
	return ms.matchByte(ms.src[s], p, ep)
}

// matchByte reports whether the byte c matches the class from p to ep.
func (ms *matchState) matchByte(c byte, p, ep int) bool {
	switch ms.pat[p] {
	case '.':
		return true
	case luaEsc:
		return singleClass(c, ms.pat[p+1])
	case '[':
		return ms.matchClass(c, p, ep)
	default:
		return ms.pat[p] == c
	}
}

func (ms *matchState) matchBalance(s, p int) int {
	if p+1 >= len(ms.pat) {
		return ms.fail("malformed pattern (missing arguments to '%%b')")
	}
	if s >= len(ms.src) || ms.src[s] != ms.pat[p] {
		return -1
	}
	b, e := ms.pat[p], ms.pat[p+1]
	cont := 1
	for s++; s < len(ms.src); s++ {
		if !ms.step() {
			return -1
		}
		switch ms.src[s] {
		case e:
			if cont--; cont == 0 {
				return s + 1
			}
		case b:
			cont++
		}
	}
	return -1
}

func (ms *matchState) maxExpand(s, p, ep int) int {
	i := 0
	for ms.singleMatch(s+i, p, ep) {
		i++
		if !ms.work(ep - p) { // a set costs its length
			return -1
		}
	}
	for ; i >= 0; i-- { // try with the maximum repetitions, then fewer
		if r := ms.match(s+i, ep+1); r != -1 {
			return r
		}
		if ms.err != nil || !ms.step() {
			return -1
		}
	}
	return -1
}

func (ms *matchState) minExpand(s, p, ep int) int {
	for {
		if r := ms.match(s, ep+1); r != -1 {
			return r
		}
		if ms.err != nil || !ms.work(ep-p) { // a set costs its length
			return -1
		}
		if ms.singleMatch(s, p, ep) {
			s++
		} else {
			return -1
		}
	}
}

func (ms *matchState) startCapture(s, p, what int) int {
	if ms.level >= maxCaptures {
		return ms.fail("too many captures")
	}
	ms.capture[ms.level] = capture{init: s, len: what}
	ms.level++
	r := ms.match(s, p)
	if r == -1 {
		ms.level-- // undo the capture
	}
	return r
}

func (ms *matchState) endCapture(s, p int) int {
	l := ms.captureToClose()
	if l < 0 {
		return -1
	}
	ms.capture[l].len = s - ms.capture[l].init
	r := ms.match(s, p)
	if r == -1 {
		ms.capture[l].len = capUnfinished
	}
	return r
}

func (ms *matchState) matchCapture(s int, l byte) int {
	i := ms.checkCapture(l)
	if i < 0 {
		return -1
	}
	c := ms.capture[i]
	if c.len > 0 && !ms.work(min(c.len, len(ms.src)-s)) {
		return -1
	}
	// a position capture has no text, and like C Lua a back-reference to one never matches
	if c.len >= 0 && len(ms.src)-s >= c.len && ms.src[c.init:c.init+c.len] == ms.src[s:s+c.len] {
		return s + c.len
	}
	return -1
}

// match matches the pattern from p against the subject from s, and returns the end of the
// match or -1.
func (ms *matchState) match(s, p int) int {
	if ms.depth++; ms.depth > maxMatchDepth {
		return ms.fail("pattern too complex")
	}
	r := ms.doMatch(s, p)
	ms.depth--
	return r
}

// doMatch is match without the depth count, which a defer would make costly.
func (ms *matchState) doMatch(s, p int) int {
	pat := ms.pat
	for {
		if ms.err != nil || !ms.step() {
			return -1
		}
		if p == len(pat) {
			return s
		}
		switch pat[p] {
		case '(':
			if p+1 < len(pat) && pat[p+1] == ')' {
				return ms.startCapture(s, p+2, capPosition)
			}
			return ms.startCapture(s, p+1, capUnfinished)
		case ')':
			return ms.endCapture(s, p+1)
		case '$':
			if p+1 == len(pat) {
				if s == len(ms.src) {
					return s
				}
				return -1
			}
		case luaEsc:
			if p+1 < len(pat) {
				switch pat[p+1] {
				case 'b':
					s = ms.matchBalance(s, p+2)
					if s != -1 {
						p += 4
						continue
					}
					return -1
				case 'f':
					p += 2
					if p >= len(pat) || pat[p] != '[' {
						return ms.fail("missing '[' after '%%f' in pattern")
					}
					ep := ms.classEnd(p)
					if ep < 0 {
						return -1
					}
					var prev, cur byte
					if s > 0 {
						prev = ms.src[s-1]
					}
					if s < len(ms.src) {
						cur = ms.src[s]
					}
					if !ms.matchClass(prev, p, ep) && ms.matchClass(cur, p, ep) {
						p = ep
						continue
					}
					return -1
				case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
					s = ms.matchCapture(s, pat[p+1])
					if s != -1 {
						p += 2
						continue
					}
					return -1
				}
			}
		}
		// default: a single class, possibly followed by a quantifier
		ep := ms.classEnd(p)
		if ep < 0 {
			return -1
		}
		var epc byte
		if ep < len(pat) {
			epc = pat[ep]
		}
		if !ms.singleMatch(s, p, ep) {
			if epc == '*' || epc == '?' || epc == '-' { // accept empty
				p = ep + 1
				continue
			}
			return -1
		}
		switch epc {
		case '?':
			if r := ms.match(s+1, ep+1); r != -1 {
				return r
			}
			if ms.err != nil {
				return -1
			}
			p = ep + 1
		case '+':
			return ms.maxExpand(s+1, p, ep)
		case '*':
			return ms.maxExpand(s, p, ep)
		case '-':
			return ms.minExpand(s, p, ep)
		default:
			s++
			p = ep
		}
	}
}

// getCapture returns capture i as a value: a substring of the subject (no copy), or a
// position. With no captures, capture 0 is the whole match.
func (ms *matchState) getCapture(i, s, e int) Value {
	if i >= ms.level {
		if i != 0 {
			ms.fail("invalid capture index %%%d", i+1)
			return Value{}
		}
		return subString(String(ms.src), s, e)
	}
	c := ms.capture[i]
	switch c.len {
	case capUnfinished:
		ms.fail("unfinished capture")
		return Value{}
	case capPosition:
		return Int(int64(c.init) + 1)
	}
	return subString(String(ms.src), c.init, c.init+c.len)
}

// captures appends the captures of a match from s to e to dst, or the whole match when
// there are none and whole is set.
func (ms *matchState) captures(dst []Value, s, e int, whole bool) []Value {
	n := ms.level
	if n == 0 && whole {
		n = 1
	}
	for i := range n {
		dst = append(dst, ms.getCapture(i, s, e))
	}
	return dst
}

// firstSet is the bytes a match of an unanchored pattern can begin with, when its first
// item must take one: a literal, a class or a set, not followed by a quantifier that
// accepts none. A scan for a match skips the bytes outside it without entering the matcher,
// as most of a text is outside it: find, gsub and gmatch then look at each byte of the
// subject once, and the matcher sees only the bytes a match could start at.
type firstSet struct {
	one int // the one byte of the set, searched for as memchr does, or -1
	set [256]bool
}

// maxCachedPattern is the longest pattern whose first set is kept in the executor's cache,
// and maxFirstItem the longest first item a set is built for: the build tests 256 bytes
// against the item, and a long set is left to the matcher.
const (
	maxCachedPattern = 256
	maxFirstItem     = 64
)

// firstSet returns the set for ms's pattern, or nil when its first item may match nothing
// or anything, or is not a single class, or on an error of the budget, which is in ms.err.
// The sets are cached by pattern in the executor, as a set's 256 tests cost more than a
// short subject's scan; the cache is cleared when it fills with patterns built at run
// time, and long patterns are not kept.
func (ms *matchState) firstSet() *firstSet {
	ex := ms.c.ex
	if fs, ok := ex.firstSets[ms.pat]; ok {
		return fs
	}
	fs := ms.buildFirstSet()
	if ms.err != nil || len(ms.pat) > maxCachedPattern {
		return fs
	}
	if ex.firstSets == nil {
		ex.firstSets = make(map[string]*firstSet)
	} else if len(ex.firstSets) >= 64 {
		clear(ex.firstSets)
	}
	ex.firstSets[strings.Clone(ms.pat)] = fs // its own bytes: the pattern may be part of a large string
	return fs
}

// buildFirstSet reads the first item of the pattern and tests every byte against it,
// charging the tests as the matcher charges a scan of the item. A malformed pattern, or
// one the matcher fails on before its first item, gives no set, and the matcher reports it.
func (ms *matchState) buildFirstSet() *firstSet {
	pat := ms.pat
	p := 0
	for p < len(pat) && pat[p] == '(' && !(p+1 < len(pat) && pat[p+1] == ')') {
		p++ // a capture begins with its first item; a position capture takes no byte
	}
	if p >= len(pat) || p > maxCaptures { // past maxCaptures the matcher fails: "too many captures"
		return nil
	}
	var ep int
	switch pat[p] {
	case '^', '.', '(', ')':
		return nil // a literal '^' here would be one, but is left to the matcher
	case '$':
		if p+1 == len(pat) {
			return nil // the end anchor takes no byte
		}
		ep = p + 1
	case luaEsc:
		if p+1 >= len(pat) {
			return nil
		}
		if c := pat[p+1]; c == 'b' || c == 'f' || c >= '0' && c <= '9' {
			return nil // a balance, a frontier or a back-reference
		}
		ep = p + 2
	case '[':
		if ep = setEnd(pat, p+1); ep < 0 || ep-p > maxFirstItem {
			return nil
		}
	default:
		ep = p + 1
	}
	if ep < len(pat) && (pat[ep] == '*' || pat[ep] == '?' || pat[ep] == '-') {
		return nil // may take no byte
	}
	if !ms.work(256 * (ep - p)) {
		return nil
	}
	fs := &firstSet{one: -1}
	n := 0
	for c := range 256 {
		if ms.matchByte(byte(c), p, ep) {
			fs.set[c] = true
			n, fs.one = n+1, c
		}
	}
	if n > 1 {
		fs.one = -1
	}
	return fs
}

// next returns the first position at or after s where a match can begin, or -1.
func (fs *firstSet) next(src string, s int) int {
	if fs.one >= 0 {
		if i := strings.IndexByte(src[s:], byte(fs.one)); i >= 0 {
			return s + i
		}
		return -1
	}
	for ; s < len(src); s++ {
		if fs.set[src[s]] {
			return s
		}
	}
	return -1
}

// skip returns the next position at or after s a match can begin at, charging for the
// bytes passed over, or -1 when there is none, or the budget is spent (ms.err).
func (ms *matchState) skip(fs *firstSet, s int) int {
	n := fs.next(ms.src, s)
	if n < 0 {
		ms.work(len(ms.src) - s) // the whole rest was scanned
		return -1
	}
	if n > s && !ms.work(n-s) {
		return -1
	}
	return n
}
