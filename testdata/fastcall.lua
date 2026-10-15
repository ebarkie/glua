-- Built-ins that FASTCALL does inline, and the cases it must leave to the ordinary call.
--== math.floor and ceil
return math.floor(3.5), math.floor(-3.5), math.floor(7), math.floor(-0.0), math.floor(2^63), math.floor(-2^63),
  math.ceil(3.2), math.ceil(-3.2), math.ceil(1e300), math.floor(0/0) ~= math.floor(0/0), math.type(math.floor(2.0))
--== math.abs
return math.abs(-3), math.abs(3), math.abs(-2.5), math.abs(-0.0), math.abs(math.mininteger), math.abs(-1/0)
--== math.sqrt
return math.sqrt(16), math.sqrt(2), math.sqrt(-1) ~= math.sqrt(-1), math.sqrt(0)
--== math.max and min
return math.max(1, 2), math.max(2, 1), math.min(1, 2), math.max(1.5, 2.5), math.min(-0.0, 0.0), math.max(1, 2.5),
  math.max(0/0, 1) ~= math.max(0/0, 1), math.max(1, 0/0), math.max(3), math.min(4, 2, 9), math.max(1, 2, 3)
--== type
return type(nil), type(true), type(1), type(1.5), type("s"), type({}), type(print), type(type), type(function() end)
--== string.len and byte
local s = "hello"
return string.len(s), string.len(""), string.byte(s), string.byte(s, 2), string.byte(s, -1), string.byte(s, 10),
  string.byte(""), string.byte(s, 0), select("#", string.byte(s, 10)), string.byte(s, 2, 4)
--== errors are the ordinary call's
local function e(f, ...) return select(2, pcall(f, ...)) end
return e(function() return math.floor("x") end), e(function() return math.abs() end),
  e(function() return string.byte({}) end), e(function() return math.max() end), e(function() return type() end)
--== numeric strings
return math.floor("3.7"), math.abs("-2"), math.max("10", 9), string.len(123), string.byte(65)
--== a local math is not the built-in
local math = {floor = function(x) return "mine " .. x end}
return math.floor(1.5)
--== a redefined global is not the built-in
local saved = type
type = function() return "mine" end
local r = type(1)
type = saved
return r, type(1)
--== results discarded or kept
math.floor(1.5)
local a, b = math.floor(2.5)
local t = {math.floor(3.5)}
return a, b, t[1], #t
--== in a loop
local s = 0
for i = 1, 1000 do s = s + math.floor(i / 3) + math.max(i % 7, 3) + math.abs(-i) % 5 end
return s
--== an alias of a built-in
local floor, sqrt, tp = math.floor, math.sqrt, type
local function f(x) return floor(x) + sqrt(x * x), tp(x) end
local a, b = f(2.5)
return a, b, floor(-1.5)
--== an alias that is assigned is not one
local floor = math.floor
local r1 = floor(1.5)
floor = function() return "mine" end
return r1, floor(1.5)
--== string.sub
local s = "hello world"
return string.sub(s, 1, 5), string.sub(s, 7), string.sub(s, -5), string.sub(s, -5, -2), string.sub(s, 0),
  string.sub(s, 3, 2), string.sub(s, 20), string.sub(s, -100, 3), string.sub(s, 5, 100), string.sub("", 1, 1),
  string.sub(s, math.mininteger, math.maxinteger), string.sub(s, math.maxinteger), #string.sub(s, 12)
--== string.sub with arguments the ordinary call converts
local s = "hello"
return string.sub(s, 2.0, 3.0), string.sub(s, "2", "3"), string.sub(123456, 2, 3), string.sub(s, 2, nil),
  select(2, pcall(string.sub, s)), select(2, pcall(string.sub, s, 1.5)), select(2, pcall(string.sub, s, {}))
--== string methods
local s = "hello"
return s:sub(2, 3), s:sub(-3), s:len(), s:byte(), s:byte(-1), ("abc"):sub(2), ("xyz"):byte(2), (""):len(),
  s:sub(2.0), s:byte(1, 3), select("#", s:byte(10))
--== a method of the same name on a table is not the built-in
local t = {sub = function(self, i, j) return "mine", i, j end, len = function() return 42 end}
return t:sub(1, 2), t:len()
--== string.sub through a table is not a string receiver
local t = {sub = string.sub}
return select(2, pcall(function() return t:sub(1, 2) end))
--== methods in a loop
local n = 0
for i = 1, 1000 do
  local s = "/api/" .. i
  if s:sub(1, 5) == "/api/" then n = n + s:len() + s:byte(6) end
end
return n
