package export

// The inputs of Xenium Ranger's import-segmentation (10x Genomics), for those who have it
// installed: SituCraft prepares them and the command, and does not run it. Its image-based mode
// takes label masks on the morphology image's pixels, as 32-bit TIFF or NumPy files: the cells
// (each pixel the cell's label, 0 for none) and the nuclei. SituCraft writes
//
//	cells.npy        the segmentation's label raster (label k is row k of cell_labels.csv)
//	nuclei.npy       the dataset's nucleus mask, as the bundle holds it
//	cell_labels.csv  label, cell_id: each label's cell in SituCraft
//	command.sh       the command, with the files' places
//
// NumPy's format (.npy version 1.0): the magic "\x93NUMPY", the version, the header's length and
// a Python dictionary describing the array (here little-endian uint32, C order, rows × columns),
// padded so that the data start at a multiple of 64 bytes; then the elements.
//
// Before the command is prepared, the inputs are checked: the masks' size against the
// morphology image's, cells the raster gives no pixel (Xenium Ranger cannot keep them),
// transcripts this segmentation assigns otherwise than its raster holds at their pixel (Xenium
// Ranger assigns them by the mask), a dataset without nuclei, and a bundle Xenium Ranger cannot
// read in place (a zip).

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"situcraft/internal/xenium"
)

// npyHeader is the header of a .npy file of a height × width uint32 array.
func npyHeader(height, width int) []byte {
	dict := fmt.Sprintf("{'descr': '<u4', 'fortran_order': False, 'shape': (%d, %d), }", height, width)
	// magic (6) + version (2) + length (2) + dict + padding + newline, a multiple of 64.
	total := 10 + len(dict) + 1
	pad := (64 - total%64) % 64
	header := []byte("\x93NUMPY\x01\x00")
	header = binary.LittleEndian.AppendUint16(header, uint16(len(dict)+pad+1))
	header = append(header, dict...)
	header = append(header, strings.Repeat(" ", pad)...)
	return append(header, '\n')
}

// writeNPY writes a height × width uint32 array, its rows given by rows(y0, y1, dst).
func writeNPY(path string, width, height int, rows func(y0, y1 int, dst []uint32) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	w.Write(npyHeader(height, width))
	const band = 256
	labels := make([]uint32, band*width)
	raw := make([]byte, 4*width)
	for y0 := 0; y0 < height && err == nil; y0 += band {
		y1 := min(y0+band, height)
		if err = rows(y0, y1, labels[:(y1-y0)*width]); err != nil {
			break
		}
		for y := 0; y < y1-y0; y++ {
			for x, v := range labels[y*width : (y+1)*width] {
				binary.LittleEndian.PutUint32(raw[4*x:], v)
			}
			w.Write(raw)
		}
	}
	if e := w.Flush(); err == nil {
		err = e
	}
	if e := f.Close(); err == nil {
		err = e
	}
	return err
}

// ReadNPY reads a .npy file of a two-dimensional little-endian uint32 array (as writeNPY writes).
func ReadNPY(path string) (width, height int, values []uint32, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, nil, err
	}
	if len(raw) < 10 || string(raw[:6]) != "\x93NUMPY" {
		return 0, 0, nil, fmt.Errorf("%s is not a NumPy file", filepath.Base(path))
	}
	n := int(binary.LittleEndian.Uint16(raw[8:]))
	if 10+n > len(raw) {
		return 0, 0, nil, fmt.Errorf("%s ends in its header", filepath.Base(path))
	}
	dict := string(raw[10 : 10+n])
	m := regexp.MustCompile(`'descr': '<u4', 'fortran_order': False, 'shape': \((\d+), (\d+)\)`).FindStringSubmatch(dict)
	if m == nil {
		return 0, 0, nil, fmt.Errorf("%s is not a 2-D uint32 array in C order: %s", filepath.Base(path), strings.TrimSpace(dict))
	}
	height, _ = strconv.Atoi(m[1])
	width, _ = strconv.Atoi(m[2])
	data := raw[10+n:]
	if len(data) != 4*width*height {
		return 0, 0, nil, fmt.Errorf("%s holds %d bytes for %d × %d values", filepath.Base(path), len(data), height, width)
	}
	values = make([]uint32, width*height)
	for i := range values {
		values[i] = binary.LittleEndian.Uint32(data[4*i:])
	}
	return width, height, values, nil
}

