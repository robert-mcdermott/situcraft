package imports

// SituCraft's own segmentation files (docs/FORMATS.md), as `situcraft refine` writes them: already
// what an import makes, so they are checked against the dataset (the morphology image's grid, the
// transcripts' count) and copied, every file by its SHA-256. The segmentation check then checks
// them as any segmentation's.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"situcraft/internal/artifacts"
	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

// SituCraft is the format of SituCraft's own segmentation files.
const SituCraft = "situcraft"

// ownFiles are the files a SituCraft segmentation may hold, in the order they are copied.
var ownFiles = []string{segment.FileDescription, segment.FileCells, segment.FileMask, segment.FileAssignments, segment.FileCounts, segment.FileOutlines,
	segment.FileProbabilities, segment.FileDiagnostics, segment.FileLayers}

// copyOwn imports a folder of SituCraft's segmentation files made for the dataset.
func copyOwn(ctx context.Context, ds Dataset, opt Options, dir string, progress func(float64, string)) (*segment.Description, []artifacts.Finding, []SourceFile, error) {
	if len(opt.Paths) != 1 {
		return nil, nil, nil, fmt.Errorf("name the one folder of SituCraft's segmentation files")
	}
	from := opt.Paths[0]
	d, err := segment.ReadDescription(from)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%s is not a folder of SituCraft's segmentation files: %v", from, err)
	}
	w, h, ps := ds.Grid()
	if d.Width != w || d.Height != h || d.PixelSize != ps {
		return nil, nil, nil, fmt.Errorf("the segmentation is of a %d × %d image of %g µm pixels; %s's morphology image is %d × %d of %g µm", d.Width, d.Height, d.PixelSize, ds.Name(), w, h, ps)
	}
	progress(0.1, "Counting the dataset's transcripts")
	rows := 0
	if err := ds.Transcripts(func(b *xenium.TranscriptBatch) error {
		rows += b.Rows
		return ctx.Err()
	}); err != nil {
		return nil, nil, nil, err
	}
	if d.Transcripts != rows {
		return nil, nil, nil, fmt.Errorf("the segmentation assigns %d transcripts; %s has %d: it was made for another dataset", d.Transcripts, ds.Name(), rows)
	}
	progress(0.5, "Copying the segmentation's files")
	var files []SourceFile
	for _, name := range ownFiles {
		src := filepath.Join(from, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		f, err := copyFile(src, filepath.Join(dir, name))
		if err != nil {
			return nil, nil, nil, err
		}
		files = append(files, f)
	}
	if recipe := filepath.Join(from, "recipe.json"); fileExists(recipe) {
		f, err := hashOnly(recipe)
		if err != nil {
			return nil, nil, nil, err
		}
		files = append(files, f)
	}
	findings := []artifacts.Finding{{Severity: "info", Code: "situcraft-segmentation",
		Message: fmt.Sprintf("SituCraft's segmentation files (%s), made for %s's image and transcripts, copied as they are.", d.Method, ds.Name())}}
	return d, findings, files, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// copyFile copies a file, answering it by its SHA-256.
func copyFile(src, dst string) (SourceFile, error) {
	in, err := os.Open(src)
	if err != nil {
		return SourceFile{}, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return SourceFile{}, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return SourceFile{}, err
	}
	return SourceFile{Path: src, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

func hashOnly(p string) (SourceFile, error) {
	f, err := os.Open(p)
	if err != nil {
		return SourceFile{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return SourceFile{}, err
	}
	return SourceFile{Path: p, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
