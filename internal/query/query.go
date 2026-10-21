// Package query is SituCraft's typed filter tree (wave 9, slice 3; requirement A3): which cells, or
// which transcripts, a question is about, said as JSON (all, any and not of conditions, each of a
// known kind with typed fields), checked before anything is read and evaluated over a dataset's
// columns. Nothing in a tree is executed: it names attributes, genes, label sets, clusterings,
// niches, regions and saved filters, which the program reads and compares with the bounds and
// values the tree gives. The page's saved filters are the same tree (their conditions under all),
// and agents, scripts and the page ask the program to evaluate one.
//
// A tree of cells:
//
//	{"all": [...]}, {"any": [...]}, {"not": {...}}
//	{"attribute": "transcripts", "min": 10, "max": 500}   a QC attribute, bounds included
//	{"attribute": "method", "in": ["Cell boundary", 2]}    the instrument's method, by name or number
//	{"gene": "EPCAM", "min": 2}                            the cell's Q20 transcripts of a gene (min 1 if no bound)
//	{"labels": "lab-…", "level": 0, "in": ["T cells"]}     a label set's labels at a level (0 the first, or its name)
//	{"clusters": "clu-…", "in": [3, "Tumor"]}              a clustering's clusters, by number from 1 or name
//	{"niches": "spa-…", "in": [1, 2]}                      a niches analysis's niches, by number from 1
//	{"region": "Tumor nest"}                               its centroid inside a region (by name or id)
//	{"distance": "Tumor nest", "min": -20, "max": 40}      its centroid's signed distance from a region's edge (µm, inside negative)
//	{"box": [x0, y0, x1, y1]}                              its centroid inside a rectangle (µm)
//	{"filter": "Good cells"}                               a saved filter's cells (its latest version, or by id)
//
// A tree of transcripts takes gene (a name, or a list), region, distance and box as above,
// {"assigned": true} (in a cell of the instrument's segmentation, or not) and {"cells": a tree of
// cells} (in one of those cells).
package query

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Of is what a tree selects.
type Of int

const (
	Cells Of = iota
	Transcripts
)

// Attributes are the cells' QC attributes a tree can test (web/lib/cell-attributes.js's).
var Attributes = []string{"transcripts", "genes", "total", "controls", "area", "nucleusArea", "nucleusShare", "density", "nuclearShare", "method"}

// Node is a tree: one of all, any, not or a condition.
type Node struct {
	All  []*Node
	Any  []*Node
	Not  *Node
	Cond *Condition
}

// Condition is a leaf of a tree.
type Condition struct {
	// Kind is the condition's key: attribute, gene, labels, clusters, niches, region, distance, box,
	// filter, assigned or cells.
	Kind string
	// Name is what it names: an attribute, a gene, a label set, clustering or analysis, a region, a
	// filter.
	Name string
	// Genes are a transcripts tree's genes (one or more).
	Genes []string
	// Level is a label set's level, a number from 0 or a name ("" and -1: the first).
	Level      int
	LevelName  string
	Min, Max   *float64
	In         []Value
	Box        [4]float64
	Assigned   bool
	Cells      *Node
	hasBounds  bool
	categories bool
}

// Value is a category named in "in": a name or a number.
type Value struct {
	Name   string
	Number int
	IsName bool
}

func (v Value) String() string {
	if v.IsName {
		return v.Name
	}
	return strconv.Itoa(v.Number)
}

// Limits on a tree's size, so that one cannot be made to take long to read.
const (
	maxDepth = 32
	maxNodes = 1000
	maxIn    = 1000
)

// Parse reads and checks a tree of cells or transcripts: every key known, every field of its type,
// numbers finite, a range's minimum at most its maximum. It reads nothing else.
func Parse(raw json.RawMessage, of Of) (*Node, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, errors.New("the tree is empty: give a condition, or all, any or not of them")
	}
	count := 0
	return parseNode(raw, of, 0, &count, "the tree")
}

