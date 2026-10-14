-- load.
--== load a string
local f = load("return 1 + 2")
local g = load("local a, b = ... return a * b")
return f(), g(6, 7)
--== load with an environment
local env = {x = 5}
local f = load("y = x * 2 return y", "chunk", "t", env)
return f(), env.y, y
--== load uses the globals table by default
return load("return _ENV")() == _G, load("x = 1 return _ENV.x")() == 1
--== load from a reader function
local parts = {"return ", "4", "2", nil}
local i = 0
local f = load(function() i = i + 1 return parts[i] end)
return f()
--== load errors
local f1, e1 = load("return +")
local f2, e2 = load("x =", "=mychunk")
local f3, e3 = load("x =", "@file.lua")
local f4, e4 = load("return 1", "x", "b")
local f5, e5 = load(function() return {} end)
local f6, e6 = load("syntax error here\nmore")
return f1, e1, e2, e3, e4, e5, e6
--== runtime errors in loaded chunks name the chunk
local f = load("local t = nil\nreturn t.x", "=loaded")
local ok, e = pcall(f)
local g = load("error('boom')")
local ok2, e2 = pcall(g)
return e, e2
--== _ENV in loaded chunks
local f = load("_ENV = {z = 3} return z")
local g = load("local _ENV = ... return w", "w", "t")
return f(), g({w = 4})
--== load nil environment
local f = load("return x", "n", "t", nil)
return pcall(f)
