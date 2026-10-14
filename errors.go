package glua

import (
	"context"
	"errors"
	"fmt"
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
	Value Value // what was raised; a string for Go errors
	where string
	cause error // the Go error, a limit error or the context's cause
}

// Error returns one line, such as "policy.lua:4: boom".
func (e *Error) Error() string {
	msg := e.Value.String()
	if e.where != "" {
		return e.where + ": " + msg
	}
	return msg
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
	return &Error{Value: String(cause.Error()), where: ex.where(), cause: cause}
}

// hostError turns an error returned by a host function into a Lua error. The Go error is
// kept on the side, so errors.Is and errors.As still find it.
func (ex *executor) hostError(err error) error {
	var le *Error
	if errors.As(err, &le) {
		return err
	}
	return &Error{Value: String(err.Error()), where: ex.where(), cause: err}
}

func ctxError(ctx context.Context) error {
	return context.Cause(ctx)
}
