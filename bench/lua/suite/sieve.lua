-- the sieve of Eratosthenes: boolean arrays
function work()
  local count = 0
  for _ = 1, 20 do
    local n, flags = 50000, {}
    for i = 2, n do flags[i] = true end
    count = 0
    for i = 2, n do
      if flags[i] then
        count = count + 1
        for k = i + i, n, i do flags[k] = false end
      end
    end
  end
  return count
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
