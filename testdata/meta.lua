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
