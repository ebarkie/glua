-- building strings with table.concat and tostring
function work()
  local n = 0
  for _ = 1, 40 do
    local parts = {}
    for i = 1, 5000 do parts[#parts + 1] = tostring(i) end
    local s = table.concat(parts, ",")
    n = n + #s
  end
  return n
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
