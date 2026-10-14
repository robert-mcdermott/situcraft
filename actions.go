package main

// The remote-control actions (remote.go): what a program on this computer can ask the open
// SituCraft page to do. The page performs them (web/ui/remote.js, whose action table must name
// exactly these; TestEveryActionHasAPageHandler checks it). GET /api/remote/tools lists them with
// their arguments' JSON schemas; the MCP server (wave 9) will offer the same list to agents.

// apiVersion is the version of what these actions are: their names, their arguments and what their
// results hold (GET /api/info and /api/remote/tools give it). It changes when an action is removed
// or renamed, an argument removed or made to mean something else, or a field of a result removed
// or changed; adding an action, an optional argument or a field does not change it.
const apiVersion = 1

type remoteAction struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	// long: may run for minutes (checking a bundle, building its derived data, a download).
	long bool
	// output: writes a file at args.path (output.go), so it needs the token.
	output bool
	// reads: opens items on this computer by their paths (local.go), so it needs the token.
	reads bool
}

func schema(properties map[string]any, required ...string) map[string]any {
	out := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func textArg(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func flagArg(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func numberArg(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

var datasetArg = textArg("A dataset of the project by name or id; the one in focus, else the first, if not given.")

var remoteActions = []remoteAction{
	{Name: "get_state", Title: "Describe the project", Description: "The open project: its datasets (each bundle's place, whether it is found, its check, its derived data), filters and saved views; the view shown, the item in focus, the cells chosen, what the Viewer or QC view shows (dataset, place, zoom, genes, map); the items on this computer; the examples; the jobs running.", InputSchema: schema(map[string]any{})},
	{Name: "new_project", Title: "Start a new project", Description: "Starts a new, empty project (the open one is saved in the library first).", InputSchema: schema(map[string]any{
		"name": textArg("A name for it."),
	})},
	{Name: "open_project", Title: "Open a saved project", Description: "Opens a project from the library by its name or id; each dataset's bundle is looked for where the project recorded it.", InputSchema: schema(map[string]any{
		"project": textArg("The project's name (or part of it) or id."),
	}, "project")},
	{Name: "open_paths", Title: "Open bundles by path", Description: "Opens what is on this computer, as Open a bundle does: Xenium output bundles (their folder, .zip or experiment.xenium), project archives (.scz), or folders of them. A bundle is checked and shown in the Data view (add: true adds it to the project, which builds its derived data); an archive's project opens. Needs the token.", InputSchema: schema(map[string]any{
		"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Absolute paths."},
		"add":   flagArg("Add each bundle with no fatal problem to the project, and wait for its derived data."),
	}, "paths"), long: true, reads: true},
	{Name: "add_dataset", Title: "Add a bundle to the project", Description: "Adds a bundle already on this computer (open_paths, the command line or Open a bundle opened it) to the project: it is checked, recorded, and its derived data built (waited for unless wait is false). Refused when the check finds a fatal problem.", InputSchema: schema(map[string]any{
		"bundle": textArg("The bundle by its name or path."),
		"wait":   flagArg("Wait for its derived data to be built (true if not given)."),
	}, "bundle"), long: true},
	{Name: "open_example", Title: "Open an example dataset", Description: "Opens an example dataset (xenium-lung-2fov, xenium-breast-2fov: 10x Genomics' Xenium 2-FOV bundles, CC BY 4.0) in the project, or in a new project when this one has other datasets, and builds its derived data. One not on this computer is downloaded from 10x Genomics only with download: true (280 to 380 MB, checked by checksums).", InputSchema: schema(map[string]any{
		"example":  textArg("xenium-lung-2fov or xenium-breast-2fov (or lung, breast)."),
		"download": flagArg("Download it first if it is not on this computer."),
	}, "example"), long: true},
	{Name: "tour", Title: "Take an example's tour", Description: "Starts the guided tour of an example (opening it as open_example does), or goes to one of its steps; end: true closes it. Returns the step's title and text.", InputSchema: schema(map[string]any{
		"example": textArg("xenium-lung-2fov or xenium-breast-2fov; the tour running if not given."),
		"step":    map[string]any{"type": "integer", "minimum": 1, "description": "The step to show, from 1 (1 if not given)."},
		"end":     flagArg("Close the tour."),
	})},
	{Name: "set_mode", Title: "Show a view", Description: "Shows a view: start, data, viewer, qc, segmentation, celltypes, spatial, cohort, reports or jobs.", InputSchema: schema(map[string]any{
		"mode": textArg("start, data, viewer, qc, segmentation, celltypes, spatial, cohort, reports or jobs."),
	}, "mode")},
	{Name: "focus", Title: "Focus an item", Description: "Shows an item in the inspector, and in the view that shows it: a dataset, filter, saved view or item on this computer, by name. With no name, clears the focus.", InputSchema: schema(map[string]any{
		"kind": textArg("dataset, filter, view or local; found by name among all of them if not given."),
		"name": textArg("Its name."),
	})},
	{Name: "show_tissue", Title: "Show the tissue", Description: "Shows a dataset in the Viewer as asked, everything else as the Viewer shows it now: where (a center in µm and µm per screen pixel, or a width in µm; the whole section with whole: true), the stains (by name: visible, color, brightness window, gamma), the transcripts of genes by name (up to 16) or all of them, their minimum quality and point size, the cell and nucleus outlines, a cell fill and its opacity, and a cell to select. Returns what it shows, as a saved view records it, and what the dataset lacks.", InputSchema: schema(map[string]any{
		"dataset":         datasetArg,
		"center":          map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "minItems": 2, "maxItems": 2, "description": "The center, [x, y] in µm."},
		"um_per_pixel":    numberArg("The zoom: µm per screen (CSS) pixel."),
		"width_um":        numberArg("The zoom as the width of tissue shown, in µm."),
		"whole":           flagArg("Show the whole section."),
		"genes":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Genes whose transcripts are shown, by name (each in its color); [] for none."},
		"all_transcripts": flagArg("Show every transcript, in one color."),
		"transcripts":     flagArg("Show transcripts at all (false hides them)."),
		"min_quality":     numberArg("The lowest quality (qv) shown: 20 (the default) or 0 for all."),
		"point_size":      numberArg("The transcripts' point size, 1 to 8 pixels."),
		"channels":        map[string]any{"type": "object", "description": "By stain name (DAPI, ATP1A1/CD45/E-Cadherin, 18S, alphaSMA/Vimentin…): {visible, color (#rrggbb), window: [low, high], gamma}, each optional."},
		"outlines":        flagArg("Draw the cell outlines."),
		"nuclei":          flagArg("Draw the nucleus outlines."),
		"fill":            textArg("Fill the cells by transcripts, total, area, nucleusArea or method; none for no fill."),
		"opacity":         numberArg("The fill's opacity, 0 to 1."),
		"cell":            textArg("A cell to select, by its ID (as cells.parquet names it)."),
	}), long: true},
	{Name: "select_cell", Title: "Select a cell", Description: "Selects a cell in the Viewer and shows it in the inspector: its ID, position, areas, segmentation method, and its transcripts by gene. With no cell, clears the selection.", InputSchema: schema(map[string]any{
		"cell":    textArg("The cell's ID (as cells.parquet names it), or its row from 0 as \"#123\"."),
		"dataset": datasetArg,
		"center":  flagArg("Center the view on it (true if not given)."),
	}), long: true},
	{Name: "save_view", Title: "Save the view", Description: "Saves what the Viewer or the QC view shows, under a name, in the project: the place and zoom, the stains, genes and layers, the selected cell, and in QC the map and filter. Returns the view and its link.", InputSchema: schema(map[string]any{
		"name": textArg("The view's name."),
	}, "name"), long: true},
	{Name: "open_view", Title: "Open a saved view", Description: "Opens a saved view exactly as it was saved. Returns it, what the dataset no longer has, and its link.", InputSchema: schema(map[string]any{
		"view": textArg("The view's name or id."),
	}, "view"), long: true},
	{Name: "remove_view", Title: "Remove a saved view", Description: "Removes a saved view from the project (undoable, in the history).", InputSchema: schema(map[string]any{
		"view": textArg("The view's name or id."),
	}, "view")},
	{Name: "qc_metrics", Title: "Read the run's metrics", Description: "The run's recorded metrics, each with what it measures and, where SituCraft recomputes it from the data, its value and whether they agree; shows them in the QC view.", InputSchema: schema(map[string]any{
		"dataset": datasetArg,
	}), long: true},
	{Name: "set_qc_map", Title: "Map quality over the tissue", Description: "Shows a QC map over the tissue: cells colored by an attribute (cell:transcripts, cell:genes, cell:total, cell:controls, cell:area, cell:nucleusArea, cell:nucleusShare, cell:density, cell:nuclearShare, cell:method), small regions by the share of gene transcripts outside cells (bin:outside), below quality 20 (bin:quality), negative-control density (bin:controls) or gene transcript density (bin:density), or none. Optionally with a filter in use, and a place.", InputSchema: schema(map[string]any{
		"map":          textArg("The map's id (above) or label."),
		"dataset":      datasetArg,
		"filter":       textArg("A filter in use, by name (its latest version) or id; none for no filter."),
		"center":       map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "minItems": 2, "maxItems": 2, "description": "The center, [x, y] in µm."},
		"um_per_pixel": numberArg("The zoom: µm per screen pixel."),
	}, "map"), long: true},
	{Name: "select_cells", Title: "Choose cells by an attribute", Description: "Chooses the cells whose attribute is between min and max (each optional), as dragging across a QC plot does; they are shown in the QC view, the Viewer and the inspector. Returns how many. clear: true clears the choice.", InputSchema: schema(map[string]any{
		"attribute": textArg("transcripts, genes, total, controls, area, nucleusArea, nucleusShare, density or nuclearShare."),
		"min":       numberArg("The lowest value chosen."),
		"max":       numberArg("The highest value chosen."),
		"dataset":   datasetArg,
		"clear":     flagArg("Clear the choice."),
	}), long: true},
	{Name: "save_filter", Title: "Save a cell filter", Description: "Saves a cell filter as a new version (a filter's earlier versions are kept): conditions on the cells' attributes, each {attribute, min, max} (bounds included, either optional) or {attribute: \"method\", values: [segmentation methods by name or number]}. Returns its version and result: the cells it keeps and the SHA-256 of which.", InputSchema: schema(map[string]any{
		"name":       textArg("The filter's name."),
		"conditions": map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "description": "The conditions, all of which a cell must meet."},
		"dataset":    datasetArg,
	}, "name", "conditions"), long: true},
	{Name: "methods", Title: "Write the methods", Description: "A methods paragraph for a dataset, from what the project records (its bundle's metadata and check, the filters chosen, the data's source for an example) and SituCraft's version, with its references. show: true shows it as the Data view's Methods does.", InputSchema: schema(map[string]any{
		"dataset": datasetArg,
		"filters": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Filters to describe, by name (their latest version) or id; the latest version of each if not given; [] for none."},
		"show":    flagArg("Show the paragraph in the window."),
	}), long: true},
	{Name: "export", Title: "Write a file", Description: "Writes a file: the project document (project, JSON), a project archive (archive, .scz: the project, its bundles recorded relative to the archive), or a dataset's methods paragraph with its references (methods, Markdown). Needs the token.", InputSchema: schema(map[string]any{
		"what":      textArg("project, archive or methods."),
		"path":      textArg("The absolute path of the file to write."),
		"overwrite": flagArg("Replace the file if it exists."),
		"dataset":   datasetArg,
	}, "what", "path"), long: true, output: true},
}

func findAction(name string) (remoteAction, bool) {
	for _, action := range remoteActions {
		if action.Name == name {
			return action, true
		}
	}
	return remoteAction{}, false
}

// actionNames lists the actions, for the banner and the tests.
func actionNames() []string {
	names := make([]string, len(remoteActions))
	for i, action := range remoteActions {
		names[i] = action.Name
	}
	return names
}
