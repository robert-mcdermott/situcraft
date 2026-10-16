// Package label labels cells from marker genes: a marker set is a hierarchy of labels (a class,
// then a lineage, a subtype…) and states, each with positive and negative markers; each cell's
// score for each label is its markers' expression against control genes of matched expression
// (Tirosh I, et al. Dissecting the multicellular ecosystem of metastatic melanoma by single-cell
// RNA-seq. Science 2016;352:189–196, doi:10.1126/science.aad0501), computed as scanpy's
// score_genes computes it; labels are assigned from the top of the hierarchy down, a level only
// when its best label leads the next by a margin.
package label

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Format names a marker set's file.
const Format = "situcraft-markers"

// Limits of a marker set.
const (
	MaxLevels = 6
	MaxLabels = 500
	MaxStates = 50
	MaxGenes  = 200 // positive or negative markers of one label
)

// Reference is a published source of a set's markers.
type Reference struct {
	DOI      string `json:"doi"`
	Citation string `json:"citation"`
}

// Label is one label (or state) of a marker set: its name, its parent (a label of the level above;
// for a state, the label whose cells alone can carry it), its positive and negative markers, and
// the references (DOIs among the set's) its markers come from.
type Label struct {
	Name       string   `json:"name"`
	Parent     string   `json:"parent,omitempty"`
	Positive   []string `json:"positive"`
	Negative   []string `json:"negative,omitempty"`
	References []string `json:"references,omitempty"`
}

// MarkerSet is a marker set's file: its name, what it is for, the names of its levels (the first
// the top), its labels (each after its parent), its states, and its references.
type MarkerSet struct {
	Format     string      `json:"format"`
	Version    int         `json:"version"`
	Name       string      `json:"name"`
	About      string      `json:"about,omitempty"`
	Panel      string      `json:"panel,omitempty"`
	Levels     []string    `json:"levels"`
	Labels     []Label     `json:"labels"`
	States     []Label     `json:"states,omitempty"`
	References []Reference `json:"references,omitempty"`
}

// Parse reads a marker set's file and checks it.
func Parse(raw []byte) (*MarkerSet, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var s MarkerSet
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("the marker set is not a marker set's JSON: %w", err)
	}
	if err := s.Check(); err != nil {
		return nil, err
	}
	return &s, nil
}

// Check says what is wrong with a marker set, if anything. Names are trimmed and must be unique
// regardless of case; a label's parent comes before it; genes are listed once per label and not
// both positive and negative.
func (s *MarkerSet) Check() error {
	if s.Format != Format {
		return fmt.Errorf("the file's format is %q, not %q", s.Format, Format)
	}
	if s.Version != 1 {
		return fmt.Errorf("the marker set's version is %d; this SituCraft reads version 1", s.Version)
	}
	if s.Name = strings.TrimSpace(s.Name); s.Name == "" || len(s.Name) > 200 {
		return errors.New("the marker set needs a name (at most 200 characters)")
	}
	if len(s.Levels) == 0 || len(s.Levels) > MaxLevels {
		return fmt.Errorf("the marker set names %d levels; it needs 1 to %d", len(s.Levels), MaxLevels)
	}
	seen := map[string]bool{}
	for i, l := range s.Levels {
		s.Levels[i] = strings.TrimSpace(l)
		if s.Levels[i] == "" || seen[strings.ToLower(s.Levels[i])] {
			return fmt.Errorf("level %d needs a name of its own", i+1)
		}
		seen[strings.ToLower(s.Levels[i])] = true
	}
	if len(s.Labels) == 0 || len(s.Labels) > MaxLabels {
		return fmt.Errorf("the marker set has %d labels; it needs 1 to %d", len(s.Labels), MaxLabels)
	}
	if len(s.States) > MaxStates {
		return fmt.Errorf("the marker set has %d states; at most %d", len(s.States), MaxStates)
	}
	dois := map[string]bool{}
	for i := range s.References {
		r := &s.References[i]
		r.DOI, r.Citation = strings.TrimSpace(r.DOI), strings.TrimSpace(r.Citation)
		if r.DOI == "" || r.Citation == "" || dois[strings.ToLower(r.DOI)] {
			return fmt.Errorf("reference %d needs a DOI of its own and a citation", i+1)
		}
		dois[strings.ToLower(r.DOI)] = true
	}
	names := map[string]bool{}
	depth := map[string]int{}
	check := func(l *Label, what string) error {
		if l.Name = strings.TrimSpace(l.Name); l.Name == "" || len(l.Name) > 100 {
			return fmt.Errorf("a %s needs a name (at most 100 characters)", what)
		}
		if names[strings.ToLower(l.Name)] {
			return fmt.Errorf("two labels or states are named %q", l.Name)
		}
		names[strings.ToLower(l.Name)] = true
		l.Parent = strings.TrimSpace(l.Parent)
		if len(l.Positive) == 0 || len(l.Positive) > MaxGenes || len(l.Negative) > MaxGenes {
			return fmt.Errorf("%s %q needs 1 to %d positive markers and at most %d negative", what, l.Name, MaxGenes, MaxGenes)
		}
		genes := map[string]string{}
		for _, list := range []struct {
			genes []string
			sign  string
		}{{l.Positive, "positive"}, {l.Negative, "negative"}} {
			for i, g := range list.genes {
				g = strings.TrimSpace(g)
				list.genes[i] = g
				if g == "" {
					return fmt.Errorf("%s %q lists an empty gene", what, l.Name)
				}
				if was, ok := genes[strings.ToUpper(g)]; ok {
					if was == list.sign {
						return fmt.Errorf("%s %q lists %s twice", what, l.Name, g)
					}
					return fmt.Errorf("%s %q lists %s as both a positive and a negative marker", what, l.Name, g)
				}
				genes[strings.ToUpper(g)] = list.sign
			}
		}
		for i, d := range l.References {
			l.References[i] = strings.TrimSpace(d)
			if !dois[strings.ToLower(l.References[i])] {
				return fmt.Errorf("%s %q cites %q, which is not among the set's references", what, l.Name, d)
			}
		}
		return nil
	}
	for i := range s.Labels {
		l := &s.Labels[i]
		if err := check(l, "label"); err != nil {
			return err
		}
		d := 0
		if l.Parent != "" {
			p, ok := depth[strings.ToLower(l.Parent)]
			if !ok {
				return fmt.Errorf("label %q's parent %q is not a label listed before it", l.Name, l.Parent)
			}
			d = p + 1
		}
		if d >= len(s.Levels) {
			return fmt.Errorf("label %q is at level %d; the set names %d levels", l.Name, d+1, len(s.Levels))
		}
		depth[strings.ToLower(l.Name)] = d
	}
	for i := range s.States {
		l := &s.States[i]
		if err := check(l, "state"); err != nil {
			return err
		}
		if _, ok := depth[strings.ToLower(l.Parent)]; l.Parent != "" && !ok {
			return fmt.Errorf("state %q is of %q, which is not a label of the set", l.Name, l.Parent)
		}
	}
	return nil
}

