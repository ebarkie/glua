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
--== argument errors of the coroutine library
local function e(...) return select(2, pcall(...)) end
return e(coroutine.status), e(coroutine.status, 1), e(coroutine.wrap, 1), e(coroutine.resume, {}),
  e(coroutine.close), e(coroutine.create), e(coroutine.isyieldable, 1),
  e(function() coroutine.status() end), e(function() coroutine.wrap() end),
  e(function()
    local lib = coroutine
    lib.create(5)
  end)
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
--== Go functions as coroutine bodies
local co = coroutine.create(pcall)
local r1 = {coroutine.resume(co, function(a) local b = coroutine.yield(a + 1); error(b) end, 10)}
local r2 = {coroutine.resume(co, "boom")}
local w = coroutine.wrap(coroutine.yield)
local y = w(1, 2)
local z = w(3)
local e = coroutine.wrap(error)
return r1[1], r1[2], r2[1], r2[2], r2[3], coroutine.status(co), y, z, pcall(e, "x")
--== yielding inside pcall of pcall
local f = function (s, i) return coroutine.yield(i) end
local f1 = coroutine.wrap(function ()
  return xpcall(pcall, function (...) return ... end,
    function ()
      local s = 0
      for i in f, nil, 1 do pcall(function () s = s + i end) end
      error({s})
    end)
end)
f1()
for i = 1, 10 do assert(f1(i) == i) end
local r1, r2, v = f1(nil)
local g = coroutine.wrap(function() return pcall(pcall, coroutine.yield, 5) end)
local y = g()
return r1, r2, v[1], y, g(6)
--== closers that yield on return
local function func2close(f) return setmetatable({}, {__close = f}) end
local function foo2 ()
  local x <close> = func2close(coroutine.yield)
  local extra <close> = func2close(coroutine.yield)
  return table.unpack({1, 2, 3}, 1, 5)
end
local co = coroutine.wrap(foo2)
local a = select("#", co())
local b = select("#", co())
local r = table.pack(co())
return a, b, r.n, r[1], r[3], r[5]
--== an error in a wrapped coroutine closes its variables
local x, y = false, false
local co = coroutine.wrap(function ()
  local xv <close> = setmetatable({}, {__close = function () x = true end})
  local yv <close> = setmetatable({}, {__close = function (_, e) y = e; error("closer") end})
  coroutine.yield(1)
  error("body")
end)
co()
local ok, e = pcall(co)
return ok, e, x, y
--== a variable that gets a non-closable value
local function g() local a, b <close> = 1, {} end
return pcall(g)
--== yield inside __pairs
local t = setmetatable({10, 20, 30}, {__pairs = function (t)
  local inc = coroutine.yield()
  return function (t, i)
           if i > 1 then return i - inc, t[i - inc] else return nil end
         end, t, #t + 1, "extra"
end})
local res = {}
local co = coroutine.wrap(function ()
  for i, p in pairs(t) do res[#res + 1] = p end
  return select("#", pairs(setmetatable({}, {__pairs = function() return 1, 2, 3, 4 end})))
end)
co()
local n = co(1)
return table.concat(res, ","), n
--== coroutines one after another
local function wide(n) local a, b, c, d, e, f, g, h = n, 2, 3, 4, 5, 6, 7, 8 return a + h end
local function fresh(...) local x, y, z return select("#", ...), x, y, z end
local r = {}
for i = 1, 6 do
  r[#r + 1] = coroutine.wrap(wide)(i)
  local n, x, y, z = coroutine.wrap(fresh)()
  r[#r + 1] = n .. tostring(x) .. tostring(y) .. tostring(z)
  local ok, e = coroutine.resume(coroutine.create(function() error("e" .. i, 0) end))
  r[#r + 1] = tostring(ok) .. e
  local held = coroutine.create(function() coroutine.yield(i) return "late" end)
  r[#r + 1] = select(2, coroutine.resume(held))
  r[#r + 1] = coroutine.wrap(function() return coroutine.wrap(function() return i * 2 end)() + 1 end)()
  r[#r + 1] = select(2, coroutine.resume(held))
end
return table.concat(r, ",")
