-- a JSON-like encoder: type, pairs, string building
local encode
local function encodeTable(t, out)
  if #t > 0 then
    out[#out + 1] = "["
    for i = 1, #t do if i > 1 then out[#out + 1] = "," end encode(t[i], out) end
    out[#out + 1] = "]"
  else
    local keys = {}
    for k in pairs(t) do keys[#keys + 1] = k end
    table.sort(keys)
    out[#out + 1] = "{"
    for i, k in ipairs(keys) do
      if i > 1 then out[#out + 1] = "," end
      out[#out + 1] = string.format("%q:", k)
      encode(t[k], out)
    end
    out[#out + 1] = "}"
  end
end
function encode(v, out)
  local tv = type(v)
  if tv == "table" then encodeTable(v, out)
  elseif tv == "string" then out[#out + 1] = string.format("%q", v)
  elseif tv == "number" then out[#out + 1] = tostring(v)
  elseif tv == "boolean" then out[#out + 1] = v and "true" or "false"
  else out[#out + 1] = "null" end
end
function work()
  local doc = {}
  for i = 1, 100 do
    doc[i] = {id = i, name = "item" .. i, tags = {"a", "b", "c"}, price = i * 1.25, active = i % 2 == 0,
      dims = {w = i, h = i * 2, d = 3}}
  end
  local n = 0
  for _ = 1, 30 do
    local out = {}
    encode(doc, out)
    n = n + #table.concat(out)
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
