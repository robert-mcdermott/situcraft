package cohort

// The samples' panels compared (wave 7, slice 5): before any gene is compared across samples, which
// genes every panel has, which some panel lacks, and which are read differently (other codewords,
// another Ensembl ID). Genes are matched by name, as transcripts carry them.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Panel is a sample's gene panel as its bundle names it (gene_panel.json).
type Panel struct {
	Name, Design, Version string
	Genes                 []PanelGene
}

// PanelGene is one of a panel's genes: its name, its Ensembl ID and the codewords that read it.
type PanelGene struct {
	Name      string
	ID        string
	Codewords []int
}

// PanelRef names a panel: its name, design, version and genes.
type PanelRef struct {
	Name    string `json:"name"`
	Design  string `json:"design"`
	Version string `json:"version"`
	Genes   int    `json:"genes"`
}

// PanelDoc is one of the samples' panels: what it is, a checksum of its genes (their names, IDs
// and codewords), and the samples (by id) read with it.
type PanelDoc struct {
	PanelRef
	Checksum string   `json:"checksum"`
	Samples  []string `json:"samples"`
}

// PartialGene is a gene some panel lacks: its name, its ID (the first panel's that has it) and the
// panels (indices) that have it.
type PartialGene struct {
	Gene   string `json:"gene"`
	ID     string `json:"id,omitempty"`
	Panels []int  `json:"panels"`
}

// CodewordGene is a gene every panel has, read by other codewords in some: its codewords by panel.
type CodewordGene struct {
	Gene      string  `json:"gene"`
	Codewords [][]int `json:"codewords"`
}

// IDGene is a gene named alike in every panel with other Ensembl IDs (IDs by panel), or one ID
// under other names (Names by panel).
type IDGene struct {
	Gene  string   `json:"gene,omitempty"`
	IDs   []string `json:"ids,omitempty"`
	ID    string   `json:"id,omitempty"`
	Names []string `json:"names,omitempty"`
}

// PanelsDoc is the samples' panels compared: the panels (distinct by their genes, in the order
// the samples first use them), the genes every panel has (Shared, in the first panel's order),
// those some panel lacks, those read by other codewords, and those whose IDs or names differ.
type PanelsDoc struct {
	Panels    []PanelDoc     `json:"panels"`
	Shared    []string       `json:"shared"`
	Partial   []PartialGene  `json:"partial"`
	Codewords []CodewordGene `json:"codewords"`
	IDs       []IDGene       `json:"ids"`
}

// checksum names a panel by its genes: each one's name, ID and codewords, in order.
func (p *Panel) checksum() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%s\n%s\n", p.Name, p.Design, p.Version)
	for _, g := range p.Genes {
		fmt.Fprintf(h, "%s\t%s\t%v\n", g.Name, g.ID, g.Codewords)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ComparePanels compares the samples' panels (samples[k]'s is panels[k]).
func ComparePanels(samples []string, panels []Panel) *PanelsDoc {
	out := &PanelsDoc{Panels: []PanelDoc{}, Shared: []string{}, Partial: []PartialGene{}, Codewords: []CodewordGene{}, IDs: []IDGene{}}
	var distinct []*Panel
	index := map[string]int{}
	for k := range panels {
		p := &panels[k]
		sum := p.checksum()
		i, ok := index[sum]
		if !ok {
			i = len(distinct)
			index[sum] = i
			distinct = append(distinct, p)
			out.Panels = append(out.Panels, PanelDoc{PanelRef: PanelRef{Name: p.Name, Design: p.Design, Version: p.Version, Genes: len(p.Genes)}, Checksum: sum, Samples: []string{}})
		}
		out.Panels[i].Samples = append(out.Panels[i].Samples, samples[k])
	}
	P := len(distinct)
	if P == 0 {
		return out
	}
	// Each panel's genes by name.
	byName := make([]map[string]PanelGene, P)
	for i, p := range distinct {
		byName[i] = map[string]PanelGene{}
		for _, g := range p.Genes {
			if _, seen := byName[i][g.Name]; !seen {
				byName[i][g.Name] = g
			}
		}
	}
	// Every gene in the order first met (the first panel's, then the next's new ones).
	var order []string
	seen := map[string]bool{}
	for _, p := range distinct {
		for _, g := range p.Genes {
			if !seen[g.Name] {
				seen[g.Name] = true
				order = append(order, g.Name)
			}
		}
	}
	for _, name := range order {
		var have []int
		var first PanelGene
		for i := range distinct {
			if g, ok := byName[i][name]; ok {
				if len(have) == 0 {
					first = g
				}
				have = append(have, i)
			}
		}
		if len(have) < P {
			out.Partial = append(out.Partial, PartialGene{Gene: name, ID: first.ID, Panels: have})
			continue
		}
		out.Shared = append(out.Shared, name)
		codewords := make([][]int, P)
		ids := make([]string, P)
		sameCodewords, sameIDs := true, true
		for i := range distinct {
			g := byName[i][name]
			codewords[i] = sortedInts(g.Codewords)
			ids[i] = g.ID
			if i > 0 && fmt.Sprint(codewords[i]) != fmt.Sprint(codewords[0]) {
				sameCodewords = false
			}
			if i > 0 && ids[i] != ids[0] {
				sameIDs = false
			}
		}
		if !sameCodewords {
			out.Codewords = append(out.Codewords, CodewordGene{Gene: name, Codewords: codewords})
		}
		if !sameIDs {
			out.IDs = append(out.IDs, IDGene{Gene: name, IDs: ids})
		}
	}
	// An ID in every panel under other names: a gene renamed between them.
	if P > 1 {
		names := map[string][]string{}
		var ids []string
		for i := range distinct {
			for _, g := range distinct[i].Genes {
				if g.ID == "" {
					continue
				}
				if _, ok := names[g.ID]; !ok {
					names[g.ID] = make([]string, P)
					ids = append(ids, g.ID)
				}
				if names[g.ID][i] == "" {
					names[g.ID][i] = g.Name
				}
			}
		}
		for _, id := range ids {
			n := names[id]
			all, alike := true, true
			for i := range n {
				if n[i] == "" {
					all = false
				}
				if n[i] != n[0] {
					alike = false
				}
			}
			if all && !alike {
				out.IDs = append(out.IDs, IDGene{ID: id, Names: n})
			}
		}
	}
	return out
}

func sortedInts(v []int) []int {
	out := append([]int{}, v...)
	sort.Ints(out)
	return out
}

// PanelWords says the comparison in a sentence: "one panel" or "2 panels: 241 genes in every one,
// 87 in some".
func (d *PanelsDoc) PanelWords() string {
	if len(d.Panels) <= 1 {
		return "one panel"
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("%d panels: %d genes in every one", len(d.Panels), len(d.Shared)))
	if len(d.Partial) > 0 {
		parts = append(parts, fmt.Sprintf("%d in some", len(d.Partial)))
	}
	return strings.Join(parts, ", ")
}
