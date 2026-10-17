// Guided tours of the example datasets: a few steps through the Data view, the Viewer and QC, each
// opening a view of the tissue and saying what it shows. A step is data: the view it opens (a
// saved view's shape, lib/views.js, kept out of the project; its channels name only what they
// change) and its text. The genes are among
// those the section's transcripts show most (quality 20 or more), and each zoomed step looks where
// its genes are found together (the 300 µm square where their shares of the section's transcripts
// are highest together).

import { REFINE_METHOD } from './refine.js';

// Behind colored transcripts, nuclei in gray (the first gene's color is close to the nuclear
// stain's blue).
const GRAY_NUCLEI = [{ name: 'DAPI', visible: true, color: '#8c8c8c' }];
const GRAY_NUCLEI_ONLY = [...GRAY_NUCLEI, { name: 'ATP1A1/CD45/E-Cadherin', visible: false }];

const LUNG = {
  title: 'A tour of the Lung example',
  steps: [
    {
      mode: 'data',
      title: 'The bundle, checked',
      text: 'SituCraft read every file of the bundle with its own readers and checked them against each other and against the values the run recorded. The Data view says what the run was (panel, cells, transcripts), what was found, and how much of the bundle\'s derived data are built.',
    },
    {
      mode: 'viewer',
      title: 'The section',
      text: 'The DAPI stain of the two fields of view, with the outlines of the 11,898 cells the instrument segmented (nucleus expansion of 5 µm). Scroll or pinch to zoom, drag to pan; outlines appear as cells grow large enough to see.',
      transcripts: { genes: [] },
    },
    {
      mode: 'viewer',
      title: 'Epithelium and its surroundings',
      text: 'Each dot is one transcript of quality 20 or more. EPCAM marks epithelial cells, COL5A2 fibroblasts, PLVAP the endothelium of small vessels and CD68 macrophages. Click a cell to see its transcripts by gene in the inspector.',
      transcripts: { genes: ['EPCAM', 'COL5A2', 'PLVAP', 'CD68'] },
      channels: GRAY_NUCLEI,
      camera: { cx: 750, cy: 300, umPerCss: 0.4 },
    },
    {
      mode: 'viewer',
      title: 'Vessels',
      text: 'VWF and PLVAP mark endothelial cells; RGS5 and PDGFRB the pericytes and smooth muscle cells around them.',
      transcripts: { genes: ['VWF', 'PLVAP', 'RGS5', 'PDGFRB'] },
      channels: GRAY_NUCLEI,
      camera: { cx: 450, cy: 150, umPerCss: 0.4 },
    },
    {
      mode: 'qc',
      title: 'Where transcripts fall outside cells',
      text: 'The QC view maps the section\'s quality. Here, the share of each bin\'s gene transcripts not assigned to any cell: high where cells were missed or drawn too tight. The panel on the right lists the run\'s metrics, recomputed by SituCraft from the data.',
      qc: { map: 'bin:outside' },
    },
    {
      mode: 'qc',
      title: 'Cells by their transcripts',
      text: 'Cells colored by their gene transcripts. Drag across a histogram to pick cells (they light up on the map), and save the conditions as a filter: its version, and the checksum of the cells it keeps, go into the project.',
      qc: { map: 'cell:transcripts' },
    },
    {
      mode: 'viewer',
      title: 'Keep what you find',
      text: 'Save view (at the top) keeps exactly what the viewer shows: the place, zoom, channels, genes and selected cell. Saved views are listed in the project tree, and each has a link that reopens it. The Data view writes a methods paragraph for the dataset from what the project records.',
      transcripts: { genes: ['EPCAM', 'COL5A2', 'PLVAP', 'CD68'] },
      channels: GRAY_NUCLEI,
    },
  ],
};

const BREAST = {
  title: 'A tour of the Breast example',
  steps: [
    {
      mode: 'data',
      title: 'The bundle, checked',
      text: 'SituCraft read every file of the bundle with its own readers, including its four JPEG 2000 morphology images, and checked them against each other and against the values the run recorded. The Data view says what the run was and what was found.',
    },
    {
      mode: 'viewer',
      title: 'Stains and cells',
      text: 'The DAPI (nuclei) and boundary stains, with the outlines of the cells the instrument segmented from them. The interior RNA and protein stains are a click away under Image, where each stain\'s color, brightness window and gamma can be changed.',
      transcripts: { genes: [] },
    },
    {
      mode: 'viewer',
      title: 'How each cell was found',
      text: 'Cells colored by the method that found them: from the boundary stain, from the interior RNA stain, or by expanding the nucleus where neither stain outlined the cell. The legend counts each.',
      transcripts: { genes: [] },
      cells: { outlines: true, fill: 'method', opacity: 0.55 },
    },
    {
      mode: 'viewer',
      title: 'Ducts and stroma',
      text: 'KRT7 marks luminal epithelial cells; KRT14 and ACTA2 the myoepithelial cells around them (ACTA2 also smooth muscle and myofibroblasts); LUM fibroblasts of the stroma.',
      transcripts: { genes: ['KRT7', 'KRT14', 'ACTA2', 'LUM'] },
      channels: GRAY_NUCLEI_ONLY,
      camera: { cx: 300, cy: 300, umPerCss: 0.4 },
    },
    {
      mode: 'viewer',
      title: 'Immune cells and vessels',
      text: 'PTPRC (CD45) marks immune cells, CD68 macrophages, PECAM1 the endothelium of vessels and MKI67 dividing cells. Click a cell to see its transcripts by gene.',
      transcripts: { genes: ['PTPRC', 'CD68', 'PECAM1', 'MKI67'] },
      channels: GRAY_NUCLEI_ONLY,
      camera: { cx: 150, cy: 600, umPerCss: 0.4 },
    },
    {
      mode: 'qc',
      title: 'Where transcripts fall outside cells',
      text: 'The share of each bin\'s gene transcripts not assigned to any cell: high where cells were missed or drawn too tight. The panel on the right lists the run\'s metrics, recomputed by SituCraft from the data.',
      qc: { map: 'bin:outside' },
    },
    {
      mode: 'qc',
      title: 'Cells by their transcripts',
      text: 'Cells colored by their gene transcripts. Drag across a histogram to pick cells, and save the conditions as a filter: its version, and the checksum of the cells it keeps, go into the project. Save view keeps the map, the filter and the place.',
      qc: { map: 'cell:transcripts' },
    },
  ],
};

