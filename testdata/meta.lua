-- Metamethods. Each case returns plain values; gofirst is a Go function that returns its
-- first argument (in C Lua it is defined below as a Lua function to the same effect).
--== arithmetic metamethods in both orders
local mt = {}
for _, e in next, {"add", "sub", "mul", "div", "mod", "pow", "idiv", "band", "bor", "bxor", "shl", "shr"} do
  mt["__" .. e] = function(a, b)
    local x = type and 0 or (getmetatable and 0 or 0)
    return e .. ":" .. (rawequal and "" or "") .. ((a == mt.v) and "v" or tostring_or(a)) .. "," .. ((b == mt.v) and "v" or tostring_or(b))
  end
end
function tostring_or(x) if x == nil then return "nil" end return x .. "" end
local v = setmetatable({}, mt)
mt.v = v
return v + 1, 2 - v, v * v, v / 3, 4 % v, v ^ 5, v // 6, v & 7, 8 | v, v ~ 9, v << 10, 11 >> v
--== unary metamethods
local mt = {__unm = function(a, b) return "unm" end, __bnot = function(a) return "bnot" end, __len = function(a) return 42 end}
local v = setmetatable({}, mt)
return -v, ~v, #v
--== __len overrides the border
local t = setmetatable({1, 2, 3}, {__len = function() return 99 end})
return #t
--== concat metamethods inside a chain
local mt = {__concat = function(a, b)
  local function s(x) if type_is_table(x) then return "T" end return x end
  return s(a) .. "+" .. s(b)
end}
function type_is_table(x) return getmetatable_t(x) end
function getmetatable_t(x) return x == T1 or x == T2 end
T1 = setmetatable({}, mt)
T2 = setmetatable({}, mt)
return "a" .. T1 .. "b" .. "c", T1 .. T2, 1 .. T1, T1 .. 2 .. 3
--== a Go __concat raises at the line that concatenates
local t = setmetatable({{}}, {__concat = table.concat})
return select(2, pcall(function() return t .. "x" end))
--== __eq
local mt = {__eq = function(a, b) return a.id == b.id end}
local a, b, c = setmetatable({id = 1}, mt), setmetatable({id = 1}, mt), setmetatable({id = 2}, mt)
local d = {id = 1}
return a == b, a == c, a ~= c, a == d, d == a, a == 1
--== __lt and __le
local mt = {
  __lt = function(a, b) return (type_n(a)) < (type_n(b)) end,
  __le = function(a, b) return (type_n(a)) <= (type_n(b)) end,
}
function type_n(x) if x == X or x == Y then return x.n end return x end
X = setmetatable({n = 1}, mt)
Y = setmetatable({n = 2}, mt)
return X < Y, Y < X, X <= Y, X > Y, X >= X, X < 5, 0 < X, 1 <= X
--== metamethods written in Go
local t = setmetatable({}, {__add = gofirst, __index = gofirst, __call = gofirst})
local r = t + 1
return r == t, t.anything == t, t() == t
--== argument errors in Go metamethods name the event
local rep = string.rep
local mt = {__concat = rep, __index = rep, __newindex = rep, __add = rep, __unm = rep, __len = rep,
  __eq = rep, __lt = rep, __le = rep, __band = rep, __shl = rep, __idiv = rep}
