// Package glua is a pure-Go Lua 5.4 for small embedded scripts.
//
// A script is loaded once, frozen, and then called from any goroutine as a
// typed Go function. The package needs no cgo and no assembly, and supports
// 64-bit platforms only.
//
// Clients that prefer the shorter name can alias the import:
//
//	import lua "github.com/folbricht/glua"
package glua
