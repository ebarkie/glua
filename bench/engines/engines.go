package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"

	"github.com/Liam0205/wangshu"
	golib "github.com/arnodel/golua/lib"
	golua "github.com/arnodel/golua/runtime"
	lua "github.com/folbricht/glua"
	gopher "github.com/yuin/gopher-lua"
)

func gluaEngine() engine {
	return engine{name: "glua", version: "(checked loop)", load: func(name, src string) (func() (string, error), error) {
		rt := lua.MustNew(lua.LibSafe|lua.LibCoroutine, lua.Limits{Steps: lua.NoLimit, AllocBytes: lua.NoLimit,
			InitSteps: lua.NoLimit, InitAllocBytes: lua.NoLimit})
		s, err := rt.Load(context.Background(), name, src)
		if err != nil {
			return nil, err
		}
		work, err := s.Func0[lua.Value]("")
		if err != nil {
			return nil, err
		}
		return func() (string, error) {
			v, err := work(context.Background())
			return v.String(), err
		}, nil
	}}
}

func gopherEngine() engine {
	return engine{name: "GopherLua", version: "v1.1.2", load: func(name, src string) (func() (string, error), error) {
		// the default data stack does not grow, and some scripts need more than it holds
		L := gopher.NewState(gopher.Options{RegistryMaxSize: 1 << 20})
		fn, err := L.LoadString(src)
		if err != nil {
			return nil, err
		}
		if err := L.CallByParam(gopher.P{Fn: fn, NRet: 1, Protect: true}); err != nil {
			return nil, err
		}
		work := L.Get(-1)
		L.Pop(1)
		return func() (string, error) {
			if err := L.CallByParam(gopher.P{Fn: work, NRet: 1, Protect: true}); err != nil {
				return "", err
			}
			v := L.Get(-1)
			L.Pop(1)
			return v.String(), nil
		}, nil
	}}
}

func goluaEngine() engine {
	return engine{name: "golua", version: "v0.4.0", load: func(name, src string) (func() (string, error), error) {
		r := golua.New(io.Discard)
		golib.LoadAll(r)
		chunk, err := r.CompileAndLoadLuaChunk(name, []byte(src), golua.TableValue(r.GlobalEnv()))
		if err != nil {
			return nil, err
		}
		work, err := golua.Call1(r.MainThread(), golua.FunctionValue(chunk))
		if err != nil {
			return nil, err
		}
		return func() (string, error) {
			v, err := golua.Call1(r.MainThread(), work)
			if err != nil {
				return "", err
			}
			s, _ := v.ToString()
			return s, nil
		}, nil
	}}
}

func wangshuEngine() engine {
	return engine{name: "wangshu", version: "v0.2.0", load: func(name, src string) (func() (string, error), error) {
		prog, err := wangshu.Compile([]byte(src), name)
		if err != nil {
			return nil, err
		}
		st := wangshu.NewState(wangshu.Options{})
		res, err := prog.Run(st)
		if err != nil {
			return nil, err
		}
		if len(res) == 0 || !res[0].IsFunction() {
			return nil, errors.New("the chunk did not return a function")
		}
		work := res[0]
		return func() (string, error) {
			res, err := st.Call(work)
			if err != nil {
				return "", err
			}
			if len(res) == 0 {
				return "nil", nil
			}
			switch v := res[0]; {
			case v.IsNumber():
				f := v.Number()
				if f == math.Trunc(f) && math.Abs(f) < 1e15 {
					return strconv.FormatInt(int64(f), 10), nil
				}
				return strconv.FormatFloat(f, 'g', 14, 64), nil
			case v.IsString():
				return v.Str(), nil
			default:
				return fmt.Sprint(v), nil
			}
		}, nil
	}}
}