export const TOURS = { 'xenium-lung-2fov': LUNG, 'xenium-breast-2fov': BREAST };

// --- Worked examples ------------------------------------------------------------------------------
//
// A question answered on an example dataset, step by step, saying what to expect. Besides opening
// views, a step may do something before the example goes on (do: expand the nuclei, plant errors,
// compare two segmentations, refine the cells, import the truth; cluster a segmentation's cells,
// label its clustering from a starter marker set, import the truth's types as labels, make a
// clustering and its labels again on another segmentation), show a comparison in the Compare view
// (compare: its two segmentations, the view, the reviewer's rules, the difference shown, the side
// panel's tab, 'labels' reading both segmentations' labels through it), or show a segmentation's
// clustering in the Cell types view (celltypes: its segmentation, the map's colors, the clusters
// selected, the section in view, the review queue made). A Viewer step may draw a segmentation other
// than the instrument's (segmentation), and fill its cells by the clustering's clusters or labels
// ('clusters', 'labels:<level>'). Segmentations are named by what they are: 'instrument',
// 'expansion-5' (SituCraft's nucleus expansion by 5 µm), 'planted' (errors planted for practice),
// 'refined' (SituCraft's transcript-informed segmentation), 'truth' (a simulated tissue's true cells,
// imported); a segmentation's clustering is its latest of SituCraft's, its labels the clustering's
// latest from markers.

const BREAST_COMPARE = {
  id: 'compare-expansion',
  title: 'The instrument\'s cells against a nucleus expansion',
  question: 'Where does growing each nucleus by 5 µm disagree with the cells the instrument drew from its boundary and interior stains, and which does the evidence favor there?',
  expect: 'About 3,600 cells the same in both. The expansion splits 263 of the instrument\'s cells and merges 128 pairs, and has no cell where the instrument drew 119 without a nucleus. Its spurious co-expression is higher (about 2.0 against 1.8): its cells take in more of their neighbors\' transcripts.',
  steps: [
    {
      mode: 'segmentation',
      title: 'The question',
      text: 'The instrument drew most of this section\'s cells from its boundary and interior stains. A nucleus expansion is what many pipelines do without stains: each nucleus grows by 5 µm. Where do the two disagree, and which is right there? The Segmentation view lists the dataset\'s segmentations.',
    },
    {
      mode: 'segmentation',
      title: 'Grow the nuclei',
      text: 'SituCraft grows each nucleus by up to 5 µm, every pixel going to the nearest nucleus, and gives each transcript to the cell holding its pixel. It takes seconds and is kept as an artifact.',
      do: { action: 'expand', distance: 5 },
      label: 'Expand the nuclei by 5 µm',
    },
    {
      mode: 'segmentation',
      title: 'Compare the two',
      text: 'The comparison matches the cells (a shared nucleus, or half of one within the other), classes each difference, and measures what changed: pixels, transcripts, and how often each segmentation puts transcripts of unlike cells together.',
      do: { action: 'compare', a: 'instrument', b: 'expansion-5' },
      label: 'Compare them',
    },
    {
      mode: 'compare',
      title: 'Splits and merges first',
      text: 'Both segmentations on the tissue with one camera, cells filled by their difference. The review queue is ranked by disagreement: splits, merges and cells in one only come first. Step through it with Next (or ]): here, one of the instrument\'s cells drawn from a stain holds several nuclei, and the expansion splits it.',
      compare: { a: 'instrument', b: 'expansion-5', view: 'side', rank: 'disagreement', difference: 1, tab: 'queue' },
    },
    {
      mode: 'compare',
      title: 'Where they disagree',
      text: 'The expansion\'s outlines over the instrument\'s, and each 50 µm region shaded by its share of pixels the two give to different cells: they disagree most where the stains drew large cells.',
      compare: { a: 'instrument', b: 'expansion-5', view: 'overlay', map: true },
    },
    {
      mode: 'compare',
      title: 'Transcripts in the wrong cell',
      text: 'Ranked by contamination: differences whose cell in B expresses more pairs of mutually exclusive markers than in A. The metrics above say the expansion\'s spurious co-expression is higher than the instrument\'s: its cells reach into their neighbors.',
      compare: { a: 'instrument', b: 'expansion-5', view: 'side', map: false, rank: 'contamination', difference: 1, tab: 'queue' },
    },
    {
      mode: 'compare',
      title: 'Decide, with your reason',
      text: 'Promote, keep for exploration or reject either segmentation, with a reason and this comparison recorded; nothing is deleted. Here the evidence favors the instrument\'s: keep the expansion for exploration, or reject it.',
      compare: { a: 'instrument', b: 'expansion-5', tab: 'decide' },
    },
  ],
};

