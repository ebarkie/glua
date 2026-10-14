-- gsub, upper, sub and find on text
function work()
  local text = string.rep("the quick brown fox jumps over the lazy dog ", 50)
  local n = 0
  for _ = 1, 150 do
    local s = text:gsub("%w+", function(w) return #w > 3 and w:upper() or w end)
    n = n + #s + (s:find("LAZY", 1, true) or 0)
    s = s:gsub("o", "0")
    n = n + #s:sub(10, 200)
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
