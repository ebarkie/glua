package syntax

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// lexAll returns the tokens of src, or the error the lexer raised.
func lexAll(src string) (toks []Token, err error) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*Error)
			if !ok {
				panic(r)
			}
			err = e
		}
	}()
	l := NewLexer("t", src, false)
	for {
		tok := l.Next()
		toks = append(toks, tok)
		if tok.Kind == EOF {
			return toks, nil
		}
	}
}

func hexString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		fmt.Fprintf(&b, "\\x%02x", s[i])
	}
	return b.String()
}

// The expectations were produced by C Lua 5.4.9: the value of `return <literal>`, or the
// error message of load.
func TestLiterals(t *testing.T) {
	tests := []struct{ src, want, err string }{
		{"return 3", "int:3", ""},
		{"return 345", "int:345", ""},
		{"return 0xff", "int:255", ""},
		{"return 0XA", "int:10", ""},
		{"return 3.0", "float:3", ""},
		{"return 3.1416", "float:3.1415999999999999", ""},
		{"return 314.16e-2", "float:3.1415999999999999", ""},
		{"return 0.31416E1", "float:3.1415999999999999", ""},
		{"return 34e1", "float:340", ""},
		{"return 0x0.1E", "float:0.1171875", ""},
		{"return 0xA23p-4", "float:162.1875", ""},
		{"return 0X1.921FB54442D18P+1", "float:3.1415926535897931", ""},
		{"return 9223372036854775807", "int:9223372036854775807", ""},
		{"return 9223372036854775808", "float:9.2233720368547758e+18", ""},
		{"return 0xffffffffffffffff", "int:-1", ""},
		{"return 0x7fffffffffffffff", "int:9223372036854775807", ""},
		{"return 0x10000000000000000", "int:0", ""},
		{"return .5", "float:0.5", ""},
		{"return 5.", "float:5", ""},
		{"return 1e400", "float:+Inf", ""},
		{"return 1e-400", "float:0", ""},
		{"return 0x.8", "float:0.5", ""},
		{"return 0x8.", "float:8", ""},
		{"return 0xp1", "", "t:1: malformed number near '0xp1'"},
		{"return 3x", "", "t:1: malformed number near '3x'"},
		{"return 3e", "", "t:1: malformed number near '3e'"},
		{"return 3e+", "", "t:1: malformed number near '3e+'"},
		{"return 0x", "", "t:1: malformed number near '0x'"},
		{"return 1..2", "", "t:1: malformed number near '1..2'"},
		{"return 3.4.5", "", "t:1: malformed number near '3.4.5'"},
		{"return 0xep1", "float:28", ""},
		{"return 08", "int:8", ""},
		{"return 1e5f", "", "t:1: malformed number near '1e5f'"},
		{"return 0x1P4", "float:16", ""},
		{"return 12abc", "", "t:1: malformed number near '12abc'"},
		{"return \"a\\tb\"", "str:\\x61\\x09\\x62", ""},
		{"return '\\65\\066\\0677'", "str:\\x41\\x42\\x43\\x37", ""},
		{"return \"\\x41\\x7a\"", "str:\\x41\\x7a", ""},
		{"return \"\\u{48}\\u{20AC}\\u{10FFFF}\\u{7FFFFFFF}\\u{D800}\"", "str:\\x48\\xe2\\x82\\xac\\xf4\\x8f\\xbf\\xbf\\xfd\\xbf\\xbf\\xbf\\xbf\\xbf\\xed\\xa0\\x80", ""},
		{"return \"a\\z  \n  b\"", "str:\\x61\\x62", ""},
		{"return \"a\\\nb\"", "str:\\x61\\x0a\\x62", ""},
		{"return [[\nline1\nline2]]", "str:\\x6c\\x69\\x6e\\x65\\x31\\x0a\\x6c\\x69\\x6e\\x65\\x32", ""},
		{"return [==[a]]b]=]c]==]", "str:\\x61\\x5d\\x5d\\x62\\x5d\\x3d\\x5d\\x63", ""},
		{"return \"\\065x\"", "str:\\x41\\x78", ""},
		{"return \"abc", "", "t:1: unfinished string near <eof>"},
		{"return \"abc\ndef\"", "", "t:1: unfinished string near '\"abc'"},
		{"return \"\\q\"", "", "t:1: invalid escape sequence near '\"\\q'"},
		{"return \"\\x4g\"", "", "t:1: hexadecimal digit expected near '\"\\x4g'"},
		{"return \"\\300\"", "", "t:1: decimal escape too large near '\"\\300\"'"},
		{"return \"\\u{110000000}\"", "", "t:1: UTF-8 value too large near '\"\\u{110000000'"},
		{"return \"\\u12\"", "", "t:1: missing '{' near '\"\\u1'"},
		{"return \"\\u{12\"", "", "t:1: missing '}' near '\"\\u{12\"'"},
		{"return \"\\u{}\"", "", "t:1: hexadecimal digit expected near '\"\\u{}'"},
		{"return [==[abc", "", "t:1: unfinished long string (starting at line 1) near <eof>"},
		{"return --[[ abc", "", "t:1: unfinished long comment (starting at line 1) near <eof>"},
		{"return [=a", "", "t:1: invalid long string delimiter near '[='"},
		{"return \"\\x\"", "", "t:1: hexadecimal digit expected near '\"\\x\"'"},
	}
	for _, tt := range tests {
		toks, err := lexAll(tt.src)
		if tt.err != "" {
			if err == nil || err.Error() != tt.err {
				t.Errorf("%q: got error %v, want %q", tt.src, err, tt.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tt.src, err)
			continue
		}
		var got string
		switch tok := toks[1]; tok.Kind {
		case Int:
			got = fmt.Sprintf("int:%d", tok.Int)
		case Float:
			got = "float:" + strings.Replace(fmt.Sprintf("%.17g", tok.Num), "e+0", "e+", 1)
			if math.IsInf(tok.Num, 1) {
				got = "float:+Inf"
			}
		case String:
			got = "str:" + hexString(tok.Str)
		default:
			got = "kind:" + tok.Kind.String()
		}
		if got != tt.want {
			t.Errorf("%q: got %s, want %s", tt.src, got, tt.want)
		}
		if toks[2].Kind != EOF {
			t.Errorf("%q: trailing token %v", tt.src, toks[2].Kind)
		}
	}
}

func TestTokens(t *testing.T) {
	src := "a.b:c(1, ...) .. x // y >> 2 << 3 <= >= == ~= ~ & | :: ; # [ ] { } --comment\n" +
		"and break do else elseif end false for function goto if in local nil not or repeat return then true until while"
	want := []Kind{Name, Dot, Name, Colon, Name, LParen, Int, Comma, Dots, RParen, Concat, Name, DSlash, Name,
		Shr, Int, Shl, Int, Le, Ge, Eq, Ne, Tilde, Amp, Pipe, DColon, Semi, Hash, LBracket, RBracket, LBrace, RBrace,
		And, Break, Do, Else, Elseif, End, False, For, Function, Goto, If, In, Local, Nil, Not, Or, Repeat,
		Return, Then, True, Until, While, EOF}
	toks, err := lexAll(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d", len(toks), len(want))
	}
	for i := range want {
		if toks[i].Kind != want[i] {
			t.Errorf("token %d: got %v, want %v", i, toks[i].Kind, want[i])
		}
	}
	if toks[len(toks)-2].Line != 2 {
		t.Errorf("line of 'while' is %d", toks[len(toks)-2].Line)
	}
}

func TestLinesAndColumns(t *testing.T) {
	// "\n\r" and "\r\n" count as one line each; "\n\n" as two
	toks, err := lexAll("a\n\rb\r\nc\n\nd [[x\ny]] e  --[==[\n\n]==] f")
	if err != nil {
		t.Fatal(err)
	}
	lines := []int{1, 2, 3, 5, 5, 6, 8}
	for i, l := range lines {
		if toks[i].Line != l {
			t.Errorf("token %d (%v %q): line %d, want %d", i, toks[i].Kind, toks[i].Str, toks[i].Line, l)
		}
	}
	if toks[4].Col != 3 || toks[5].Col != 5 {
		t.Errorf("columns %d and %d, want 3 and 5", toks[4].Col, toks[5].Col)
	}
}

func TestOtherBytes(t *testing.T) {
	for src, near := range map[string]string{"@": "'@'", "\x01": "'<\\1>'", "$": "'$'", "\xc3": "'<\\195>'"} {
		toks, err := lexAll(src)
		if err != nil {
			t.Fatal(err)
		}
		if toks[0].Kind != Other || toks[0].Near() != near {
			t.Errorf("%q: %v near %s, want %s", src, toks[0].Kind, toks[0].Near(), near)
		}
	}
}

func TestShebang(t *testing.T) {
	l := NewLexer("t", "#!/usr/bin/lua\nreturn", true)
	if tok := l.Next(); tok.Kind != Return || tok.Line != 2 {
		t.Fatalf("got %v on line %d", tok.Kind, tok.Line)
	}
}

func FuzzLexer(f *testing.F) {
	for _, s := range []string{"return 0x1p4", "\"\\u{48}\"", "[==[x]==]", "--[[c]] a.b", "3x", "\"\\z\n x\""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		toks, err := lexAll(src)
		if err != nil {
			return
		}
		for _, tok := range toks {
			if tok.Line < 1 || tok.Col < 1 {
				t.Fatalf("bad position %d:%d", tok.Line, tok.Col)
			}
		}
	})
}
