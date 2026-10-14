-- classes: methods inherited through __index
local Point = {}
Point.__index = Point
function Point.new(x, y) return setmetatable({x = x, y = y}, Point) end
function Point:add(o) return Point.new(self.x + o.x, self.y + o.y) end
function Point:len2() return self.x * self.x + self.y * self.y end
local Point3 = setmetatable({}, {__index = Point})
Point3.__index = Point3
function Point3.new(x, y, z) local p = Point.new(x, y); p.z = z; return setmetatable(p, Point3) end
function Point3:len2() return Point.len2(self) + self.z * self.z end
function work()
  local acc, s = Point.new(0, 0), 0
  for i = 1, 50000 do
    local p = Point3.new(i, 1, 2)
    s = s + p:len2() % 7
    acc = acc:add(p)
  end
  return s + acc.x + acc.y
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
