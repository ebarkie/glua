-- policy rules over request tables: string compares and set lookups
local admins = {alice = true, bob = true}
local blocked = {["/admin"] = true, ["/internal"] = true}
local function allow(req)
  if blocked[req.path] and not admins[req.user] then return false end
  if req.method ~= "GET" and req.method ~= "HEAD" and not admins[req.user] then return false end
  return req.path:sub(1, 5) == "/api/" or req.path == "/"
end
function work()
  local users, paths, methods = {"alice", "carol", "dave", "bob"}, {"/", "/api/x", "/admin", "/internal", "/web"}, {"GET", "POST", "HEAD"}
  local reqs = {}
  for i = 1, 60 do reqs[i] = {user = users[i % 4 + 1], path = paths[i % 5 + 1], method = methods[i % 3 + 1]} end
  local n = 0
  for _ = 1, 4000 do
    for i = 1, #reqs do if allow(reqs[i]) then n = n + 1 end end
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
