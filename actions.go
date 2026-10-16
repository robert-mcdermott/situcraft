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
	// writes: a job writes files on this computer where the arguments say, so it needs the token.
	writes bool
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
	{Name: "open_example", Title: "Open an example dataset", Description: "Opens an example dataset (xenium-lung-2fov, xenium-breast-2fov: 10x Genomics' Xenium 2-FOV bundles, CC BY 4.0; simulated-standard: a tissue SituCraft simulates on this computer, its truth beside it) in the project, or in a new project when this one has other datasets, and builds its derived data. A 10x Genomics example not on this computer is downloaded only with download: true (280 to 380 MB, checked by checksums); the simulated one is made in seconds.", InputSchema: schema(map[string]any{
		"example":  textArg("xenium-lung-2fov, xenium-breast-2fov or simulated-standard (or lung, breast, simulated)."),
		"download": flagArg("Download it first if it is not on this computer."),
	}, "example"), long: true},
	{Name: "tour", Title: "Take an example's tour", Description: "Starts the guided tour of an example (opening it as open_example does), or one of its worked examples (name: compare-expansion or refine-compare on the Breast example, planted-errors on the Lung example, score-against-truth on the simulated one: a question answered step by step, some steps running a job first), or goes to one of its steps; run: true does the step's job (expanding, planting, refining, importing the truth, comparing) and goes on to the next; end: true closes it. Returns the step's title and text, and the comparison a step shows.", InputSchema: schema(map[string]any{
		"example": textArg("xenium-lung-2fov, xenium-breast-2fov or simulated-standard; the tour running if not given."),
		"name":    textArg("tour (the default; the 10x Genomics examples), or a worked example: compare-expansion, refine-compare, planted-errors, score-against-truth."),
		"step":    map[string]any{"type": "integer", "minimum": 1, "description": "The step to show, from 1 (1 if not given)."},
		"run":     flagArg("Do the step's job, then go on to the next step."),
		"end":     flagArg("Close the tour."),
	}), long: true},
	{Name: "set_mode", Title: "Show a view", Description: "Shows a view: start, data, viewer, qc, segmentation, compare, celltypes, spatial, cohort, reports or jobs.", InputSchema: schema(map[string]any{
		"mode": textArg("start, data, viewer, qc, segmentation, celltypes, spatial, cohort, reports or jobs."),
	}, "mode")},
	{Name: "focus", Title: "Focus an item", Description: "Shows an item in the inspector, and in the view that shows it: a dataset, filter, saved view or item on this computer, by name. With no name, clears the focus.", InputSchema: schema(map[string]any{
		"kind": textArg("dataset, filter, view or local; found by name among all of them if not given."),
		"name": textArg("Its name."),
	})},
	{Name: "show_tissue", Title: "Show the tissue", Description: "Shows a dataset in the Viewer as asked, everything else as the Viewer shows it now: where (a center in µm and µm per screen pixel, or a width in µm; the whole section with whole: true), the stains (by name: visible, color, brightness window, gamma), the transcripts of genes by name (up to 16) or all of them, their minimum quality and point size, which of the dataset's segmentations is drawn, the cell and nucleus outlines, a cell fill and its opacity, the transcripts colored by gene or by their assignment's probability, and a cell to select. Returns what it shows, as a saved view records it, and what the dataset lacks.", InputSchema: schema(map[string]any{
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
		"segmentation":    textArg("The segmentation drawn: by its name or artifact ID, instrument for the instrument's, default for the dataset's default."),
		"fill":            textArg("Fill the cells by transcripts, total, area, nucleusArea or method (the instrument's); limitations, certainty or probability (a refinement's); clusters (the latest clustering of the segmentation drawn) or clusters:<artifact id>; labels (the latest label set, its first level), labels:<level> or labels:<artifact id>:<level>; none for no fill."),
		"opacity":         numberArg("The fill's opacity, 0 to 1."),
		"color_by":        textArg("Color the transcripts by gene (as listed) or probability (their assignment's, a refinement's; seen close up)."),
		"cell":            textArg("A cell to select, by its ID in the segmentation drawn (as cells.parquet names the instrument's)."),
	}), long: true},
	{Name: "select_cell", Title: "Select a cell", Description: "Selects a cell of the segmentation the Viewer draws and shows it in the inspector: its ID, position, areas, segmentation method (or, for another segmentation, the instrument's cell it came from and what its method flags), and its transcripts by gene. With no cell, clears the selection.", InputSchema: schema(map[string]any{
		"cell":    textArg("The cell's ID in the segmentation drawn (as cells.parquet names the instrument's), or its row from 0 as \"#123\"."),
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
	{Name: "segmentations", Title: "List the segmentations", Description: "A dataset's segmentations (all datasets' if none is named), each an artifact with its lineage (method and version, software, the job that made or registered it, what it was made from), its files by SHA-256, what its check found, its state, and whether it is the dataset's default (promoted, with the decision's reason). show: true shows the first in the inspector.", InputSchema: schema(map[string]any{
		"dataset": textArg("A dataset by name or id; every dataset's if not given."),
		"show":    flagArg("Show it in the inspector."),
	})},
	{Name: "expand_nuclei", Title: "Expand the nuclei", Description: "SituCraft's nucleus expansion of a dataset: each nucleus of the instrument's segmentation grows by up to distance_um (5 if not given), every pixel going to the nearest nucleus within it, and transcripts to the cell holding their pixel. Runs as a job (seconds) in the Segmentation view, which shows it; the result is a segmentation artifact recorded in the project, compared with the instrument's segmentation. Answers its summary and that comparison.", InputSchema: schema(map[string]any{
		"dataset":     datasetArg,
		"distance_um": map[string]any{"type": "number", "minimum": 0, "maximum": 100, "description": "How far each nucleus grows, in µm (5 if not given)."},
	}), long: true},
	{Name: "refine_segmentation", Title: "Refine the segmentation from the transcripts", Description: "SituCraft's transcript-informed segmentation of a dataset (after Jones et al. 2025), in the Segmentation view's studio: starting from the instrument's cells and their nuclei, each cell's boundary moves to where its own transcripts are, sampled from the posterior; the result is its summary over the last sweeps, with each transcript's probability of its cell. A preset sets the model (balanced if not given); regions or sections limit what it covers, the rest keeping the instrument's cells. Runs as a job (minutes per square millimetre); its failures are classified with what to do. The segmentation artifact is recorded in the project and compared with the instrument's segmentation. Answers its summary, diagnostics and that comparison.", InputSchema: schema(refineArgs()), long: true},
	{Name: "refinement_preflight", Title: "Check a refinement before it runs", Description: "What a transcript-informed segmentation of a dataset would model, cover and cost, without running it: the transcripts at the quality, the cells it starts from, the regions or sections, its memory, disk and time against what the computer has, its problems (which stop it) and risks, and its recipe (JSON, to keep or run with `situcraft refine --recipe`). Takes the same arguments as refine_segmentation.", InputSchema: schema(refineArgs()), long: true},
	{Name: "import_segmentation", Title: "Import a segmentation", Description: "Imports another tool's segmentation into a dataset, in the Segmentation view, which shows it: a SpatialData Zarr store (spatialdata), Baysor's segmentation.csv with its polygons (baysor), GeoJSON polygons with an optional transcript table (geojson), a Xenium Ranger output bundle of the same run (xenium), or a folder of SituCraft's own segmentation files, as `situcraft refine` writes them (situcraft). A job reads it, checks it against the dataset (the transcripts it names, where its cells lie, its ids) and records it as a segmentation artifact with the files' SHA-256; an import whose check finds a fatal problem is recorded as invalid. Coordinates are read in units (um, else pixels of the morphology image), never guessed. Needs the token (it reads files).", InputSchema: schema(map[string]any{
		"dataset":           datasetArg,
		"format":            textArg("spatialdata, baysor, geojson, xenium or situcraft."),
		"paths":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "The files: a store or bundle; or Baysor's segmentation.csv and its polygons; or GeoJSON polygons and a transcript table (CSV or Parquet); or the folder situcraft refine wrote."},
		"units":             textArg("um (the default) or pixels (of the morphology image)."),
		"coordinate_system": textArg("SpatialData: the coordinate system (global)."),
		"cells":             textArg("SpatialData: the shapes or labels element holding the cells."),
		"points":            textArg("SpatialData: the points element with each transcript's cell."),
		"table":             textArg("SpatialData: the table annotating the cells."),
		"cell_column":       textArg("The assignment's cell column (chosen from the usual names if not given)."),
		"cell_property":     textArg("GeoJSON: the property naming each polygon's cell."),
		"tool":              textArg("What made it (recorded with it)."),
		"tool_version":      textArg("Its version."),
		"citation":          textArg("The paper to cite for it."),
	}, "format", "paths"), long: true, reads: true},
	{Name: "compare_segmentations", Title: "Compare two segmentations", Description: "Compares two segmentations of a dataset (by name or artifact id; A the default segmentation and B the latest other if not given), as a job, in the Segmentation view: which cells correspond (a shared nucleus, or half of one within the other), each difference's class (one-to-one, expanded, contracted, split, merged, rearranged, only in A or B, assignment-only), each segmentation's cells, nuclei, assigned fraction, transcripts per cell, areas, spurious co-expression (Jones et al. 2025) and mutually exclusive co-expression (Hartman and Satija 2024), and between them the pixels that agree, the transcripts whose cell changed, the IoU and the boundary displacement. Recorded as a comparison artifact; asking again answers it.", InputSchema: schema(map[string]any{
		"dataset": datasetArg,
		"a":       textArg("Segmentation A, by name or artifact id."),
		"b":       textArg("Segmentation B, by name or artifact id."),
	}), long: true},
	{Name: "cluster_cells", Title: "Cluster the cells", Description: "Clusters a segmentation's cells by their gene expression, as a job, in the Cell types view: the cells with at least a number of gene transcripts of quality 20 or more (or those a QC filter keeps), each cell's counts scaled to the median total and log-transformed, each gene scaled (clipped at ±10), exact principal components, each cell's nearest neighbors weighted as UMAP's graph, and Leiden communities at a resolution, seeded; each cluster's markers by the Wilcoxon rank-sum test, exploratory. Recorded as a clustering artifact; asking again answers it. Answers the clusters, their sizes and leading markers.", InputSchema: schema(map[string]any{
		"dataset":         datasetArg,
		"segmentation":    textArg("The segmentation, by name or artifact id (the default if not given)."),
		"filter":          textArg("A QC filter choosing the cells (the instrument's segmentation only), by name (its latest version), \"name vN\" or id."),
		"min_transcripts": numberArg("Without a filter, the cells analyzed have at least this many gene transcripts (10)."),
		"components":      numberArg("Principal components (50)."),
		"neighbors":       numberArg("Each cell's neighborhood, the cell included (15)."),
		"resolution":      numberArg("Leiden's resolution: higher, more clusters (1)."),
		"seed":            numberArg("Leiden's seed (20250)."),
	}), long: true},
	{Name: "show_map", Title: "Show the map", Description: "Shows a clustering in the Cell types view, with its map (UMAP; made first if it has none) beside the tissue, linked: the cells colored by a clustering's clusters, a gene's expression, their gene transcripts, area or how faithfully the map keeps their neighbors, and a selection of cells (by cluster, or inside a polygon on the map or the tissue) drawn on both. Answers the map's trustworthiness, the selection's size and its composition by cluster.", InputSchema: schema(map[string]any{
		"dataset":    datasetArg,
		"clustering": textArg("A clustering, by artifact id or name; SituCraft's latest if not given."),
		"map":        textArg("The map: a map's artifact id, or instrument for the instrument's; the clustering's own if not given."),
		"color_by":   textArg("clusters (the clustering's), clusters:<clustering>, labels (its latest label set, the first level), labels:<level>, labels:<label set>:<level>, confidence, a gene's name, transcripts, area or reliability."),
		"select":     map[string]any{"description": "{\"clusters\": [numbers from 1]}, {\"polygon\": [[x, y], …], \"on\": \"map\" or \"tissue\" (µm)}, or \"none\"."},
	}), long: true},
	{Name: "name_clusters", Title: "Name clusters", Description: "Names a clustering's clusters (a new version of its names, appended; the clusters keep their numbers). Clusters given the same name are merged on the map and the tissue. An empty name removes one.", InputSchema: schema(map[string]any{
		"dataset":    datasetArg,
		"clustering": textArg("A clustering, by artifact id or name; SituCraft's latest if not given."),
		"names":      map[string]any{"type": "object", "description": "By cluster number from 1: {\"1\": \"T cells\", \"4\": \"Fibroblasts\"}."},
	}, "names"), long: true},
	{Name: "instrument_clusters", Title: "Add the instrument's clusters", Description: "Adds one of the instrument's own clusterings (analysis.tar.gz: graph-based, or k-means of 2 to 10 clusters) to the project as a clustering of its segmentation, its markers computed by SituCraft, and the instrument's map with it the first time.", InputSchema: schema(map[string]any{
		"dataset":    datasetArg,
		"clustering": textArg("graph-based (the default), k-means N, or its folder's name (gene_expression_kmeans_5_clusters)."),
	}), long: true},
	{Name: "label_cells", Title: "Label the cells from markers", Description: "Labels a clustering's cells from a marker set, as a job, in the Cell types view: each cell scored for each label by its markers' expression against control genes binned by mean expression (Tirosh et al. 2016, as scanpy's score_genes), a label's negative markers counting against it where expressed; labels assigned from the top of the hierarchy down, a level only when its best label leads the next by the margin. The marker set: one of the project's (by name or id, its latest version), one SituCraft ships (breast, lung, or simulated for a simulated tissue; the first that fits the panel if none is given), or a set given in full (saved in the project). Recorded as a labels artifact; asking again answers it. Answers the cells labeled at each level, each label's cells and markers, and each cluster's leading labels.", InputSchema: schema(map[string]any{
		"dataset":       datasetArg,
		"clustering":    textArg("A clustering, by artifact id or name; SituCraft's latest if not given."),
		"markers":       textArg("A marker set: the project's by name or id, or SituCraft's breast, lung or simulated."),
		"set":           map[string]any{"type": "object", "description": "A marker set in full (docs/FORMATS.md): {\"format\": \"situcraft-markers\", \"version\": 1, \"name\", \"levels\", \"labels\": [{\"name\", \"parent\", \"positive\", \"negative\"}], \"states\"}."},
		"margin":        numberArg("How far a level's best label must lead the next (0.1)."),
		"minimum":       numberArg("The score a label must reach (0)."),
		"state":         numberArg("The score a state's markers must reach (0.25)."),
		"bins":          numberArg("Expression bins (25)."),
		"controls":      numberArg("Control genes per bin (50)."),
		"lineage":       map[string]any{"type": "boolean", "description": "Leave each label's lineage's markers out of its controls (true)."},
		"seed":          numberArg("The draws of control genes (20250)."),
		"name_clusters": map[string]any{"description": "Name the clusters by their labels: true, or the share of a cluster's cells its label must hold (0.3 to 1; 0.5 for true)."},
	}), long: true},
	{Name: "import_labels", Title: "Import labels", Description: "Imports labels made elsewhere for a segmentation's cells, as a job: a table (CSV or TSV, gzipped or not; Parquet; an AnnData object's obs in Zarr, a folder or a zip, or a SpatialData store's table; Xenium Explorer's cell groups), its rows matched to the cells by id, or one of the instrument's clusterings. A table naming cells the segmentation does not have is refused, with how many; cells it does not name are counted. The columns are found by their usual names unless given. Recorded as a labels artifact with its source; asking again answers it. Answers the labels and their cells.", InputSchema: schema(map[string]any{
		"dataset":           datasetArg,
		"segmentation":      textArg("The segmentation whose cells the labels are, by name or artifact id, or instrument (the default if not given)."),
		"path":              textArg("The table's path on this computer."),
		"instrument":        textArg("Or the instrument's clusters: graph-based, k-means N, or its folder's name."),
		"id_column":         textArg("The column of the cells' ids (cell_id, Cell ID, barcode, an AnnData index … if not given)."),
		"column":            textArg("The column of the labels (group, cell_type, label, annotation, cluster … if not given)."),
		"confidence_column": textArg("A column of numbers: each cell's confidence (optional)."),
		"color_column":      textArg("A column of colors (#rrggbb), each label's (optional)."),
	}), long: true},
	{Name: "review_labels", Title: "Make the review queue", Description: "Ranks a label set's cells for review: those whose label at most a share of their nearest labeled cells in the tissue carry (most carrying another: spatially implausible), and those of low confidence (from markers, by default under twice the margin), the reviewed left out. Answers the counts and the queue's first cells, each with why.", InputSchema: schema(map[string]any{
		"dataset":        datasetArg,
		"segmentation":   textArg("The segmentation, by name, id or instrument (the default if not given)."),
		"labels":         textArg("A label set's artifact id, or a curated (cur-…) or consensus (con-…) set's; the latest if not given."),
		"level":          textArg("Its level, by name or number from 1 (the first if not given)."),
		"neighbors":      numberArg("The nearest labeled cells counted (10)."),
		"implausible":    numberArg("Flag a cell when at most this share of them carry its label (0.2)."),
		"low_confidence": numberArg("Flag a cell whose confidence is below this."),
		"limit":          numberArg("The cells answered (20)."),
	}), long: true},
	{Name: "label_evidence", Title: "A cell's label evidence", Description: "Answers a cell's evidence: its label in each label set of its segmentation; from markers, at each level the label it took and the next, with their markers' transcripts in the cell; its nearest cells in expression; and its reviews (who, when and why).", InputSchema: schema(map[string]any{
		"dataset":      datasetArg,
		"segmentation": textArg("The segmentation, by name, id or instrument (the default if not given)."),
		"cell":         textArg("The cell's row in the segmentation (#123 or 123)."),
		"labels":       textArg("The label set whose markers explain it (the latest if not given)."),
		"level":        textArg("Its level."),
		"clustering":   textArg("The clustering (SituCraft's latest of the segmentation if not given)."),
	}, "cell"), long: true},
	{Name: "curate_labels", Title: "Curate labels", Description: "Appends an edit to a curated label set: cells or a clustering's clusters relabeled, or cells' labels accepted, with the reason kept and who. Given a predicted set, the curated set begun from it (made the first time). Edits are never changed or removed: undoing one is another edit.", InputSchema: schema(map[string]any{
		"dataset":      datasetArg,
		"segmentation": textArg("The segmentation, by name, id or instrument (the default if not given)."),
		"labels":       textArg("A curated set (cur-…), or a predicted label set whose curated set to append to."),
		"level":        textArg("The predicted set's level."),
		"cells":        map[string]any{"type": "array", "description": "Cells' rows (#123 or 123)."},
		"clusters":     map[string]any{"type": "array", "description": "Or a clustering's clusters, by number from 1."},
		"clustering":   textArg("The clustering of the clusters (SituCraft's latest if not given)."),
		"label":        textArg("The new label (a new name is added)."),
		"accept":       map[string]any{"type": "boolean", "description": "Accept the cells' label as it is, instead."},
		"reason":       textArg("Why (kept with the edit)."),
		"by":           textArg("Who (remote control if not given)."),
		"name":         textArg("The curated set's name, when it is begun."),
	}, "reason"), long: true},
	{Name: "consensus_labels", Title: "Make a consensus", Description: "Makes a consensus label set of a segmentation's label sets (two or more): each cell's label by a rule — unanimous (every set gives it), majority (at least min sets give it, more than any other) or priority (the first set's, else the next's) — with each set's labels mapped to common names where they differ. Answers the cells labeled and how far the first two sets agree.", InputSchema: schema(map[string]any{
		"dataset":      datasetArg,
		"segmentation": textArg("The segmentation, by name, id or instrument (the default if not given)."),
		"members":      map[string]any{"type": "array", "description": "The sets: their ids, with a level after a colon (lab-…:1), or their names."},
		"rule":         textArg("unanimous, majority or priority (majority)."),
		"min":          numberArg("For a majority, the sets agreeing at least (more than half if not given)."),
		"mapping":      map[string]any{"type": "array", "description": "For each set, {its label: the common name}."},
		"name":         textArg("The consensus's name."),
	}, "members"), long: true},
	{Name: "show_comparison", Title: "Show a comparison", Description: "Opens a comparison of two segmentations (by id, or 'A and B' by their names; the dataset's latest if not given) in the Compare workspace: A and B side by side, overlaid or flickering, cells filled by the class of their difference, a map of disagreement. Ranks the review queue (reassigned transcripts, contamination, disagreement or low counts) among the classes and reassigned transcripts the rules keep, and shows a difference (by its place in the queue, or by a cell's id). Answers the queue's first ten, the difference shown and the cautions about the comparison's measures.", InputSchema: schema(map[string]any{
		"dataset":     datasetArg,
		"comparison":  textArg("A comparison, by artifact id or 'A and B' (the segmentations' names)."),
		"mode":        textArg("side, overlay or flicker."),
		"map":         flagArg("Show the disagreement map."),
		"classes":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "The classes the queue and the fills keep (one-to-one, expanded, contracted, split, merged, rearranged, only in A, only in B, assignment-only); every differing class if not given."},
		"rank":        textArg("reassigned, contamination, disagreement or low-counts."),
		"min_changed": map[string]any{"type": "number", "minimum": 0, "description": "Keep only differences with at least this many reassigned transcripts."},
		"difference":  map[string]any{"type": "number", "minimum": 1, "description": "Show the difference at this place in the queue (from 1)."},
		"cell":        textArg("Show the difference of this cell (an id of either segmentation's)."),
	}), long: true},
	{Name: "decide_segmentation", Title: "Decide about a segmentation", Description: "Records a decision about one of a dataset's segmentations: promote (the dataset's default), keep (for exploration) or reject, with a reason (required), and optionally the comparison it rests on. Decisions are appended to the project and never removed; the default cannot be rejected (promote another first).", InputSchema: schema(map[string]any{
		"dataset":      datasetArg,
		"segmentation": textArg("The segmentation, by name or artifact id."),
		"decision":     textArg("promote, keep or reject."),
		"rationale":    textArg("Why (recorded with the decision)."),
		"comparison":   textArg("The comparison it rests on (artifact id)."),
	}, "segmentation", "decision", "rationale"), long: true},
	{Name: "plant_errors", Title: "Plant errors in a segmentation", Description: "For practice: a copy of a dataset's instrument segmentation with known errors planted in it (neighbouring cells of different lineages merged, large cells split in two, cells missed, cells drawn too wide), simulated and named so (never promoted or exported), made as a job in the Segmentation view and recorded as a segmentation artifact with its answer key (planted.json). Compare it with the instrument's to find them. The same parameters (20 merges, 20 splits, 10 missed, 10 grown by 1 µm, seed 20261014 if not given) answer the same artifact. Answers its summary, and with reveal the answer key.", InputSchema: schema(map[string]any{
		"dataset": datasetArg,
		"merges":  map[string]any{"type": "integer", "minimum": 0, "maximum": 1000, "description": "Neighbouring cells merged (20 if not given)."},
		"splits":  map[string]any{"type": "integer", "minimum": 0, "maximum": 1000, "description": "Cells split in two (20)."},
		"missed":  map[string]any{"type": "integer", "minimum": 0, "maximum": 1000, "description": "Cells missed (10)."},
		"grown":   map[string]any{"type": "integer", "minimum": 0, "maximum": 1000, "description": "Cells drawn too wide (10)."},
		"grow_um": map[string]any{"type": "number", "minimum": 0, "maximum": 20, "description": "How far a grown cell reaches into its neighbours, µm (1)."},
		"seed":    map[string]any{"type": "integer", "minimum": 0, "description": "The seed choosing the cells (20261014)."},
		"reveal":  flagArg("Answer the planted errors: each one's kind, cells and place."),
	}), long: true},
	{Name: "export_segmentation", Title: "Export a segmentation", Description: "Writes a segmentation of a dataset (its default if not named) into a folder, in the formats asked for: spatialdata (a SpatialData Zarr store: the cells as labels and polygons, the transcripts as points with their cells, the counts as an AnnData table; µm), anndata (AnnData Zarr), parquet and csv (cells, transcripts with their cells, outlines and counts, in Xenium's column names), mex (Matrix Market, as Read10X reads it), geojson (the outlines) and xenium-ranger (label masks for Xenium Ranger's import-segmentation, the inputs checked, and the command, which SituCraft does not run). A job in the Segmentation view; situcraft-export.json lists every file by its SHA-256 with the totals and content fingerprints every format holds. A folder that exists is replaced only with overwrite, and only when it is a SituCraft export; a segmentation with planted errors is refused. Needs the token (it writes files). Answers the manifest.", InputSchema: schema(map[string]any{
		"dataset":      datasetArg,
		"segmentation": textArg("The segmentation, by name or artifact id (the dataset's default if not given)."),
		"formats":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "spatialdata, anndata, parquet, csv, mex, geojson and xenium-ranger: one or more."},
		"folder":       textArg("The export's folder, an absolute path; the export makes it."),
		"overwrite":    flagArg("Replace the folder if it is an earlier SituCraft export."),
	}, "formats", "folder"), long: true, writes: true},
	{Name: "jobs", Title: "List and control jobs", Description: "The program's jobs (building derived data, downloading examples, registering segmentations…): each one's id, kind, state, progress, runs and error, and whether an interrupted one can be resumed. With job, that job with its whole log and its spec; with job and do, stops (cancel), resumes (resume) or forgets (discard) it. show: true shows the Jobs view.", InputSchema: schema(map[string]any{
		"job":  textArg("A job's id."),
		"do":   textArg("cancel, resume or discard."),
		"show": flagArg("Show the Jobs view."),
	})},
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

