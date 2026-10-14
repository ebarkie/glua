-- string.format of log lines
function work()
  local n = 0
  for i = 1, 60000 do
    local s = string.format("%s [%5d] %-8s %.3f %x", "2026-10-14", i, "INFO", i / 7, i)
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
