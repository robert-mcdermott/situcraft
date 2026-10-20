//go:build race

package cache

// raceDetector: the tests run with the race detector, which multiplies their time tenfold or more.
const raceDetector = true
