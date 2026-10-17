package glua

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Soak tests: long entries that renew their budgets every round must not keep anything from
// one round to the next. GLUA_SOAK_ROUNDS runs them longer than the default.

// roundState is what an entry's executor looks like between rounds.
type roundState struct {
	base, top, depth, nny, errExtra, tbc int
	inCoroutine                          bool
	stack, frames, goErrs                int
	live                                 int64
}

func stateOf(c *Call) roundState {
	ex := c.ex
	return roundState{
		base: c.base, top: ex.top, depth: ex.depth, nny: ex.nny, errExtra: ex.errExtra, tbc: len(ex.tbc),
		inCoroutine: ex.co != nil,
		stack:       len(ex.stack), frames: len(ex.pframes), goErrs: len(ex.goErrs),
		live: c.LiveBytes(),
	}
}

// drift reports how round r strays from round first: the state the executor returns to after
// every round must be the same, and what it holds must not grow.
func drift(first, r roundState) error {
	switch {
	case r.base != first.base || r.top != first.top || r.depth != first.depth || r.nny != first.nny ||
		r.errExtra != first.errExtra || r.tbc != first.tbc || r.inCoroutine != first.inCoroutine:
		return fmt.Errorf("the executor is not back where it was: %+v, at first %+v", r, first)
	case r.stack > first.stack || r.frames > first.frames:
		return fmt.Errorf("the stack grew: %d slots and %d frames, at first %d and %d", r.stack, r.frames, first.stack, first.frames)
	case r.goErrs > maxGoErrs:
		return fmt.Errorf("%d host errors kept", r.goErrs)
	case r.live > 2*first.live+64<<10:
		return fmt.Errorf("live memory grew from %d to %d bytes", first.live, r.live)
	}
	return nil
}

func soakRounds(t *testing.T) int {
	if s := os.Getenv("GLUA_SOAK_ROUNDS"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if testing.Short() {
		t.Skip("a soak test")
	}
	return 20_000
}

func TestLongEntrySoak(t *testing.T) {
	rounds := soakRounds(t)
	for _, sc := range []struct{ name, work string }{
		{"mixed work", `
			local keep = {}
			function work(i)
				local t = {}
				for j = 1, 20 do t["k" .. j] = j .. "v" end
				local s = string.format("%d:%s", i, t.k3) .. string.rep("x", 50)
				s = s:gsub("x", "y")
				local f = function() return s end
				pcall(error, {code = i})
				local co = coroutine.create(function(a) local b = coroutine.yield(a) return b end)
				coroutine.resume(co, 1)
				if i % 2 == 0 then coroutine.resume(co, 2) end -- the others are dropped suspended
				local w = coroutine.wrap(function() coroutine.yield(1) end)
				w()
				pcall(coroutine.wrap(function() error("in a coroutine") end))
				local arr = {}
				for j = 1, 10 do arr[j] = (i * j) % 7 end
				table.sort(arr)
				do local c <close> = setmetatable({}, {__close = function() end}) end
				keep[i % 100] = f
			end`},
		{"errors and message handlers", `
			function work(i) end
			function main(n) -- caught in the frame that loops, where a slot left behind adds up
				for i = 1, n do
					pcall(fail, i)
					xpcall(error, debug.traceback, "e" .. i)
					xpcall(fail, function(m) return m end, i)
					xpcall(function() error("e") end, tostring)
					round(i)
				end
			end`},
		{"a queue and a churning hash", `
			local q, head, tail, h = {}, 1, 0, {}
			function work(i)
				tail = tail + 1; q[tail] = {i}
				if tail - head >= 100 then q[head] = nil; head = head + 1 end
				h["key" .. i] = i
				h["key" .. (i - 100)] = nil
			end`},
		{"deep calls now and then", `
			local function deep(n)
				local s = string.rep("z", 100) .. n
				if n > 0 then return deep(n - 1) + #s end
				return #s
			end
			function work(i) if i % 1000 == 1 then deep(150) end end`},
	} {
		t.Run(sc.name, func(t *testing.T) {
			var first roundState
			var failed error
			rt := MustNew(LibSafe|LibCoroutine, Limits{Steps: 200_000, AllocBytes: 4 << 20},
				Global("fail", GoFunc(func(c *Call) error { return fmt.Errorf("not found: %d", c.Arg[int64](0)) })),
				Global("round", GoFunc(func(c *Call) error {
					c.Renew()
					st := stateOf(c)
					switch i := c.Arg[int64](0); {
					case i == 2: // the first round may grow what the rest reuse
						first = st
					case i > 2 && failed == nil:
						if err := drift(first, st); err != nil {
							failed = fmt.Errorf("round %d: %w", i, err)
						}
					}
					return nil
				})))
			src := sc.work
			if !strings.Contains(src, "function main") {
				src += "\nfunction main(n) for i = 1, n do work(i) round(i) end end"
			}
			vm := must(mustLoad(t, rt, src).NewVM(context.Background()))
			if _, err := vm.Call(context.Background(), vm.Globals().Get(String("main")), Int(int64(rounds))); err != nil {
				t.Fatal(err)
			}
			if failed != nil {
				t.Error(failed)
			}
		})
	}
}

// FuzzLongEntry runs a program round after round in one entry that renews its budgets, each
// round in globals of its own and from the same random seed, so that every round does the
// same work. Whatever the program does, the executor must be back where it was after each
// round, and what it holds must not grow.
func FuzzLongEntry(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, src string) {
		if len(src) > 1<<14 {
			return
		}
		if _, _, err := compileChunk("fuzz", src); err != nil {
			return
		}
		var first roundState
		var failed error
		rounds := 0
		round := goFnValue(&goFunc{name: "round", f: func(c *Call) error {
			c.Renew()
			st := stateOf(c)
			rounds++
			switch i, _ := c.Value(0).Int(); {
			case i == 2:
				first = st
			case i > 2 && failed == nil:
				if err := drift(first, st); err != nil {
					failed = fmt.Errorf("round %d: %w", i, err)
				}
			}
			return nil
		}})
		rt := MustNew(LibSafe|LibCoroutine|LibLoad, fuzzLimits, RandSeed(1),
			Global("fail", GoFunc(func(c *Call) error { return errors.New("host error") })))
		s, err := rt.Load(context.Background(), "driver", `
			function main(src, round)
				for i = 1, 6 do
					math.randomseed(1)
					local f = load(src, "=fuzz", "t", setmetatable({}, {__index = _G}))
					pcall(f)
					round(i)
				end
			end`)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Call(context.Background(), s.Globals().Get(String("main")), String(src), round)
		if pe, ok := errors.AsType[*PanicError](err); ok {
			t.Fatalf("the VM panicked: %v\n%s", pe.Value, pe.Stack)
		}
		if failed != nil {
			t.Fatal(failed)
		}
		if err == nil && rounds != 6 {
			t.Fatalf("the entry ended after %d rounds", rounds)
		}
	})
}
