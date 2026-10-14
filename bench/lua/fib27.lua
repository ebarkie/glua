-- local function fib(n) if n < 2 then return n end return fib(n-1) + fib(n-2) end; return fib(27)
local runs = tonumber(arg[1]) or 5
local function work()
  local function fib(n) if n < 2 then return n end return fib(n-1) + fib(n-2) end
  return fib(27)
end
work()
for _ = 1, runs do
  local t = os.clock()
  work()
  print(string.format("%.4f", (os.clock() - t) * 1000))
end
