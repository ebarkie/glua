package glua

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestErrorChain(t *testing.T) {
	get := Global("get", Fn1E(func(key string) (string, error) {
		if key == "" {
			return "", errNotFound
		}
		return key, nil
	}))
	s := mustLoad(t, MustNew(get), `
		function reraise() local ok, e = pcall(get, "") error(e, 0) end
		function rebuilt() local ok, e = pcall(get, "") error("ctx: " .. e, 0) end
		function direct() get("") end
		function tbl() error({code = 7}) end
		function nested() local function inner() error("deep") end inner() end`)
	ctx := context.Background()
	call := func(name string) error {
		f, err := s.Func0[struct{}](name)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f(ctx)
		return err
	}
	if err := call("reraise"); !errors.Is(err, errNotFound) || err.Error() != "not found" {
		t.Errorf("reraise: %v", err)
	}
	if err := call("rebuilt"); errors.Is(err, errNotFound) || err.Error() != "ctx: not found" {
		t.Errorf("rebuilt: a new string kept the chain: %v", err)
	}
	if err := call("direct"); !errors.Is(err, errNotFound) || err.Error() != "policy.lua:4: not found" {
		t.Errorf("direct: %v", err)
	}
	err := call("tbl")
	le, ok := errors.AsType[*Error](err)
	if !ok || err.Error() != "(error object is a table value)" {
		t.Fatalf("tbl: %v", err)
	}
	if code, _ := le.Value.As[*Table](); code == nil || code.Get(String("code")).String() != "7" {
		t.Errorf("tbl value: %v", le.Value)
	}
	err = call("nested")
	le, _ = errors.AsType[*Error](err)
	if le == nil || le.Error() != "policy.lua:6: deep" {
		t.Fatalf("nested: %v", err)
	}
	var fns []string
	for _, f := range le.Traceback() {
		fns = append(fns, f.Function+"@"+f.Source+":"+itoa(f.Line))
	}
	want := "function 'inner'@policy.lua:6,function 'nested'@policy.lua:6"
	if got := strings.Join(fns, ","); got != want {
		t.Errorf("traceback\n got %s\nwant %s", got, want)
	}
}

func itoa(n int) string { return Int(int64(n)).String() }

func TestErrorUnwrapLimits(t *testing.T) {
	s := mustLoad(t, MustNew(Limits{Steps: 100}), "function f() while true do end end")
	f, _ := s.Func0[struct{}]("f")
	_, err := f(context.Background())
	le, ok := errors.AsType[*Error](err)
	if !ok || !errors.Is(err, ErrSteps) || len(le.Traceback()) == 0 || le.Traceback()[0].Line != 1 {
		t.Errorf("steps: %v", err)
	}
	if strings.Contains(err.Error(), "\n") {
		t.Errorf("Error() is more than one line: %q", err.Error())
	}
}

func TestSyntaxErrorFields(t *testing.T) {
	_, err := MustNew().Load(context.Background(), "cfg.lua", "x = 1\ny = = 2")
	se, ok := errors.AsType[*SyntaxError](err)
	if !ok || se.Source != "cfg.lua" || se.Line != 2 || se.Col == 0 || se.Msg != "unexpected symbol near '='" {
		t.Fatalf("%#v", err)
	}
	if err.Error() != "cfg.lua:2: unexpected symbol near '='" {
		t.Errorf("Error() = %q", err.Error())
	}
}