// Canonical is the set's file as SituCraft writes it: compact JSON, its fields in order.
func (s *MarkerSet) Canonical() []byte {
	raw, _ := json.Marshal(s)
	return raw
}

// Checksum is the SHA-256 of the canonical file: the same set, the same checksum.
func (s *MarkerSet) Checksum() string {
	h := sha256.Sum256(s.Canonical())
	return hex.EncodeToString(h[:])
}

// MissingError lists the markers a panel does not have, by label.
type MissingError struct {
	Missing map[string][]string // label or state → genes
}

func (e *MissingError) Error() string {
	var labels []string
	for l := range e.Missing {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	var parts []string
	for _, l := range labels {
		parts = append(parts, l+": "+strings.Join(e.Missing[l], ", "))
	}
	return "markers not in the panel (" + strings.Join(parts, "; ") + ")"
}

// Set is a label or a state resolved against a panel: its level (−1 for a state), its parent (an
// index among the labels, −1 for none), and its markers as the panel's gene indices.
type Set struct {
	Name     string
	Level    int
	Parent   int
	State    bool
	Positive []int
	Negative []int
}

// Resolve matches a set's markers to a panel's genes (exactly, else regardless of case when one
// gene matches): the labels first, then the states. Markers the panel lacks are refused, listed.
func (s *MarkerSet) Resolve(genes []string) ([]Set, error) {
	exact := map[string]int{}
	folded := map[string][]int{}
	for i, g := range genes {
		exact[g] = i
		folded[strings.ToUpper(g)] = append(folded[strings.ToUpper(g)], i)
	}
	missing := map[string][]string{}
	find := func(owner string, names []string) []int {
		var out []int
		for _, n := range names {
			if i, ok := exact[n]; ok {
				out = append(out, i)
			} else if f := folded[strings.ToUpper(n)]; len(f) == 1 {
				out = append(out, f[0])
			} else {
				missing[owner] = append(missing[owner], n)
			}
		}
		return out
	}
	index := map[string]int{}
	var out []Set
	for i, l := range s.Labels {
		index[strings.ToLower(l.Name)] = i
		set := Set{Name: l.Name, Parent: -1, Positive: find(l.Name, l.Positive), Negative: find(l.Name, l.Negative)}
		if l.Parent != "" {
			set.Parent = index[strings.ToLower(l.Parent)]
			set.Level = out[set.Parent].Level + 1
		}
		out = append(out, set)
	}
	for _, l := range s.States {
		set := Set{Name: l.Name, Level: -1, Parent: -1, State: true, Positive: find(l.Name, l.Positive), Negative: find(l.Name, l.Negative)}
		if l.Parent != "" {
			set.Parent = index[strings.ToLower(l.Parent)]
		}
		out = append(out, set)
	}
	if len(missing) > 0 {
		return nil, &MissingError{Missing: missing}
	}
	return out, nil
}
