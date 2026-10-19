//go:build race

package validation

// raceDetector: the tests run with the race detector, which multiplies their time tenfold, so the
// error rates' study runs fewer cohorts under it.
const raceDetector = true
