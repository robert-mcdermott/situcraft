package main

// Recipes (requirements E5): a job's spec as readable JSON, to keep with a result, share, and run
// again headless (`situcraft refine --recipe`). A refinement's recipe holds its spec (preset,
// every setting, area, quality, seed check), the dataset it was made for, the method and the
// software; the same recipe on the same bundle gives the same segmentation, byte for byte.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"situcraft/internal/segment/refine"
	"situcraft/internal/xenium"
)

const (
	recipeFormat  = "situcraft-recipe"
	recipeVersion = 1
)

// recipe is a refinement's recipe.
type recipe struct {
	Format  string        `json:"format"`
	Version int           `json:"version"`
	Kind    string        `json:"kind"`
	Dataset recipeDataset `json:"dataset"`
	// Refinement is the spec; PresetsVersion the presets' version it was made with.
	Refinement     refine.Spec `json:"refinement"`
	PresetsVersion string      `json:"presets_version"`
	Method         string      `json:"method"`
	Citation       string      `json:"citation"`
	Software       struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"software"`
	// Run says how to run it again.
	Run string `json:"run"`
}

// recipeDataset is the bundle a recipe was made for.
type recipeDataset struct {
	Name       string `json:"name"`
	Path       string `json:"path,omitempty"`
	RunName    string `json:"run_name,omitempty"`
	RegionName string `json:"region_name,omitempty"`
	Simulated  bool   `json:"simulated,omitempty"`
}

// newRecipe is a spec's recipe for a bundle.
func newRecipe(b *xenium.Bundle, spec refine.Spec) recipe {
	r := recipe{Format: recipeFormat, Version: recipeVersion, Kind: "refinement", Refinement: spec, PresetsVersion: refine.PresetsVersion,
		Method: refine.Method + " " + refine.MethodVersion, Citation: refine.Citation,
		Run: "situcraft refine --recipe <this file> <bundle> <new output folder>"}
	r.Software.Name, r.Software.Version = "situcraft", version
	r.Dataset = datasetOf(b)
	return r
}

func datasetOf(b *xenium.Bundle) recipeDataset {
	d := recipeDataset{Name: b.Name, Path: b.Path, Simulated: b.Simulation != nil}
	if b.Experiment != nil {
		d.RunName, d.RegionName = b.Experiment.RunName, b.Experiment.RegionName
	}
	return d
}

// readRecipe reads a refinement's recipe, refusing another format or kind, a newer version and
// fields it does not know (a newer SituCraft's).
func readRecipe(raw []byte) (recipe, error) {
	var head struct {
		Format  string `json:"format"`
		Version int    `json:"version"`
		Kind    string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return recipe{}, fmt.Errorf("not a recipe: %v", err)
	}
	if head.Format != recipeFormat {
		return recipe{}, fmt.Errorf("not a SituCraft recipe (its format is %q)", head.Format)
	}
	if head.Version > recipeVersion || head.Version < 1 {
		return recipe{}, fmt.Errorf("a recipe of version %d, which this SituCraft (%s) does not read: a newer one made it", head.Version, version)
	}
	if head.Kind != "refinement" {
		return recipe{}, fmt.Errorf("a recipe for %q, not a refinement", head.Kind)
	}
	r := recipe{Refinement: refine.Spec{MinQV: 20, Area: refine.Area{Mode: refine.AreaWhole}}}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return recipe{}, fmt.Errorf("the recipe: %v", err)
	}
	if err := r.Refinement.Check(); err != nil {
		return recipe{}, errors.New("the recipe's refinement: " + strings.TrimPrefix(err.Error(), "refine: "))
	}
	return r, nil
}

// sameDataset says whether a bundle is the one a recipe was made for, as far as its record says.
func (d recipeDataset) sameDataset(b *xenium.Bundle) bool {
	o := datasetOf(b)
	if d.RunName != "" || d.RegionName != "" {
		return d.RunName == o.RunName && d.RegionName == o.RegionName
	}
	return d.Name == o.Name
}