func parseNode(raw json.RawMessage, of Of, depth int, count *int, where string) (*Node, error) {
	*count++
	if *count > maxNodes {
		return nil, fmt.Errorf("the tree has more than %d nodes", maxNodes)
	}
	if depth > maxDepth {
		return nil, fmt.Errorf("the tree is deeper than %d", maxDepth)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("%s is not an object (%s)", where, short(raw))
	}
	for _, combinator := range []string{"all", "any"} {
		if value, ok := fields[combinator]; ok {
			if len(fields) != 1 {
				return nil, fmt.Errorf("%s: %s takes no other key beside it", where, combinator)
			}
			var list []json.RawMessage
			if err := json.Unmarshal(value, &list); err != nil {
				return nil, fmt.Errorf("%s: %s is a list of conditions", where, combinator)
			}
			if len(list) == 0 {
				return nil, fmt.Errorf("%s: %s is empty", where, combinator)
			}
			n := &Node{}
			for i, item := range list {
				child, err := parseNode(item, of, depth+1, count, fmt.Sprintf("%s → %s[%d]", where, combinator, i))
				if err != nil {
					return nil, err
				}
				if combinator == "all" {
					n.All = append(n.All, child)
				} else {
					n.Any = append(n.Any, child)
				}
			}
			return n, nil
		}
	}
	if value, ok := fields["not"]; ok {
		if len(fields) != 1 {
			return nil, fmt.Errorf("%s: not takes no other key beside it", where)
		}
		child, err := parseNode(value, of, depth+1, count, where+" → not")
		if err != nil {
			return nil, err
		}
		return &Node{Not: child}, nil
	}
	c, err := parseCondition(fields, of, depth, count, where)
	if err != nil {
		return nil, err
	}
	return &Node{Cond: c}, nil
}

// The keys each kind of condition takes beside its own.
var conditionKeys = map[string][]string{
	"attribute": {"min", "max", "in"},
	"gene":      {"min", "max"},
	"labels":    {"level", "in"},
	"clusters":  {"in"},
	"niches":    {"in"},
	"region":    {},
	"distance":  {"min", "max"},
	"box":       {},
	"filter":    {},
	"assigned":  {},
	"cells":     {},
}

// The kinds of condition a tree of each target takes.
var kindsOf = map[Of][]string{
	Cells:       {"attribute", "gene", "labels", "clusters", "niches", "region", "distance", "box", "filter"},
	Transcripts: {"gene", "region", "distance", "box", "assigned", "cells"},
}

