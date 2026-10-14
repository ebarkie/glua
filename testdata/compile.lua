-- Each case is a chunk; cases are separated by lines that start with "--==". A case
-- returns its results, which compile_test.go compares with what C Lua returns.
--== arithmetic and precedence
return 1 + 2 * 3, (1 + 2) * 3, 2^3^2, -2^2, 7 // 2, -7 // 2, 7 % -3, -7 % 3, 7.5 // 2, 1 / 2, 3 // 0.0
--== integer and float
return 1 + 1, 1 + 1.0, 10 / 2, 3 * 1.5, 2^53 + 1, 9007199254740993, math_huge
--== integer wraparound and division
local m = 9223372036854775807
return m + 1, -m - 2, m * 2, 5 // -2, -5 % -2, 0x7fffffffffffffff // -1
--== bitwise
return 5 & 3, 5 | 3, 5 ~ 3, ~0, 1 << 62, 1 << 63, 1 << 64, -1 >> 1, 3.0 & 1, 1 >> -1
--== comparisons
return 1 < 2, 2 <= 2, 3 > 4, 1 == 1.0, "a" < "b", "abc" < "abd", "Z" < "a", 1 ~= 2, 0.0 == -0.0
--== logic values
local a, b = nil, false
return a or 1, b or 2, 1 and 2, nil and 1, false or nil, not nil, not 0, 1 and nil or 3, (a or b) == false
--== numeric strings in arithmetic
return "10" + 1, "3" * "4", "0x10" + 0, " 5 " * 2, 10 .. 20, 1.5 .. "", -"2"
--== locals and multiple assignment
local x, y, z = 1, 2
local p, q = 1, 2, 3
x, y = y, x
return x, y, z, p, q
--== assignment conflicts
local t = {}
local u = t
t, t.x = {}, 1
local v = {}
v.y, v = 2, {}
return u.x, t.x, v.y
--== globals
g1 = 5
g2 = g1 * 2
return g1, g2, g3
--== table constructors
local t = {1, 2, 3, x = "a", ["y"] = "b", [10] = "c"; 4}
return #t, t[1], t[4], t.x, t.y, t[10], t[5]
--== constructors with calls and varargs
local function three() return 1, 2, 3 end
local a = {three()}
local b = {three(), three()}
local c = {(three())}
local d = {three(), 10}
return #a, #b, #c, #d, b[4], d[2]
--== varargs
local function f(...)
  local a, b = ...
  local t = {...}
  return #t, a, b, ...
end
return f(10, 20, 30)
--== varargs with fixed parameters
local function f(a, ...)
  local n = #{...}
  return a, n, ...
end
local function g(...) return f(...) end
return g(), g(1), g(1, 2, 3)
--== result adjustment
local function two() return 1, 2 end
local a, b, c = two()
local d, e = two(), 10
local f = (two())
return a, b, c, d, e, f
--== method calls
local obj = {n = 5}
function obj.get(self) return self.n end
function obj:add(k) self.n = self.n + k; return self end
return obj:add(2):add(3):get(), obj.n
--== closures capture by value
local fs = {}
for i = 1, 3 do fs[i] = function() return i end end
local x = 10
local function getx() return x end
return fs[1](), fs[2](), fs[3](), getx()
--== closures share cells
local function counter()
  local n = 0
  return function() n = n + 1; return n end, function() return n end
end
local inc, get = counter()
inc(); inc()
local inc2 = counter()
inc2()
return get(), inc()
--== upvalues through several levels
local a = 1
local function f()
  local b = 2
  return function()
    return function() a = a + b; return a end
  end
end
local g = f()()
g()
return g(), a
--== fresh cell per iteration
local fs = {}
for i = 1, 3 do
  local j = i
  fs[i] = function() j = j + 10; return j end
