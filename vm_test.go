package glua

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestVM(t *testing.T) {
	ctx := context.Background()
	hostCalls := 0
	rt := MustNew(
		Limits{Steps: 10_000},
		Global("tick", Fn0(func() int64 { hostCalls++; return int64(hostCalls) })),
		SourceModule("counter", "local M = {n = 0} function M.inc() M.n = M.n + 1 return M.n end return M"),
	)
	s := mustLoad(t, rt, `
		started = tick()
		local c = require("counter")
		n = 0
		function bump() n = n + 1 return n, c.inc() end
		function keep(r) last = r end
		function lastcost() return last.cost end
		function spin() while true do end end
		verbose = false
		function mode() return verbose end`)
	if hostCalls != 1 {
		t.Fatalf("load made %d host calls", hostCalls)
	}
	vm, err := s.NewVM(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if hostCalls != 2 {
		t.Errorf("NewVM did not run the top level again: %d host calls", hostCalls)
	}
	bump, err := vm.Func0[Pair[int64, int64]]("bump")
	if err != nil {
		t.Fatal(err)
	}
	for want := int64(1); want <= 3; want++ {
		if p, err := bump(ctx); err != nil || p.First != want || p.Second != want {
			t.Errorf("bump: %+v %v", p, err)
		}
	}
	// a second VM has state of its own, modules included
	vm2, _ := s.NewVM(ctx)
	bump2, _ := vm2.Func0[Pair[int64, int64]]("bump")
	if p, _ := bump2(ctx); p.First != 1 || p.Second != 1 {
		t.Errorf("second VM: %+v", p)
	}
	// the shared script stays frozen
	sb, _ := s.Func0[int64]("bump")
	if _, err := sb(ctx); err == nil {
		t.Error("the script's bump changed frozen state")
	}
	if n, err := vm.Get[int]("n"); err != nil || n != 3 {
		t.Errorf("Get: %v %v", n, err)
	}
	if err := vm.Set("verbose", true); err != nil {
		t.Fatal(err)
	}
	mode, _ := vm.Func0[bool]("mode")
	if v, _ := mode(ctx); !v {
		t.Error("Set did not reach the script")
	}
	if err := vm.Set("tick", 1); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Errorf("Set of a reserved name: %v", err)
	}
	if err := vm.Set("cfg", map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if err := vm.Set("cfg.b", 2); err != nil {
		t.Fatal(err)
	}
	if b, err := vm.Get[int]("cfg.b"); err != nil || b != 2 {
		t.Errorf("dotted Set: %v %v", b, err)
	}
	// a struct by value is copied to the heap, so the VM can keep it
	keep, _ := vm.Func1[scriptReq, struct{}]("keep")
	if _, err := keep(ctx, scriptReq{Cost: 5}); err != nil {
		t.Fatal(err)
	}
	keep(ctx, scriptReq{Cost: 6})
	lc, _ := vm.Func0[int64]("lastcost")
	if c, err := lc(ctx); err != nil || c != 6 {
		t.Errorf("kept struct: %v %v", c, err)
	}
	// a terminal error leaves the globals as they are, on a fresh executor
	spin, _ := vm.Func0[struct{}]("spin")
	if _, err := spin(ctx); !errors.Is(err, ErrSteps) {
		t.Errorf("spin: %v", err)
	}
	if p, err := bump(ctx); err != nil || p.First != 4 {
		t.Errorf("after a terminal error: %+v %v", p, err)
	}
	// Call and CallN
	if v, err := vm.Call(ctx, vm.Globals().Get(String("bump"))); err != nil || v.String() != "5" {
		t.Errorf("Call: %v %v", v, err)
	}
	res, err := vm.CallN(ctx, nil, vm.Globals().Get(String("bump")))
	if err != nil || joinValues(res) != "6\t6" {
		t.Errorf("CallN: %v %v", joinValues(res), err)
	}
}

func TestVMConcurrentUse(t *testing.T) {
	var vm *VM
	reenter := GoFunc(func(c *Call) error {
		_, err := vm.Call(c.Context(), vm.Globals().Get(String("noop")))
		return err
	})
	s := mustLoad(t, MustNew(Global("reenter", reenter)), "function noop() end function f() reenter() end")
	vm, _ = s.NewVM(context.Background())
	f, _ := vm.Func0[struct{}]("f")
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "concurrent use") {
			t.Errorf("recovered %v", r)
		}
	}()
	f(context.Background())
	t.Error("re-entering a running VM did not panic")
}

func TestVMNewVMFails(t *testing.T) {
	calls := 0
	rt := MustNew(Global("once", Do0E(func() error {
		calls++
		if calls > 1 {
			return errors.New("only once")
		}
		return nil
	})))
	s := mustLoad(t, rt, "once()")
	if _, err := s.NewVM(context.Background()); err == nil || !strings.Contains(err.Error(), "only once") {
		t.Errorf("NewVM: %v", err)
	}
}
