package glua

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"sync"
	"unsafe"
)

// The io library of LibIO, for trusted scripts, after C Lua 5.4's liolib.c: io.open, lines,
// close, read, write and type, and the standard files. io.stdout and io.write go to the
// Runtime's Output; io.stderr is the process's; io.stdin reads the process's standard input.
// There is no popen, tmpfile, input or output.

// ioFile is a file as a script sees it: a userdata with methods.
type ioFile struct {
	mu     sync.Mutex // the standard files are shared by every executor
	f      *os.File
	r      *bufio.Reader
	w      *bufio.Writer
	closed bool
	std    byte // 'i', 'o' or 'e' for the standard files; 0 otherwise
}

var (
	ioFileID     uint64
	ioStdin      = &ioFile{std: 'i'}
	ioStdout     = &ioFile{std: 'o'}
	ioStderr     = &ioFile{std: 'e'}
	ioFileMethod = map[string]GoFunc{
		"close": fileClose, "flush": fileFlush, "lines": fileLines, "read": fileRead,
		"seek": fileSeek, "setvbuf": fileSetvbuf, "write": fileWrite,
	}
)

func init() {
	ioFileID = bindType(reflect.TypeFor[ioFile]())
	ti := (*types.Load())[ioFileID]
	ti.builtin, ti.stateful = true, true // set here, before any goroutine reads the plan
	ti.methods = map[string]Value{}
	for name, f := range ioFileMethod {
		ti.methods[name] = goFnValue(&goFunc{name: name, qualname: "file:" + name, f: f})
	}
	mt := NewTable(0, 4)
	mt.Set(String("__name"), String("FILE*"))
	mt.Set(String("__tostring"), goFnValue(&goFunc{name: "__tostring", f: fileToString}))
	mt.Set(String("__close"), goFnValue(&goFunc{name: "__close", f: fileGC}))
	mt.Set(String("__gc"), goFnValue(&goFunc{name: "__gc", f: fileGC}))
	mt.freeze()
	ti.meta = mt
	ioStdin.r = bufio.NewReader(os.Stdin)
}

func fileValue(f *ioFile) Value { return Value{p: unsafe.Pointer(f), n: kindUser<<56 | ioFileID} }

func openIO(g *Table, rt *Runtime) {
	lib := newLib("io", map[string]GoFunc{
		"close": ioClose, "lines": ioLines, "open": ioOpen, "read": ioRead, "type": ioType, "write": ioWrite,
	})
	lib.Set(String("stdin"), fileValue(ioStdin))
	lib.Set(String("stdout"), fileValue(ioStdout))
	lib.Set(String("stderr"), fileValue(ioStderr))
	g.Set(String("io"), tableValue(lib))
}

// toFile returns argument i as an open file.
func toFile(c *Call, i int, fname string) (*ioFile, error) {
	v := c.Value(i)
	if !isFile(v) {
		return nil, c.typeArgError(i, fname, "FILE*")
	}
	f := (*ioFile)(v.p)
	if f.closed {
		return nil, c.libErrorf("attempt to use a closed file")
	}
	return f, nil
}

// fileResult is C Lua's luaL_fileresult: true, or nil, a message and an error number.
func fileResult(c *Call, err error, name string) error {
	if err == nil {
		return c.ReturnValues(Bool(true))
	}
	msg, code := err.Error(), int64(0)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		msg, code = "No such file or directory", 2
	case errors.Is(err, fs.ErrPermission):
		msg, code = "Permission denied", 13
	case errors.Is(err, fs.ErrExist):
		msg, code = "File exists", 17
	}
	if name != "" {
		msg = name + ": " + msg
	}
	return c.ReturnValues(Value{}, String(msg), Int(code))
}

