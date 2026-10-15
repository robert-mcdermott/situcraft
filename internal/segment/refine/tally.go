package refine

// Posterior summaries: over a run's last sweeps, how often each voxel had each label and each
// transcript lay in each cell, so that the segmentation reported is each voxel's most frequent
// label and each transcript's most frequent cell, with its posterior probability (the share of
// the samples), rather than any one sample, whose boundaries are as uncertain as the posterior.

// tallySlots is how many labels a voxel's or transcript's tally keeps; further ones are counted
// as overflow (rare: a voxel rarely changes among more than two cells and the background).
const tallySlots = 4

type tally struct {
	labels   [][tallySlots]uint32
	counts   [][tallySlots]uint16
	used     [][tallySlots]bool
	overflow int
	samples  int
}

func newTally(n int) *tally {
	return &tally{labels: make([][tallySlots]uint32, n), counts: make([][tallySlots]uint16, n), used: make([][tallySlots]bool, n)}
}

func (t *tally) add(i int, label uint32) {
	for k := 0; k < tallySlots; k++ {
		if t.used[i][k] && t.labels[i][k] == label {
			t.counts[i][k]++
			return
		}
		if !t.used[i][k] {
			t.used[i][k], t.labels[i][k], t.counts[i][k] = true, label, 1
			return
		}
	}
	t.overflow++
}

// mode is item i's most frequent label (ties to the lower) and its share of the samples.
func (t *tally) mode(i int) (uint32, float64) {
	var best uint32
	var bestN uint16
	for k := 0; k < tallySlots; k++ {
		if !t.used[i][k] {
			break
		}
		if n, l := t.counts[i][k], t.labels[i][k]; n > bestN || n == bestN && l < best {
			best, bestN = l, n
		}
	}
	if t.samples == 0 {
		return best, 0
	}
	return best, float64(bestN) / float64(t.samples)
}
