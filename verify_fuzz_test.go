package glua

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"
)

// The programs whose code FuzzVerifiedCode mutates.
var fuzzBases = []string{
	`local t = {1, 2, x = 3} local s = 0 for i = 1, #t do s = s + t[i] end return s, t.x`,
	`local function f(a, ...) local n = select and 0 or #{...} return a, ... end return f(1, 2, 3)`,
	`local c = 0 local function inc() c = c + 1 return c end inc() return inc(), c`,
	`local s = "" for k, v in next, {a = 1} do s = s .. k .. v end return s`,
	`local x <close> = nil local a, b = 1, 2 a, b = b, a if a < b then return a else return b end`,
	`local t = setmetatable({}, {__index = function(t, k) return k end}) return t.x, t[1]`,
	`local co = coroutine.wrap(function(a) local b = coroutine.yield(a) return pcall(coroutine.yield, b) end) return co(1), co(2), co(3)`,
}

// FuzzVerifiedCode mutates the code of compiled programs. Whatever the verifier accepts must
// run without a Go panic: the fast loop reads through raw pointers, so a gap in the verifier
// would show up here as a panic or a crash.
func FuzzVerifiedCode(f *testing.F) {
	f.Add(uint8(0), []byte{3, 0, 0, 0, 1, 2, 3, 4})
	f.Add(uint8(1), []byte{9, 0, 0, 0, 0x1a, 0, 0, 0})
	f.Add(uint8(4), []byte{2, 0, 0, 0, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, which uint8, muts []byte) {
		src := fuzzBases[int(which)%len(fuzzBases)]
		p, _, err := compileChunk("f", src)
		if err != nil {
			t.Fatal(err)
		}
		protos := allProtos(p)
		// each mutation is 8 bytes: a word index and a new value; the index picks a
		// prototype and a word in it, never the padding word
		for len(muts) >= 8 {
			idx := int(binary.LittleEndian.Uint32(muts))
			val := binary.LittleEndian.Uint32(muts[4:])
			muts = muts[8:]
			q := protos[idx%len(protos)]
			q.code[(idx/len(protos))%(len(q.code)-1)] = val
		}
		for _, q := range protos {
			if verify(q) != nil {
				return
			}
		}
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("verified code panicked: %v\n%s", r, dumpAll(protos))
			}
		}()
		ex := newExecutor()
		ex.maxSteps = 10_000
		runClosure(context.Background(), ex, &Closure{p: p, env: testGlobals()})
	})
}

func allProtos(p *Proto) []*Proto {
	list := []*Proto{p}
	for _, c := range p.protos {
		list = append(list, allProtos(c)...)
	}
	return list
}

func dumpAll(ps []*Proto) string {
	s := ""
	for i, p := range ps {
		s += fmt.Sprintf("-- proto %d\n%s\n", i, p.disasm())
	}
	return s
}
