-- Error cases; each one must fail with C Lua's message, position included.
--== index a nil global
return x.y
--== index a nil field
local t = {}
return t.a.b
--== index a nil local
local a
return a.x
--== index a nil upvalue
local u
local function f() return u.x end
return f()
--== index with a computed key
local t = {}
local k = "key"
return t[k].z
--== index with a constant key through a register
local t = {}
return t["a b"].c
--== integer index
local t = {}
return t[1].x
--== assign to a field of nil
local t
t.x = 1
--== assign to a global's field
g.x = 1
--== call a nil global
undefinedfn()
--== call a nil field
local t = {}
t.go()
--== call a nil method
local t = {}
t:method()
--== call a number
local n = 1
n()
--== arithmetic on nil
local a
return a + 1
--== arithmetic on a field
local t = {}
return 1 + t.x
--== arithmetic on a table
return {} * 2
--== arithmetic on a non-numeric string
local s = "abc"
return s + 1
--== unary minus on nil
local x
return -x
--== length of nil
local x
return #x
--== length of a number
return #5
--== bitwise on a float
return 1.5 | 0
--== bitwise on nil
local z
return z & 1
--== concatenate nil
local x
return "a" .. x
--== concatenate a table global
t = {}
return t .. "a"
--== concatenate both bad
local a, b = {}, {}
return a .. b
--== compare number with nil
return 1 < nil
--== compare two tables
return {} < {}
--== compare string with number
return "1" <= 1
--== integer division by zero
local z = 0
return 1 // z
--== modulo by zero
local z = 0
return 1 % z
--== for loop with a string start
for i = "a", 2 do end
--== for loop with a nil limit
for i = 1, nil do end
--== for loop with a zero step
for i = 1, 10, 0 do end
--== error on a later line
local x = 1
local y = 2

return x + nil
--== nil in a nested function
local function outer()
  local function inner()
    return missing.field
  end
  return inner()
end
return outer()
--== generic for over nil
for k in nil do end
--== global in a field chain
return a.b.c
--== upvalue arithmetic
local n
return (function() return n * 2 end)()
--== index a boolean through a global table
cfg = {debug = true}
return cfg.debug.level
--== bitwise on a numeric string
return "3" & 1
--== bitwise not on a string
local s = "1"
return ~s
--== float for loop with a bad step
for i = 1.0, 2, "x" do end
--== call a table without __call
local t = {}
t()
--== call through a __call that is not callable
local t = setmetatable({}, {__call = 1})
t()
--== stack overflow
local function f() return 1 + f() end
return f()
--== assign with a nil key
local t, k = {}, nil
t[k] = 1
