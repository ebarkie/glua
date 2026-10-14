-- local t = {x = 1}; local s = 0; for i = 1, n do s = s + t.x end; return s
local runs = tonumber(arg[1]) or 5
local function work()
  local t = {x = 1}
  local s = 0
  for i = 1, 1000000 do s = s + t.x end
  return s
end
work()
for _ = 1, runs do
  local t = os.clock()
  work()
  print(string.format("%.4f", (os.clock() - t) * 1000))
end
