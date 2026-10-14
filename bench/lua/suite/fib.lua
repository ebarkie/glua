-- recursive calls
local function fib(n) if n < 2 then return n end return fib(n - 1) + fib(n - 2) end
function work() return fib(27) end

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
