-- binary trees: table allocation
local function bottomUp(depth)
  if depth == 0 then return {} end
  depth = depth - 1
  return {bottomUp(depth), bottomUp(depth)}
end
local function check(tree)
  if tree[1] then return 1 + check(tree[1]) + check(tree[2]) end
  return 1
end
function work()
  local total = 0
  for d = 4, 10, 2 do
    local iters = 1 << (10 - d + 4)
    for _ = 1, iters do total = total + check(bottomUp(d)) end
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