end
return fs[1](), fs[1](), fs[2](), fs[3]()
--== loop variable assigned and captured
local fs = {}
for i = 1, 3 do
  i = i * 2
  fs[#fs + 1] = function() return i end
end
return fs[1](), fs[2](), fs[3]()
--== while, repeat, break
local n, s = 0, 0
while true do
  n = n + 1
  if n > 10 then break end
  s = s + n
end
local r = 0
repeat local k = r; r = r + 1 until k >= 4
return n, s, r
--== numeric for loops
local s1, s2, s3, s4 = 0, 0, 0, 0
for i = 1, 10 do s1 = s1 + i end
for i = 10, 1, -2 do s2 = s2 + i end
for i = 1, 2, 0.5 do s3 = s3 + i end
for i = 1, 0 do s4 = s4 + 1 end
return s1, s2, s3, s4
--== generic for with an iterator
local function range(n)
  return function(_, i) if i < n then return i + 1 end end, nil, 0
end
local s = 0
for i in range(5) do s = s + i end
local t = {}
for i, sq in function(_, i) if i < 3 then return i + 1, (i + 1)^2 end end, nil, 0 do t[i] = sq end
return s, t[1], t[2], t[3]
--== goto continue and nested loops
local s = 0
for i = 1, 10 do
  if i % 2 == 0 then goto continue end
  for j = 1, 3 do
    if j == 2 then break end
    s = s + i * j
  end
  ::continue::
end
do
  local k = 0
  ::top::
  k = k + 1
  if k < 5 then goto top end
  s = s + k
end
return s
--== if chains
local function classify(n)
  if n < 0 then return "neg" elseif n == 0 then return "zero" elseif n < 10 then return "small" else return "big" end
end
return classify(-1), classify(0), classify(5), classify(50)
--== recursion and tail calls
local function fib(n) if n < 2 then return n end return fib(n - 1) + fib(n - 2) end
local function loop(n, acc) if n == 0 then return acc end return loop(n - 1, acc + 1) end
return fib(20), loop(100000, 0)
--== mutual recursion through locals
local even, odd
function even(n) if n == 0 then return true end return odd(n - 1) end
function odd(n) if n == 0 then return false end return even(n - 1) end
return even(10), odd(7), even(7)
--== concatenation
local a, b = "x", 2
return a .. b .. 3.0 .. "y", a .. (b + 1), 1 .. 2
--== const locals
local k <const> = 10
local s <const> = "s"
local function f() return k * 2 end
return k + 1, s .. k, f()
--== and/or in conditions and values
local function f(x) return x and x > 0 and "pos" or "nonpos" end
local t = {}
local v = t.missing or t.other or "default"
local w = not (1 > 2) and 3
return f(1), f(-1), f(nil), v, w
--== comparison results as values
local a = 1 < 2
local b = "a" == "b"
local c = not (a and b)
return a, b, c, 2 > 1 == true
--== length of strings and tables
local t = {1, 2, 3, nil, 5}
return #"hello", #"", #{}, #{1, 2, 3}
--== large constructor
local t = {1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,
  31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60}
return #t, t[1], t[50], t[51], t[60]
--== __index and __newindex
local base = {greet = "hi"}
local log = {}
local t = setmetatable({}, {__index = base, __newindex = function(t, k, v) log[#log + 1] = k end})
t.a = 1
t.b = 2
local f = setmetatable({}, {__index = function(t, k) return k .. "!" end})
return t.greet, t.a, #log, log[1], f.x, f[1]
--== to-be-closed variables
local log = {}
local function closer(name) return setmetatable({}, {__close = function() log[#log + 1] = name end}) end
do
  local a <close> = closer("a")
  local b <close> = closer("b")
end
for i = 1, 2 do
  local c <close> = closer("c" .. i)
  if i == 2 then break end
end
local function f()
  local d <close> = closer("d")
  return "ret"
end
local r = f()
return #log, log[1], log[2], log[3], log[4], log[5], r
--== _ENV as a local
local function f()
  local _ENV = {x = 1, y = 2}
  z = x + y
  return z, _ENV.z
end
return f()
--== upvalue _ENV
local t = {v = 7}
local function f() local _ENV = t; return function() return v end end
return f()()
--== nested functions and varargs in closures
local function outer(...)
  local n = #{...}
  return function() return n end
end
return outer(1, 2, 3)()
--== string escapes and long strings
return "a\tb", "\65\066", [[
line]], "\u{48}\z
       i"
--== float formatting through tostring
return 1e15, 1e16, 2^63, -0.0, 1/0, -1/0, 3.0, 0.1
--== for loops over numeric strings
local s, t = 0, 0
for i = "1", "3" do s = s + i end
for i = 1, "3" do t = t + i end
return s, t
--== string coercions in arithmetic
return "0x10" + 0, "1e2" + 0, " 0x1p4 " * 1, "10" // "3", "7" % "4", "2" ^ "10", "10" / "4", - " 3 "
--== number formatting in concatenation
return 1e100 .. "", 2^63 .. "", -0.0 .. "", 1/0 .. "", 3 .. "", 3.0 .. "", 123456789012 .. "", 0.1 + 0.2 .. ""
--== string comparison
return "a\0b" < "a\0c", "" < "a", "abc" == "ab" .. "c", "Z" < "a", "10" < "9", "\xff" > "a"
