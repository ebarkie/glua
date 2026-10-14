//go:build lua_stackstress

package glua

// stackStress makes every growth check move the register stack and poison the old one, so
// that tests catch code that keeps a pointer into the stack across a call that may grow it.
const stackStress = true
