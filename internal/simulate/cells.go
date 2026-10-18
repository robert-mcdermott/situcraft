package simulate

// A tissue's true cells alone (wave 6, slice 8): laid out as Run lays them out, a mosaic a tile at
// a time, each cell with its gene transcripts as drawn, without the images, transcripts and
// instrument a bundle carries: a tissue's cells at sizes no bundle need reach, to measure analyses
// of cells on.

import (
	"context"
	"errors"
	"sync"

	"situcraft/internal/random"
	"situcraft/internal/segment"
)

// TrueCells are a tissue's true cells, as a bundle's truth has them: each one's centroid (µm, in
// the whole tissue), its type (an index into Types) and its domain (numbered across a mosaic), and
// its gene transcripts of quality 20 or more (the molecules drawn for it that stay in the field),
// gene by gene, as a segmentation counts them.
type TrueCells struct {
	X, Y   []float32
	Type   []int32
	Domain []int32
	Types  []string
	Counts *segment.Counts
}

// Cells lays out p's tissue and draws its cells' transcripts, its fields (a mosaic's tiles) on up
// to workers at once, each field from its own streams: the cells of Run's truth, in its order.
func Cells(ctx context.Context, p Params, workers int, progress func(float64)) (*TrueCells, error) {
	if err := p.Check(); err != nil {
		return nil, err
	}
	if workers < 1 {
		workers = 1
	}
	if progress == nil {
		progress = func(float64) {}
	}
	across, down := p.tiles()
	tiles := across * down
	type field struct {
		x, y        []float32
		typ, domain []int32
		offsets     []uint32
		gene, count []uint32
		genes       int
		err         error
	}
	fields := make([]field, tiles)
	stop := func() bool { return ctx.Err() != nil }
	next := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	done := 0
	for w := 0; w < min(workers, tiles); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := range next {
				f := &fields[k]
				pk := p
				pk.part = 0
				if tiles > 1 {
					pk.TilesAcross, pk.TilesDown, pk.part = 0, 0, uint64(k)
				}
				t := newTissue(pk)
				layout := random.New(pk.seed(streamLayout), streamLayout)
				t.makeDomains(layout)
				t.placeCells(layout)
				if f.err = t.rasterize(stop); f.err != nil {
					continue
				}
				t.placeNuclei(layout)
				pn := makePanel(pk, random.New(pk.seed(streamPanel), streamPanel), t.typeShares())
				ps := pk.PixelUm
				ox, oy := mul(float64((k%across)*t.w), ps), mul(float64((k/across)*t.h), ps)
				f.genes = pn.genes
				f.offsets = []uint32{0}
				tally := make([]uint32, pn.genes)
				var mols []molecule
				for c := range t.cells {
					if c%256 == 0 && stop() {
						f.err = errStopped
						break
					}
					cl := &t.cells[c]
					var sx, sy float64
					for _, px := range cl.pixels {
						sx += float64(int(px)%t.w) + 0.5
						sy += float64(int(px)/t.w) + 0.5
					}
					n := float64(len(cl.pixels))
					f.x = append(f.x, float32(ox+sx/n*ps))
					f.y = append(f.y, float32(oy+sy/n*ps))
					f.typ = append(f.typ, int32(cl.typ))
					f.domain = append(f.domain, int32(k*p.Domains+cl.domain))
					mols = t.cellMolecules(c, pn, mols[:0])
					for _, m := range mols {
						if m.qv >= 20 {
							tally[m.feature]++
						}
					}
					for g, v := range tally {
						if v > 0 {
							f.gene, f.count = append(f.gene, uint32(g)), append(f.count, v)
							tally[g] = 0
						}
					}
					f.offsets = append(f.offsets, uint32(len(f.gene)))
				}
				mu.Lock()
				done++
				progress(float64(done) / float64(tiles))
				mu.Unlock()
			}
		}()
	}
	for k := 0; k < tiles && !stop(); k++ {
		next <- k
	}
	close(next)
	wg.Wait()
	if stop() {
		return nil, errStopped
	}
	out := &TrueCells{Counts: &segment.Counts{Offsets: []uint32{0}}}
	for _, ty := range p.Types {
		out.Types = append(out.Types, ty.Name)
	}
	for k := range fields {
		f := &fields[k]
		if f.err != nil {
			return nil, f.err
		}
		if k == 0 {
			for g := 0; g < f.genes; g++ {
				out.Counts.Genes = append(out.Counts.Genes, GeneName(g))
			}
		} else if f.genes != len(out.Counts.Genes) {
			return nil, errors.New("simulate: the fields' panels differ")
		}
		base := uint32(len(out.Counts.Gene))
		if uint64(base)+uint64(len(f.gene)) > 1<<32-1 {
			return nil, errors.New("simulate: more transcripts' counts than a count matrix holds")
		}
		out.X, out.Y = append(out.X, f.x...), append(out.Y, f.y...)
		out.Type, out.Domain = append(out.Type, f.typ...), append(out.Domain, f.domain...)
		out.Counts.Gene, out.Counts.Count = append(out.Counts.Gene, f.gene...), append(out.Counts.Count, f.count...)
		for _, o := range f.offsets[1:] {
			out.Counts.Offsets = append(out.Counts.Offsets, base+o)
		}
		fields[k] = field{}
	}
	return out, nil
}