var notInID = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// rangerID is a run name Xenium Ranger accepts (letters, digits, underscore, dash, period).
func rangerID(dataset, segmentation string) string {
	id := strings.Trim(notInID.ReplaceAllString(dataset+"-"+segmentation, "-"), "-.")
	if len(id) > 64 {
		id = strings.TrimRight(id[:64], "-.")
	}
	if id == "" {
		id = "situcraft-segmentation"
	}
	return id
}

// shellQuote quotes a word for a POSIX shell.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func writeRanger(ctx context.Context, d *data, stage, final string, step Progress) (*Ranger, []Finding, error) {
	dir := filepath.Join(stage, "xenium-ranger")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, err
	}
	var findings []Finding
	find := func(severity, code, message string, count int) {
		findings = append(findings, Finding{Format: XeniumRanger, Severity: severity, Code: code, Message: message, Count: count})
	}
	r, err := d.loadRaster()
	if err != nil {
		return nil, nil, err
	}
	if iw, ih := d.ds.ImageSize(); iw == 0 {
		find("warning", "ranger.image-size", "The morphology image's size is unknown, so the masks could not be checked against it: Xenium Ranger needs them the same size.", 0)
	} else if iw != r.Width || ih != r.Height {
		find("fatal", "ranger.image-size", fmt.Sprintf("The label mask is %d × %d pixels and the morphology image %d × %d: Xenium Ranger needs them the same size.", r.Width, r.Height, iw, ih), 0)
	}
	step(0.05, "Writing the cells' mask")
	err = writeNPY(filepath.Join(dir, "cells.npy"), r.Width, r.Height, func(y0, y1 int, dst []uint32) error {
		for y := y0; y < y1; y++ {
			r.Row(y, dst[(y-y0)*r.Width:(y-y0+1)*r.Width])
		}
		return ctx.Err()
	})
	if err != nil {
		return nil, nil, err
	}
	stats := r.Stats(len(d.cells))
	empty := 0
	for _, s := range stats {
		if s.Pixels == 0 {
			empty++
		}
	}
	if empty > 0 {
		find("warning", "ranger.cells-without-pixels", fmt.Sprintf("%d cells have no pixel in the mask (they hold transcripts but no area): Xenium Ranger cannot keep them.", empty), empty)
	}

	step(0.4, "Writing the nuclei's mask")
	nuclei := false
	mask, err := d.ds.NucleusMask()
	if err != nil {
		return nil, nil, err
	}
	if mask == nil {
		find("warning", "ranger.no-nuclei", "The dataset has no nucleus mask, so the command names the cells alone; some versions of Xenium Ranger need the nuclei with the cells.", 0)
	} else {
		w, h := mask.Size()
		if w != r.Width || h != r.Height {
			mask.Close()
			find("fatal", "ranger.nuclei-size", fmt.Sprintf("The nucleus mask is %d × %d pixels and the cells' %d × %d.", w, h, r.Width, r.Height), 0)
		} else {
			err = writeNPY(filepath.Join(dir, "nuclei.npy"), w, h, func(y0, y1 int, dst []uint32) error {
				if err := mask.Rows(y0, y1, dst); err != nil {
					return err
				}
				return ctx.Err()
			})
			mask.Close()
			if err != nil {
				return nil, nil, err
			}
			nuclei = true
		}
	}

	step(0.6, "Checking the transcripts against the mask")
	at, differ, genes := 0, 0, 0
	err = d.ds.Transcripts(func(b *xenium.TranscriptBatch) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		for i := 0; i < b.Rows && at+i < len(d.assign); i++ {
			if r.AtMicrons(float64(b.X[i]), float64(b.Y[i])) != d.assign[at+i] {
				differ++
				if b.QV[i] >= 20 && xenium.FeatureKind(string(b.Feature[i])) == xenium.KindGene {
					genes++
				}
			}
		}
		at += b.Rows
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if differ > 0 {
		find("warning", "ranger.assigned-otherwise", fmt.Sprintf("%d transcripts (%s; %d gene transcripts of quality 20 or more) are assigned by this segmentation otherwise than its mask holds at their pixel. Xenium Ranger assigns transcripts by the mask, so it will count them otherwise.", differ, percent(differ, at), genes), differ)
	}
	if d.ds.BundleFolder() == "" {
		find("warning", "ranger.bundle-zipped", "The dataset's bundle is a zip: Xenium Ranger reads a bundle's folder. Unzip it and give that folder as --xenium-bundle.", 0)
	}

	// Each label's cell.
	f, err := os.Create(filepath.Join(dir, "cell_labels.csv"))
	if err != nil {
		return nil, nil, err
	}
	cw := csv.NewWriter(f)
	cw.Write([]string{"label", "cell_id"})
	for i, c := range d.cells {
		cw.Write([]string{strconv.Itoa(i + 1), c.ID})
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		f.Close()
		return nil, nil, err
	}
	if err := f.Close(); err != nil {
		return nil, nil, err
	}

	// The command, with the files where the export will be.
	where := filepath.Join(final, "xenium-ranger")
	bundle := d.ds.BundleFolder()
	if bundle == "" {
		bundle = "/path/to/the/unzipped/bundle"
	}
	rg := &Ranger{ID: rangerID(d.ds.Name(), d.seg.Name)}
	rg.Args = []string{"xeniumranger", "import-segmentation", "--id=" + rg.ID, "--xenium-bundle=" + bundle}
	if nuclei {
		rg.Args = append(rg.Args, "--nuclei="+filepath.Join(where, "nuclei.npy"))
	}
	rg.Args = append(rg.Args, "--cells="+filepath.Join(where, "cells.npy"))
	quoted := make([]string, len(rg.Args))
	for i, a := range rg.Args {
		if k := strings.IndexByte(a, '='); k > 0 && strings.HasPrefix(a, "--") {
			quoted[i] = a[:k+1] + shellQuote(a[k+1:])
		} else {
			quoted[i] = shellQuote(a)
		}
	}
	rg.Command = strings.Join(quoted, " ")
	var sh strings.Builder
	fmt.Fprintf(&sh, "#!/bin/sh\n# Xenium Ranger's import-segmentation with %s of %s, prepared by %s.\n", d.seg.Name, d.ds.Name(), d.software)
	sh.WriteString("# SituCraft does not run it: run it where Xenium Ranger (10x Genomics) is installed.\n#\n")
	sh.WriteString("#   cells.npy        the cells: each pixel's label, k for row k of cell_labels.csv, 0 for none,\n")
	sh.WriteString("#                    on the morphology image's pixels\n")
	if nuclei {
		sh.WriteString("#   nuclei.npy       the bundle's own nucleus mask\n")
	}
	sh.WriteString("#   cell_labels.csv  each label's cell id in SituCraft\n#\n")
	sh.WriteString("# Xenium Ranger names the cells its own way, assigns each transcript by the mask's pixels, and\n")
	sh.WriteString("# writes a new output bundle into a folder named by --id, where it is run. --localcores and\n")
	sh.WriteString("# --localmem set the cores and memory (GB) it uses.\n")
	for _, f := range findings {
		if f.Severity != "info" {
			fmt.Fprintf(&sh, "#\n# %s: %s\n", strings.ToUpper(f.Severity[:1])+f.Severity[1:], f.Message)
		}
	}
	sh.WriteString("\n" + quoted[0] + " " + quoted[1])
	for _, q := range quoted[2:] {
		sh.WriteString(" \\\n  " + q)
	}
	sh.WriteString("\n")
	if err := os.WriteFile(filepath.Join(dir, "command.sh"), []byte(sh.String()), 0o755); err != nil {
		return nil, nil, err
	}
	return rg, findings, nil
}

func percent(n, of int) string {
	if of == 0 {
		return "0%"
	}
	return strconv.FormatFloat(100*float64(n)/float64(of), 'f', 2, 64) + "%"
}
