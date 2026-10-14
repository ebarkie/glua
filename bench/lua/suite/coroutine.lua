-- generators with coroutine.wrap
local function range(n)
  return coroutine.wrap(function() for i = 1, n do coroutine.yield(i) end end)
end
function work()
  local s = 0
  for _ = 1, 50 do
    for v in range(2000) do s = s + v end
  end
  return s
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
