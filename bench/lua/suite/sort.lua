-- table.sort with a comparator, and table.insert
function work()
  local seed, total = 42, 0
  for _ = 1, 1 do
    local t = {}
    for i = 1, 20000 do
      seed = (seed * 1103515245 + 12345) % 2147483648
      table.insert(t, {key = seed % 100000, id = i})
    end
    table.sort(t, function(a, b) if a.key ~= b.key then return a.key < b.key end return a.id < b.id end)
    total = total + t[1].key + t[#t].key + t[10000].id
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
