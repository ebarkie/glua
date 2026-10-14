-- varargs, select and table.unpack
local function sum(...)
  local s = 0
  for i = 1, select("#", ...) do s = s + (select(i, ...)) end
  return s
end
local function pack2(...) return {n = select("#", ...), ...} end
function work()
  local s = 0
  for i = 1, 40000 do
    s = s + sum(i, 2, 3, 4)
    local p = pack2(i, i + 1)
    s = s + p.n + table.unpack(p, 1, 1)
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