func parseCondition(fields map[string]json.RawMessage, of Of, depth int, count *int, where string) (*Condition, error) {
	var kind string
	for _, k := range kindsOf[of] {
		if _, ok := fields[k]; ok {
			if kind != "" {
				return nil, fmt.Errorf("%s names both %s and %s: a condition is of one kind (all or any joins them)", where, kind, k)
			}
			kind = k
		}
	}
	target := map[Of]string{Cells: "cells", Transcripts: "transcripts"}[of]
	if kind == "" {
		var keys []string
		for k := range fields {
			keys = append(keys, k)
		}
		return nil, fmt.Errorf("%s (%s) is not a condition of %s: its kinds are %s, joined by all, any and not", where, strings.Join(keys, ", "), target, strings.Join(kindsOf[of], ", "))
	}
	allowed := map[string]bool{kind: true}
	for _, k := range conditionKeys[kind] {
		allowed[k] = true
	}
	for k := range fields {
		if !allowed[k] {
			return nil, fmt.Errorf("%s: a %s condition takes %s, not %s", where, kind, strings.Join(append([]string{kind}, conditionKeys[kind]...), ", "), k)
		}
	}
	c := &Condition{Kind: kind}
	text := func(key string) (string, error) {
		var s string
		if err := json.Unmarshal(fields[key], &s); err != nil || strings.TrimSpace(s) == "" {
			return "", fmt.Errorf("%s: %s is a name (text)", where, key)
		}
		return strings.TrimSpace(s), nil
	}
	number := func(key string) (*float64, error) {
		raw, ok := fields[key]
		if !ok || string(raw) == "null" {
			return nil, nil
		}
		var v float64
		if err := json.Unmarshal(raw, &v); err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("%s: %s is a number", where, key)
		}
		return &v, nil
	}
	var err error
	switch kind {
	case "box":
		var box []float64
		if err := json.Unmarshal(fields["box"], &box); err != nil || len(box) != 4 {
			return nil, fmt.Errorf("%s: box is [x0, y0, x1, y1] in µm", where)
		}
		for _, v := range box {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("%s: box is four numbers", where)
			}
		}
		if !(box[2] > box[0] && box[3] > box[1]) {
			return nil, fmt.Errorf("%s: a box's x1 and y1 are above its x0 and y0", where)
		}
		copy(c.Box[:], box)
		return c, nil
	case "assigned":
		if err := json.Unmarshal(fields["assigned"], &c.Assigned); err != nil {
			return nil, fmt.Errorf("%s: assigned is true or false", where)
		}
		return c, nil
	case "cells":
		c.Cells, err = parseNode(fields["cells"], Cells, depth+1, count, where+" → cells")
		return c, err
	case "gene":
		if of == Transcripts {
			var one string
			if json.Unmarshal(fields["gene"], &one) == nil && strings.TrimSpace(one) != "" {
				c.Genes = []string{strings.TrimSpace(one)}
			} else if json.Unmarshal(fields["gene"], &c.Genes) != nil || len(c.Genes) == 0 || len(c.Genes) > maxIn {
				return nil, fmt.Errorf("%s: gene is a gene's name or a list of them", where)
			}
			c.Name = strings.Join(c.Genes, ", ")
			return c, nil
		}
	}
	if c.Name, err = text(kind); err != nil {
		return nil, err
	}
	if c.Min, err = number("min"); err != nil {
		return nil, err
	}
	if c.Max, err = number("max"); err != nil {
		return nil, err
	}
	if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
		return nil, fmt.Errorf("%s: min %g is above max %g", where, *c.Min, *c.Max)
	}
	c.hasBounds = c.Min != nil || c.Max != nil
	if raw, ok := fields["in"]; ok {
		var list []json.RawMessage
		if err := json.Unmarshal(raw, &list); err != nil || len(list) == 0 || len(list) > maxIn {
			return nil, fmt.Errorf("%s: in is a list of names or numbers (1 to %d)", where, maxIn)
		}
		for _, item := range list {
			var s string
			var n float64
			switch {
			case json.Unmarshal(item, &s) == nil && strings.TrimSpace(s) != "":
				c.In = append(c.In, Value{Name: strings.TrimSpace(s), IsName: true})
			case json.Unmarshal(item, &n) == nil && n == math.Trunc(n) && n >= 0 && n < 1<<31:
				c.In = append(c.In, Value{Number: int(n)})
			default:
				return nil, fmt.Errorf("%s: in holds names and whole numbers, not %s", where, short(item))
			}
		}
		c.categories = true
	}
	if raw, ok := fields["level"]; ok {
		var n float64
		var s string
		switch {
		case json.Unmarshal(raw, &n) == nil && n == math.Trunc(n) && n >= 0 && n < 100:
			c.Level = int(n)
		case json.Unmarshal(raw, &s) == nil && strings.TrimSpace(s) != "":
			c.LevelName = strings.TrimSpace(s)
		default:
			return nil, fmt.Errorf("%s: level is a number from 0, or a level's name", where)
		}
	}
	switch kind {
	case "attribute":
		known := false
		for _, a := range Attributes {
			known = known || a == c.Name
		}
		if !known {
			return nil, fmt.Errorf("%s: no attribute %q; the attributes are %s", where, c.Name, strings.Join(Attributes, ", "))
		}
		if c.Name == "method" {
			if !c.categories || c.hasBounds {
				return nil, fmt.Errorf("%s: the method is tested with in (its names or numbers), not bounds", where)
			}
		} else if c.categories {
			return nil, fmt.Errorf("%s: %s is a number, tested with min and max", where, c.Name)
		}
	case "labels", "clusters", "niches":
		if !c.categories {
			return nil, fmt.Errorf("%s: a %s condition names what it keeps with in", where, kind)
		}
	case "gene":
		if !c.hasBounds {
			one := 1.0
			c.Min, c.hasBounds = &one, true
		}
	}
	return c, nil
}

