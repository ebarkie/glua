-- closures and higher-order functions
local function map(t, f) local r = {} for i = 1, #t do r[i] = f(t[i]) end return r end
local function filter(t, f) local r = {} for i = 1, #t do if f(t[i]) then r[#r + 1] = t[i] end end return r end
local function reduce(t, f, acc) for i = 1, #t do acc = f(acc, t[i]) end return acc end
local function counter() local n = 0 return function() n = n + 1 return n end end
function work()
  local data = {}
  for i = 1, 1000 do data[i] = i end
  local total = 0
  for k = 1, 300 do
    local sq = map(data, function(x) return x * k end)
    local ev = filter(sq, function(x) return x % 3 == 0 end)
    total = total + reduce(ev, function(a, b) return a + b end, 0)
    local c = counter()
    for _ = 1, 100 do c() end
    total = total + c()
  end
  return total
end

if arg then -- run by the C Lua driver: time work() after a warm-up run
  local runs = tonumber(arg[1]) or 5
  work()
  for _ = 1, runs do
    local t = os.clock()
    work()
    print(string.format("%.4f", (os.clock() - t) * 1000))
  end
end
return work
