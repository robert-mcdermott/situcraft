//go:build !race

package refine

// raceDetector: the tests run with the race detector, which multiplies memory and time.
const raceDetector = false
