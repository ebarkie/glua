-- The string library.
--== basics
local s = "Hello, World"
return #s, s:len(), s:upper(), s:lower(), s:reverse(), s:rep(2, "-"), ("x"):rep(0), ("ab"):rep(3)
--== sub and byte with negative indices
local s = "abcdef"
return s:sub(2, 4), s:sub(-3), s:sub(-100, 2), s:sub(4, 2), s:sub(0), s:sub(3, -2), s:byte(), s:byte(-1), s:byte(2, 4)
--== char
return string.char(72, 105), string.char(), #string.char(0, 255)
--== find plain and patterns
local s = "the quick brown fox"
return s:find("quick"), s:find("o", 13), s:find("q.i"), s:find("x", -1), s:find("(%a+) (%a+)$"), s:find("[aeiou]+", 1, true), s:find("", 50), s:find("", 20)
--== match with captures
return ("key = value"):match("(%w+)%s*=%s*(%w+)"), ("2024-10-14"):match("(%d+)-(%d+)-(%d+)"), ("  trim  "):match("^%s*(.-)%s*$"), ("abc"):match("()b()")
--== character classes
local s = "aZ9 _!\t"
local r = {}
for _, cl in next, {"%a", "%d", "%l", "%u", "%s", "%w", "%p", "%c", "%x", "%A", "%S"} do
  r[#r + 1] = select(2, s:gsub(cl, ""))
end
return r[1], r[2], r[3], r[4], r[5], r[6], r[7], r[8], r[9], r[10], r[11]
--== sets and ranges
return ("hello world"):gsub("[lo]", "X"), ("a-b_c"):gsub("[%-_]", "."), ("abc123"):gsub("[^%d]", ""), ("x]y"):match("[]]"), ("A1b2"):gsub("[a-zA-Z]", "*")
--== quantifiers
return ("aaa"):match("a-"), ("aaa"):match("a-$"), ("<<a>><<b>>"):match("<<(.-)>>"), ("<<a>><<b>>"):match("<<(.*)>>"), ("ac"):match("ab?c"), ("abbbc"):match("ab+c")
--== balance and frontier
return ("f(a(b)c)d"):match("%b()"), ("THE (quick) fox"):find("%f[%a]%a+"), ("THE (quick) fox"):gsub("%f[%w]%w+", "W")
--== back references
return ("hello there"):match("(l)%1"), ([[say "hi" and 'bye']]):match("(['\"])(.-)%1")
--== gsub replacements
local t = {name = "Lua", ver = 5.4}
return ("$name $ver $none"):gsub("%$(%w+)", t),
  ("abc"):gsub("%w", "%0%0"),
  ("hello world"):gsub("(%w+) (%w+)", "%2 %1"),
  ("abc"):gsub("", "-"),
  ("abc"):gsub("b*", "X"),
  ("one two"):gsub("%w+", function(w) return w:upper() end, 1),
  ("x y"):gsub("%w", function() return false end)
--== gmatch
local words = {}
for w in ("the quick brown fox"):gmatch("%a+") do words[#words + 1] = w end
local pairs_ = {}
for k, v in ("a=1, b=2"):gmatch("(%w+)=(%w+)") do pairs_[#pairs_ + 1] = k .. v end
local e = 0
for _ in ("abc"):gmatch("") do e = e + 1 end
return #words, words[4], pairs_[1], pairs_[2], e
--== anchored patterns
return ("hello"):find("^h"), ("hello"):find("^e"), ("hello"):gsub("^h", "J"), ("aaa"):gsub("^a", "b")
--== format integers
return string.format("%d|%5d|%-5d|%05d|%+d|% d|%x|%X|%#x|%o|%i", 42, 42, 42, 42, 42, 42, 255, 255, 255, 8, -7)
--== format floats
return string.format("%f|%.2f|%10.3f|%e|%.3E|%g|%g|%g|%G", 3.14159, 3.14159, 3.14159, 12345.678, 12345.678, 0.0001, 1e20, 100, 1e-10)
--== format special floats
return string.format("%f|%e|%g|%.1f|%5.1f", 1/0, -1/0, 0/0 ~= 0/0 and 1/0 or 0, -0.0, 2.25)
--== format strings and chars
return string.format("[%s][%10s][%-10s][%.3s][%c%c]", "abc", "abc", "abc", "abcdef", 72, 105), string.format("%s %s %s", 1, 1.5, true)
--== format q
return string.format("%q", 'he said "hi"\n\0end'), string.format("%q", 42), string.format("%q", 1.5), string.format("%q", 1/0), string.format("%q", -9223372036854775807 - 1)
--== format a
return string.format("%a", 1.0), string.format("%a", 0.5), string.format("%.2a", 1.0 / 3)
--== format errors
local function e(f, ...) local ok, msg = pcall(string.format, f, ...) return msg end
return e("%d", 1.5), e("%d", "x"), e("%z", 1), e("%123d", 1), e("%"), e("%q", {}), e("%s"), e("%#d", 1)
--== string methods through the metatable
local s = "abc"
return s:upper(), ("%d"):format(5), getmetatable("").__index == string, #getmetatable("").__index.format("%5s", "x")
--== pattern errors
local function e(p) local ok, msg = pcall(string.find, "abc", p) return msg end
return e("[a"), e("%"), e("(a"), e("a)"), e("%b"), e("%f"), e("%1")
--== rep and long results
local s = ("ab"):rep(1000)
return #s, s:sub(-4), #("x"):rep(10, ", ")
--== tostring of numbers in string functions
return ("x"):rep(3.0), (12):len(), (1.5):rep(2)
--== format specification errors
local aux = string.rep('0', 600)
local r = {}
for _, f in ipairs{"%100.3d", "%1"..aux..".3d", "%1.100d", "%"..aux.."d", "%010c", "%.10c", "%0.34s", "%#i", "%3.1p", "%0.s", "%10q", "%F", "%05d", "%-5.2s|", "%+.3f"} do
  r[#r + 1] = select(2, pcall(string.format, f, 10))
end
return table.unpack(r)
--== %q writes control characters as decimal escapes
return string.format("%q", "a\rb\r1\0\0012\t\127x\200\n\"\\"), string.format("%q", "\r"), string.format("%q", "\r9")
--== %a as glibc writes it: rounding carries, subnormals, # and the 0 flag
return string.format("%.1a|%a|%a|%.0a|%#.0a|%010A|%-12a|%+a|% a|%.20a", 63, 1e-310, 5e-324, 1.5, -1, 0, 1, 2.5, 0.1, 1/3),
  string.format("%.3a|%.1a|%.0a|%.0a|%.0a|%a|%a|%.13a|%.2a", 1/3, 1.96875, 1.5, 2.5, 3.5, -0.0, 2^-1022, 2^-1074, 0x1.ff8p0),
  string.format("%q|%q|%q", 1e-310, 5e-324, -2.5)
--== width applies to inf and nan, # adds no prefix to zero, the spec is checked before the argument
return string.format("%10f|%-10e|%+10E|%010.3f|%#x|%#o|%#X|%#X", 1/0, -1/0, 1/0, -1/0, 0, 0, 0, 255),
  select(2, pcall(string.format, "%#+100A", true)), select(2, pcall(string.format, "%10.123d", "x")),
  select(2, pcall(string.format, "%10.123f", "x")), select(2, pcall(string.format, "%10.123c", "x")),
  select(2, pcall(string.format, "%.123a", {}))
--== format conversions that are no conversion
local function try(...) return select(2, pcall(string.format, ...)) end
return try("%\200", 1), try("%-\255", 1), try("%5\0d", 1), try("%\0", 1), try("%y", 1)
