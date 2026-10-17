//go:build !race

package nuclei

// raceDetector: the tests run with the race detector, which multiplies their time tenfold.
const raceDetector = false
