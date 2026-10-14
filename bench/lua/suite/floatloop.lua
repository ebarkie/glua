-- float arithmetic in a numeric for
function work()
  local s = 0.0
  for i = 1, 1000000 do s = s + i * 1.5 end
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
