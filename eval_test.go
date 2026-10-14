package glua

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestEval(t *testing.T) {
	ctx := context.Background()
	if n, err := Eval[int64](ctx, "return 6 * 7"); err != nil || n != 42 {
		t.Errorf("6 * 7: %v %v", n, err)
	}
	if s, err := Eval[string](ctx, "return ('ab'):rep(2)"); err != nil || s != "abab" {
		t.Errorf("rep: %v %v", s, err)
	}
	if p, err := Eval[Pair[*Table, string]](ctx, "return nil, 'missing'"); err != nil || p.First != nil || p.Second != "missing" {
		t.Errorf("pair: %+v %v", p, err)
	}
	if _, err := Eval[int64](ctx, "return 'x'"); err == nil {
		t.Error("a string decoded as int64")
	}
	if _, err := Eval[int64](ctx, "error('boom')"); err == nil || err.Error() != "eval:1: boom" {
		t.Errorf("error: %v", err)
	}
	if v, err := Eval[Value](ctx, "return io, os, load, coroutine"); err != nil || !v.IsNil() {
		t.Errorf("LibSafe has io: %v %v", v, err)
	}
	if _, err := Eval[struct{}](ctx, "while true do end"); !errors.Is(err, ErrSteps) {
		t.Errorf("Eval is unbounded: %v", err)
	}
}

// TestSetHashSeed runs itself in child processes: SetHashSeed must come before any Runtime.
func TestSetHashSeed(t *testing.T) {
	if seed := os.Getenv("GLUA_HASH_SEED_CHILD"); seed != "" {
		SetHashSeed(uint64(len(seed)))
		s, err := MustNew().Load(context.Background(), "order", `
			local t = {} for i = 1, 40 do t["k" .. i] = i end
			local out = {} for k in pairs(t) do out[#out + 1] = k end
			return table.concat(out, ",") .. "|" .. tostring({}):sub(1, 7)`)
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout.WriteString("ORDER " + s.Result().String() + "\n")
		func() {
			defer func() { recover() }()
			SetHashSeed(1)
			os.Stdout.WriteString("no panic\n")
		}()
		return
	}
	run := func(seed string) string {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSetHashSeed$", "-test.count=1")
		cmd.Env = append(os.Environ(), "GLUA_HASH_SEED_CHILD="+seed)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if strings.Contains(string(out), "no panic") {
			t.Error("SetHashSeed after first use did not panic")
		}
		for line := range strings.Lines(string(out)) {
			if order, ok := strings.CutPrefix(line, "ORDER "); ok {
				return order
			}
		}
		t.Fatalf("no order in\n%s", out)
		return ""
	}
	a, b, c := run("x"), run("y"), run("zzzzz")
	if a != b {
		t.Errorf("the same seed gave two orders:\n%s\n%s", a, b)
	}
	if a == c {
		t.Errorf("two seeds gave the same order: %s", a)
	}
}