func short(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if len(s) > 60 {
		s = s[:60] + "…"
	}
	return s
}

// Source answers a condition for every cell or transcript of what a tree is evaluated over.
type Source interface {
	Len() int
	Match(c *Condition) ([]bool, error)
}

// Eval evaluates a tree over a source: which of its items it keeps.
func (n *Node) Eval(s Source) ([]bool, error) {
	size := s.Len()
	switch {
	case n.Cond != nil:
		keep, err := s.Match(n.Cond)
		if err != nil {
			return nil, err
		}
		if len(keep) != size {
			return nil, fmt.Errorf("%s answered %d items of %d", n.Cond.Kind, len(keep), size)
		}
		return keep, nil
	case n.Not != nil:
		inner, err := n.Not.Eval(s)
		if err != nil {
			return nil, err
		}
		out := make([]bool, size)
		for i, v := range inner {
			out[i] = !v
		}
		return out, nil
	case len(n.All) > 0:
		out := make([]bool, size)
		for i := range out {
			out[i] = true
		}
		for _, child := range n.All {
			keep, err := child.Eval(s)
			if err != nil {
				return nil, err
			}
			for i, v := range keep {
				out[i] = out[i] && v
			}
		}
		return out, nil
	}
	out := make([]bool, size)
	for _, child := range n.Any {
		keep, err := child.Eval(s)
		if err != nil {
			return nil, err
		}
		for i, v := range keep {
			out[i] = out[i] || v
		}
	}
	return out, nil
}

// InRange keeps the values within the condition's bounds (included); a value that is not a number
// (NaN: a cell without one) is never kept.
func (c *Condition) InRange(values []float64) []bool {
	out := make([]bool, len(values))
	for i, v := range values {
		if math.IsNaN(v) {
			continue
		}
		out[i] = (c.Min == nil || v >= *c.Min) && (c.Max == nil || v <= *c.Max)
	}
	return out
}

// Codes resolves "in" against a list of categories (codes 0 to len(names)−1): a name matches its
// category (ignoring case), a number its category numbered from base (1 for clusters and niches,
// 0 for the instrument's methods). It answers which codes are kept, or what is not among them.
func (c *Condition) Codes(names []string, base int, what string) ([]bool, error) {
	keep := make([]bool, len(names))
	for _, v := range c.In {
		if !v.IsName {
			k := v.Number - base
			if k < 0 || k >= len(names) {
				return nil, fmt.Errorf("%s has no %d (it numbers %d to %d)", what, v.Number, base, len(names)-1+base)
			}
			keep[k] = true
			continue
		}
		found := false
		for k, name := range names {
			if strings.EqualFold(name, v.Name) {
				keep[k], found = true, true
			}
		}
		if !found {
			return nil, fmt.Errorf("%s has no %q; it has %s", what, v.Name, strings.Join(names, ", "))
		}
	}
	return keep, nil
}

// Count is the items kept.
func Count(keep []bool) int {
	n := 0
	for _, v := range keep {
		if v {
			n++
		}
	}
	return n
}

