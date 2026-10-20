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
	// readsAt: reads the file at args.path where it lies (the page asks for it), so it needs the token.
	readsAt bool
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
	{Name: "open_example", Title: "Open an example dataset", Description: "Opens an example dataset (xenium-lung-2fov, xenium-breast-2fov: 10x Genomics' Xenium 2-FOV bundles, CC BY 4.0; simulated-standard: a tissue SituCraft simulates on this computer, with an H&E and an immunofluorescence image of the section, its truth beside it; simulated-cohort: eight simulated sections of subjects in two groups, with known effects between the groups; simulated-null-cohort: eight sections in two groups with no difference between them; simulated-paired-cohort: six subjects' sections before and after, with known effects) in the project, or in a new project when this one has other datasets, and builds its derived data; a cohort's samples are added together, each with its subject and condition. A 10x Genomics example not on this computer is downloaded only with download: true (280 to 380 MB, checked by checksums); the simulated one is made in seconds, and one made by an earlier SituCraft (without its images) is made again with remake: true (the same run, cells and transcripts).", InputSchema: schema(map[string]any{
		"example":  textArg("xenium-lung-2fov, xenium-breast-2fov, simulated-standard, simulated-cohort, simulated-null-cohort or simulated-paired-cohort (or lung, breast, simulated, cohort, null-cohort, paired-cohort)."),
		"download": flagArg("Download it first if it is not on this computer."),
		"remake":   flagArg("Make the simulated example again if an earlier SituCraft made it."),
	}, "example"), long: true},
	{Name: "tour", Title: "Take an example's tour", Description: "Starts the guided tour of an example (opening it as open_example does), or one of its worked examples (name: compare-expansion, refine-compare, labels-two-segmentations, neighbors-breast, tumor-edge or pseudo-samples on the Breast example; planted-errors, cluster-label, moran-genes or remake-enrichment on the Lung example; score-against-truth, labels-against-truth, align-he, regions-qupath, pathologist-review or niches-domains on the simulated one; cohort-gene on the simulated cohort, cohort-null on the null cohort, cohort-shift or cohort-paired on the paired cohort: a question answered step by step, some steps running a job first), or goes to one of its steps; run: true does the step's job (expanding, planting, refining, importing the truth, comparing, clustering, labeling, adding and aligning the H&E, checking its alignments, drawing regions, reviewing, analyzing, setting the samples, describing the design, summarizing, comparing the groups) and goes on to the next; end: true closes it. Returns the step's title and text, and the comparison a step shows.", InputSchema: schema(map[string]any{
		"example": textArg("xenium-lung-2fov, xenium-breast-2fov, simulated-standard, simulated-cohort, simulated-null-cohort or simulated-paired-cohort; the tour running if not given."),
		"name":    textArg("tour (the default; the 10x Genomics examples), or a worked example: compare-expansion, refine-compare, labels-two-segmentations, neighbors-breast, tumor-edge, pseudo-samples, planted-errors, cluster-label, moran-genes, remake-enrichment, score-against-truth, labels-against-truth, align-he, regions-qupath, pathologist-review, niches-domains, cohort-gene, cohort-null, cohort-shift, cohort-paired."),
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
	{Name: "show_tissue", Title: "Show the tissue", Description: "Shows a dataset in the Viewer as asked, everything else as the Viewer shows it now: where (a center in µm and µm per screen pixel, or a width in µm; the whole section with whole: true), the stains (by name: visible, color, brightness window, gamma), the transcripts of genes by name (up to 16) or all of them, their minimum quality and point size, which of the dataset's segmentations is drawn, the cell and nucleus outlines, a cell fill and its opacity, a graph of the cells, a spatial analysis's cells, the transcripts colored by gene or by their assignment's probability, and a cell to select. Returns what it shows, as a saved view records it, and what the dataset lacks.", InputSchema: schema(map[string]any{
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
		"fill":            textArg("Fill the cells by transcripts, total, area, nucleusArea or method (the instrument's); limitations, certainty or probability (a refinement's); clusters (the latest clustering of the segmentation drawn) or clusters:<artifact id>; labels (the latest label set, its first level), labels:<level> or labels:<artifact id>:<level>; review (each cell by its review: excluded, a segmentation concern, ambiguous, relabeled, accepted or commented); none for no fill."),
		"opacity":         numberArg("The fill's opacity, 0 to 1."),
		"color_by":        textArg("Color the transcripts by gene (as listed) or probability (their assignment's, a refinement's; seen close up)."),
		"cell":            textArg("A cell to select, by its ID in the segmentation drawn (as cells.parquet names the instrument's)."),
		"images":          map[string]any{"type": "object", "description": "Post-Xenium images, by name or artifact ID: {visible, opacity (0 to 1), channels: {by name: {visible, color, window, gamma}} (immunofluorescence)}, each optional."},
		"graph":           textArg("A graph of the segmentation drawn, its edges drawn over the cells when zoomed in: by artifact ID, latest for the last built, none for none."),
		"cells_of":        map[string]any{"type": "object", "description": "A spatial analysis's cells: {analysis (its ID, or latest), a, b (labels by name), distance (a co-occurrence's, a nearest distance's or a Ripley radius, µm)}: an enrichment's pair, their cells neighboring each other and the edges between them; or label b's cells within the distance of a's. Regions compared: {analysis, a, region (a region's name, or the row of the tissue in none)}: label a's cells in it. Across an edge: {analysis, a (any label if not given), zone (core, margin, around or all)}: label a's cells there. Niches: {analysis, niche (its number from 1)}: the niche's cells, filled by niche. Moran's I: {analysis, gene (one whose local Moran's I was kept), class (high, low, high-low, low-high or all)}: the gene's cells of that local class. The cells filled by the analysis's labels."},
	}), long: true},
	{Name: "add_image", Title: "Add a post-Xenium image", Description: "Adds an H&E or immunofluorescence image of a dataset's section, stained and scanned after the Xenium run, as a job: a pyramidal, tiled OME-TIFF (as Xenium Explorer requires; RGB, or channels of 8 or 16 bits; deflate, LZW, JPEG or JPEG 2000), read where it lies, never copied, and recorded as an image artifact with its SHA-256. Its alignment, the transformation matrix or keypoints Xenium Explorer saves (found beside the image when 10x named it, else named; null for none), is recorded as the image's first transform and places it; without one the image is not aligned until align_image places it. The Viewer draws an aligned image under the transcripts and cells. Needs the token (it reads files). Answers what the image is and how it is placed.", InputSchema: schema(map[string]any{
		"dataset":   datasetArg,
		"path":      textArg("The image: an OME-TIFF on this computer."),
		"alignment": map[string]any{"type": []string{"string", "null"}, "description": "Its alignment: <name>_imagealignment.csv (the transformation matrix) or <name>_keypoints.csv (found beside the image if not given; null to add it unaligned)."},
		"name":      textArg("What to call it (from its file's name if not given)."),
	}, "path"), long: true, readsAt: true},
	{Name: "align_image", Title: "Align an image by landmarks", Description: "Fits a post-Xenium image's alignment to landmarks, each a point placed on both images, and records it as a version of the image's alignment (a transform artifact: its landmarks, the fit, each landmark's residual and leave-one-out error in µm, who made it, a note and the version it was made from). A similarity (move, turn, one scale and a mirror when that fits better; Umeyama 1991) or an affine transform (also stretched and sheared), by least squares, from three landmarks or more. With use (the default), the version places the image. Answers the fit.", InputSchema: schema(map[string]any{
		"dataset": datasetArg,
		"image":   textArg("The image, by name or artifact ID (the dataset's only image if not given)."),
		"landmarks": map[string]any{"type": "array", "description": "Each landmark: {\"fixed\": [x, y] on the tissue (µm, Xenium's coordinates), \"moving\": [x, y] on the image (its full-resolution pixels, (0, 0) the corner of its first)}.",
			"items": map[string]any{"type": "object", "properties": map[string]any{"fixed": map[string]any{"type": "array", "items": map[string]any{"type": "number"}}, "moving": map[string]any{"type": "array", "items": map[string]any{"type": "number"}}}}},
		"model":  textArg("similarity (the default) or affine."),
		"use":    flagArg("Place the image by it (the default true)."),
		"parent": textArg("The version it is made from, by name or ID."),
		"by":     textArg("Who placed the landmarks (the client's name if not given)."),
		"note":   textArg("A note kept with the version (and the reason, when it is used)."),
	}, "landmarks"), long: true},
	{Name: "register_image", Title: "Align an image automatically", Description: "Aligns a post-Xenium image to its dataset's Xenium image automatically, by the nuclei (an H&E's hematoxylin by color deconvolution, an immunofluorescence image's nuclear stain, against DAPI), as a job: its scale, turn and mirroring found whatever its metadata says (phase correlation and its log-polar form), refined by normalized mutual information, then fitted to local matches over the tissue; optionally a smooth deformation after it (B-splines), kept only when it predicts the matches better. Recorded as a version of the image's alignment with its matches, fit and what the search found; with use (the default), it places the image. The same images give the same version on any computer. Answers the fit.", InputSchema: schema(map[string]any{
		"dataset":  datasetArg,
		"image":    textArg("The image, by name or artifact ID (the dataset's only image if not given)."),
		"model":    textArg("similarity (the default) or affine."),
		"nonrigid": flagArg("Allow a smooth deformation after the model, for a section that stretched."),
		"channel":  textArg("An immunofluorescence image's nuclear stain, by name (the channel named as one, DAPI or Hoechst, if not given)."),
		"start":    textArg("A version to start from instead of searching, by name or ID ('in use' for the one placing the image)."),
		"use":      flagArg("Place the image by it (the default true)."),
		"by":       textArg("Who asked for it (the client's name if not given)."),
		"note":     textArg("A note kept with the version (and the reason, when it is used)."),
	}), long: true},
	{Name: "use_transform", Title: "Place an image by a version of its alignment", Description: "Chooses which version of a post-Xenium image's alignment places it, as a decision recorded with its reason, who and when.", InputSchema: schema(map[string]any{
		"dataset":   datasetArg,
		"image":     textArg("The image, by name or artifact ID (the dataset's only image if not given)."),
		"transform": textArg("The version, by name or artifact ID."),
		"rationale": textArg("Why it is chosen."),
		"by":        textArg("Who chooses it (the client's name if not given)."),
	}, "transform")},
	{Name: "show_alignment", Title: "Show an image's alignment", Description: "Opens the Alignment view on a post-Xenium image: the Xenium image and the post-Xenium image side by side with the landmarks, their fit and each landmark's residual and leave-one-out error, and the image's versions of its alignment; with transform, that version's landmarks loaded to edit. What the Xenium image shows of the image: show (a version, 'in use', 'fit' for the landmarks' fit, 'none'), as display (overlay at opacity, checkerboard of cell µm squares, or flicker), with DAPI's edges and the nuclei's outlines over it, and where a checked version is off (map). Answers the landmarks, the fit, what is shown and the versions with their latest checks.", InputSchema: schema(map[string]any{
		"dataset":   datasetArg,
		"image":     textArg("The image, by name or artifact ID (the dataset's only image if not given)."),
		"transform": textArg("A version whose landmarks to load, by name or ID."),
		"show":      textArg("What the Xenium image shows of the image: a version by name or ID, 'in use', 'fit' or 'none'."),
		"display":   textArg("overlay, checkerboard or flicker."),
		"opacity":   numberArg("Over the stains, the image's opacity, 0 to 1."),
		"cell":      numberArg("A checkerboard's squares' side, 10 to 400 µm."),
		"edges":     flagArg("DAPI's edges over the image."),
		"outlines":  flagArg("The nuclei's outlines over the image."),
		"map":       flagArg("Where the version shown is off, by its latest check."),
	}), long: true},
	{Name: "check_alignment", Title: "Check an image's alignment", Description: "Measures where a version of a post-Xenium image's alignment places it off, as a job: patches of the Xenium image's DAPI over the tissue (between those an automatic alignment matched) against the image's nuclear signal drawn as the version places it, each one's residual shift by phase correlation in µm. Recorded as a check of the version. Answers how far it is off (median, 90th percentile, most, root-mean-square), over how many patches, and a bound of the matches' own error, below which an error is not the alignment's.", InputSchema: schema(map[string]any{
		"dataset":   datasetArg,
		"image":     textArg("The image, by name or artifact ID (the dataset's only image if not given)."),
		"transform": textArg("The version, by name or ID (the one in use if not given)."),
	}), long: true},
	{Name: "draw_region", Title: "Draw a region", Description: "Draws a region on a dataset's tissue, as the Viewer's tools draw one, and keeps it in the project as its first version (who drew it, when and why): a rectangle, a polygon, or a brush stroke (its points and radius), in µm (the dataset's coordinates), the Xenium image's pixels, or a post-Xenium image's pixels (through the version of its alignment placing it, and any deformation). Named and classed: QuPath's classes (Tumor, Stroma, Immune cells, Necrosis, Other, Region*, Ignore*) or any other. Opens the Viewer on the dataset. Answers the region: its area and the cells in it (by centroid, of the segmentation the Viewer draws).", InputSchema: schema(map[string]any{
		"dataset":     datasetArg,
		"rectangle":   map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "description": "[x0, y0, x1, y1]: two opposite corners."},
		"polygon":     map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "number"}}, "description": "Its corners, [[x, y], …], three or more, in order."},
		"brush":       map[string]any{"type": "object", "description": "A brush stroke: {\"points\": [[x, y], …] along it, \"radius\": the brush's radius in µm}."},
		"coordinates": textArg("What the points are in: um (the default), xenium (the Xenium image's full-resolution pixels) or a post-Xenium image, by name or artifact ID (its full-resolution pixels, as QuPath opens it)."),
		"name":        textArg("Its name (its class and a number if not given)."),
		"class":       textArg("Its class (Region* if not given)."),
		"by":          textArg("Who drew it (the client's name if not given)."),
		"note":        textArg("A note kept with it."),
	}), long: true},
	{Name: "edit_region", Title: "Change a region", Description: "Changes a region of a dataset, each change a version appended (who, when and why; the earlier kept): its name or class, its shape painted onto or erased from with a brush stroke, an earlier version restored, or the region removed (or returned). Answers the region as it now is.", InputSchema: schema(map[string]any{
		"dataset":     datasetArg,
		"region":      textArg("The region, by name or ID."),
		"name":        textArg("Its new name."),
		"class":       textArg("Its new class."),
		"brush":       map[string]any{"type": "object", "description": "A brush stroke painted onto it: {\"points\": [[x, y], …], \"radius\": µm, \"erase\": true to erase it from the region}."},
		"coordinates": textArg("What the brush's points are in: um (the default), xenium or a post-Xenium image by name or artifact ID (its pixels)."),
		"restore":     numberArg("A version to restore, from 1."),
		"remove":      flagArg("true removes it (kept with its versions), false returns it."),
		"by":          textArg("Who changed it (the client's name if not given)."),
		"note":        textArg("Why (kept with the version)."),
	}, "region"), long: true},
	{Name: "regions", Title: "List the regions", Description: "Lists a dataset's regions: each one's name, class, version, area (µm²) and the cells in it (by centroid, of the segmentation the Viewer draws); with versions, each one's versions too. With choose, chooses the cells of the dataset's own segmentation in a region, as select_cells chooses cells: the Viewer, the QC view and the inspector show them. Opens the Viewer on the dataset.", InputSchema: schema(map[string]any{
		"dataset":  datasetArg,
		"choose":   textArg("A region whose cells to choose, by name or ID."),
		"versions": flagArg("Each region's versions too."),
		"removed":  flagArg("Regions removed too."),
	}), long: true},
	{Name: "import_regions", Title: "Read regions from GeoJSON", Description: "Reads regions from a GeoJSON file as QuPath exports annotations (a FeatureCollection or a list of Features; Polygon and MultiPolygon shapes, each one's name and classification, its name and color; detections, cells, points and lines are left out), its coordinates in µm, the Xenium image's pixels or a post-Xenium image's pixels (as QuPath opened that image, placed through the version of its alignment in use), each added to the project as a region. Needs the token (it reads a file). Answers how many were added and what was left out.", InputSchema: schema(map[string]any{
		"dataset":     datasetArg,
		"path":        textArg("The GeoJSON file on this computer."),
		"coordinates": textArg("What its coordinates are in: um (the default), xenium or a post-Xenium image by name or artifact ID (its pixels)."),
		"by":          textArg("Who imported them (the client's name if not given)."),
	}, "path"), long: true, readsAt: true},
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
	{Name: "build_graph", Title: "Build a graph of the cells", Description: "Builds a graph of which of a segmentation's cells are neighbors, as a job, in the Spatial view: by the Delaunay triangulation of their centroids (its edges up to a longest length), each cell's k nearest (an edge when either is among the other's k), a radius, or contact in the segmentation's label raster (cells touching, or within a gap, with the boundary they share). Built over all the cells, a QC filter's or those in regions, never cells a review excluded. Recorded as a graph artifact; asking again answers it. Answers the cells and edges, the neighbors a cell, the cells without one, the pieces and the edges' lengths.", InputSchema: schema(map[string]any{
		"dataset":      datasetArg,
		"segmentation": textArg("The segmentation, by name or artifact id (the default if not given)."),
		"kind":         textArg("delaunay (the default), knn, radius or contact."),
		"longest":      numberArg("A Delaunay graph's longest edge, µm (50; 0 keeps every edge)."),
		"k":            numberArg("A k-nearest graph's neighbors of each cell (6)."),
		"radius":       numberArg("A radius graph's distance, µm (20)."),
		"gap":          numberArg("A contact graph's gap closed, µm (0: touching)."),
		"filter":       textArg("A QC filter choosing the cells (the instrument's segmentation only), by name (its latest version), \"name vN\" or id."),
		"regions":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Only the cells in these regions, by name or ID."},
	}), long: true},
	{Name: "spatial_analysis", Title: "Ask how the cells are arranged", Description: "Asks a question of a label set's cells, as a job, in the Spatial view. Neighborhood enrichment: for each pair of labels, the neighbors between their cells in a graph against the number expected if the labels were dealt to the same cells at random (exactly), as a ratio, with a z-score and two-sided p-value from seeded permutations of the labels (within regions, optionally, keeping each one's composition), Benjamini–Hochberg adjusted. Co-occurrence: at each distance, how much likelier each label's cells are within it (or since the distance before) of another label's than of any cell. Nearest distances: each label's cells' distance to the nearest of each label, its median against the labels dealt at random (z, p), and its distribution with an envelope. Ripley's K and L: of each label and across each pair, in a window (the tissue or a region), with Ripley's isotropic edge correction, against the envelope of the labels dealt at random, with a global test. Regions compared: each region's cells by label per mm² of the tissue within it (a density, Garwood's interval) and per labeled cell (a share, Wilson's), the tissue in none of them a row of its own; each region over it as ratios (exact conditional and Koopman's intervals). Across a region's edge: each label's density in bands of signed distance from the edge (inside negative), each band's own tissue its denominator; the core, the margin and around compared, saying which labels fill the region, stay out or gather at its margin. Niches (cellular neighborhoods): each cell's neighborhood (its nearest labeled cells, or those within a radius) by its labels' shares, the cells grouped by k-means with k-means++ seeding into niches, each with its make-up against the tissue's, its defining labels and leading genes, and the niches from further seeds compared by the adjusted Rand index; the niches are a label set too (fill labels:<their id>:0, or niches). Spatially variable genes (moran, of genes, no labels): each gene's Moran's I over a graph, its variances under normality and randomization, the analytic test and permutations, Benjamini–Hochberg over the genes; the leading genes' local Moran's I (each cell high among high, low among low, or unlike its neighbors). A section's cells are not replicates: these describe this section. Recorded as an analysis artifact; asking again answers it. Answers the pairs, the ratios at each distance, the curves, the regions' table, the bands and readings, the niches, or the genes ranked by Moran's I.", InputSchema: schema(map[string]any{
		"dataset":            datasetArg,
		"segmentation":       textArg("The segmentation, by name or artifact id (the default if not given)."),
		"question":           textArg("enrichment (the default), cooccurrence, distances, ripley, regions, boundary, niches or moran."),
		"labels":             textArg("A label set of the segmentation, by id (the latest if not given); niches too, by their analysis's id."),
		"level":              textArg("The label set's level, by number from 1 or name."),
		"graph":              textArg("Enrichment, Moran's I: a graph of the segmentation, by id (the latest built if not given)."),
		"permutations":       numberArg("How many times the labels are dealt again: enrichment 1,000; nearest distances and Ripley's K 199. Moran's I: the cells' values dealt again, 199 (0: the analytic test)."),
		"seed":               numberArg("The permutations' seed (20250)."),
		"weight":             textArg("Enrichment: edges (the default) or boundary (a contact graph's shared boundary)."),
		"within":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Enrichment: deal the labels within each of these regions (names or IDs)."},
		"filter":             textArg("All but enrichment: a QC filter choosing the cells (the instrument's segmentation only)."),
		"regions":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Regions compared: the regions to compare (names or IDs, up to 20). Co-occurrence, nearest distances, Ripley's K: only the cells in these regions."},
		"region":             textArg("Across an edge: the region whose edge to cross, by name or ID."),
		"margin":             numberArg("Across an edge: the margin's half-width, µm (20): the core is inside beyond it, around is outside beyond it."),
		"neighbors":          numberArg("Niches: each cell's neighborhood, its nearest labeled cells (10)."),
		"radius":             numberArg("Niches: each cell's neighborhood, the labeled cells within this, µm (instead of the nearest)."),
		"niches":             numberArg("Niches: how many (6)."),
		"starts":             numberArg("Niches: k-means's starts, the best kept (10)."),
		"seeds":              numberArg("Niches: further seeds whose niches are compared with these (4)."),
		"weights":            textArg("Moran's I: row (standardized, the default) or binary."),
		"local":              numberArg("Moran's I: the leading genes whose local Moran's I is kept (10)."),
		"local_permutations": numberArg("Moran's I: each cell's conditional permutations for its local Moran's I (199)."),
		"up_to":              numberArg("Co-occurrence: the farthest distance, µm (150); Ripley's K: the farthest radius (100)."),
		"every":              numberArg("The step between distances, µm: co-occurrence 10, Ripley's K 5, nearest distances' distribution 2.5, the bands across an edge 10."),
		"pairs":              textArg("Co-occurrence: within (each distance, as Squidpy; the default) or between (since the distance before)."),
		"reach":              numberArg("Nearest distances: the farthest searched, µm (100); a cell with none nearer counts at it. Across an edge: how far the bands reach either side, µm (100)."),
		"window":             textArg("Ripley's K: the window, tissue (the default: the cells' footprint, its gaps closed) or a region by name or ID."),
		"closing":            numberArg("Ripley's K in the tissue, regions compared, across an edge: gaps between cells narrower than twice this are tissue, µm (10)."),
		"correction":         textArg("Ripley's K: the edge correction, isotropic (the default) or none."),
	}), long: true},
	{Name: "remake_analysis", Title: "Make an analysis again on another segmentation", Description: "Makes a spatial analysis again on another segmentation of its dataset, in one step, in the Spatial view: the two segmentations compared (unless they are), its labels made again from the same clustering parameters and marker set (or reference), its graph built again with the same definition over the same regions' cells, and the question asked with the same parameters, regions and window. The new analysis is recorded as made from the first. Answers the two read together: each number under both (keyed by label and gene names), the change, the numbers' rank correlation, and the largest changes. Labels imported, curated or in a consensus, and niches, are not made again.", InputSchema: schema(map[string]any{
		"dataset":      datasetArg,
		"analysis":     textArg("The analysis, by id (the latest if not given)."),
		"segmentation": textArg("The other segmentation, by name or artifact id."),
	}, "segmentation"), long: true},
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
	{Name: "reference_labels", Title: "Label the cells from a reference", Description: "Labels a clustering's cells from a labeled single-cell reference, as a job, in the Cell types view: an AnnData object in Zarr (a folder or a zip; its cells' labels in an obs column, its counts or log-normalized values in X, raw/X or a layer), or a table of each label's mean expression (CSV, TSV or Parquet; a row per label or per gene). The reference is restricted to the panel's genes; each label's profile is its cells' mean log-normalized expression; each cell is correlated (Spearman) with each label's profile over the genes most higher in one label than another, then again on the leading labels' own genes until one is left (Aran et al. 2019), and takes the last round's best when it leads the next by the margin. An .h5ad file is read once written as Zarr (anndata's write_zarr). The columns are found by their usual names unless given. Recorded as a labels artifact; asking again answers it. Answers the genes shared, the cells labeled, each label's cells and each cluster's leading labels.", InputSchema: schema(map[string]any{
		"dataset":       datasetArg,
		"clustering":    textArg("A clustering, by artifact id or name; SituCraft's latest if not given."),
		"path":          textArg("The reference's path on this computer: a .zarr folder, a zip of one, or a table of profiles."),
		"matrix":        textArg("The AnnData object's matrix: X, raw/X or layers/<name> (counts preferred; found if not given)."),
		"label_column":  textArg("The obs column of the reference cells' labels (or, for a table of a row per label, its labels' column)."),
		"gene_column":   textArg("The var column of gene names (the index if not given; or, for a table of a row per gene, its genes' column)."),
		"tune":          numberArg("Fine-tuning: the labels within this of the best correlation are scored again (0.05)."),
		"margin":        numberArg("How far the best label must lead the next, in correlation (0)."),
		"markers":       numberArg("Genes per pair of labels (0: 500 × (2/3)^log₂(labels))."),
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
	{Name: "review_labels", Title: "Make the review queue", Description: "Ranks a label set's cells for review: those whose label at most a share of their nearest labeled cells in the tissue carry (most carrying another: spatially implausible), those whose counterparts under another segmentation carry another label in a set of it (segmentation-sensitive, with against), and those of low confidence (from markers, by default under twice the margin), the reviewed left out. Answers the counts and the queue's first cells, each with why.", InputSchema: schema(map[string]any{
		"dataset":        datasetArg,
		"segmentation":   textArg("The segmentation, by name, id or instrument (the default if not given)."),
		"labels":         textArg("A label set's artifact id, or a curated (cur-…) or consensus (con-…) set's; the latest if not given."),
		"level":          textArg("Its level, by name or number from 1 (the first if not given)."),
		"neighbors":      numberArg("The nearest labeled cells counted (10)."),
		"implausible":    numberArg("Flag a cell when at most this share of them carry its label (0.2)."),
		"low_confidence": numberArg("Flag a cell whose confidence is below this."),
		"against":        textArg("A label set of another segmentation (its id, id:level, or name) the two segmentations' comparison matches cells with: flag the cells whose counterparts carry another label."),
		"limit":          numberArg("The cells answered (20)."),
	}), long: true},
	{Name: "repeat_on_segmentation", Title: "Make a clustering and its labels again on another segmentation", Description: "Makes a clustering of SituCraft's, its map and its label sets from markers or a reference again on another segmentation of the dataset, with the same parameters, as jobs, in one step: to compare the labels under the two segmentations (label_impact, review_labels with against). A QC filter's cells are the instrument segmentation's, so the cells are then chosen by the rule; imported label sets are not made again. Answers the clustering and each label set made, and any not made with why.", InputSchema: schema(map[string]any{
		"dataset":      datasetArg,
		"clustering":   textArg("The clustering, by artifact id or name; SituCraft's latest if not given."),
		"segmentation": textArg("The segmentation to make it again on, by name, id or instrument."),
	}), long: true},
	{Name: "label_impact", Title: "What a segmentation changes in the labels", Description: "Opens a comparison's Labels tab in the Compare view: a label set of each of its two segmentations, read through its differences. Answers each label's cells under each, the cells matched one to one labeled alike and every pair of labels, what split and merged cells carry, the cells without a counterpart by label, the differences whose labels change, the cells whose label changes with the segmentation, and marker mixing by label (the share of each label's cells expressing a pair of mutually exclusive markers together, and the MECR among them).", InputSchema: schema(map[string]any{
		"dataset":    datasetArg,
		"comparison": textArg("A comparison, by id or \"A and B\"; the latest if not given."),
		"labels_a":   textArg("A label set of A's segmentation (its id, id:level, or name); by default one made as B's was, else the latest."),
		"labels_b":   textArg("A label set of B's segmentation, the same way."),
		"color":      map[string]any{"type": "boolean", "description": "Color the tissue's cells by each side's labels (true) or by their difference (false)."},
	}), long: true},
	{Name: "label_evidence", Title: "A cell's label evidence", Description: "Answers a cell's evidence: its label in each label set of its segmentation; from markers, at each level the label it took and the next, with their markers' transcripts in the cell; its nearest cells in expression; and its reviews (who, when and why).", InputSchema: schema(map[string]any{
		"dataset":      datasetArg,
		"segmentation": textArg("The segmentation, by name, id or instrument (the default if not given)."),
		"cell":         textArg("The cell's row in the segmentation (#123 or 123)."),
		"labels":       textArg("The label set whose markers explain it (the latest if not given)."),
		"level":        textArg("Its level."),
		"clustering":   textArg("The clustering (SituCraft's latest of the segmentation if not given)."),
	}, "cell"), long: true},
	{Name: "curate_labels", Title: "Curate labels", Description: "Appends an edit to a curated label set: cells or a clustering's clusters relabeled, or cells' labels accepted, with the reason kept and who, recorded as a review (as review records one). Given a predicted set, the curated set begun from it (made the first time). Edits are never changed or removed: undoing one is another edit.", InputSchema: schema(map[string]any{
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
	{Name: "review", Title: "Review cells or a region", Description: "Reviews cells of a segmentation (by ID or row), the cells chosen, or a region, recorded in the project with its provenance: who, the action, the value before and after, when, the segmentation, the label set, each cell's label and its confidence as shown, and the post-Xenium images' alignments in use. Actions: accept (the label, or a region's class), relabel (a new label, with a reason; a region is reclassed), ambiguous (with the labels it could be), exclude (from analyses, with a reason; a region's cells too unless cells_too is false), include (again), concern (its segmentation looks wrong: merged, split, boundary, not-a-cell, missed or other), resolve (the concern) and comment. Accepting and relabeling cells append to the curated set begun from the label set reviewed. Answers the review.", InputSchema: schema(map[string]any{
		"dataset":      datasetArg,
		"action":       textArg("accept, relabel, ambiguous, exclude, include, concern, resolve or comment."),
		"cells":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Cells by ID (as the segmentation names them), or row as \"#123\"."},
		"chosen":       flagArg("The cells chosen (the instrument's), instead."),
		"region":       textArg("Or a region, by name or ID."),
		"segmentation": textArg("The cells' segmentation, by name, ID or instrument (the default if not given)."),
		"labels":       textArg("The label set reviewed (its ID; the latest if not given): for accept, relabel and ambiguous."),
		"level":        textArg("The label set's level."),
		"label":        textArg("relabel: the new label (or class)."),
		"alternatives": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "ambiguous: the labels it could be."},
		"concern":      textArg("concern: merged, split, boundary, not-a-cell, missed or other."),
		"note":         textArg("Why, or the comment (needed to relabel, exclude and comment)."),
		"cells_too":    flagArg("Excluding a region: its cells too (true if not given)."),
		"by":           textArg("Who reviews (the client's name if not given)."),
	}, "action"), long: true},
	{Name: "reviews", Title: "List the reviews", Description: "Lists a dataset's reviews, the latest first (those of a reviewer, an action, a cell or a region if asked), with how many of a segmentation's cells are accepted, relabeled, ambiguous, excluded, of concern and commented, and each reviewed region's state. Label edits made before reviews were recorded are listed as reviews too.", InputSchema: schema(map[string]any{
		"dataset":      datasetArg,
		"reviewer":     textArg("Only this reviewer's."),
		"action":       textArg("Only reviews of this action."),
		"cell":         textArg("Only a cell's, by ID or row (#123)."),
		"region":       textArg("Only a region's, by name or ID."),
		"segmentation": textArg("The segmentation whose cells are counted (the default if not given)."),
		"limit":        numberArg("At most this many (50 if not given)."),
	}), long: true},
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
	{Name: "plant_errors", Title: "Plant errors in a segmentation", Description: "For practice: a copy of a dataset's instrument segmentation with known errors planted in it (neighboring cells of different lineages merged, large cells split in two, cells missed, cells drawn too wide), simulated and named so (never promoted or exported), made as a job in the Segmentation view and recorded as a segmentation artifact with its answer key (planted.json). Compare it with the instrument's to find them. The same parameters (20 merges, 20 splits, 10 missed, 10 grown by 1 µm, seed 20261014 if not given) answer the same artifact. Answers its summary, and with reveal the answer key.", InputSchema: schema(map[string]any{
		"dataset": datasetArg,
		"merges":  map[string]any{"type": "integer", "minimum": 0, "maximum": 1000, "description": "Neighboring cells merged (20 if not given)."},
		"splits":  map[string]any{"type": "integer", "minimum": 0, "maximum": 1000, "description": "Cells split in two (20)."},
		"missed":  map[string]any{"type": "integer", "minimum": 0, "maximum": 1000, "description": "Cells missed (10)."},
		"grown":   map[string]any{"type": "integer", "minimum": 0, "maximum": 1000, "description": "Cells drawn too wide (10)."},
		"grow_um": map[string]any{"type": "number", "minimum": 0, "maximum": 20, "description": "How far a grown cell reaches into its neighbors, µm (1)."},
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
	{Name: "methods", Title: "Write the methods", Description: "A methods section written from the record (as record answers it), of a dataset and everything made of it (by default), of a result, or of the whole project: every step from the bundles said as what was done (the data and its check, images and their alignment, regions, segmentations and their comparisons and choices, filters, clusters, maps, marker sets and labels, reviews, graphs, spatial analyses, a cohort's design, summary and comparisons, figures) with its parameters, seeds and what it found, in paragraphs by topic, and the software; its references numbered as first cited, each with its DOI. show: true shows it (a dataset's as the Data view's Methods does; any other in the Reports view).", InputSchema: schema(map[string]any{
		"dataset": datasetArg,
		"result":  textArg("A result instead, by ID or name (any record answers)."),
		"project": flagArg("The whole project instead."),
		"filters": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "For a dataset: the filters to describe, by name (their latest version) or id; the latest version of each if not given; [] for none."},
		"show":    flagArg("Show the methods in the window."),
	}), long: true},
	{Name: "samples", Title: "List the samples", Description: "Lists the project's samples, in the Cohort view: each dataset is a sample, or its regions are instead (a tissue microarray's cores, sections on one slide). Answers each sample's name, its dataset and region (a region removed is flagged and left out of a cohort), its cells under its dataset's default segmentation (counted in its region), its region's area, its fields (the user's: subject, condition and any added; missing values null, never zero) and those read from its bundle (run, slide, Xenium region, panel, software), and the fields with their kinds.", InputSchema: schema(map[string]any{}), long: true},
	{Name: "set_samples", Title: "Describe the samples", Description: "Sets what is known of the project's samples, in one step (nothing changes if any part is refused): a dataset's regions made its samples instead of the dataset (or, with none, the dataset one sample again); fields added (text or number) or retyped (to a number only when every value is one); fields removed with their values; samples named; a table read (CSV or tab-separated text with a sample column: its rows matched to samples by name, its columns that are fields set, an empty cell clearing a value, its other columns added as fields, the bundles' columns left alone; rows naming no sample said); and values set by sample and field (null or an empty string clears). Answers the samples, as samples does.", InputSchema: schema(map[string]any{
		"regions":       map[string]any{"type": "object", "description": "A dataset's regions as its samples: {dataset (name or ID), regions (names or IDs; empty for the whole section)}."},
		"fields":        map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "description": "Fields to add or retype: [{name, kind (text or number; text if not given)}]."},
		"remove_fields": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Fields to remove, with their values."},
		"names":         map[string]any{"type": "object", "description": "New names: {sample (name or ID): name}; an empty name gives back the dataset's or region's."},
		"table":         textArg("A table of the samples' fields as CSV or tab-separated text, with a sample column."),
		"values":        map[string]any{"type": "object", "description": "Values: {sample (name or ID): {field: value}}; null or an empty string clears one."},
	}), long: true},
	{Name: "design", Title: "Describe the experimental design", Description: "Makes the cohort's experimental design from its samples, in the Cohort view, before any comparison of groups: the replicate unit (a field naming each biological replicate, such as subject), the field whose groups are compared and its reference level, the contrasts (each level against the reference if not given), covariates (fields, or the bundles' run, slide and panel: numbers as they are, text as factors) and samples left out with their reasons. A unit's samples in one group are technical replicates, combined into one row; a unit in more than one group pairs them (the units enter the model). Answers the design in words, its rows (units in groups, with their samples), its matrix's columns (treatment coding, as patsy's) and rows, its rank and residual degrees of freedom, the contrasts, the samples left out, notes, and, when it cannot be used, why: a group of fewer than two units, regions of one section as separate units (unless pseudo_samples, for teaching), a term confounded with another, no degrees of freedom left, or the groups confounded with a slide, run or panel. With save, keeps it in the project as a new version of that name. Without a unit or group, answers the latest saved design.", InputSchema: schema(map[string]any{
		"unit":           textArg("The replicate unit: a field, such as subject."),
		"group":          textArg("The field whose groups are compared, such as condition."),
		"reference":      textArg("The reference group (the first in code-point order if not given)."),
		"contrasts":      map[string]any{"type": "array", "items": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "description": "Contrasts as [a, b] pairs, each a − b (each level against the reference if not given)."},
		"covariates":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Fields to adjust for: the user's, or run, slide, panel."},
		"exclude":        map[string]any{"type": "object", "description": "Samples left out: {sample (name or ID): reason}."},
		"save":           textArg("Keep the design in the project under this name, as its next version."),
		"pseudo_samples": flagArg("For a teaching example only: regions of one section given different units kept as pseudo-samples (not independent replicates), said so in the design and in every comparison made of it, instead of refused."),
	}), long: true},
	{Name: "cohort_steps", Title: "Run the same steps on every sample", Description: "Runs the same steps on every sample of the cohort, in the Cohort view: each dataset's cells clustered (SituCraft's defaults, on its default segmentation) and labeled from one marker set (a starter's resolved on the first dataset and used for the rest) or one labeled single-cell reference, so that every sample's labels have the same names; then a graph of each sample's cells (a region sample's, its region's; never those a review excluded) and the same questions asked of it with the same parameters: neighborhood enrichment over the graph, nearest distances, Moran's I. A step made already is answered at once. Answers the labels' source (the marker set or the reference) and, for each sample, its clustering, labels, graph and answers.", InputSchema: schema(map[string]any{
		"markers":   textArg("The marker set: a project set's name or ID, or a starter's ID (SituCraft's starter set that fits the first sample's panel if not given)."),
		"reference": textArg("Instead of a marker set, a labeled single-cell reference on this computer, by its path (an AnnData object saved with write_zarr): every sample's cells labeled from it."),
		"level":     map[string]any{"type": "integer", "minimum": 1, "description": "The labels' level the questions are asked of (1, the first, if not given)."},
		"graph":     textArg("The graph of each sample's cells: delaunay (the default), knn, radius or contact, with its default parameter."),
		"questions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "The questions: enrichment, distances, moran (enrichment if not given; [] for none)."},
	}), long: true},
	{Name: "summarize_samples", Title: "Summarize the samples", Description: "Summarizes every sample of the cohort the same way, in the Cohort view, as an artifact of kind cohort-summary: each sample's cells by label (counts, shares of its labeled cells, densities per mm² of its tissue: the tissue window's area, within its region for a region sample), its niches' shares (niches found across the samples together, so that a niche is the same kind of place in every sample), its quality (cells, median transcripts, genes and areas, nuclei, transcripts per area), its graph's numbers, its answers' numbers (an enrichment's ratios, nearest distances' medians, Moran's I by gene) and its pseudobulk (each label's cells' transcripts summed gene by gene), each number traced to the artifact it came from. The samples are labeled from one marker set (cohort_steps labels them); the cohort's names are every label of the level. Answers the summary's ID, the cohort's labels, each sample's cells, labeled cells, counts by label, tissue and niches' shares, the niches (each one's defining labels), and the table's families.", InputSchema: schema(map[string]any{
		"markers":   textArg("The marker set every sample is labeled from, by name or ID (the one the steps used, or the first that labels every sample, if not given)."),
		"reference": textArg("Instead, the reference every sample is labeled from, by its path or SHA-256."),
		"level":     map[string]any{"type": "integer", "minimum": 1, "description": "The labels' level (1, the first, if not given)."},
		"niches":    map[string]any{"type": "integer", "minimum": 0, "maximum": 30, "description": "The niches to find across the samples (6 if not given; 0 for none)."},
		"neighbors": map[string]any{"type": "integer", "minimum": 2, "maximum": 200, "description": "The labeled cells each cell's window holds, nearest first, in its own sample (10 if not given)."},
		"closing":   map[string]any{"type": "number", "minimum": 0, "maximum": 100, "description": "The tissue window's closing radius in µm (10 if not given)."},
		"samples":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Only these samples, by name or ID (every sample if not given)."},
	}), long: true},
	{Name: "cohort_table", Title: "Read the cohort table", Description: "Reads a cohort summary's table, in the Cohort view: for one family of numbers (cells, share, density, niche, qc, graph, enrichment, nearest or moran), each sample's numbers by key with the artifact each came from. Answers the families, the keys and the samples' values.", InputSchema: schema(map[string]any{
		"summary": textArg("The summary, by ID (the latest if not given)."),
		"family":  textArg("cells (the default), share, density, niche, qc, graph, enrichment, nearest or moran."),
		"keys":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Only these keys (all if not given)."},
	}), long: true},
	{Name: "cohort_qc", Title: "Read the cohort's QC", Description: "Reads a cohort summary's QC in the Cohort view, before any comparison: each sample's quality numbers (cells per mm², median transcripts and genes, transcripts per 100 µm², median cell and nucleus areas, the shares of cells with a nucleus and without a transcript; a nucleus's numbers null where the segmentation records none) with its bundle's run, slide and panel; the samples far out on one (a robust z-score, the median absolute deviation's, of 3.5 or more, and a difference that matters: 20% for a quantity, 5 points for a share), each with the reason, never passed or failed; the numbers' medians by slide, run and panel; the design draft's samples by batch and group, with what it says of a group confounded with a batch; the pseudobulk's principal components (each sample's scores, each component's share of the variance, over the genes every panel has); and the panels compared. Nothing is corrected for a batch unless the design adjusts for it.", InputSchema: schema(map[string]any{
		"summary": textArg("The summary, by ID (the latest if not given)."),
	}), long: true},
	{Name: "compare_abundance", Title: "Compare the groups' abundances", Description: "Compares each label's cells between the design's groups, in the Cohort view, with the replicate unit (not the cell) as the unit, as an artifact of kind abundance made from the summary: each label's share of the labeled cells by a quasi-binomial generalized linear model of the design (its pairing and covariates), its density per mm² of tissue by a quasi-Poisson model with the tissue's area as offset, and the niches' shares as shares; each contrast's effect (an odds ratio or density ratio) with its interval, its t test on the residual degrees of freedom, the dispersion, and p-values adjusted by Benjamini and Hochberg over each family's tests, the universe said. The design is the Cohort view's draft unless a saved one is named. Answers every test.", InputSchema: schema(map[string]any{
		"families": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "share, density, niche (share and density if not given)."},
		"design":   textArg("A saved design, by name (its latest version; the Cohort view's draft if not given)."),
		"summary":  textArg("The summary, by ID (the one shown, or the latest, if not given)."),
		"level":    map[string]any{"type": "number", "minimum": 0.5, "maximum": 0.999, "description": "The intervals' level (0.95 if not given)."},
	}), long: true},
	{Name: "compare_expression", Title: "Compare the groups' expression", Description: "Compares each label's genes between the design's groups, in the Cohort view, with the replicate unit (not the cell) as the unit, as an artifact of kind expression made from the summary: each label's pseudobulk (its cells' transcripts summed over each unit's samples) over the genes every panel has, in the units with enough of its cells; genes filtered by expression (a count of 10 over the median library size in the smallest group's number of units, and 15 in all); TMM normalization; voom's precision weights; each gene's weighted model of the design (its pairing and covariates) and limma's moderated t, its variance moderated toward the genes' prior; each contrast's log2 fold change with its interval, and p-values adjusted by Benjamini and Hochberg over every label, gene and contrast tested, the universe said. A label in too few units is left untested, with the reason. The design is the Cohort view's draft unless a saved one is named. Answers each label's genes below an adjusted p of 0.05 (and the 10 lowest p's of each), its units, genes tested and prior.", InputSchema: schema(map[string]any{
		"design":    textArg("A saved design, by name (its latest version; the Cohort view's draft if not given)."),
		"summary":   textArg("The summary, by ID (the one shown, or the latest, if not given)."),
		"level":     map[string]any{"type": "number", "minimum": 0.5, "maximum": 0.999, "description": "The intervals' level (0.95 if not given)."},
		"min_cells": map[string]any{"type": "integer", "minimum": 1, "maximum": 10000, "description": "The fewest cells of a label a unit is tested with (10 if not given)."},
	}), long: true},
	{Name: "show_gene", Title: "Show a gene's expression by group", Description: "Shows a gene's expression in a label, by group, in the Cohort view's differential expression: each unit's log2 counts per million of the label's pseudobulk (its count and library size), with the gene's test of each contrast.", InputSchema: schema(map[string]any{
		"gene":       textArg("The gene, by name."),
		"label":      textArg("The label, by name."),
		"comparison": textArg("The expression comparison, by ID (the latest if not given)."),
	}, "gene", "label"), long: true},
	{Name: "compare_spatial", Title: "Compare the groups' spatial statistics", Description: "Compares the samples' spatial statistics between the design's groups, in the Cohort view, with the replicate unit as the unit (its samples' mean), as an artifact of kind spatial-comparison made from the summary: a pair of labels' neighbor enrichment (found over expected) and a label's median distance to the nearest cell of another, as log2 ratios; a gene's Moran's I (a niche's share is compared by compare_abundance). Each statistic in every unit is a linear model of the design (its pairing and covariates); each family's variances are moderated across its statistics (limma's empirical Bayes); each contrast's effect (a ratio, or a difference) with its interval and moderated t, and p-values adjusted by Benjamini and Hochberg over each family's tests, the universe said. A statistic some unit has none of is left untested. The summary's samples need a spatial question answered (summarize_samples). Answers every test.", InputSchema: schema(map[string]any{
		"families": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "enrichment, nearest, moran (all the summary has if not given)."},
		"design":   textArg("A saved design, by name (its latest version; the Cohort view's draft if not given)."),
		"summary":  textArg("The summary, by ID (the one shown, or the latest, if not given)."),
		"level":    map[string]any{"type": "number", "minimum": 0.5, "maximum": 0.999, "description": "The intervals' level (0.95 if not given)."},
	}), long: true},
	{Name: "show_sample_cells", Title: "Show a sample's cells of a label", Description: "Shows a sample's cells of a label on its tissue, in the Viewer: of its cells chosen (its section's, or its region's; never those a review excluded), those whose label in the summary's label set is the one named. Answers the cells shown.", InputSchema: schema(map[string]any{
		"sample":  textArg("The sample, by name or ID."),
		"label":   textArg("The label, by name."),
		"summary": textArg("The summary, by ID (the latest if not given)."),
	}, "sample", "label"), long: true},
	{Name: "compare_panels", Title: "Compare the samples' panels", Description: "Compares the gene panels of the project's samples (their bundles' gene_panel.json), before any summary: the panels (each one's name, design, version and genes, and the samples read with it), the genes every panel has (the only ones compared across samples), those in some panels only, shared genes read by other codewords, and genes whose Ensembl IDs or names differ.", InputSchema: schema(map[string]any{}), long: true},
	{Name: "export", Title: "Write a file", Description: "Writes a file: the project document (project, JSON), a project archive (archive, .scz: the project, its bundles recorded relative to the archive), the methods of a dataset, a result or the project with their references (methods, Markdown), a post-Xenium image's alignment as Xenium Explorer reads it (alignment: its transformation matrix, the image's pixels to the Xenium image's, or its landmarks as keypoints; CSV), a dataset's regions as QuPath imports annotations (regions: GeoJSON, in µm, the Xenium image's pixels or a post-Xenium image's), a dataset's reviews (reviews: CSV, a row a cell, cluster or region reviewed; or JSON, the record), the project's samples (samples: CSV, a row a sample with its fields and its bundle's), or a cohort summary's table (cohort-table: CSV, a row each number of each sample, with the artifact it came from) or pseudobulk (pseudobulk: CSV, a row a sample's label, a column a gene), what made a result (record: Markdown, JSON or BibTeX, as record answers it), a certificate (certificate: a zip of what a result, a dataset's results or the project rests on and reports, fingerprinted, with a README saying how situcraft verify checks it; answers its fingerprint), a report of results chosen (report: one HTML file, opened in any browser offline, of their figures, a table of each other result's numbers, each traced to its place in the certificate, the methods and their references, and the certificate itself, which the report checks as it opens; answers how many numbers it traces and the fingerprint), or a result package (package: the report, each saved view its figures show drawn in advance as tiles with a reader of its own, and the certificate, as a folder, or a zip when the path ends in .zip, that opens in any browser without SituCraft). A report and a package leave out where files lay on this computer, people's names and their notes, and with names: true the samples' and subjects' names; every text they hold is searched for them before they are written. Needs the token.", InputSchema: schema(map[string]any{
		"what":        textArg("project, archive, methods, alignment, regions, reviews, samples, cohort-table, pseudobulk, record, certificate, report or package."),
		"summary":     textArg("For a cohort table or pseudobulk: the summary, by ID (the latest if not given)."),
		"result":      textArg("For a record, methods or a certificate: the result, by ID or name."),
		"project":     flagArg("For a record, methods or a certificate: the whole project's (a dataset's if neither this nor result is given)."),
		"path":        textArg("The absolute path of the file to write (for a package, a folder that does not exist yet, or a .zip)."),
		"overwrite":   flagArg("Replace the file if it exists."),
		"dataset":     datasetArg,
		"image":       textArg("For an alignment: the image, by name or artifact ID."),
		"transform":   textArg("For an alignment: the version, by name or ID (the one in use if not given)."),
		"format":      textArg("For an alignment: matrix (the default) or keypoints; for reviews: csv (the default) or json; for a record: markdown, json or bibtex (from the path's extension, .md, .json or .bib, if not given)."),
		"coordinates": textArg("For regions: um (the default), xenium (the Xenium image's pixels) or a post-Xenium image by name or artifact ID (its pixels, as QuPath opens it)."),
		"regions":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "For regions: which, by name or ID (all if not given)."},
		"results":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "For a report or a package: the results it reports, by ID or name (figures among them); its figures first, then the others' tables in this order."},
		"title":       textArg("For a report or a package: its title (\"Results of\" the project if not given)."),
		"names":       flagArg("For a report or a package: leave out the samples' and subjects' names (each written Sample 1, Subject 1, …)."),
	}, "what", "path"), long: true, output: true},
	{Name: "figure", Title: "Make or change a figure", Description: "Makes or changes a figure of the project, in the Reports view: a page of panels (a journal's column, 85, 114 or 174 mm wide, or a 16:9 slide), each a saved view of the tissue at the figure's size (its stains composited from their samples, its cells' outlines and fill and its genes' transcripts, in µm, with a scale bar and a legend), lettered A, B, C in reading order. With views, the figure's panels are those views side by side; with panels, any panels (the tissue, a QC distribution, the cells' map, a spatial answer, a cohort's comparison, text) laid out in rows; either makes a new figure when none has the name. The tissue panels of a row share their images' height. Answers what it shows, each panel's letter, region in µm, µm a point, scale bar and the cells and transcripts it draws, and notes (a fill not drawn, too many transcripts). remove: true removes it.", InputSchema: schema(map[string]any{
		"name":       textArg("The figure, by name or ID (a new one with views)."),
		"views":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Saved views of the tissue, by name or ID, each a panel, side by side."},
		"panels":     map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "description": "The panels, in order, each an object of its kind: {kind: tissue, view, width_um?, center?, regions? (true for all, or their names)}; {kind: qc, dataset, metric (transcripts, total, area, nucleusArea), log?, filter?, segmentation?}; {kind: map, map, color? (clusters, or labels:<label set ID>:<level>)}; {kind: spatial, analysis (enrichment, distances or Moran's I)}; {kind: cohort, comparison, plot (forest, units, volcano, gene), family?, label?, gene?, contrast?, key?}; {kind: text, text}. Each names what it draws by name or ID; the title of any."},
		"columns":    numberArg("How many panels a row holds (as many as the panels, up to 3, if not given)."),
		"size":       textArg("single (85 mm), onehalf (114 mm), double (174 mm, the default) or slide."),
		"height_mm":  numberArg("The page's height in mm (the size's if not given)."),
		"text":       numberArg("The text's size in pt (6 to 36; 7 if not given, 14 on a slide)."),
		"width_um":   numberArg("Each tissue panel's width of tissue in µm (what the view shows in a Viewer 1,000 CSS pixels wide if not given)."),
		"center":     map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "minItems": 2, "maxItems": 2, "description": "Each tissue panel's center, [x, y] in µm (its view's if not given)."},
		"scale_bar":  numberArg("The scale bar's length in µm (a round length about a fifth of the width if not given)."),
		"point_size": numberArg("The transcripts' points, in pt across (1.2 if not given)."),
		"remove":     flagArg("Remove the figure."),
	}), long: true},
	{Name: "write_figure", Title: "Write a figure", Description: "Writes a figure as SVG, PDF (vector, its text in Helvetica) or PNG (300 dpi by default), or its manifest as JSON, with its manifest inside: the figure, what each panel shows and was drawn from (the dataset by its bundle's run and fingerprint, the saved view, the artifacts drawn), a digest of each panel's drawing, SituCraft's version and commit, and a fingerprint. Answers the manifest's fingerprint and digests and each panel's facts. The same figure written at the same time is the same bytes in SVG and PDF.", InputSchema: schema(map[string]any{
		"name":      textArg("The figure, by name or ID (the only one if not given)."),
		"path":      textArg("The absolute path of the file to write."),
		"format":    textArg("svg, pdf, png or json (from the path's extension if not given)."),
		"dpi":       numberArg("A PNG's resolution, and the stains' in every format (300 if not given)."),
		"time":      textArg("The time written, ISO 8601 (now if not given): the same time, the same bytes."),
		"overwrite": flagArg("Replace the file if it exists."),
	}, "path"), long: true, output: true},
	{Name: "record", Title: "What made a result", Description: "Answers what made a result, as the Reports view shows it: each step from the bundles to it (a bundle, a segmentation, a clustering, a marker set, labels, a graph, an analysis, a design, a summary, a comparison, a figure…), in an order where each comes after the steps it was made from, with its method and version, its parameters and seed, the software that ran it, the decisions about it, and the publications it follows (numbered as first cited, each with its DOI). The result is any analysis, comparison, summary, label set, clustering, map, graph, segmentation, alignment, filter or figure of the project; or the record is of a dataset's results (dataset) or the whole project's (project). format markdown or bibtex also answers the record as that text; export (what: record) writes it to a file, and export (what: certificate) certifies it.", InputSchema: schema(map[string]any{
		"result":  textArg("The result, by ID or name."),
		"dataset": textArg("A dataset instead (its results), by name or ID."),
		"project": flagArg("The whole project instead."),
		"format":  textArg("json (the default), markdown or bibtex."),
		"time":    textArg("The time recorded, ISO 8601 (now if not given)."),
	}), long: true},
	{Name: "verify_records", Title: "Compute the page's records again", Description: "Used by situcraft verify: the page's own records named (a design, a filter, a figure, by ID) computed again in the project open, a design from its samples, a filter's cells from their attributes, a figure's drawing; answers each one's id, whether it was computed (checked), whether it is the same (same), how (detail) and what differs (differ).", InputSchema: schema(map[string]any{
		"steps": map[string]any{"type": "array", "items": map[string]any{"type": "object"}, "description": "Each {id, kind (design, filter or figure), drawing (a figure's drawing as its certificate records it: {drawing, panels: [{id, letter, drawing}]})}."},
	}, "steps"), long: true},
	{Name: "verify_certificate", Title: "Verify a certificate", Description: "Verifies a certificate, a report or a result package as situcraft verify does, in SituCraft itself: each step SituCraft made is made again, in a SituCraft of its own, from the bundles this library knows (each project's datasets and the examples, found by their files' SHA-256), and every file compared bit for bit; the page's records are checked by their digests and its designs, filters and figures computed again in a headless browser. With path, the file there; with result, dataset or project, what made it, certified first. Answers the verdict (verified, differs or not checkable), each bundle found and each step's verdict, as verify's --report writes them. Needs the token with path (it reads the file).", InputSchema: schema(map[string]any{
		"path":    textArg("The absolute path of a certificate (.zip), a report (.html) or a result package (its folder or .zip)."),
		"result":  textArg("Or a result, by ID or name: certified, then verified."),
		"project": flagArg("Or the whole project's results: certified, then verified."),
		"dataset": datasetArg,
	}), long: true, readsAt: true},
	{Name: "read_figure", Title: "Read a figure back", Description: "Reads a figure SituCraft wrote (SVG, PDF, PNG or JSON) and compares it with the project: whether its manifest is as written, what changed since in each panel's view, dataset and artifacts, and whether each panel draws the same now.", InputSchema: schema(map[string]any{
		"path": textArg("The figure file on this computer."),
	}, "path"), long: true, readsAt: true},
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
		"min_qv":     map[string]any{"type": "number", "minimum": 0, "maximum": 40, "description": "The quality the modeled transcripts reach (20 if not given)."},
		"seed_check": flagArg("Run again with the next seed and report how many transcripts both runs put in the same cell."),
		"threads":    map[string]any{"type": "integer", "minimum": 1, "description": "Threads to run on (every core if not given); the result does not depend on them."},
	}
}
