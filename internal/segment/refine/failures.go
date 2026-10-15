package refine

// The failures a refinement meets, classified, each with what to do about it (requirements S7):
// its prior is missing, its input is malformed, it would need more memory or storage than the
// computer has. A run's errors carry their class (jobs.Classified), so that the page says what
// went wrong in a sentence and what to try.

import (
	"errors"
	"fmt"
	"syscall"

	"situcraft/internal/jobs"
)

// Failure classes.
const (
	ClassPrior   = "prior"
	ClassInput   = "input"
	ClassMemory  = "memory"
	ClassStorage = "storage"
	ClassRegion  = "region"
)

// Failed is a refinement's classified failure.
type Failed struct {
	Class  string
	Remedy string
	Err    error
}

func (f *Failed) Error() string         { return f.Err.Error() }
func (f *Failed) Unwrap() error         { return f.Err }
func (f *Failed) Failure() jobs.Failure { return jobs.Failure{Class: f.Class, Remedy: f.Remedy} }

func fail(class, remedy string, format string, args ...any) error {
	return &Failed{Class: class, Remedy: remedy, Err: fmt.Errorf(format, args...)}
}

// Remedies, by class.
const (
	remedyPrior   = "The refinement starts from the instrument's cells and nuclei (cells.zarr.zip). Use the complete output bundle the instrument or Xenium Ranger wrote; its cells.zarr.zip may have been left behind when it was copied."
	remedyInput   = "The bundle's files disagree with one another. Check the bundle in the Data view; a bundle that validates without fatal problems can be refined."
	remedyMemory  = "Refine a region first (the ROI pilot preset), or use the Low memory preset; a computer with more memory can refine the whole tissue."
	remedyStorage = "Free space on the disk holding SituCraft's data folder, or move the data folder to a larger disk (--data-dir)."
	remedyRegion  = "Choose a region that holds cells and transcripts: frame it in the Viewer and add the region in view, or type its corners in µm."
)

// Classify classifies an error a refinement met: its own classified failures as they are, a full
// disk as storage, anything else unclassified (nil).
func Classify(err error) *Failed {
	var f *Failed
	if errors.As(err, &f) {
		return f
	}
	if errors.Is(err, syscall.ENOSPC) {
		return &Failed{Class: ClassStorage, Remedy: remedyStorage, Err: err}
	}
	return nil
}