const LUNG_PLANTED = {
  id: 'planted-errors',
  title: 'Find the planted errors',
  simulated: true,
  question: 'Can you find a segmentation\'s errors with the comparison workspace, and which ranking finds which kind?',
  expect: 'SituCraft plants 60 errors in a copy of the instrument\'s segmentation (simulated): 20 merges of neighboring cells of different lineages, 20 splits, 10 missed cells and 10 cells drawn 1 µm too wide. The comparison lists exactly those as differences, every other cell the same in both; ranked by contamination, the merges come first.',
  steps: [
    {
      mode: 'segmentation',
      title: 'Errors to find',
      text: 'Practice on errors whose answers are known: SituCraft plants them in a copy of the instrument\'s segmentation and keeps where it put them. The copy is simulated, named so, and is never promoted or exported.',
      do: { action: 'plant' },
      label: 'Plant the errors',
    },
    {
      mode: 'segmentation',
      title: 'Compare with the instrument\'s',
      text: 'The comparison matches every cell of the instrument\'s segmentation (A) with the planted copy\'s (B), and classes each difference.',
      do: { action: 'compare', a: 'instrument', b: 'planted' },
      label: 'Compare',
    },
    {
      mode: 'compare',
      title: 'The review queue',
      text: 'Every planted error is a difference, and nothing else is: by disagreement, the merges, splits and missed cells come first. Step through with Next (or ]) and look at each in both.',
      compare: { a: 'instrument', b: 'planted', view: 'side', rank: 'disagreement', difference: 1, tab: 'queue' },
    },
    {
      mode: 'compare',
      title: 'Merged cells mix lineages',
      text: 'Ranked by contamination: a merged cell expresses markers of two lineages (say an epithelial and an immune one) that neither cell did alone. That is how a real segmentation\'s merges show in its counts.',
      compare: { a: 'instrument', b: 'planted', rank: 'contamination', difference: 1, tab: 'queue' },
    },
    {
      mode: 'compare',
      title: 'Cells drawn too wide',
      text: 'Only expanded and contracted cells: a cell drawn too wide takes its neighbors\' pixels and transcripts. The overlay shows how far its outline reaches.',
      compare: { a: 'instrument', b: 'planted', view: 'overlay', classes: ['expanded', 'contracted'], rank: 'reassigned', difference: 1, tab: 'queue' },
    },
    {
      mode: 'segmentation',
      title: 'The answers',
      text: 'The answer key lists every planted error, what makes it one and where; Show takes you to it in the workspace. Did the queue find them all?',
      reveal: true,
    },
    {
      mode: 'compare',
      title: 'Decide',
      text: 'Reject the planted segmentation, with your reason and the comparison recorded. A planted segmentation can never be promoted.',
      compare: { a: 'instrument', b: 'planted', view: 'side', classes: ['split', 'merged', 'only in A', 'expanded', 'contracted'], tab: 'decide' },
    },
  ],
};

const BREAST_REFINE = {
  id: 'refine-compare',
  title: 'Redraw the cells from their transcripts',
  question: 'How does SituCraft\'s transcript-informed segmentation redraw the instrument\'s cells, and where is it unsure of itself?',
  expect: 'It keeps all 7,275 of the instrument\'s cells and redraws their outlines from where their transcripts lie, in about two minutes on a laptop. It assigns about 97% of gene transcripts to cells (the instrument 87%) and moves about 23% of them to another cell: about 4,700 cells stay as they were, 1,700 grow and 760 shrink. Its spurious co-expression is a little higher (1.83 against 1.77): there is no truth here to say which is right, only evidence to weigh.',
  steps: [
    {
      mode: 'segmentation',
      title: 'The question',
      text: 'The instrument drew these cells from its stains. SituCraft\'s transcript-informed segmentation (after Jones et al. 2025) redraws each one from where its transcripts lie, starting from the instrument\'s, and says how sure it is of each boundary and each transcript.',
    },
    {
      mode: 'segmentation',
      title: 'Refine the cells',
      text: 'With the Balanced settings: the model samples the cells\' boundaries, their expression and the background many times over, and keeps where each cell and transcript most often was. It takes about two minutes, and is compared with the instrument\'s at once.',
      do: { action: 'refine', preset: 'balanced' },
      label: 'Refine the cells',
    },
    {
      mode: 'viewer',
      title: 'How sure each boundary is',
      text: 'The refined cells over the stains, filled by how sure their boundaries are: bright where the samples disagreed, dark where every one agreed. Click a cell for where it came from, its transcripts and what the model flags about it.',
      segmentation: 'refined',
      cells: { fill: 'certainty', opacity: 0.6 },
      camera: { cx: 300, cy: 300, umPerCss: 0.4 },
    },
    {
      mode: 'viewer',
      title: 'Doubtful transcripts',
      text: 'Close up, each transcript is drawn by its probability of being in the cell it is in: the bright ones are those the model is least sure of, most of them where neighboring cells express the same genes.',
      segmentation: 'refined',
      transcripts: { all: true, colorBy: 'probability' },
      camera: { cx: 300, cy: 300, umPerCss: 0.15 },
    },
    {
      mode: 'compare',
      title: 'What moved',
      text: 'The instrument\'s cells (A) and the refined ones (B) with one camera, ranked by the transcripts that changed cell. Step through with Next (or ]): each difference shows which cells gained and lost transcripts, and the model\'s flags for them.',
      compare: { a: 'instrument', b: 'refined', view: 'side', rank: 'reassigned', difference: 1, tab: 'queue' },
    },
    {
      mode: 'compare',
      title: 'Decide, with your reason',
      text: 'Promote the refinement as the dataset\'s default, keep it for exploration, or reject it, with your reason and this comparison recorded. Nothing is deleted.',
      compare: { a: 'instrument', b: 'refined', tab: 'decide' },
    },
  ],
};

const SIMULATED_TRUTH = {
  id: 'score-against-truth',
  title: 'Score segmentations against the truth',
  simulated: true,
  question: 'When every cell is known, how close do the instrument\'s nucleus expansion and SituCraft\'s transcript-informed segmentation come to the true cells?',
  expect: 'Against the true cells, the instrument\'s expansion has a median IoU of 0.62 and puts about 82% of the true cells\' transcripts in them; SituCraft\'s refinement, made in about half a minute, a median IoU of 0.72 and about 85%. In the workspace, about 20% of the transcripts change cell from the truth to the instrument\'s, 17% to the refinement\'s.',
  steps: [
    {
      mode: 'data',
      title: 'A tissue whose cells are known',
      text: 'SituCraft simulated this tissue on this computer: cells of four types in domains, their nuclei, their transcripts drifted a little, a background, and an instrument that grows each nucleus by 5 µm. Every file is laid out as a Xenium bundle\'s, labeled simulated; its truth is in its truth folder.',
    },
    {
      mode: 'segmentation',
      title: 'Import the truth',
      text: 'The truth is shaped as a Xenium Ranger output of the same run: imported, it is a segmentation like any other, and any other can be compared with it.',
      do: { action: 'import-truth' },
      label: 'Import the truth',
    },
    {
      mode: 'segmentation',
      title: 'Refine the cells',
      text: 'SituCraft\'s transcript-informed segmentation of the instrument\'s cells, with the Balanced settings: about half a minute.',
      do: { action: 'refine', preset: 'balanced' },
      label: 'Refine the cells',
    },
    {
      mode: 'segmentation',
      title: 'Score the instrument\'s',
      text: 'The truth (A) compared with the instrument\'s expansion (B): how much of each true cell its cells cover, and which transcripts they put elsewhere.',
      do: { action: 'compare', a: 'truth', b: 'instrument' },
      label: 'Compare with the truth',
    },
    {
      mode: 'segmentation',
      title: 'Score the refinement',
      text: 'The truth (A) compared with the refinement (B), measured the same way.',
      do: { action: 'compare', a: 'truth', b: 'refined' },
      label: 'Compare with the truth',
    },
    {
      mode: 'compare',
      title: 'The refinement against the truth',
      text: 'The true cells and the refined ones overlaid. The median IoU above (each true cell\'s overlap with its refined one) and the transcripts that changed cell say how close it came; open the other comparison (the selector at the top) to see the instrument\'s.',
      compare: { a: 'truth', b: 'refined', view: 'overlay', rank: 'disagreement', difference: 1, tab: 'queue' },
    },
    {
      mode: 'viewer',
      title: 'Where it is unsure',
      text: 'The refined cells filled by how sure their boundaries are, bright where the samples disagreed. Compare with the truth: the doubtful cells are where it is most often wrong.',
      segmentation: 'refined',
      cells: { fill: 'certainty', opacity: 0.6 },
      camera: { cx: 250, cy: 250, umPerCss: 0.3 },
    },
  ],
};

