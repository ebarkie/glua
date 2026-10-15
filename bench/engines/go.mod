module github.com/folbricht/glua/bench/engines

go 1.27

require (
	github.com/Liam0205/wangshu v0.2.0
	github.com/arnodel/golua v0.4.0
	github.com/folbricht/glua v0.0.0
	github.com/yuin/gopher-lua v1.1.2
)

require (
	github.com/arnodel/strftime v0.1.6 // indirect
	github.com/tetratelabs/wazero v1.12.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
)

replace github.com/folbricht/glua => ../..
