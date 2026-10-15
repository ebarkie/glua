-- The table, math and utf8 libraries.
--== table.insert and remove
local t = {1, 2, 3}
table.insert(t, 4)
table.insert(t, 1, 0)
local r1 = table.remove(t)
local r2 = table.remove(t, 1)
local r3 = table.remove({})
return #t, t[1], t[3], r1, r2, r3, table.concat(t, ",")
--== table.insert errors
local function e(...) local ok, m = pcall(table.insert, ...) return m end
return e({}, 5, 1), e({}, 1, 2, 3), e(1, 2)
--== table.concat
return table.concat({1, 2, 3}), table.concat({"a", "b", "c"}, ", ", 2), table.concat({}, "x"), table.concat({1, 2.5, "z"}, "|", 1, 3),
  select(2, pcall(table.concat, {1, {}, 3}))
--== table.unpack and pack
local p = table.pack(1, nil, 3)
return p.n, p[3], table.unpack({1, 2, 3}), table.unpack({1, 2, 3}, 2), table.unpack({1, 2, 3}, 2, 3), select("#", table.unpack({}, 1, 3))
--== table.move
local a = {1, 2, 3, 4, 5}
table.move(a, 2, 4, 1)
local b = table.move({1, 2, 3}, 1, 3, 3, {})
return table.concat(a, ","), b[3], b[5], #b
--== table.sort
local t = {5, 2, 8, 1, 9, 3, 7, 4, 6, 10, 0}
table.sort(t)
local s1 = table.concat(t, ",")
table.sort(t, function(a, b) return a > b end)
local words = {"pear", "Apple", "fig", "banana"}
table.sort(words)
return s1, table.concat(t, ","), table.concat(words, " ")
--== table.sort errors
local ok, e = pcall(table.sort, {1, "x", 2})
local ok2, e2 = pcall(table.sort, {3, 2, 1, 5, 4, 7, 6, 9, 8}, function(a, b) return true end)
return ok, e, ok2, e2
--== table functions honour metamethods
local log = {}
local proxy = setmetatable({}, {
  __index = function(_, k) return k * 10 end,
  __len = function() return 3 end,
  __newindex = function(t, k, v) log[#log + 1] = k .. "=" .. tostring(v) end,
})
table.insert(proxy, "x")
return table.concat(proxy, ","), table.unpack(proxy), log[1]
--== math basics
return math.floor(3.7), math.ceil(3.2), math.floor(-3.5), math.floor(2^62 * 4), math.abs(-3), math.abs(-2.5), math.abs(math.mininteger),
  math.max(1, 3.5, 2), math.min(4, 2, 3), math.max(1, 1.0), math.type(1), math.type(1.0), math.type("1")
--== math conversions
return math.tointeger(3.0), math.tointeger(3.5), math.tointeger("8"), math.tointeger({}), math.fmod(7, 3), math.fmod(-7, 3), math.fmod(7, -3),
  math.fmod(7.5, 2), math.modf(3.7), math.modf(-3.7), math.modf(1/0), math.ult(1, -1)
--== math functions
return math.sqrt(16), math.exp(0), math.log(1), math.log(8, 2), math.log(100, 10), math.log(27, 3), math.sin(0), math.cos(0),
  math.atan(1, 1) == math.pi / 4, math.huge, -math.huge, math.pi, math.maxinteger, math.mininteger
--== math errors
local function e(f, ...) local ok, m = pcall(f, ...) return m end
return e(math.floor, "x"), e(math.fmod, 1, 0), e(math.random, 5, 1), e(math.random, 1, 2, 3)
--== math.random with a seed
math.randomseed(42)
local a, b, c = math.random(100), math.random(100), math.random(1, 6)
local f = math.random()
local big = math.random(0)
math.randomseed(42)
local a2 = math.random(100)
math.randomseed(7, 13)
local d = math.random(1000000)
return a, b, c, f, big, a == a2, d, math.random(-5, 5), math.random(math.mininteger, math.maxinteger)
--== math.randomseed with floats and its results
local x, y = math.randomseed(1.5)
local r1 = math.random(1000)
local x2, y2 = math.randomseed(-3)
return x, y, r1, x2, y2, math.random(1000)
--== utf8 basics
local s = "héllo wörld €"
return utf8.len(s), #s, utf8.char(72, 233, 8364, 128512), utf8.codepoint(s, 2), utf8.codepoint(s, 1, 3), utf8.offset(s, 3), utf8.offset(s, -1)
--== utf8.codes
local cs = {}
for p, c in utf8.codes("aé€") do cs[#cs + 1] = p .. ":" .. c end
return table.concat(cs, " ")
--== utf8 invalid input
return utf8.len("abc\xff"), utf8.len("\xe4\xbd"), select(2, pcall(utf8.codepoint, "\xff")), utf8.len("\u{7FFFFFFF}", 1, -1, true), utf8.len("\u{7FFFFFFF}"),
  select(2, pcall(utf8.len, "abc", 5)), utf8.offset("abc", 5), select(2, pcall(utf8.offset, "é", 1, 2))
--== utf8.charpattern
local n = 0
for ch in ("aé€"):gmatch(utf8.charpattern) do n = n + 1 end
return n, utf8.charpattern
--== integer extremes as strings
return math.type(tonumber(tostring(math.mininteger))), tonumber("-9223372036854775808"), math.type(tonumber("-9223372036854775809")), math.type(-9223372036854775808)
--== the %z class
return ("a\0b"):find("%z"), ("a\0b"):gsub("%Z", "x")
--== math.log of subnormals, and modf of -0.0
return math.log(5e-324), math.log(5e-324, 10), math.log(1e-310, 10), math.log(5e-324, 2), math.log(1e-310, 3),
  math.log(2^-1022), math.modf(-0.0), math.type((math.modf(-0.0))), math.modf(-0.5)
--== sort while the order function changes the table
local function all(t, n) local o = {} for i = 1, n do o[i] = tostring(t[i]) end return table.concat(o, ",") end
local reads, calls = 0, 0
local t = {5, nil, 3, nil, 1, 4, 2, 6}
table.sort(t, function(a, b)
  calls = calls + 1
  if calls == 2 then setmetatable(t, {__index = function(_, k) reads = reads + 1 return 100 + k end}) end
  return (a or 0) < (b or 0)
end)
local writes = 0
local w = {3, nil, 1, nil, 4}
table.sort(w, function(a, b)
  if not getmetatable(w) then setmetatable(w, {__newindex = function(t, k, v) writes = writes + 1 rawset(t, k, v) end}) end
  return (a or 100) < (b or 100)
end)
local counted = 0
local c = setmetatable({3, nil, 1, nil, 4}, {__newindex = function(t, k, v) counted = counted + 1 rawset(t, k, v) end})
table.sort(c, function(a, b) return (a or 100) < (b or 100) end)
local x = setmetatable({3, 1, 2}, {__len = function() return 5 end})
table.sort(x, function(a, b) setmetatable(x, nil) return (a or 0) < (b or 0) end)
local u = {3, nil, 1}
table.sort(u, function(a, b) return (a or 100) < (b or 100) end)
local v = {"b", "a", "d", "c"}
table.sort(v)
return reads > 0, all(t, 8), writes, all(w, 5), counted, all(c, 5), all(x, 5), #u, all(u, 3), table.concat(v)
--== unpack from the array part and past it
local t = {1, 2, nil, 4}
local m = setmetatable({1, nil, 3}, {__index = function(_, k) return k * 10 end})
local h = {[1] = "a", [2] = "b", x = 1}
local function all(...) local o = {} for i = 1, select("#", ...) do o[i] = tostring((select(i, ...))) end return table.concat(o, ",") end
return all(table.unpack(t)), all(table.unpack(t, 2, 6)), all(table.unpack(m, 1, 4)), all(table.unpack(h)),
  all(table.unpack({}, 1, 2)), all(table.unpack({1, 2, 3}, -1, 1)), all(table.unpack({1, 2, 3}, 3, 2))
