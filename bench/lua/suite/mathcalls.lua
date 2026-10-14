-- calls to math library functions
function work()
  local s = 0
  for i = 1, 300000 do
    s = s + math.floor(i / 3) + math.max(i % 7, 3) + math.abs(-i) % 5 + math.sqrt(i) // 1
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