const LUNG_CELL_TYPES = {
  id: 'cluster-label',
  title: 'Cluster and label the Lung cells',
  question: 'Which cells are in this section, as their expression groups them and as known markers name them, and how far can each label be trusted?',
  expect: 'SituCraft clusters the 11,820 cells with at least 10 gene transcripts into 29 clusters and lays them out on a map (trustworthiness 0.74). From the panel\'s starter markers, 47% of the cells take a class, 12% a lineage and 1% a subtype. The section is mostly tumor, whose cells a normal lung\'s lineage markers do not describe: about half of them stay unlabeled, and the clusters\' markers say what they are. The review queue lists about 2,000 of the 5,555 labeled cells.',
  steps: [
    {
      mode: 'celltypes',
      title: 'The question',
      text: 'The Cell types view groups a segmentation\'s cells by their gene expression and names them from known markers. Each result is an artifact of the segmentation, every parameter recorded, so it can be made again on another and the two compared.',
    },
    {
      mode: 'celltypes',
      title: 'Cluster the cells',
      text: 'scanpy\'s steps and defaults: each cell\'s counts scaled to the median total and log-transformed, 50 principal components, each cell\'s 15 nearest neighbors, and Leiden at resolution 1, seeded. The 78 cells with fewer than 10 gene transcripts are left out. It takes seconds, and the map is made at once.',
      do: { action: 'cluster' },
      label: 'Cluster the cells',
    },
    {
      mode: 'celltypes',
      title: 'The map and the tissue',
      text: 'Each cell on the map (UMAP of the clustering\'s graph) and on the tissue, in its cluster\'s color, linked: a cluster, a cell or a lasso on either selects it on both. The note under the map says how faithful it is (trustworthiness 0.74): it shows which cells are alike, not how alike. Judge relationships on the clusters and their markers.',
      celltypes: { color: 'clusters' },
    },
    {
      mode: 'celltypes',
      title: 'A cluster by its markers',
      text: 'Cluster 13 selected on the map lights up its cells on the tissue, along the vessels. In the table of clusters below, its leading markers are VWF, CD34 and KDR, endothelial genes (Markers of the selection finds them for any selection). A cluster\'s markers (the Wilcoxon rank-sum test) are exploratory: the cells were grouped by the same genes.',
      celltypes: { color: 'clusters', select: [13] },
    },
    {
      mode: 'celltypes',
      title: 'Label from markers',
      text: 'The panel\'s starter marker set names a lung\'s classes, lineages and subtypes by positive and negative markers, each cited. Each cell is scored for every label as scanpy\'s score_genes scores it, and takes a label, from the top level down, where it leads the next by the margin; its lead is its confidence.',
      do: { action: 'label', markers: 'lung' },
      label: 'Label the cells',
    },
    {
      mode: 'celltypes',
      title: 'The labels, cluster by cluster',
      text: 'The map and the tissue above are colored by class: 47% of the cells take one, most of them Epithelial; 12% take a lineage and 1% a subtype. The label set\'s card gives each cluster\'s labels at each level: cluster 13 is Endothelial (48% of its cells), cluster 10 Stromal (56%; pericytes at the lineage level).',
      celltypes: { color: 'labels', section: 'labels' },
    },
    {
      mode: 'celltypes',
      title: 'Why so many cells have no label',
      text: 'The largest cluster, selected, is the tumor: 57% of its cells take no label. They express CD24, CXCL13 and TM4SF4 rather than a normal lung\'s lineage markers, and on a panel built of markers those genes also fill the score\'s control bins, canceling the epithelial score. The clusters\' markers name what the labels cannot: name such clusters yourself, or label from a reference of the tissue.',
      celltypes: { color: 'labels', select: [1], section: 'clusters' },
    },
    {
      mode: 'celltypes',
      title: 'Cells to review',
      text: 'The review queue: labeled cells whose label few of their ten nearest cells in the tissue share (implausible), and those of low confidence (a lead under twice the margin), about 2,000 of the 5,555 labeled. Choose one to see its evidence: its markers\' transcripts for its label and the next. Accept or relabel it, saying why: each review is kept in a curated set.',
      celltypes: { color: 'labels', section: 'review', review: true },
    },
    {
      mode: 'viewer',
      title: 'In the Viewer',
      text: 'The Viewer\'s Fill by lists each clustering and label set of the segmentation drawn: here each cell filled by its class over the stains, with a legend (choose the lineage level there for the 12% that take one). Save view keeps it; the Data view\'s methods paragraph cites the methods and the marker set.',
      cells: { outlines: true, fill: 'labels:0', opacity: 0.7 },
      transcripts: { genes: [] },
      channels: GRAY_NUCLEI,
    },
  ],
};

