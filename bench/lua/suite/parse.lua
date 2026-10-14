-- parsing key=value lines with gmatch and match
function work()
  local lines = {}
  for i = 1, 200 do lines[i] = "user=u" .. i .. " path=/api/v1/item/" .. i .. " status=" .. (200 + i % 5) .. " ms=" .. i % 97 end
  local total = 0
  for _ = 1, 75 do
    for i = 1, #lines do
      local rec = {}
      for k, v in lines[i]:gmatch("(%w+)=(%S+)") do rec[k] = v end
      local id = rec.path:match("/item/(%d+)$")
      total = total + tonumber(id) + tonumber(rec.ms) + (rec.status == "200" and 1 or 0)
    end
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
