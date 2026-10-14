# glua

A pure-Go Lua 5.4 for embedding small scripts in Go programs. Scripts are loaded once, frozen,
and then called from any goroutine as typed Go functions, without allocating per call.

Status: early development, not usable yet.

```go
import "github.com/folbricht/glua"
```

Requirements: Go 1.27 or later, a 64-bit platform. No cgo.

## License

BSD 3-Clause, see [LICENSE](LICENSE).