const SIMULATED_LABELS = {
  id: 'labels-against-truth',
  title: 'Labels against the truth',
  simulated: true,
  question: 'When every cell\'s type is known, how many cells do markers label correctly, and how much of the error is the segmentation\'s?',
  expect: 'Labeled from the simulated types\' own markers, 99% of the instrument\'s 2,925 cells analyzed take a type. Of the cells matched to a true cell and labeled, 98.3% take their true type; most errors are immune-like cells, the smallest, into which the 5 µm expansion draws neighbors\' transcripts (marker mixing 0.22, against the true cells\' 0.06). Under SituCraft\'s refinement, 99.9% take their true type, marker mixing 0.09.',
  steps: [
    {
      mode: 'data',
      title: 'A tissue whose cells and types are known',
      text: 'SituCraft simulated this tissue on this computer: cells of four types, each expressing its own markers, and an instrument that grows each nucleus by 5 µm. Its truth folder holds every true cell and its type.',
    },
    {
      mode: 'segmentation',
      title: 'Import the true cells',
      text: 'The truth is shaped as a Xenium Ranger output of the same run: imported, it is a segmentation like any other, and any other can be compared with it.',
      do: { action: 'import-truth' },
      label: 'Import the truth',
    },
    {
      mode: 'celltypes',
      title: 'Import their types',
      text: 'The truth\'s cells.parquet names each true cell\'s type. Imported for the true cells, matched by cell id, the types are a label set like any other, kept with the file\'s SHA-256.',
      do: { action: 'import-truth-labels' },
      label: 'Import the true types',
    },
    {
      mode: 'celltypes',
      title: 'Cluster the instrument\'s cells',
      text: 'The cells of the instrument\'s segmentation clustered with the defaults: 5 clusters of 2,925 cells. The map is made at once.',
      do: { action: 'cluster' },
      label: 'Cluster the cells',
    },
    {
      mode: 'celltypes',
      title: 'Label them from the types\' markers',
      text: 'The simulation offers its types\' own markers as a starter set. Each cell takes the type whose markers lead by the margin: 99% of the cells take one. On the map, each cluster is one type.',
      do: { action: 'label', markers: 'simulated' },
      label: 'Label the cells',
    },
    {
      mode: 'segmentation',
      title: 'Match the cells to the true ones',
      text: 'The comparison matches each of the instrument\'s cells to the true cell sharing its nucleus (or most of one): the cells whose label can be checked against a true type.',
      do: { action: 'compare', a: 'truth', b: 'instrument' },
      label: 'Compare with the truth',
    },
    {
      mode: 'compare',
      title: 'Each label against its true type',
      text: 'The Labels tab reads the true types (A) and the labels (B) through the comparison: of the matched cells labeled, 98.3% take their true type. The table gives each type\'s cells under each, the cross-table where they go; the tissue is colored by both. Most errors are immune-like cells: the expansion draws their neighbors\' transcripts into them, and marker mixing (MECR) is 0.22 against the true cells\' 0.06.',
      compare: { a: 'truth', b: 'instrument', view: 'side', tab: 'labels' },
    },
    {
      mode: 'segmentation',
      title: 'Refine the cells',
      text: 'SituCraft\'s transcript-informed segmentation of the instrument\'s cells, with the Balanced settings: about half a minute.',
      do: { action: 'refine', preset: 'balanced' },
      label: 'Refine the cells',
    },
    {
      mode: 'segmentation',
      title: 'Match the refined cells to the true ones',
      text: 'The truth (A) compared with the refinement (B), as before.',
      do: { action: 'compare', a: 'truth', b: 'refined' },
      label: 'Compare with the truth',
    },
    {
      mode: 'celltypes',
      title: 'Label the refined cells the same way',
      text: 'The instrument\'s clustering and its labels made again on the refined cells in one step, with the same parameters and markers.',
      do: { action: 'remake', from: 'instrument', to: 'refined' },
      label: 'Make them again',
    },
    {
      mode: 'compare',
      title: 'The refinement\'s labels against the truth',
      text: 'Under the refinement, 99.9% of the matched cells take their true type, and marker mixing falls to 0.09: cells drawn nearer the true ones hold fewer of their neighbors\' transcripts, and are labeled better. Real data have no truth; the Breast example reads two segmentations\' labels against each other.',
      compare: { a: 'truth', b: 'refined', view: 'side', tab: 'labels' },
    },
  ],
};