local r, r2 = setmetatable({}, mt), setmetatable({}, mt)
local function e(f) return (select(2, pcall(f)):gsub("^.-:%d+: ", "")) end
return e(function() return "x" .. r end), e(function() return r.k end), e(function() return r[1] end),
  e(function() r.k = 1 end), e(function() return 1 + r end), e(function() return r // 2 end),
  e(function() return r & 1 end), e(function() return r << 1 end), e(function() return -r end),
  e(function() return #r end), e(function() return r == r2 end), e(function() return r < r2 end),
  e(function() return r <= r2 end), e(function() return r > r2 end), e(function() return r:m() end),
  e(function() for _ in next, 5 do end end),
  e(function() local x <close> = setmetatable({}, {__close = rep}) end)
--== __name in error messages
local t = setmetatable({}, {__name = "MyType"})
return t + 1
--== metamethod results are truncated to one value
local t = setmetatable({}, {__add = function() return 1, 2, 3 end, __index = function() return "x", "y" end})
local a, b = t + 0
local c, d = t.k
return a, b, c, d
--== __index function receives table and key
local seen
local t = setmetatable({}, {__index = function(tt, k) seen = k; return k * 2 end})
return t[21], seen
--== a tail call through __call stays a tail call
local t
t = setmetatable({}, {__call = function(self, n) if n == 0 then return "done" end return t(n - 1) end})
local inner = setmetatable({}, {__call = function(self, a, b) return a, b end})
local outer = setmetatable({}, {__call = inner})
local function viaChain() return outer(1) end
local function viaGo() return setmetatable({1, 2, 3}, {__call = rawlen})() end
local function bad() local x = {} return x(1) end
local function badChain() return setmetatable({}, {__call = 5})() end
return t(200000), (viaChain()) == outer, select("#", viaChain()), viaGo(),
  select(2, pcall(bad)), select(2, pcall(badChain))
--== metamethods that are not functions
local callable = setmetatable({}, {__call = function(self, a, b) return "called" end})
local t = setmetatable({}, {__add = "x", __lt = callable, __len = true, __concat = 1, __unm = callable})
local r = {}
r[#r + 1] = select(2, pcall(function() return t + 1 end))
r[#r + 1] = t < t
r[#r + 1] = select(2, pcall(function() return #t end))
r[#r + 1] = select(2, pcall(function() return t .. "a" end))
r[#r + 1] = -t
local e = setmetatable({}, {__eq = {}})
r[#r + 1] = select(2, pcall(function() return e == setmetatable({}, getmetatable(e)) end))
local mt = {__close = function() end}
r[#r + 1] = select(2, pcall(function() local x <close> = setmetatable({}, mt); mt.__close = 3 end))
mt.__close = function() end
r[#r + 1] = select(2, pcall(function() local x <close> = setmetatable({}, mt); mt.__close = nil end))
mt.__close = function() end
r[#r + 1] = select(2, pcall(function() local x <close> = setmetatable({}, mt); mt.__close = 4; error("e") end))
local seen
mt.__close = setmetatable({}, {__call = function(s, o, err) seen = err end})
do local x <close> = setmetatable({}, mt) end
r[#r + 1] = tostring(seen)
local g = setmetatable({}, {__concat = setmetatable({}, {__call = gofirst})})
r[#r + 1] = type(g .. "x")
return table.unpack(r)
--== __index and __newindex chains that loop
local L = {}
L.__index = L
L.__newindex = L
setmetatable(L, L)
local o = setmetatable({}, L)
local ok1, e1 = pcall(function() return o.nothing end)
local ok2, e2 = pcall(function() o.nothing = 1 end)
local ok3, e3 = pcall(table.unpack, setmetatable({}, {__index = L}), 1, 1)
local ok4, e4 = pcall(table.insert, setmetatable({}, {__newindex = L}), 1)
return ok1, e1, ok2, e2, ok3, e3, ok4, e4
--== index errors raised by the library, called from Lua
local L = {}
L.__index = L
L.__newindex = L
setmetatable(L, L)
local function err(f) return select(2, pcall(f)) end
return err(function() return table.unpack(setmetatable({}, L), 1, 1) end),
  err(function() table.insert(setmetatable({}, L), 1) end),
  err(function() for _ in ipairs(5) do end end),
  err(function() table.insert(setmetatable({}, {__newindex = 5}), 1) end)
--== fields added to tables just built, with and without __newindex
local function new(x, y) return {x = x, y = y} end
local r, sum = {}, 0
for i = 1, 20 do
  local p = new(i, 2 * i)
  p.z = i; p.w = -i -- adds, the second one past the size the constructor asked for
  p.y = nil; p.y = 3 -- sets a deleted key again
  sum = sum + p.x + p.y + p.z + p.w
end
r[#r + 1] = sum
local logged = {}
local mt = {__newindex = function(t, k, v) logged[#logged + 1] = k; rawset(t, k, v * 10) end}
for i = 1, 6 do
  local p = setmetatable(new(i, i), mt)
  p.x = i -- present: no __newindex
  p.z = i -- absent: __newindex
  r[#r + 1] = p.x + p.z
end
r[#r + 1] = #logged
local m = new(1, 2)
local o = setmetatable({}, m)
m.__index = function(_, k) return k .. "!" end -- a "__" key added to a table in use as a metatable
r[#r + 1] = o.q
local n = 0
local t = new(1, 2)
for i = 1, 40 do t["k" .. i] = i end
t.v = 0
for _ in pairs(t) do n = n + 1 end
r[#r + 1] = n
return table.unpack(r)
--== integer and string keys outside the array part, with and without __newindex
local t, r = {}, {}
for i = 0, 9 do t[i] = i * 2 end -- 0 lives in the hash part
for i = -3, -1 do t[i] = i end
t[100], t[1000] = "a", "b"
local k = "s"
t[k .. 1], t[k .. 2] = 1, 2
t[0] = t[0] + 5
r[#r + 1] = t[0] + t[9] + t[-3] + #t
r[#r + 1] = t[100] .. t[1000] .. tostring(t[101]) .. t[k .. 1] + t[k .. 2]
local logged = {}
local mt = {__newindex = function(u, key, v) logged[#logged + 1] = tostring(key); rawset(u, key, v) end,
  __index = function(_, key) return "idx" .. tostring(key) end}
local u = setmetatable({[0] = "zero"}, mt)
u[0] = "z" -- present: no __newindex
u[-1] = "m" -- absent: __newindex
u[k] = "s"
r[#r + 1] = u[0] .. u[-1] .. u[k] .. u[7] .. u[k .. "x"]
r[#r + 1] = table.concat(logged, ",")
local a = {1, 2, 3}
a[5] = 5 -- past the end: the hash part
a[4] = 4 -- one past the end: appended, and 5 moves over
r[#r + 1] = #a
local n = 0
for _ in pairs(t) do n = n + 1 end
r[#r + 1] = n
return table.unpack(r)
