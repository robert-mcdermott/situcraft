package register

// patternSearch minimizes f from x by compass steps (Hooke R, Jeeves TA. "Direct search" solution of
// numerical and statistical problems. J ACM 1961;8(2):212–229): each coordinate in turn tried a step
// up and a step down, the better kept when f falls; when no coordinate's step helps, every step
// halves. It stops when the steps are below their least, or after maxEvals evaluations. It needs no
// gradient (mutual information has none to trust) and takes the same path on every computer.
func patternSearch(f func([]float64) float64, x, step, least []float64, maxEvals int) ([]float64, float64, int) {
	x = append([]float64(nil), x...)
	step = append([]float64(nil), step...)
	best := f(x)
	evals := 1
	for evals < maxEvals {
		improved := false
		for i := range x {
			for _, sign := range []float64{1, -1} {
				if evals >= maxEvals {
					break
				}
				y := append([]float64(nil), x...)
				y[i] += mul(sign, step[i])
				v := f(y)
				evals++
				if v < best {
					x, best, improved = y, v, true
					break
				}
			}
		}
		if improved {
			continue
		}
		done := true
		for i := range step {
			step[i] *= 0.5
			if step[i] >= least[i] {
				done = false
			}
		}
		if done {
			break
		}
	}
	return x, best, evals
}