const BREAST_LABELS = {
  id: 'labels-two-segmentations',
  title: 'Labels under two segmentations',
  question: 'How much do the cells\' labels depend on the segmentation they were counted in, and which cells does the choice change?',
  expect: 'From the panel\'s starter markers, 81% of the instrument\'s 7,163 cells analyzed take a class; made again on a 5 µm nucleus expansion, 82%. Of the 6,512 cells the two match one to one, 96.9% of those labeled under both keep their class; 223 labeled cells are split by the expansion and 106 of its cells merge the instrument\'s, and marker mixing rises from 0.047 to 0.051. The review queue lists the 688 cells whose label changes under the expansion.',
  steps: [
    {
      mode: 'celltypes',
      title: 'The question',
      text: 'A cell\'s label is read from the transcripts its outline holds. Drawn otherwise, a cell may gain a neighbor\'s transcripts, be split or merged, and its label change. Here the instrument\'s cells, drawn from stains, and a 5 µm nucleus expansion are labeled the same way and read against each other.',
    },
    {
      mode: 'celltypes',
      title: 'Cluster the instrument\'s cells',
      text: 'With the defaults: 15 clusters of the 7,163 cells with at least 10 gene transcripts. The map is made at once.',
      do: { action: 'cluster' },
      label: 'Cluster the cells',
    },
    {
      mode: 'celltypes',
      title: 'Label them from markers',
      text: 'The panel\'s starter marker set names a breast\'s classes, lineages and subtypes. 81% of the cells take a class, 49% a lineage and 6% a subtype; the proliferating state marks 1,886, most of them in the cluster led by TOP2A and MKI67.',
      do: { action: 'label', markers: 'breast' },
      label: 'Label the cells',
    },
    {
      mode: 'celltypes',
      title: 'The labels',
      text: 'Colored by class on the map and the tissue: Epithelial (luminal and basal), Immune, Stromal, Endothelial. The label set\'s card gives each cluster\'s labels.',
      celltypes: { color: 'labels', section: 'labels' },
    },
    {
      mode: 'segmentation',
      title: 'Grow the nuclei',
      text: 'The other segmentation: each nucleus grown by up to 5 µm, every pixel going to the nearest nucleus, as many pipelines do without stains.',
      do: { action: 'expand', distance: 5 },
      label: 'Expand the nuclei by 5 µm',
    },
    {
      mode: 'segmentation',
      title: 'Compare the two',
      text: 'The comparison matches the cells (a shared nucleus, or half of one within the other) and classes each difference: the matches through which the labels are read.',
      do: { action: 'compare', a: 'instrument', b: 'expansion-5' },
      label: 'Compare them',
    },
    {
      mode: 'celltypes',
      title: 'Label the expansion\'s cells the same way',
      text: 'The clustering and its labels made again on the expansion in one step, with the same parameters and markers: 17 clusters of 7,284 cells, 82% taking a class.',
      do: { action: 'remake', from: 'instrument', to: 'expansion-5' },
      label: 'Make them again',
    },
    {
      mode: 'compare',
      title: 'Both read through the comparison',
      text: 'The Labels tab: of the 6,512 cells matched one to one, 96.9% of those labeled under both keep their class, and 920 are labeled under one only. The cross-table lists where the rest go; the tissue is colored by each side\'s labels. Choose a finer level above: subtypes, read from fewer transcripts, change more.',
      compare: { a: 'instrument', b: 'expansion-5', view: 'side', tab: 'labels' },
    },
    {
      mode: 'compare',
      title: 'Split and merged cells',
      text: '223 of the instrument\'s labeled cells are split by the expansion: every piece keeps the cell\'s label in 66%, none in 5%. 106 of the expansion\'s cells merge the instrument\'s, 2% joining cells of two labels or more. Marker mixing rises from 0.047 to 0.051, the expansion\'s cells reaching into their neighbors.',
      compare: { a: 'instrument', b: 'expansion-5', view: 'overlay', tab: 'labels' },
    },
    {
      mode: 'celltypes',
      title: 'Cells whose label depends on the segmentation',
      text: 'The review queue, its labels read against the expansion\'s: the 688 cells whose counterparts carry another label there are listed with what they carry, beside the implausible and low-confidence ones. These are the labels another segmentation would change: review them, or report them as uncertain.',
      celltypes: { color: 'labels', section: 'review', review: true },
    },
  ],
};

// The worked examples, by example dataset.
// The simulated tissue's H&E (wave 5): its truth says where each of its pixels lies, so each way of
// placing it can be scored, and each cell's true type, so a reviewer's decisions can be.
const HE_ON_STAINS = { channels: [{ name: 'DAPI', visible: false }], images: { he: { visible: true, opacity: 1 } }, transcripts: { genes: [] }, cells: { outlines: false } };

const SIMULATED_ALIGN = {
  id: 'align-he',
  title: 'Align an H&E three ways',
  simulated: true,
  images: true,
  question: 'The section was stained with H&E and scanned after the run, the image turned, mirrored and slightly stretched. Placed by Xenium Explorer\'s matrix, by six landmarks, or automatically by the nuclei: which places it best, and how do you tell without a truth?',
  expect: 'Checked over the tissue in µm, the matrix is off by 0.63 µm (median) and 1.09 µm (90th percentile), the six landmarks by 0.58 and 1.19 µm (they fit their own points within 0.57 µm, but are 1.0 µm off where none is), and the automatic version, with its deformation, by 0.41 and 0.62 µm. The simulation\'s truth agrees: truly 0.69 and 1.12 µm, 0.72 and 1.22 µm, 0.47 and 0.75 µm. Without a truth, the check ranks them as the truth does.',
  steps: [
    {
      mode: 'viewer',
      title: 'The section, and its H&E',
      text: 'The simulated tissue\'s DAPI. Beside the bundle, as 10x supplies them, an H&E of the same section and the transformation matrix Xenium Explorer saves when an image is aligned by hand: here the simulation\'s own transform, without the stretch of the section.',
      do: { action: 'add-image', image: 'he', alignment: 'matrix' },
      label: 'Add the H&E',
      transcripts: { genes: [] },
      cells: { outlines: false },
    },
    {
      mode: 'viewer',
      title: 'Placed by the matrix',
      text: 'The H&E over the stains as the matrix places it: turned 2.5°, mirrored, 0.36 µm a pixel. Zoomed in, its nuclei lie on the cells\' (outlines on); by eye it looks right.',
      ...HE_ON_STAINS,
      cells: { outlines: true },
      camera: { cx: 250, cy: 250, umPerCss: 0.25 },
    },
    {
      mode: 'alignment',
      title: 'Six landmarks',
      text: 'The Alignment view: the Xenium image on the left, the H&E on the right. A landmark is the same point clicked on both; here six, placed where the truth puts six points of the H&E, as a careful hand clicking a nucleus on each would place them. Fitted as an affine transform, each landmark\'s residual and its error left out are listed.',
      do: { action: 'landmarks', image: 'he', count: 6, within: [40, 40, 460, 460], model: 'affine' },
      label: 'Place six landmarks',
      alignment: { image: 'he', show: 'in use' },
    },
    {
      mode: 'alignment',
      title: 'Automatically, by the nuclei',
      text: 'The landmarks\' version over the stains. Next, the H&E aligned automatically: its hematoxylin against DAPI, its scale, turn and mirroring found, refined by mutual information, fitted to local matches over the tissue, and a smooth deformation after it when it predicts the matches better.',
      do: { action: 'register', image: 'he', model: 'affine', nonrigid: true },
      label: 'Align it automatically',
      alignment: { image: 'he', show: 'landmarks', display: 'overlay', opacity: 0.7 },
    },
    {
      mode: 'alignment',
      title: 'Each checked in µm',
      text: 'The automatic version, DAPI\'s edges in yellow over the H&E\'s nuclei. Each version is now checked where it can be wrong: everywhere between its landmarks or matches, by patches of DAPI over the tissue, each compared with the H&E drawn there.',
      do: { action: 'check', image: 'he', versions: ['matrix', 'landmarks', 'automatic'] },
      label: 'Check all three',
      alignment: { image: 'he', show: 'automatic', display: 'overlay', opacity: 1, edges: true },
    },
    {
      mode: 'alignment',
      title: 'Where the matrix is off',
      text: 'Each of the 25 patches colored by how far the matrix draws the H&E from where it belongs (green at 0, yellow at 1 µm, red at 2 µm or more), with an arrow twenty times as long: off by 0.63 µm (median) and 1.09 µm (90th percentile), most where the section stretched. The landmarks\' version is off by 0.58 and 1.19 µm, the automatic one by 0.41 and 0.62 µm; choose one under See and check to see its own map.',
      alignment: { image: 'he', show: 'matrix', display: 'overlay', opacity: 0.6, map: true },
    },
    {
      mode: 'alignment',
      title: 'The automatic version, square by square',
      text: 'A checkerboard of the H&E and the stains in 40 µm squares, the automatic version placing the H&E: nuclei run straight across the squares\' edges. Its deformation follows the stretch no single transform can; it is kept only because it predicts held-out matches better.',
      alignment: { image: 'he', show: 'automatic', display: 'checkerboard', cell: 40, map: false },
    },
    {
      mode: 'viewer',
      title: 'The H&E on the tissue',
      text: 'The automatic version places the H&E in the Viewer now, a decision kept in the project with its reason; every version stays, with its check. Real images have no truth: the check in µm is what tells versions apart.',
      ...HE_ON_STAINS,
      cells: { outlines: true },
      camera: { cx: 380, cy: 380, umPerCss: 0.2 },
    },
  ],
};

