package label

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed starters/*.json
var starterFiles embed.FS

// Starter is a marker set SituCraft ships, for a panel.
type Starter struct {
	ID  string
	Set *MarkerSet
}

// Starters are the marker sets SituCraft ships, by id (the file's name), each checked.
func Starters() []Starter {
	entries, err := starterFiles.ReadDir("starters")
	if err != nil {
		panic(err)
	}
	var out []Starter
	for _, e := range entries {
		raw, err := starterFiles.ReadFile("starters/" + e.Name())
		if err != nil {
			panic(err)
		}
		s, err := Parse(raw)
		if err != nil {
			panic(fmt.Sprintf("starter %s: %v", e.Name(), err))
		}
		out = append(out, Starter{ID: strings.TrimSuffix(e.Name(), ".json"), Set: s})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// Genes are every marker a set names, once each, in order.
func (s *MarkerSet) Genes() []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range [][]Label{s.Labels, s.States} {
		for _, l := range list {
			for _, g := range append(append([]string(nil), l.Positive...), l.Negative...) {
				if !seen[strings.ToUpper(g)] {
					seen[strings.ToUpper(g)] = true
					out = append(out, g)
				}
			}
		}
	}
	return out
}

// Simulated is the marker set of a simulated tissue: each type a label at one level, its markers
// the genes the simulation made it express (types without markers left out).
func Simulated(types []string, markers [][]string) *MarkerSet {
	s := &MarkerSet{Format: Format, Version: 1, Name: "Simulated tissue's types",
		About:  "Each simulated type's own marker genes, as the simulation made them (situcraft-simulation.json): labels to check against the tissue's truth.",
		Levels: []string{"Type"}}
	for k, name := range types {
		if k < len(markers) && len(markers[k]) > 0 {
			s.Labels = append(s.Labels, Label{Name: name, Positive: append([]string(nil), markers[k]...)})
		}
	}
	return s
}
