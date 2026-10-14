-- Protected calls, error values and levels.
--== pcall success and failure
local ok1, a, b = pcall(function(x, y) return x + y, x * y end, 3, 4)
local ok2, e2 = pcall(function() error("boom") end)
local ok3, e3 = pcall(function() local t = nil; return t.x end)
return ok1, a, b, ok2, e2, ok3, e3
--== error values that are not strings
local ok, e = pcall(error, {code = 7})
local ok2, e2 = pcall(error)
local ok3, e3 = pcall(error, 42)
return ok, e.code, ok2, e2, ok3, e3
--== error levels
local function lvl(n) error("msg", n) end
local function wrap(n) lvl(n) end
local _, e0 = pcall(lvl, 0)
local _, e1 = pcall(wrap, 1)
local _, e2 = pcall(wrap, 2)
local _, e3 = pcall(error, "direct", 1)
local _, e4 = pcall(error, "direct", 2)
return e0, e1, e2, e3, e4
--== pcall of non-callable values
local ok, e = pcall(nil)
local ok2, e2 = pcall({})
local ok3, r = pcall(setmetatable({}, {__call = function(self, x) return x * 2 end}), 21)
return ok, e, ok2, e2, ok3, r
--== nested pcall
return pcall(pcall, error, "e")
--== pcall result counts
local function none() end
local t = {pcall(none)}
local u = {pcall(function() return 1, 2, 3 end)}
return #t, #u, u[4]
--== xpcall with handlers
local function h(m) return "handled: " .. m end
local r1 = {xpcall(function() error("x") end, h)}
local r2 = {xpcall(function(...) return ... end, h, 1, 2)}
local r3 = {xpcall(function() error({}) end, function(m) return type_of(m) end)}
function type_of(v) if v == nil then return "nil" end return "value" end
return r1[1], r1[2], r2[1], r2[2], r2[3], r3[1], r3[2]
--== error in the message handler
local ok, e = xpcall(function() error("x") end, function(m) error("y") end)
local ok2, e2 = xpcall(error, error)
return ok, e, ok2, e2
--== errors unwind through many frames
local depth = 0
local function down(n) depth = depth + 1; if n == 0 then error("bottom") end return down(n - 1) + 1 end
local ok, e = pcall(down, 100)
local after = depth
local ok2, v = pcall(down, -1)
return ok, e, after
--== pcall inside loops and returns
local s = 0
for i = 1, 10 do
  local ok, v = pcall(function() if i % 3 == 0 then error("skip") end return i end)
  if ok then s = s + v end
end
local function tailp() return pcall(error, "t") end
local ok, e = tailp()
return s, ok, e
--== errors raised again keep their value
local ok, e = pcall(function()
  local ok2, e2 = pcall(error, "inner")
  error(e2, 0)
end)
local ok3, t = pcall(function()
  local _, obj = pcall(error, {n = 1})
  error(obj)
end)
return ok, e, ok3, t.n
--== runtime errors inside pcall have positions
local ok, e = pcall(function()
  local x = 1
  return x + nil
end)
return e
--== stack overflow is catchable
local function inf(n) return inf(n + 1) + 1 end
local ok, e = pcall(inf, 0)
return ok, e
--== xpcall handler sees the error object
local captured
local ok, v = xpcall(function() error(setmetatable({}, {__index = function() return "via meta" end})) end,
  function(err) captured = err.anything; return "done" end)
return ok, v, captured
--== uncaught error from a nested function
local function inner() error("uncaught here") end
local function outer() inner() end
outer()