const SIMULATED_REGIONS = {
  id: 'regions-qupath',
  title: 'Regions for QuPath',
  simulated: true,
  images: true,
  question: 'Draw the epithelial and the immune domains on the H&E, count their cells, and take the regions to QuPath in the H&E\'s own pixels: do they come back as they were?',
  expect: 'The epithelial domain\'s region holds 1,043 cells over 0.11 mm², 42% of them epithelial-like; the immune domain\'s, 475 cells, 67% immune-like: domains mix their types. Written in the H&E\'s pixels through the automatic alignment and its deformation, then read back, each region keeps its area (within 0.05 µm²) and its cells.',
  steps: [
    {
      mode: 'viewer',
      title: 'The H&E',
      text: 'Regions are drawn on the tissue over any layer; to take them to QuPath on the H&E, the H&E must be placed. Added with its matrix (as 10x supplies it), then aligned automatically for the best placement.',
      do: { action: 'add-image', image: 'he', alignment: 'matrix' },
      label: 'Add the H&E',
      transcripts: { genes: [] },
      cells: { outlines: false },
    },
    {
      mode: 'viewer',
      title: 'Aligned automatically',
      text: 'The H&E over the stains. Aligned automatically by its nuclei, with a smooth deformation where the section stretched (the worked example Align an H&E three ways compares it with the matrix).',
      do: { action: 'register', image: 'he', model: 'affine', nonrigid: true },
      label: 'Align it automatically',
      ...HE_ON_STAINS,
    },
    {
      mode: 'viewer',
      title: 'Two regions',
      text: 'Drawn on the H&E as a pathologist would draw them with the polygon tool: the epithelial domain on the left, classed Epithelium (a class of its own), and the immune domain at the lower right, classed Immune cells (one of QuPath\'s). The Viewer\'s Regions lists each with its area and the cells whose centers lie in it.',
      do: { action: 'draw-regions', regions: [{ name: 'Epithelial domain', cls: 'Epithelium', polygon: [[15, 15], [235, 15], [245, 250], [225, 490], [15, 490]] }, { name: 'Immune domain', cls: 'Immune cells', polygon: [[360, 270], [490, 260], [490, 490], [340, 490], [330, 380]] }] },
      label: 'Draw the two regions',
      ...HE_ON_STAINS,
      cells: { outlines: true },
    },
    {
      mode: 'viewer',
      title: 'What their cells are',
      text: 'The cells filled by their true types (the simulation\'s, imported as labels of the instrument\'s cells): 440 of the epithelial domain\'s 1,043 cells are epithelial-like, 304 fibroblast-like and 204 immune-like; 319 of the immune domain\'s 475 are immune-like. A region is an area, not a cell type: its cells\' labels say what is in it.',
      do: { action: 'import-instrument-types' },
      label: 'Show their true types',
      cells: { outlines: true, fill: 'true-types', opacity: 0.6 },
      transcripts: { genes: [] },
    },
    {
      mode: 'viewer',
      title: 'For QuPath, in the H&E\'s pixels',
      text: 'The regions written as QuPath reads annotations, in the H&E\'s full-resolution pixels: each point taken back through the deformation and the transform. In QuPath, open simulated_he_image.ome.tif from the example\'s post_xenium folder and import the file: the regions lie on the same nuclei. (Regions, GeoJSON, as QuPath reads and writes it, does the same for any region.)',
      do: { action: 'write-regions', image: 'he' },
      label: 'Write them for QuPath',
      cells: { outlines: true, fill: 'true-types', opacity: 0.6 },
      transcripts: { genes: [] },
    },
    {
      mode: 'viewer',
      title: 'And back',
      text: 'As QuPath\'s annotations come back: the file read in the H&E\'s pixels, each point placed through the alignment in use. Each region comes back (named with (2)) with its area within 0.05 µm² of the original and the same cells. Annotations drawn in QuPath and exported as GeoJSON are read the same way, with Read a file….',
      do: { action: 'read-regions', image: 'he' },
      label: 'Read them back',
      ...HE_ON_STAINS,
      cells: { outlines: true },
    },
  ],
};

