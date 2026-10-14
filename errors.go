package glua

import (
	"context"
	"errors"
	"fmt"
	"unsafe"
)

var (
	ErrSteps       = errors.New("glua: step limit exceeded")
	ErrAlloc       = errors.New("glua: allocation limit exceeded")
	ErrHostDepth   = errors.New("glua: too many nested calls between Go and Lua")
	ErrUndefined   = errors.New("glua: name is not defined")
	ErrNotFunction = errors.New("glua: value is not a function")
)

// errDone tells the driver that a general RETURN ended the run's goEntry frame.
var errDone = errors.New("glua: done")

// Error is a runtime error raised by a script or by a host function it called.
type Error struct {
	Value    Value // what was raised: for VM errors a message with its position; a string for Go errors
	where    string
	cause    error // the Go error, a limit error or the context's cause
	terminal bool  // limits, cancellation, host depth: pcall cannot catch it
	trace    []traceEntry
}

// traceEntry is one frame of a traceback, kept as a prototype and pc until it is read. A nil
// prototype marks Go code between two entries from Go.
type traceEntry struct {
	p  *Proto
	pc int
}

// Error returns one line, such as "policy.lua:4: boom".
func (e *Error) Error() string {
	var msg string
	switch {
	case e.Value.isString() || e.Value.isNumber():
		msg = e.Value.String()
	case e.Value.IsNil():
		msg = "nil"
	default:
		msg = "(error object is a " + typeName(e.Value) + " value)"
	}
	if e.where != "" {
		return e.where + ": " + msg
	}
	return msg
}

// Frame is one level of a traceback.
type Frame struct {
	Source   string // "" for Go code
	Line     int
	Function string // "main chunk", "function 'f'", "function <policy.lua:12>", or "Go function"
}

// Traceback returns the frames the error passed through, innermost first. Lines are looked
// up only now, so scripts that use error for control flow do not pay for them.
func (e *Error) Traceback() []Frame {
	frames := make([]Frame, 0, len(e.trace))
	for _, t := range e.trace {
		if t.p == nil {
			frames = append(frames, Frame{Function: "Go function"})
			continue
		}
		f := Frame{Source: t.p.source, Line: t.p.lineAt(t.pc - 1)}
		switch name := t.p.name; {
		case name == "main chunk":
			f.Function = name
		case name == "" || name == "<anonymous>":
			f.Function = fmt.Sprintf("function <%s:%d>", t.p.source, t.p.lineAt(0))
		default:
			f.Function = "function '" + name + "'"
		}
		frames = append(frames, f)
	}
	return frames
}

// captureTrace adds the frames of the current run, above nf0, to an error that leaves it
// uncaught.
func (ex *executor) captureTrace(le *Error, nf0 int) {
	if len(le.trace) > 0 {
		le.trace = append(le.trace, traceEntry{}) // Go code between the inner run and this one
	}
	st := &ex.st
	cl, base, pc := st.cl, st.base, st.pc
	for f := st.nf - 1; f > nf0 && cl != nil; f-- {
		le.trace = append(le.trace, traceEntry{p: cl.p, pc: pc})
		fr := ex.pframes[f]
		base -= int(fr >> 32 & 0xffffff)
		pc = int(uint32(fr))
		if v := ex.stack[base-1]; v.isObj(kindLuaFn) {
			cl = (*Closure)(v.p)
		} else {
			cl = nil
		}
	}
	if cl != nil && st.nf > nf0 {
		le.trace = append(le.trace, traceEntry{p: cl.p, pc: pc})
	}
}

// Unwrap returns the Go error behind e, if any.
func (e *Error) Unwrap() error { return e.cause }

// where reports the current position as source:line, from the instruction at pc-1.
func (ex *executor) where() string {
	if ex.st.cl == nil {
		return ""
	}
	p := ex.st.cl.p
	return fmt.Sprintf("%s:%d", p.source, p.lineAt(ex.st.pc-1))
}

// errorf raises an ordinary, catchable runtime error at the current position. As in C Lua,
// the position is part of the error value, which pcall returns.
func (ex *executor) errorf(format string, args ...any) error {
	return &Error{Value: String(ex.where() + ": " + fmt.Sprintf(format, args...))}
}

// typeError raises "attempt to <what> a <type> value", naming where the value came from
// when it is in register reg of the current frame (reg < 0: not in a register).
func (ex *executor) typeError(what string, v Value, reg int) error {
	info := ""
	if reg >= 0 {
		if vi := ex.st.cl.p.varInfo(ex.st.pc-1, reg); vi != "" {
			info = " (" + vi + ")"
		}
	}
	return ex.errorf("attempt to %s a %s value%s", what, typeName(v), info)
}

// terminal raises an error that pcall cannot catch: limits, context cancellation, host depth.
func (ex *executor) terminal(cause error) error {
	return &Error{Value: String(cause.Error()), where: ex.where(), cause: cause, terminal: true}
}

// hostError turns an error returned by a host function into a Lua error. The Go error is
// kept on the side, so errors.Is and errors.As still find it.
func (ex *executor) hostError(err error) error {
	var le *Error
	if errors.As(err, &le) {
		return err
	}
	msg := err.Error()
	if ex.goErrs == nil {
		ex.goErrs = map[unsafe.Pointer]error{}
	}
	if msg != "" {
		ex.goErrs[unsafe.Pointer(unsafe.StringData(msg))] = err // for error(e) raising it again
	}
	return &Error{Value: String(msg), where: ex.where(), cause: err}
}

func ctxError(ctx context.Context) error {
	return context.Cause(ctx)
}
