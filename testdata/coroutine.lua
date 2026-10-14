-- Coroutines.
--== generator
local gen = coroutine.wrap(function() for i = 1, 3 do coroutine.yield(i) end return "done" end)
return gen(), gen(), gen(), gen()
--== values both ways
local co = coroutine.create(function(a, b)
  local c = coroutine.yield(a + b)
  local d, e = coroutine.yield(c * 2)
  return d + e, "end"
end)
local r = {}
local function add(...) for _, v in next, {...} do r[#r + 1] = v end end
add(coroutine.resume(co, 1, 2))
add(coroutine.resume(co, 10))
add(coroutine.resume(co, 3, 4))
add(coroutine.resume(co))
return r[1], r[2], r[3], r[4], r[5], r[6], r[7], r[8], r[9]
--== status and running
local main, ismain = coroutine.running()
local co
co = coroutine.create(function()
  local me, im = coroutine.running()
  return coroutine.status(co), me == co, im, coroutine.isyieldable()
end)
local s0 = coroutine.status(co)
local ok, s1, same, im, y = coroutine.resume(co)
return s0, ok, s1, same, im, y, coroutine.status(co), ismain, coroutine.isyieldable()
--== yield across pcall
local co = coroutine.create(function()
  local ok, v = pcall(function() local x = coroutine.yield(1) return x * 10 end)
  local ok2, e = pcall(function() coroutine.yield(2) error("after yield", 0) end)
  return ok, v, ok2, e
end)
local _, a = coroutine.resume(co)
local _, b = coroutine.resume(co, 5)
local _, c, d, e, f = coroutine.resume(co)
return a, b, c, d, e, f
--== pcall of yield itself
local co = coroutine.create(function() return pcall(coroutine.yield, "y") end)
local _, a = coroutine.resume(co)
return a, coroutine.resume(co, "back")
--== yield inside metamethods and iterators
local t = setmetatable({}, {__index = function(_, k) return coroutine.yield(k) end})
local co = coroutine.wrap(function()
  local v = t.key
  local s = 0
  for i in function(_, last) last = (last or 0) + 1 if last <= 2 then coroutine.yield("it" .. last) return last end end do s = s + i end
  return v, s
end)
return co(), co("got"), co(), co()
--== errors in coroutines
local co = coroutine.create(function() error("inside", 0) end)
local ok, e = coroutine.resume(co)
local ok2, e2 = coroutine.resume(co)
local w = coroutine.wrap(function() error("wrapped") end)
local ok3, e3 = pcall(w)
return ok, e, coroutine.status(co), ok2, e2, ok3, e3
--== yield errors
local ok, e = pcall(coroutine.yield, 1)
local co = coroutine.wrap(function()
  local t = setmetatable({}, {__close = function() end})
  return pcall(function() return coroutine.status(coroutine.running()) end)
end)
return ok, e, co()
--== close
local log = ""
local co = coroutine.create(function()
  local x <close> = setmetatable({}, {__close = function(_, e) log = log .. "closed:" .. (e == nil and "nil" or e) end})
  coroutine.yield()
end)
coroutine.resume(co)
local r1 = {coroutine.close(co)}
local co2 = coroutine.create(function() error("x", 0) end)
coroutine.resume(co2)
local r2 = {coroutine.close(co2)}
return r1[1], log, coroutine.status(co), r2[1], r2[2]
--== resume non-suspended
local co
co = coroutine.create(function() return coroutine.resume(co) end)
local ok, ok2, e = coroutine.resume(co)
return ok, ok2, e, coroutine.resume(coroutine.running())
--== many coroutines and deep yields
local function walk(n) if n > 0 then walk(n - 1) end coroutine.yield(n) end
local co = coroutine.wrap(function() walk(100) end)
local s = 0
for i = 1, 101 do s = s + co() end
local cs = {}
for i = 1, 200 do cs[i] = coroutine.wrap(function(a) local b = coroutine.yield(a + 1) return b * 2 end) end
local t = 0
for i = 1, 200 do t = t + cs[i](i) end
for i = 1, 200 do t = t + cs[i](i) end
return s, t
--== a coroutine with a Go body
local co = coroutine.create(select)
local r = {coroutine.resume(co, 2, "a", "b")}
return r[1], r[2], coroutine.status(co), coroutine.isyieldable(co), coroutine.isyieldable(coroutine.create(print)), coroutine.isyieldable(coroutine.running())
--== resume nested too deep
local function f()
  local c = coroutine.create(f)
  local a, b = coroutine.resume(c)
  return b
end
return f()
