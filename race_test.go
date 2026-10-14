//go:build race

package glua

// raceEnabled is set in race builds, where sync.Pool drops items on purpose and allocation
// counts mean nothing.
const raceEnabled = true