const SIMULATED_REVIEW = {
  id: 'pathologist-review',
  title: 'A pathologist\'s review',
  simulated: true,
  images: true,
  question: 'Labels from markers are predictions. Which cells should a pathologist look at first, what do the decisions change, and what does the record keep of each?',
  expect: 'The queue holds 662 of the 2,887 labeled cells; of its first twenty, 2 are mislabeled (10%, against 1.7% of all the labels). Decided against the truth, 18 are accepted and 2 relabeled, each with its reason, and a fold\'s 43 cells are excluded. The record keeps 22 reviews, each with who, why, the value before and after, the label set, the cell\'s label and its confidence, and the H&E\'s alignment in use.',
  steps: [
    {
      mode: 'celltypes',
      title: 'Cluster the cells',
      text: 'The instrument\'s cells clustered with the defaults: 5 clusters of 2,925 cells.',
      do: { action: 'cluster' },
      label: 'Cluster the cells',
    },
    {
      mode: 'celltypes',
      title: 'Label them from the types\' markers',
      text: 'Each cell takes the simulated type whose markers lead by the margin: 2,887 of the 2,925 take one.',
      do: { action: 'label', markers: 'simulated' },
      label: 'Label the cells',
    },
    {
      mode: 'viewer',
      title: 'The H&E',
      text: 'A pathologist reviews against the H&E. Added with its matrix; every review records the version of its alignment in use, as a later version may place it elsewhere.',
      do: { action: 'add-image', image: 'he', alignment: 'matrix' },
      label: 'Add the H&E',
      transcripts: { genes: [] },
      cells: { outlines: false },
    },
    {
      mode: 'celltypes',
      title: 'The cells to look at first',
      text: 'The review queue ranks the labeled cells whose label few of their ten nearest cells share while most share another, and those of low confidence: 662 of the 2,887. Of all the labels, 49 (1.7%) are not the cell\'s true type; of the queue\'s first twenty, 2 are (10%): it finds mislabels six times as often as looking at random.',
      celltypes: { color: 'labels', review: true, section: 'review', against: 'none' },
    },
    {
      mode: 'celltypes',
      title: 'Decide the first twenty',
      text: 'Each of the first twenty decided as a reviewer who knows its type would (here, the simulation\'s truth): 18 accepted (mostly immune-like cells among others, where they belong though few of their neighbors share their label) and 2 relabeled, each with its reason. Every decision is a review, recorded with the reviewer, the label before and after, the label set, the cell\'s ID and its label\'s confidence, and the H&E\'s alignment in use; the relabels go to a curated set begun from the markers\' labels, with 47 wrong labels where they had 49.',
      do: { action: 'review-queue', first: 20 },
      label: 'Decide them',
      celltypes: { color: 'labels', review: true, section: 'review', against: 'none' },
    },
    {
      mode: 'viewer',
      title: 'Leave out a fold',
      text: 'A fold in the section at the top: drawn as a region of class Ignore* and excluded from analyses, with its reason, and the cells in it with it. An exclusion is taken back by including them again: nothing is deleted.',
      do: { action: 'exclude-fold', region: { name: 'Fold', cls: 'Ignore*', rectangle: [255, 10, 320, 60] }, note: 'A fold in the section' },
      label: 'Exclude the fold',
      ...HE_ON_STAINS,
      cells: { outlines: true },
      camera: { cx: 290, cy: 60, umPerCss: 0.3 },
    },
    {
      mode: 'viewer',
      title: 'The record',
      text: 'The cells colored by their review: green accepted, blue relabeled, gray excluded (the fold\'s 43 cells). The Viewer\'s Reviews counts them and lists all 22 reviews, each going to its cell or region; Write the record writes them as CSV (a row a cell) or JSON. A cell\'s inspector lists its own reviews.',
      cells: { outlines: true, fill: 'review', opacity: 0.7 },
      transcripts: { genes: [] },
    },
  ],
};

export const WORKED = { 'xenium-breast-2fov': [BREAST_COMPARE, BREAST_REFINE, BREAST_LABELS], 'xenium-lung-2fov': [LUNG_PLANTED, LUNG_CELL_TYPES], 'simulated-standard': [SIMULATED_TRUTH, SIMULATED_LABELS, SIMULATED_ALIGN, SIMULATED_REGIONS, SIMULATED_REVIEW] };

export function workedExamples(example) {
  return WORKED[example] ?? [];
}

// An example's tour (name 'tour' or none) or worked example (by its id).
export function guideFor(example, name = 'tour') {
  if (!name || name === 'tour') return tourFor(example);
  return workedExamples(example).find((w) => w.id === name) ?? null;
}

// A segmentation of a dataset's records by what it is: 'instrument', 'expansion-<µm>', 'planted',
// 'refined' (the latest transcript-informed segmentation), 'truth' (a simulated tissue's truth,
// imported from its truth folder).
export function segmentationByRole(segmentations, role) {
  const complete = segmentations.filter((s) => s.state === 'complete');
  if (role === 'instrument') return complete.find((s) => s.source === 'instrument') ?? null;
  if (role === 'planted') return complete.find((s) => s.source === 'planted') ?? null;
  if (role === 'refined') return [...complete].reverse().find((s) => s.method?.name === REFINE_METHOD) ?? null;
  if (role === 'truth') return complete.find((s) => s.source === 'imported' && (s.sources ?? []).some((src) => /[\\/]truth[\\/]?$/.test(src.path))) ?? null;
  const m = /^expansion-(\d+(?:\.\d+)?)$/.exec(role);
  if (m) return complete.find((s) => s.source === 'situcraft' && s.parameters?.distance_um === Number(m[1])) ?? null;
  return null;
}

export function tourFor(example) {
  return TOURS[example] ?? null;
}

// The view a step opens on a dataset (null for a step of another view than the Viewer or QC). Its id marks it as a tour's: it
// is applied, never kept. A step without a camera shows the whole section.
export function stepView(step, dataset, index) {
  if (step.mode !== 'viewer' && step.mode !== 'qc') return null;
  const view = {
    id: `tour-${index}`,
    name: step.title,
    dataset,
    mode: step.mode,
    segmentation: 'instrument',
    camera: step.camera ? { ...step.camera } : null,
    channels: (step.channels ?? []).map((c) => ({ ...c })),
    transcripts: { visible: true, all: step.transcripts?.all ?? false, genes: [...(step.transcripts?.genes ?? [])], ...(step.transcripts?.colorBy ? { colorBy: step.transcripts.colorBy } : {}) },
    cells: { outlines: true, nuclei: false, fill: null, opacity: 0.55, ...(step.cells ?? {}) },
    cell: null,
  };
  if (step.mode === 'qc') view.qc = { map: step.qc?.map ?? 'none', filter: null };
  return view;
}

// The genes the tours name, by example (for the checks that every one is in its panel).
export function tourGenes(example) {
  const tour = tourFor(example);
  return tour ? [...new Set(tour.steps.flatMap((s) => s.transcripts?.genes ?? []))] : [];
}