func ioOpen(c *Call) error {
	name, err := c.checkString(0, "open")
	if err != nil {
		return err
	}
	mode, err := c.optString(1, "open", "r")
	if err != nil {
		return err
	}
	m := strings.TrimSuffix(mode, "b")
	flags := 0
	switch m {
	case "r":
		flags = os.O_RDONLY
	case "w":
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case "a":
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	case "r+":
		flags = os.O_RDWR
	case "w+":
		flags = os.O_RDWR | os.O_CREATE | os.O_TRUNC
	case "a+":
		flags = os.O_RDWR | os.O_CREATE | os.O_APPEND
	default:
		return c.argError(2, "open", "invalid mode")
	}
	osf, err := os.OpenFile(name, flags, 0o666)
	if err != nil {
		return fileResult(c, err, name)
	}
	f := &ioFile{f: osf, r: bufio.NewReader(osf), w: bufio.NewWriter(osf)}
	if err := c.ex.charge(4096 * 2); err != nil {
		osf.Close()
		return err
	}
	return c.ReturnValues(fileValue(f))
}

func ioType(c *Call) error {
	if err := c.checkAny(0, "type"); err != nil {
		return err
	}
	v := c.Value(0)
	if !isFile(v) {
		return c.ReturnValues(Value{})
	}
	if (*ioFile)(v.p).closed {
		return c.ReturnValues(String("closed file"))
	}
	return c.ReturnValues(String("file"))
}

func ioClose(c *Call) error {
	if c.Value(0).IsNil() {
		return closeFile(c, ioStdout)
	}
	f, err := toFile(c, 0, "close")
	if err != nil {
		return err
	}
	return closeFile(c, f)
}

func fileClose(c *Call) error {
	f, err := toFile(c, 0, "close")
	if err != nil {
		return err
	}
	return closeFile(c, f)
}

