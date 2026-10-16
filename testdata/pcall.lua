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
--== error levels that reach pcall or a library function
local function f(lvl) error("m", lvl) end
local function g(lvl) f(lvl) end
local function e(...) return select(2, pcall(...)) end
local up = setmetatable({}, {__index = function() error("ix", 3) end})
local lib = setmetatable({}, {__index = function() error("ix", 2) end})
return e(f, 2), e(f, 3), e(g, 3), e(g, 4), e(function() f(3) end),
  e(function() return select(2, pcall(f, 2)) end),
  e(function() return table.unpack(up, 1, 1) end), e(function() return table.unpack(lib, 1, 1) end),
  e(function() return ("a"):gsub("a", function() error("gs", 3) end) end),
  e(coroutine.wrap(function() f(3) end))
--== error levels through pcall of pcall and in closers
local function f(lvl) error("m", lvl) end
local function cl(lvl) return setmetatable({}, {__close = function() error("c", lvl) end}) end
local function body(lvl)
  local x <close> = cl(lvl)
  error("e")
end
local co = coroutine.create(function() local x <close> = cl(2) coroutine.yield() end)
coroutine.resume(co)
return select(3, pcall(pcall, error, "x", 3)), select(3, pcall(pcall, f, 4)),
  select(2, pcall(body, 2)), select(2, pcall(body, 3)), select(2, pcall(body, 4)),
  select(2, coroutine.close(co))
--== Go closers that Go code runs, calling back into Lua
local function closing(h) return setmetatable({}, {__close = h}) end
local ok1, e1 = pcall(function()
  return ("a"):gsub("a", function() local x <close> = closing(xpcall) error(print) end)
end)
local co = coroutine.create(function() local x <close> = closing(xpcall) error(print) end)
coroutine.resume(co)
local ok2, e2 = coroutine.close(co)
return ok1, e1 == print, ok2, e2 == print
--== error levels given as nil, strings and floats
local function lvl(n) error("msg", n) end
local _, e1 = pcall(function() lvl(nil) end)
local _, e2 = pcall(function() lvl("2") end)
local _, e3 = pcall(function() lvl(2.0) end)
local _, e4 = pcall(function() lvl(2.5) end)
local _, e5 = pcall(function() lvl({}) end)
local _, e6 = pcall(error, "direct", nil)
local _, e7 = pcall(function() lvl(2^32 + 2) end)
return e1, e2, e3, e4, e5, e6, e7
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
--== xpcall with a handler that is no function
local function e(...) return select(2, pcall(...)) end
local f = function() return 1 end
return e(function() return xpcall(f, nil) end), e(function() return xpcall(f, 1) end),
  e(function() return xpcall(f) end), e(xpcall, f, {}), e(xpcall, f), e(xpcall),
  e(function() return pcall() end), e(pcall), e(table.sort, {1, 2}, xpcall)
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
--== closers run with the error, newest first
local log = ""
local function c(name, fail)
  return setmetatable({}, {__close = function(_, e)
    log = log .. name .. ":" .. (e == nil and "nil" or e) .. " "
    if fail then error(name .. " failed", 0) end
  end})
end
local r1 = {pcall(function() local a <close> = c("a") local b <close> = c("b") error("boom", 0) end)}
local l1 = log; log = ""
local r2 = {pcall(function() local a <close> = c("a") local b <close> = c("b", true) error("boom", 0) end)}
local l2 = log; log = ""
local r3 = {pcall(function() local a <close> = c("a", true) return 1 end)}
local l3 = log; log = ""
local r4 = {xpcall(function() local a <close> = c("a") error("boom", 0) end, function(m) return "h:" .. m end)}
local l4 = log; log = ""
local r5 = {pcall(function() for i in function(_, k) if not k then return 1 end end, nil, nil, c("it") do error("in loop", 0) end end)}
return r1[2], l1, r2[2], l2, r3[2], l3, r4[2], l4, r5[2], log
--== closers when an error leaves the chunk
local function c(name) return setmetatable({}, {__close = function(_, e) error(name .. " replaced " .. e, 0) end}) end
local x <close> = c("x")
error("original", 0)
--== a message handler for a stack overflow
local function loop(x) return 1 + loop(x) end
local a, b = xpcall(loop, function(m) return "h: " .. m end)
local c, d = xpcall(loop, loop)
local function deep() return 1 + deep() end
local e = pcall(deep)
return a, b, c, d, e, select(2, xpcall(loop, function(m) return "again: " .. m end))
