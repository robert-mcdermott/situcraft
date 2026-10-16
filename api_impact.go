package main

// What the choice of segmentation changes downstream (internal/compare, impact.go): two label sets,
// one of each segmentation a comparison compares, read through its differences. The page sends
// each set's labels (any version: from markers or a reference, imported, curated, consensus).

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"

	"situcraft/internal/compare"
	"situcraft/internal/segment"
	"situcraft/internal/xenium"
)

type impactSide struct {
	Labels  string   `json:"labels"`  // int32 per cell, little-endian, base64; −1 for none
	Covered string   `json:"covered"` // u8 per cell, base64: 1 where the set covers the cell
	Names   []string `json:"names"`
}

type impactRequest struct {
	A impactSide `json:"a"`
	B impactSide `json:"b"`
}

func (a *app) registerImpact(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/{index}/comparisons/{id}/impact", a.comparisonImpact)
}

func (s impactSide) decode(n int) (*compare.SideLabels, error) {
	label, err := decodeLE[int32](s.Labels, n)
	if err != nil {
		return nil, fmt.Errorf("the labels: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(s.Covered)
	if err != nil || len(raw) != n {
		return nil, fmt.Errorf("the cells covered: %d bytes for %d cells", len(raw), n)
	}
	covered := make([]bool, n)
	for i, v := range raw {
		covered[i] = v != 0
	}
	return &compare.SideLabels{Names: s.Names, Label: label, Covered: covered}, nil
}

// exclusivePairs are the marker pairs marker mixing is measured with: the comparison's (SituCraft's
// lineage markers, as far as the panel has them), else a simulated tissue's types' markers, each
// pair of two types'. Answers where they come from.
func exclusivePairs(b *xenium.Bundle, d *compare.Description) ([][2]string, string) {
	if len(d.MECRPairs) > 0 {
		return d.MECRPairs, "lineage markers"
	}
	set := simulatedSet(b)
	if set == nil {
		return nil, ""
	}
	var out [][2]string
	for i, l1 := range set.Labels {
		for _, l2 := range set.Labels[i+1:] {
			for _, g1 := range l1.Positive {
				for _, g2 := range l2.Positive {
					out = append(out, [2]string{g1, g2})
				}
			}
		}
	}
	return out, "the simulated types' markers"
}

// comparisonImpact answers two label sets read through a comparison (compare.Impact), with each
// segmentation's cells flagged as segmentation-sensitive (int32 per cell, base64: an index into
// others, or −1) and where the exclusive marker pairs come from.
func (a *app) comparisonImpact(w http.ResponseWriter, r *http.Request) {
	var body impactRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512<<20)).Decode(&body); err != nil || body.A.Labels == "" || body.B.Labels == "" {
		writeError(w, http.StatusBadRequest, "Send {\"a\": {\"labels\": int32 per cell in base64, \"covered\": u8 per cell in base64, \"names\"}, \"b\": the same}.")
		return
	}
	b, ok := a.openBundle(w, r)
	if !ok {
		return
	}
	defer b.Close()
	_, sides, groups, dir, ok := a.comparisonOf(w, r.PathValue("id"))
	if !ok {
		return
	}
	d, err := compare.ReadDescription(dir)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	var counts [2]*segment.Counts
	for k, id := range sides {
		sm, err := a.artifacts.Get(id)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		sdir, err := a.segmentationFolder(r.Context(), b, sm)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if counts[k], err = segment.ReadCounts(sdir); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
	}
	la, err := body.A.decode(len(counts[0].Offsets) - 1)
	if err != nil {
		writeError(w, http.StatusBadRequest, "A's "+err.Error()+".")
		return
	}
	lb, err := body.B.decode(len(counts[1].Offsets) - 1)
	if err != nil {
		writeError(w, http.StatusBadRequest, "B's "+err.Error()+".")
		return
	}
	pairs, source := exclusivePairs(b, d)
	im, err := compare.CompareLabels(groups, la, lb, counts[0], counts[1], pairs)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	enc := func(v []int32) string {
		out := make([]byte, 0, 4*len(v))
		for _, x := range v {
			out = binary.LittleEndian.AppendUint32(out, uint32(x))
		}
		return base64.StdEncoding.EncodeToString(out)
	}
	writeJSON(w, map[string]any{"a": sides[0], "b": sides[1], "impact": im, "pairs": source, "sensitiveA": enc(im.SensitiveA), "sensitiveB": enc(im.SensitiveB)})
}