// KeepBytes are which items are kept as lib/filters.js's keepBytes writes them: the count (32-bit,
// little-endian), then one bit an item, the first in the lowest bit.
func KeepBytes(keep []bool) []byte {
	out := make([]byte, 4+(len(keep)+7)/8)
	binary.LittleEndian.PutUint32(out, uint32(len(keep)))
	for i, v := range keep {
		if v {
			out[4+i/8] |= 1 << (i % 8)
		}
	}
	return out
}

// Checksum is the SHA-256 of KeepBytes, as a saved filter records its cells.
func Checksum(keep []bool) string {
	sum := sha256.Sum256(KeepBytes(keep))
	return hex.EncodeToString(sum[:])
}

// Text says a tree in words.
func (n *Node) Text() string {
	join := func(list []*Node, word string) string {
		var parts []string
		for _, child := range list {
			t := child.Text()
			if len(child.All) > 0 || len(child.Any) > 0 {
				t = "(" + t + ")"
			}
			parts = append(parts, t)
		}
		return strings.Join(parts, word)
	}
	switch {
	case n.Cond != nil:
		return n.Cond.Text()
	case n.Not != nil:
		return "not " + n.Not.Text()
	case len(n.All) > 0:
		return join(n.All, " and ")
	}
	return join(n.Any, " or ")
}

// Text says a condition in words.
func (c *Condition) Text() string {
	bounds := func(what string) string {
		switch {
		case c.Min != nil && c.Max != nil:
			return fmt.Sprintf("%s from %g to %g", what, *c.Min, *c.Max)
		case c.Min != nil:
			return fmt.Sprintf("%s at least %g", what, *c.Min)
		case c.Max != nil:
			return fmt.Sprintf("%s at most %g", what, *c.Max)
		}
		return what + " recorded"
	}
	values := func() string {
		var parts []string
		for _, v := range c.In {
			parts = append(parts, v.String())
		}
		return strings.Join(parts, " or ")
	}
	switch c.Kind {
	case "attribute":
		if c.Name == "method" {
			return "found by segmentation method " + values()
		}
		return "with " + bounds(c.Name)
	case "gene":
		if len(c.Genes) > 0 {
			return "of " + c.Name
		}
		return "with " + bounds(c.Name+" transcripts")
	case "labels":
		level := ""
		if c.LevelName != "" {
			level = " (" + c.LevelName + ")"
		} else if c.Level > 0 {
			level = fmt.Sprintf(" (level %d)", c.Level)
		}
		return "labeled " + values() + " by " + c.Name + level
	case "clusters":
		return "in cluster " + values() + " of " + c.Name
	case "niches":
		return "in niche " + values() + " of " + c.Name
	case "region":
		return "in " + c.Name
	case "distance":
		edge := " µm from " + c.Name + "'s edge (negative inside)"
		switch {
		case c.Min != nil && c.Max != nil:
			return fmt.Sprintf("%g to %g%s", *c.Min, *c.Max, edge)
		case c.Min != nil:
			return fmt.Sprintf("at least %g%s", *c.Min, edge)
		case c.Max != nil:
			return fmt.Sprintf("at most %g%s", *c.Max, edge)
		}
		return "at any distance from " + c.Name + "'s edge"
	case "box":
		return fmt.Sprintf("in [%g, %g]–[%g, %g] µm", c.Box[0], c.Box[1], c.Box[2], c.Box[3])
	case "filter":
		return "kept by " + c.Name
	case "assigned":
		if c.Assigned {
			return "assigned to a cell"
		}
		return "in no cell"
	case "cells":
		return "in cells " + c.Cells.Text()
	}
	return c.Kind
}

// Walk visits every condition of a tree (not those inside a cells condition's tree).
func (n *Node) Walk(visit func(*Condition)) {
	switch {
	case n.Cond != nil:
		visit(n.Cond)
	case n.Not != nil:
		n.Not.Walk(visit)
	default:
		for _, child := range append(append([]*Node{}, n.All...), n.Any...) {
			child.Walk(visit)
		}
	}
}