// refineArgs are refine_segmentation's and refinement_preflight's arguments.
func refineArgs() map[string]any {
	return map[string]any{
		"dataset":    datasetArg,
		"preset":     textArg("The settings' preset: balanced (the default), conservative, flexible, low-memory, roi-pilot (a region, run with two seeds) or expert."),
		"sweeps":     map[string]any{"type": "integer", "minimum": 10, "maximum": 100000, "description": "Sweeps of the sampler (the preset's if not given; changing it makes the settings expert)."},
		"samples":    map[string]any{"type": "integer", "minimum": 1, "description": "The last sweeps summarized (the preset's if not given, at most the sweeps)."},
		"seed":       map[string]any{"type": "integer", "minimum": 0, "description": "The random seed (20250 if not given); a seed gives the same segmentation on any computer."},
		"layers":     map[string]any{"type": "integer", "minimum": 1, "maximum": 16, "description": "Voxel layers in depth (1 if not given)."},
		"regions":    map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "minItems": 4, "maxItems": 4}, "description": "Regions to refine, each [x0, y0, x1, y1] in µm; the rest of the tissue keeps the instrument's cells."},
		"sections":   flagArg("Refine the tissue's separate sections apart, each with a model of its own."),
		"min_qv":     map[string]any{"type": "number", "minimum": 0, "maximum": 40, "description": "The quality the modelled transcripts reach (20 if not given)."},
		"seed_check": flagArg("Run again with the next seed and report how many transcripts both runs put in the same cell."),
		"threads":    map[string]any{"type": "integer", "minimum": 1, "description": "Threads to run on (every core if not given); the result does not depend on them."},
	}
}
