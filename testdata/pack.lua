-- string.pack, unpack and packsize, and os.time, date and difftime at fixed times.
--== integers
local function hex(s) return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end)) end
return hex(string.pack("i4", 100)), hex(string.pack(">i4", 100)), hex(string.pack("<i2", -2)),
  hex(string.pack("b", -1)), hex(string.pack("B", 255)), hex(string.pack("j", -1)), hex(string.pack(">I3", 0x010203)),
  hex(string.pack("i16", -3)), hex(string.pack(">i16", 5)), hex(string.pack("h", 7))
--== overflow
local function e(...) return select(2, pcall(string.pack, ...)) end
return e("i1", 128), e("i1", -129), e("I1", 256), e("I1", -1), e("i17", 1), e("i0", 1), e("c", "x"), e("q", 1)
--== pack with too few values
local function e(...) return select(2, pcall(string.pack, ...)) end
return e("i"), e("ii", 1), e("d"), e("z"), e("s1"), e("c2"), e("i", "x"), e("xi"), e("Xii", 1)
--== floats
local function hex(s) return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end)) end
return hex(string.pack("d", 1.5)), hex(string.pack(">f", -2)), hex(string.pack("n", 0.1)),
  string.unpack("f", string.pack("f", 0.1)), string.unpack(">d", string.pack(">d", -1e300))
--== strings
local function hex(s) return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end)) end
return hex(string.pack("z", "ab")), hex(string.pack("s1", "abc")), hex(string.pack(">s2", "x")),
  hex(string.pack("c5", "ab")), select(2, pcall(string.pack, "c2", "abc")), select(2, pcall(string.pack, "z", "a\0b")),
  select(2, pcall(string.pack, "s1", string.rep("x", 256)))
--== unpack
return string.unpack("i4", string.pack("i4", -7)), string.unpack("<i2 >i2", "\1\0\0\1"),
  string.unpack("z s1 c3", "hi\0\3abcxyz"), string.unpack("i4", "\0\0\0\0\1\0\0\0", 5), string.unpack("B", "abc", -1)
--== unpack errors
local function e(...) return select(2, pcall(string.unpack, ...)) end
return e("i4", "abc"), e("z", "abc"), e("s1", "\5ab"), e("i4", "abcd", 6), e("i9", "\0\0\0\0\0\0\0\0\1"),
  string.unpack("i9", "\255\255\255\255\255\255\255\255\255"), e("i9", "\0\0\0\0\0\0\0\128\0")
--== alignment
local function hex(s) return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end)) end
return hex(string.pack("!4 b i4", 1, 2)), hex(string.pack("!8 b d", 1, 0)), hex(string.pack("b Xi4 b", 1, 2)),
  hex(string.pack("!2 b i8", 1, 2)), string.packsize("!8 b d"), string.packsize("i4 i8 !4 i8"),
  select(2, pcall(string.pack, "!3 b i4", 1, 2)), select(2, pcall(string.pack, "X", 1)), select(2, pcall(string.pack, "Xc1", 1))
--== packsize
local function e(...) return select(2, pcall(string.packsize, ...)) end
return string.packsize("i4 h b"), string.packsize("c100"), string.packsize("d n j"), e("z"), e("s"),
  e("c2147483647 c2"), string.packsize("x X i4")
--== os.time and date at fixed times
local t = os.time({year = 2026, month = 10, day = 15, hour = 9, min = 30, sec = 5})
local u = os.date("*t", t)
return u.year, u.month, u.day, u.hour, u.min, u.sec, u.wday, u.yday, os.date("%Y-%m-%d %H:%M:%S", t),
  os.time({year = 2026, month = 13, day = 1, hour = 0}) - os.time({year = 2027, month = 1, day = 1, hour = 0})
--== os.date in UTC
local t = 1791972000
return os.date("!%c", t), os.date("!%x %X %p %I", t), os.date("!%a %A %b %B %d %e %j %m %y %Y", t),
  os.date("!%D %F %R %T %H %M %S %u %w %U %W %V %G %g %C %n%t%%", t), os.date("!%Ec %Ex %EY %Od %OH", t)
--== os.date errors and normalisation
local function e(...) return select(2, pcall(os.date, ...)) end
local tt = {year = 2026, month = 2, day = 31, hour = 12}
local t = os.time(tt)
return e("%Q"), e("%"), e("%E"), e("%Ez"), tt.month, tt.day, tt.yday, os.difftime(t + 10, t), select(2, pcall(os.difftime, 5)),
  select(2, pcall(os.time, {year = 2026})), select(2, pcall(os.time, {year = 2026, month = "x", day = 1}))
--== os.time reads fields as C Lua's lua_tointegerx: numeric strings and integral floats
local function e(t) return select(2, pcall(os.time, t)) end
local base = os.time({year = 2000, month = 1, day = 1, hour = 12})
return os.time({year = "2000", month = "1", day = 1, hour = 12}) == base, os.time({year = "2000.0", month = 1.0, day = "1"}) == base,
  e({year = "x", month = 1, day = 1}), e({year = 2000.5, month = 1, day = 1}), e({year = true, month = 1, day = 1}),
  e({year = {}, month = 1, day = 1}), e({year = 2^40, month = 1, day = 1})
--== os.date formats with a NUL, and *t followed by more
local function e(...) return select(2, pcall(os.date, ...)) end
return os.date("!*tX", 0), type(os.date("!*t\0x", 0)), e("!%\0", 0), e("!%Q\0abc", 0), e("!%E\0", 0),
  os.date("!a\0b", 0)
--== os.date for years a C int cannot hold
local function e(...) return select(2, pcall(os.date, ...)) end
return e("!%Y", 2^60), os.date("!%Y", 67767976233532799), os.date("!%Y", -67768040609740800), e("!%Y", -67768040609740801),
  e("!*t", 2^60)
--== os.date quotes the rest of the format from a bad conversion
local function e(...) return select(2, pcall(os.date, ...)) end
return e("!x%qx%%-%m", 0), e("%Ez %A", 0), e("!%Q"), e("%E%W rest", 0)
