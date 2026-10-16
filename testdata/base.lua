-- The base library.
--== type
return type(nil), type(true), type(1), type(1.5), type("s"), type({}), type(print), type(coroutine.create(print))
--== tonumber
return tonumber("10"), tonumber("0x1F"), tonumber(" 1e2 "), tonumber("abc"), tonumber(42), tonumber({}),
  tonumber("ff", 16), tonumber("zz", 36), tonumber("777", 8), tonumber("8", 8), tonumber("-101", 2), tonumber("1.5", 10)
--== tostring
local t = setmetatable({}, {__tostring = function() return "custom" end})
return tostring(1), tostring(1.0), tostring(-0.0), tostring(nil), tostring(true), tostring("s"), tostring(t), tostring(1e100)
--== select
return select("#"), select("#", 1, nil, 3), select(2, "a", "b", "c"), select(-1, "a", "b", "c")
--== select negative and range
local ok, e = pcall(select, 0, 1)
local ok2, e2 = pcall(select, -5, 1)
return ok, e, ok2, e2, select(5, 1, 2)
--== rawget rawset rawequal rawlen
local t = setmetatable({}, {__index = function() return "meta" end, __newindex = function() error("no") end})
rawset(t, "k", "v")
local u = setmetatable({}, {__eq = function() return true end})
return rawget(t, "k"), rawget(t, "x"), t.x, rawequal(t, t), rawequal(u, {}), u == setmetatable({}, getmetatable(u)), rawlen({1, 2}), rawlen("abc")
--== getmetatable and protection
local mt = {__metatable = "locked"}
local t = setmetatable({}, mt)
local ok, e = pcall(setmetatable, t, {})
return getmetatable(t), ok, e, getmetatable({})
--== pairs and next
local t = {10, 20, 30, x = 1}
local n, sum = 0, 0
for k, v in pairs(t) do n = n + 1; sum = sum + v end
local k1 = next({})
return n, sum, k1, next({5})
--== __pairs
local t = setmetatable({}, {__pairs = function(self) return function(_, k) if not k then return 1, "one" end end, self, nil end})
local r = {}
for k, v in pairs(t) do r[#r + 1] = k .. v end
return #r, r[1]
--== ipairs with holes and __index
local t = {1, 2, nil, 4}
local n = 0
for i, v in ipairs(t) do n = n + 1 end
local p = setmetatable({}, {__index = function(_, i) if i <= 3 then return i * 10 end end})
local s = 0
for i, v in ipairs(p) do s = s + v end
return n, s
--== assert
local ok, e = pcall(assert, false)
local ok2, e2 = pcall(assert, nil, "custom message")
local ok3, e3 = pcall(assert, false, {code = 1})
return ok, e, ok2, e2, ok3, type(e3), assert(1, 2, 3)
--== argument errors
local results = {}
local function try(f, ...) local ok, e = pcall(f, ...) results[#results + 1] = e end
try(setmetatable, 1)
try(setmetatable, {}, 1)
try(rawget, 1)
try(rawlen, 1)
try(next, {}, "nokey")
try(tonumber, {}, 10)
try(tonumber, "10", 99)
try(select, "x")
try(type)
try(ipairs)
try(rawset, {}, nil, 1)
return results[1], results[2], results[3], results[4], results[5], results[6], results[7], results[8], results[9], results[10], results[11]
--== _G and _VERSION
return _G._G == _G, type(_G.print), _VERSION
--== collectgarbage
return type(collectgarbage("count")), collectgarbage(), collectgarbage("isrunning"), collectgarbage("step")
--== setmetatable without a metatable
local ok, err = pcall(setmetatable, {})
local ok2, err2 = pcall(setmetatable, setmetatable({}, {__metatable = 1}))
local u = setmetatable({}, {})
return ok, err, ok2, err2, setmetatable(u, nil) == u, getmetatable(u)
