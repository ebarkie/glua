package glua

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIOLibrary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")
	var out bytes.Buffer
	rt := MustNew(LibSafe|LibIO, Output(&out), Global("path", path))
	s := mustLoad(t, rt, `
		function run()
			local r = {}
			local f = assert(io.open(path, "w"))
			r[#r+1] = io.type(f)
			f:write("line one\n", 42, " 3.5 0x10\n", "tail")
			f:close()
			r[#r+1] = io.type(f)
			r[#r+1] = select(2, pcall(f.read, f))
			f = assert(io.open(path))
			r[#r+1] = f:read("l")
			r[#r+1] = f:read("n")
			r[#r+1] = f:read("n")
			r[#r+1] = f:read("n")
			r[#r+1] = f:read("L")
			r[#r+1] = f:read(2)
			r[#r+1] = f:read("a")
			r[#r+1] = tostring(f:read("l"))
			r[#r+1] = f:seek("set", 5)
			r[#r+1] = f:read(3)
			r[#r+1] = f:seek("end")
			f:close()
			local n = 0
			for line in io.lines(path) do n = n + 1 end
			r[#r+1] = n
			for a, b in io.lines(path, 4, "l") do r[#r+1] = a .. "|" .. b break end
			local ok, msg, code = io.open(path .. ".missing")
			r[#r+1] = tostring(ok) .. " " .. msg:sub(-25) .. " " .. code
			r[#r+1] = select(2, pcall(io.open, path, "q"))
			r[#r+1] = select(2, io.stdout:close())
			io.write("to output ", 1, "\n")
			io.stdout:write("again\n")
			return table.concat(r, ",")
		end`)
	run, err := s.Func0[string]("run")
	if err != nil {
		t.Fatal(err)
	}
	got, err := run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := "file,closed file,attempt to use a closed file,line one,42,3.5,16,\n,ta,il,nil,5,one,25,3,line| one," +
		"nil No such file or directory 2,bad argument #2 to 'io.open' (invalid mode),cannot close standard file"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
	if out.String() != "to output 1\nagain\n" {
		t.Errorf("output %q", out.String())
	}
	if b, _ := os.ReadFile(path); string(b) != "line one\n42 3.5 0x10\ntail" {
		t.Errorf("file %q", b)
	}
	// a file cannot be part of a shared script's state
	_, err = rt.Load(context.Background(), "keep.lua", "f = io.open(path)")
	if err == nil || !strings.Contains(err.Error(), "global 'f' is a FILE*, which has hidden state") {
		t.Errorf("a file in the globals: %v", err)
	}
	if _, err := New(Hardened(), LibSafe|LibIO); err == nil {
		t.Error("the hardened profile took LibIO")
	}
}

func TestOSLibrary(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	rt := MustNew(LibSafe|LibOS, Clock(clock))
	now = now.Add(1500 * time.Millisecond) // after New: os.clock counts from there
	s := mustLoad(t, rt, `function f() return os.time(), os.clock(), os.date("!%Y-%m-%d %H:%M:%S"), os.getenv, os.exit, os.remove end`)
	f, _ := s.Func0[Value]("f")
	_ = f
	res, err := s.CallN(context.Background(), nil, s.Globals().Get(String("f")))
	if err != nil {
		t.Fatal(err)
	}
	if got := joinValues(res); got != "1792051201\t1.5\t2026-10-15 08:00:01\tnil\tnil\tnil" {
		t.Errorf("got %q", got)
	}
	if _, err := New(Hardened(), LibSafe|LibOS); err == nil {
		t.Error("the hardened profile took LibOS")
	}
}