func closeFile(c *Call, f *ioFile) error {
	if f.std != 0 {
		return c.ReturnValues(Value{}, String("cannot close standard file"))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	err := f.w.Flush()
	if cerr := f.f.Close(); err == nil {
		err = cerr
	}
	f.closed = true
	return fileResult(c, err, "")
}

// fileGC closes a file that a script let go of, or that a <close> variable held.
func fileGC(c *Call) error {
	v := c.Value(0)
	if !isFile(v) { // as C Lua's luaL_checkudata: the metamethods can be called with anything
		return c.typeArgError(0, "__gc", "FILE*")
	}
	if f := (*ioFile)(v.p); !f.closed && f.std == 0 {
		f.w.Flush()
		f.f.Close()
		f.closed = true
	}
	return nil
}

func fileToString(c *Call) error {
	if !isFile(c.Value(0)) {
		return c.typeArgError(0, "__tostring", "FILE*")
	}
	f := (*ioFile)(c.Value(0).p)
	if f.closed {
		return c.ReturnValues(String("file (closed)"))
	}
	return c.ReturnValues(String("file (" + hidePointer(c.Value(0).p) + ")"))
}

func fileFlush(c *Call) error {
	f, err := toFile(c, 0, "flush")
	if err != nil {
		return err
	}
	return fileResult(c, f.flush(c), "")
}

func (f *ioFile) flush(c *Call) error {
	if f.std != 0 {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.w.Flush()
}

func fileSetvbuf(c *Call) error {
	if _, err := toFile(c, 0, "setvbuf"); err != nil {
		return err
	}
	return c.ReturnValues(Bool(true))
}

func fileSeek(c *Call) error {
	f, err := toFile(c, 0, "seek")
	if err != nil {
		return err
	}
	whence, err := c.optString(1, "seek", "cur")
	if err != nil {
		return err
	}
	off, err := c.optInt(2, "seek", 0)
	if err != nil {
		return err
	}
	w := map[string]int{"set": io.SeekStart, "cur": io.SeekCurrent, "end": io.SeekEnd}
	wh, ok := w[whence]
	if !ok {
		return c.argError(2, "seek", "invalid option '"+whence+"'")
	}
	if f.std != 0 {
		return fileResult(c, errors.New("Illegal seek"), "")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.w.Flush(); err != nil {
		return fileResult(c, err, "")
	}
	if wh == io.SeekCurrent {
		off -= int64(f.r.Buffered()) // the reader is ahead of the script
	}
	pos, err := f.f.Seek(off, wh)
	if err != nil {
		return fileResult(c, err, "")
	}
	f.r.Reset(f.f)
	return c.ReturnValues(Int(pos))
}

func ioWrite(c *Call) error { return writeFile(c, ioStdout, 0) }

func fileWrite(c *Call) error {
	f, err := toFile(c, 0, "write")
	if err != nil {
		return err
	}
	return writeFile(c, f, 1)
}

// writeFile writes arguments from index first on, strings or numbers, and returns the file.
func writeFile(c *Call, f *ioFile, first int) error {
	var b strings.Builder
	for i := first; i < c.Len(); i++ {
		s, err := argString(c, i, c.Value(i))
		if err != nil {
			return err
		}
		b.WriteString(s)
	}
	if err := c.ex.spend(b.Len() / 64); err != nil {
		return err
	}
	var err error
	switch f.std {
	case 'o':
		err = c.ex.write(b.String())
	case 'e':
		_, err = os.Stderr.WriteString(b.String())
	case 'i':
		err = errors.New("Bad file descriptor")
	default:
		f.mu.Lock()
		_, err = f.w.WriteString(b.String())
		f.mu.Unlock()
	}
	if err != nil {
		return fileResult(c, err, "")
	}
	if first == 0 {
		return c.ReturnValues(fileValue(f))
	}
	return c.ReturnValues(c.Value(0))
}

func ioRead(c *Call) error { return readFile(c, ioStdin, 0) }

func fileRead(c *Call) error {
	f, err := toFile(c, 0, "read")
	if err != nil {
		return err
	}
	return readFile(c, f, 1)
}

// readFile reads by the formats from argument first on, "l" when there are none.
func readFile(c *Call, f *ioFile, first int) error {
	formats := make([]Value, 0, 4)
	for i := first; i < c.Len(); i++ {
		formats = append(formats, c.Value(i))
	}
	if len(formats) == 0 {
		formats = append(formats, String("l"))
	}
	res, err := readFormats(c, f, formats, first)
	if err != nil {
		return err
	}
	return c.ReturnValues(res...)
}

func readFormats(c *Call, f *ioFile, formats []Value, first int) ([]Value, error) {
	if f.std == 'o' || f.std == 'e' {
		return []Value{{}, String("Bad file descriptor"), Int(9)}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.w != nil {
		if err := f.w.Flush(); err != nil {
			return []Value{{}, String(err.Error()), Int(0)}, nil
		}
	}
	var res []Value
	for k, fv := range formats {
		var v Value
		var ok bool
		if n, isInt := fv.Int(); isInt && fv.isNumber() {
			v, ok = readCount(f.r, n)
		} else {
			s, isStr := fv.Str()
			if !isStr {
				return nil, c.argError(first+k+1, "read", "invalid format")
			}
			s = strings.TrimPrefix(s, "*")
			if s == "" {
				return nil, c.argError(first+k+1, "read", "invalid format")
			}
			switch s[0] {
			case 'n':
				v, ok = readNumber(f.r)
			case 'l':
				v, ok = readLine(f.r, false)
			case 'L':
				v, ok = readLine(f.r, true)
			case 'a':
				b, _ := io.ReadAll(f.r)
				v, ok = String(string(b)), true
			default:
				return nil, c.argError(first+k+1, "read", "invalid format")
			}
		}
		if v.isString() {
			if err := c.ex.produce(int(v.n & lenMask)); err != nil {
				return nil, err
			}
		}
		if !ok {
			res = append(res, Value{}) // fail: nil, and nothing after it
			break
		}
		res = append(res, v)
	}
	return res, nil
}

func readCount(r *bufio.Reader, n int64) (Value, bool) {
	if n == 0 { // a test for the end of file
		_, err := r.Peek(1)
		return String(""), err == nil
	}
	if n > maxAlloc {
		n = maxAlloc
	}
	buf := make([]byte, n)
	got, _ := io.ReadFull(r, buf)
	if got == 0 {
		return Value{}, false
	}
	return String(string(buf[:got])), true
}

func readLine(r *bufio.Reader, keep bool) (Value, bool) {
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return Value{}, false
	}
	if !keep {
		line = strings.TrimSuffix(line, "\n")
	}
	return String(line), true
}

// readNumber reads a numeral as C Lua's l_getn does: optional spaces, then at most 200
// characters that can make a number.
func readNumber(r *bufio.Reader) (Value, bool) {
	for {
		b, err := r.ReadByte()
		if err != nil {
			return Value{}, false
		}
		if !strings.ContainsRune(" \t\n\r\f\v", rune(b)) {
			r.UnreadByte()
			break
		}
	}
	var sb strings.Builder
	for sb.Len() < 200 {
		b, err := r.ReadByte()
		if err != nil {
			break
		}
		if !strings.ContainsRune("0123456789abcdefABCDEFxX.+-pP", rune(b)) {
			r.UnreadByte()
			break
		}
		sb.WriteByte(b)
	}
	n, ok := strToNumber(sb.String())
	return n, ok
}

func ioLines(c *Call) error {
	if c.Value(0).IsNil() {
		return linesIter(c, ioStdin, 1, false)
	}
	name, err := c.checkString(0, "lines")
	if err != nil {
		return err
	}
	osf, err := os.Open(name)
	if err != nil {
		msg := err.Error()
		if errors.Is(err, fs.ErrNotExist) {
			msg = "No such file or directory"
		}
		return c.libErrorf("%s: %s", name, msg)
	}
	f := &ioFile{f: osf, r: bufio.NewReader(osf), w: bufio.NewWriter(osf)}
	if err := linesIter(c, f, 1, true); err != nil {
		return err
	}
	// as C Lua 5.4: the iterator, nil, nil and the file, which a generic for closes
	c.res = append(c.res, Value{}, Value{}, fileValue(f))
	return nil
}

func fileLines(c *Call) error {
	f, err := toFile(c, 0, "lines")
	if err != nil {
		return err
	}
	return linesIter(c, f, 1, false)
}

// linesIter returns an iterator that reads f by the formats from argument first on, and
// closes it at the end when it owns it.
func linesIter(c *Call, f *ioFile, first int, owns bool) error {
	var formats []Value
	for i := first; i < c.Len(); i++ {
		formats = append(formats, c.Value(i))
	}
	if len(formats) > 250 {
		return c.argError(first+251, "lines", "too many arguments")
	}
	if len(formats) == 0 {
		formats = []Value{String("l")}
	}
	it := &goFunc{name: "lines iterator", stateful: true}
	it.f = func(c *Call) error {
		if f.closed {
			return c.libErrorf("file is already closed")
		}
		res, err := readFormats(c, f, formats, 0)
		if err != nil {
			return err
		}
		if len(res) == 0 || res[0].IsNil() {
			if len(res) > 1 && res[1].isString() {
				return c.libErrorf("%s", res[1].str())
			}
			if owns {
				f.f.Close()
				f.closed = true
			}
			return c.ReturnValues(Value{})
		}
		return c.ReturnValues(res...)
	}
	return c.ReturnValues(goFnValue(it))
}

// isStdFile reports whether v is one of the standard files, which are shared by design.
func isStdFile(v Value) bool {
	return isFile(v) && (*ioFile)(v.p).std != 0
}

// isFile reports whether v is a file. Check it before reading v.p as an *ioFile.
func isFile(v Value) bool { return v.isObj(kindUser) && v.n&typeIDMask == ioFileID }
