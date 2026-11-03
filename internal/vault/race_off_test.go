//go:build !race

package vault

// raceEnabled reports whether the tests run under the race detector, which
// slows code 5-20x; timing assertions scale their limits by it.
const raceEnabled = false
