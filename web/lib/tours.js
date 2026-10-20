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

// --- The spatial worked examples (wave 6) -----------------------------------------------------
//
// A step may build a graph (do: graph, of a segmentation by role, of a kind), ask a spatial
// question (do: ask: its question, of a segmentation's labels from markers or the truth's types,
// at a level from 0, over its latest graph of a kind; a region's edge by its name; niches' window
// and count), or make an answer again on another segmentation (do: remake-analysis); show an answer
// in the Spatial view (spatial: the question as asked, what in it is chosen, and to: the answer made
// again on another segmentation, read with it) or its cells on the tissue (cells_of: the question,
// and a pair, a label in a zone of an edge, a gene's local class or a niche). A Viewer step fills
// the true cells by a question's niches ('niches', with niches: the question) or the truth's
// domains ('true-domains').

const CLASSES = { segmentation: 'instrument', level: 0 };
const LINEAGES = { segmentation: 'instrument', level: 1 };

const BREAST_NEIGHBORS = {
  id: 'neighbors-breast',
  title: 'Which cell types are neighbors',
  question: 'In this breast cancer section, which kinds of cells lie next to each other more often than chance, and which keep apart?',
  expect: 'The cells are clustered and labeled from the breast starter set (81% take a class, 49% a lineage). Over the Delaunay graph, 21,678 edges among 7,275 cells, each class is its own neighbor more often than chance (immune cells with immune cells 3.90 times as often), and the epithelial cells keep apart from the stromal (0.24 times) and the immune (0.32 times): the tumor grows in nests the immune cells do not enter. Among the lineages, T cells lie next to myeloid cells 4.4 times as often as chance.',
  steps: [
    {
      mode: 'spatial',
      title: 'The question',
      text: 'The Spatial view asks how the tissue is organized. Every question starts from a graph of which cells are neighbors, and is asked of a label set: here the breast starter set\'s classes, then its lineages.',
    },
    {
      mode: 'celltypes',
      title: 'Cluster the cells',
      text: 'scanpy\'s steps and defaults: 15 clusters of the 7,163 cells with at least 10 gene transcripts. The map is made at once.',
      do: { action: 'cluster' },
      label: 'Cluster the cells',
    },
    {
      mode: 'celltypes',
      title: 'Label them from markers',
      text: 'The panel\'s breast starter set names classes, lineages and subtypes by their markers: 81% of the cells take a class, 49% a lineage and 6% a subtype. Cells that take no lineage keep their class.',
      do: { action: 'label', markers: 'breast' },
      label: 'Label the cells',
    },
    {
      mode: 'spatial',
      title: 'Which cells are neighbors',
      text: 'The Delaunay triangulation of the cells\' centers joins each cell to the cells around it, however dense the tissue; edges longer than 50 µm, across a tear or the tissue\'s edge, are left out. Here 21,678 edges among the 7,275 cells, six neighbors a cell on average, the median edge 11.4 µm.',
      do: { action: 'graph', kind: 'delaunay' },
      label: 'Build the graph',
    },
    {
      mode: 'spatial',
      title: 'Ask which classes are neighbors',
      text: 'Neighborhood enrichment counts, for each pair of classes, the edges between their cells, against the count expected if the labels were dealt to the same cells at random (computed exactly) and 1,000 such deals (how far chance strays).',
      do: { action: 'ask', question: 'enrichment', ...CLASSES },
      label: 'Ask',
    },
    {
      mode: 'spatial',
      title: 'The classes\' neighbors',
      text: '9 of the 15 pairs differ from chance (adjusted p below 0.05). Each class is its own neighbor more often than chance: endothelial cells 12.45 times, stromal 4.54, immune 3.90. The epithelial cells keep apart from the stromal cells (0.24 times) and the immune cells (0.32 times). The heat map colors each pair by its ratio, found over expected; the chosen pair is described below it.',
      spatial: { question: 'enrichment', ...CLASSES, choose: { pair: ['Epithelial', 'Immune'] } },
    },
    {
      mode: 'viewer',
      title: 'Where the tumor meets the immune cells',
      text: 'The 651 epithelial and 299 immune cells with a neighbor of the other, and the 823 edges between them, lit on the tissue: at the nests\' edges and along the stroma that runs between them. Neighbors are counted, not distances: Nearest distances and Ripley\'s K ask at what distance and scale.',
      cells_of: { question: 'enrichment', ...CLASSES, a: 'Epithelial', b: 'Immune' },
    },
    {
      mode: 'spatial',
      title: 'Ask it of the lineages',
      text: 'The same question of the lineages: 16 labels, 136 pairs.',
      do: { action: 'ask', question: 'enrichment', ...LINEAGES },
      label: 'Ask',
    },
    {
      mode: 'spatial',
      title: 'T cells and myeloid cells',
      text: '54 of the 136 pairs differ from chance. T cells lie next to myeloid cells 4.44 times as often as chance (z 22), and next to other T cells 8.59 times; luminal cells keep apart from T cells (0.12 times): the immune cells gather together in the stroma, apart from the tumor. These numbers describe this section: its cells are not replicates, and comparing conditions needs samples.',
      spatial: { question: 'enrichment', ...LINEAGES, choose: { pair: ['T cell', 'Myeloid'] } },
    },
    {
      mode: 'viewer',
      title: 'Together on the tissue',
      text: 'The 74 T cells and 127 myeloid cells with a neighbor of the other, and the 164 edges between them: most in the stroma on the section\'s left.',
      cells_of: { question: 'enrichment', ...LINEAGES, a: 'T cell', b: 'Myeloid' },
    },
  ],
};

// The tumor of the Breast section, drawn around its epithelial cells: the immune cells and stroma
// along its left side and the stroma below its middle outside (µm).
const BREAST_TUMOR = { name: 'Tumor', cls: 'Tumor', layer: 'the stains', polygon: [[255, 5], [1228, 5], [1228, 745], [1080, 745], [1080, 600], [1050, 520], [720, 505], [700, 560], [700, 745], [255, 745]] };

const BREAST_EDGE = {
  id: 'tumor-edge',
  title: 'Immune cells and a tumor\'s edge',
  question: 'Do the immune cells enter the tumor, stay out of it, or gather at its edge?',
  expect: 'A region drawn around the tumor holds 5,730 of the 7,275 cells. Measured in bands across its edge, from 100 µm inside to 100 µm outside, the epithelial cells fill it (4.8 times as dense in its core as around it), and the immune cells stay out: 420 cells per mm² of tissue in the core, 765 at the margin (within 20 µm of the edge, either side), 1,174 around, the core over around 0.36 times (0.28 to 0.45). Thinner the deeper into the tumor: an immune-excluded tumor.',
  steps: [
    {
      mode: 'spatial',
      title: 'The question',
      text: 'A tumor\'s immune cells are measured in its core, at its invasive margin and around it, as Galon et al. (2006) measured colorectal cancers. Here the region is drawn on the tissue, and each class\'s density is measured in bands of distance from its edge, each band\'s own area of tissue its denominator.',
    },
    {
      mode: 'celltypes',
      title: 'Cluster the cells',
      text: 'scanpy\'s steps and defaults: 15 clusters of the 7,163 cells with at least 10 gene transcripts.',
      do: { action: 'cluster' },
      label: 'Cluster the cells',
    },
    {
      mode: 'celltypes',
      title: 'Label them from markers',
      text: 'The breast starter set: 81% of the cells take a class.',
      do: { action: 'label', markers: 'breast' },
      label: 'Label the cells',
    },
    {
      mode: 'viewer',
      title: 'Draw the tumor',
      text: 'The tumor drawn as a region around the epithelial cells, which fill most of the section; the immune cells and stroma along its left side, and the stroma below its middle, lie outside. It holds 5,730 cells. A region drawn by hand decides where the edge is: draw it on what you can see (the stains, an H&E), and say so.',
      do: { action: 'draw-regions', regions: [BREAST_TUMOR] },
      label: 'Draw the region',
      cells: { outlines: false, fill: 'labels:0', opacity: 0.6 },
      transcripts: { genes: [] },
      channels: GRAY_NUCLEI_ONLY,
    },
    {
      mode: 'spatial',
      title: 'Measure across its edge',
      text: 'Each class\'s density in bands every 10 µm, from 100 µm inside the edge to 100 µm outside, the tissue in each band (gaps and glass left out) its denominator; then three zones: the core (inside, beyond 20 µm), the margin (within 20 µm, either side) and around.',
      do: { action: 'ask', question: 'boundary', ...CLASSES, region: 'Tumor' },
      label: 'Measure',
    },
    {
      mode: 'spatial',
      title: 'The immune cells stay out',
      text: 'The epithelial cells fill the tumor: 4.8 times as dense in its core as around it. The immune cells stay out: 420 cells per mm² in the core, 765 at the margin, 1,174 around, the core over around 0.36 times (0.28 to 0.45). The profile shows them thinning band by band into the tumor. The intervals treat cells as placed independently; cells cluster, so the true uncertainty is wider.',
      spatial: { question: 'boundary', ...CLASSES, region: 'Tumor', choose: { label: 'Immune', zone: 'margin' } },
    },
    {
      mode: 'viewer',
      title: 'The immune cells at the margin',
      text: 'The 76 immune cells within 20 µm of the edge, either side, lit on the tissue along the region\'s edge.',
      cells_of: { question: 'boundary', ...CLASSES, region: 'Tumor', a: 'Immune', zone: 'margin' },
    },
    {
      mode: 'viewer',
      title: 'And in the core',
      text: 'The 230 immune cells in the core, more than 20 µm inside: scattered through the tumor, thinner than around it.',
      cells_of: { question: 'boundary', ...CLASSES, region: 'Tumor', a: 'Immune', zone: 'core' },
    },
  ],
};

const TRUE_NICHES = { segmentation: 'truth', labels: 'true-types', niches: 4, neighbors: 30 };

const SIMULATED_NICHES = {
  id: 'niches-domains',
  title: 'Niches against the domains',
  simulated: true,
  question: 'The simulator laid the tissue out in domains, each led by one type. Do niches found from what each cell\'s neighborhood holds find them?',
  expect: 'The 3,030 true cells, labeled by their true types; each cell\'s neighborhood its 30 nearest cells; four niches, as many as the kinds of domain. Each niche is led by one type (epithelial-like, 47% of its 1,078 cells; endothelial-like, 53% of 887; immune-like, 72% of 554; fibroblast-like, 44% of 511), the seeds agree exactly, and 96.5% of the cells lie in the niche of their domain\'s kind: the two epithelial-led domains share a niche, as do the two fibroblast-led. With the defaults, six niches of the 10 nearest, the seeds agree only 0.66 to 0.93: more niches than kinds of place.',
  steps: [
    {
      mode: 'data',
      title: 'A tissue whose domains are known',
      text: 'SituCraft simulated this tissue on this computer: six domains, each led by one of four types, the cells of each domain drawn from its mix. Its truth folder holds every true cell, its type and its domain.',
    },
    {
      mode: 'segmentation',
      title: 'Import the true cells',
      text: 'The truth is shaped as a Xenium Ranger output of the same run: imported, it is a segmentation like any other.',
      do: { action: 'import-truth' },
      label: 'Import the truth',
    },
    {
      mode: 'celltypes',
      title: 'Import their types',
      text: 'The truth\'s cells.parquet names each true cell\'s type: imported for the true cells, a label set like any other.',
      do: { action: 'import-truth-labels' },
      label: 'Import the true types',
    },
    {
      mode: 'spatial',
      title: 'Import their domains',
      text: 'The same file names each true cell\'s domain (0 to 5): imported too, to read the niches against.',
      do: { action: 'import-truth-domains' },
      label: 'Import the domains',
    },
    {
      mode: 'spatial',
      title: 'Find four niches',
      text: 'Each cell\'s neighborhood, its 30 nearest cells, is described by its types\' shares, and the cells are grouped by k-means into four niches, as many as the kinds of domain (by the type leading them); then found again from four other seeds.',
      do: { action: 'ask', question: 'niches', ...TRUE_NICHES },
      label: 'Find the niches',
    },
    {
      mode: 'spatial',
      title: 'Four kinds of place',
      text: 'Each niche is led by one type: epithelial-like (47% of its 1,078 cells), endothelial-like (53% of 887), immune-like (72% of 554), fibroblast-like (44% of 511). The heat map shows each niche\'s make-up against the tissue\'s. The four other seeds give the same niches (an adjusted Rand index of 1.00): they are stable.',
      spatial: { question: 'niches', ...TRUE_NICHES, choose: { niche: 3 } },
    },
    {
      mode: 'viewer',
      title: 'The niches on the tissue',
      text: 'Every true cell filled by its niche. A niche is a kind of place, not a kind of cell: its cells are of every type, in the mix of the place.',
      segmentation: 'truth',
      cells: { outlines: false, fill: 'niches', opacity: 0.75 },
      niches: TRUE_NICHES,
      transcripts: { genes: [] },
      channels: GRAY_NUCLEI,
    },
    {
      mode: 'viewer',
      title: 'The domains on the tissue',
      text: 'The same cells filled by their domain: the niches follow them. 96.5% of the cells lie in the niche of their domain\'s kind (93% to 99% for each domain); domains 0 and 4, both epithelial-led, share a niche, as do domains 1 and 5, fibroblast-led. Composition cannot tell apart two places of the same mix.',
      segmentation: 'truth',
      cells: { outlines: false, fill: 'true-domains', opacity: 0.75 },
      transcripts: { genes: [] },
      channels: GRAY_NUCLEI,
    },
    {
      mode: 'spatial',
      title: 'Ask with the defaults',
      text: 'The defaults: six niches, each cell\'s 10 nearest cells.',
      do: { action: 'ask', question: 'niches', segmentation: 'truth', labels: 'true-types', niches: 6, neighbors: 10 },
      label: 'Find six niches',
    },
    {
      mode: 'spatial',
      title: 'More niches than places',
      text: 'Six niches split the four kinds of place by chance, and the seeds say so: an adjusted Rand index of 0.66 to 0.93 between them. Low agreement means more niches than the tissue holds kinds of place: try fewer, or larger neighborhoods.',
      spatial: { question: 'niches', segmentation: 'truth', labels: 'true-types', niches: 6, neighbors: 10 },
    },
  ],
};

const LUNG_MORAN = {
  id: 'moran-genes',
  title: 'Genes in a pattern',
  question: 'Which genes are expressed in a pattern across the Lung section, and where?',
  expect: 'Over the Delaunay graph of the 11,898 cells, 162 of the panel\'s 289 genes are in a pattern (adjusted p below 0.05). The strongest is GJA5 (Moran\'s I 0.436), expressed in 1.2% of the cells: an artery\'s endothelium, its 73 cells high among high lying together in the stroma at the section\'s top left. Then IGFBP3 (0.411) and CXCL13 (0.313).',
  steps: [
    {
      mode: 'spatial',
      title: 'The question',
      text: 'Spatially variable genes asks of genes, not labels: how alike each gene\'s expression is in neighboring cells (Moran\'s I), near 1 when neighbors are alike, about 0 for no pattern, below 0 where they differ.',
    },
    {
      mode: 'spatial',
      title: 'Which cells are neighbors',
      text: 'The Delaunay graph of the instrument\'s cells, edges to 50 µm: 35,472 edges among 11,898 cells.',
      do: { action: 'graph', kind: 'delaunay' },
      label: 'Build the graph',
    },
    {
      mode: 'spatial',
      title: 'Ask which genes are in a pattern',
      text: 'Each gene\'s log-normalized expression, as the clustering normalizes it; its I tested against its values dealt to the cells at random, analytically and by 199 permutations, the p-values adjusted over the genes; and for the 10 leading genes, local Moran\'s I: each cell high among high, low among low, or unlike its neighbors.',
      do: { action: 'ask', question: 'moran' },
      label: 'Ask',
    },
    {
      mode: 'spatial',
      title: 'The genes in a pattern',
      text: '162 of the 289 genes are in a pattern. The table ranks them by I, the effect: GJA5 0.436, IGFBP3 0.411, CXCL13 0.313, PLA2G7, LTBP2. With 11,898 cells even a small I is significant, so the rank is by I. GJA5 is expressed in 1.2% of the cells, but they lie together: its local Moran\'s I classes 73 cells high among high and 286 low among high (the cells around them).',
      spatial: { question: 'moran', choose: { gene: 'GJA5' } },
    },
    {
      mode: 'viewer',
      title: 'Where GJA5\'s pattern lies',
      text: 'GJA5\'s 73 cells high among high, with its transcripts: an artery\'s endothelium in the stroma at the section\'s top left. Local Moran\'s I shows where a pattern lies (each cell\'s test is not adjusted); it does not test it.',
      cells_of: { question: 'moran', gene: 'GJA5', localClass: 1 },
    },
  ],
};

// The figure the Lung's wave-8 worked examples make, of a view of the tissue and the results asked
// before it, and the view.
const LUNG_FIGURE_NAME = 'The Lung\'s results';
const LUNG_FIGURE_VIEW = {
  mode: 'viewer',
  camera: { cx: 750, cy: 300, umPerCss: 0.4 },
  cells: { outlines: true, fill: 'labels:0', opacity: 0.6 },
  transcripts: { genes: [] },
  channels: GRAY_NUCLEI,
};

const LUNG_FIGURE = {
  id: 'figure-and-methods',
  title: 'A figure of results, and its methods',
  question: 'How does a result become a figure for a paper, with a methods section written from what was done?',
  expect: 'A figure two columns wide (174 × 157 mm) of four panels, lettered: the view of the tissue, 400 µm across, its 2,298 cells filled by class; the map of the 11,820 cells clustered; the 4 classes\' enrichment, 7 pairs found; and the 15 genes whose Moran\'s I is highest of the 162 in a pattern. Its methods, written from its record, say each step with its parameters and seed, the figure among them, citing 23 references; what made it is 11 steps from the bundle.',
  steps: [
    {
      mode: 'reports',
      title: 'The question',
      text: 'The Reports view keeps the project\'s figures and writes their methods. A figure is drawn from the project, not pasted: each panel is a saved view of the tissue or a result, drawn at the figure\'s size, and every file written carries a manifest of what it was drawn from.',
    },
    {
      mode: 'celltypes',
      title: 'Cluster the cells',
      text: 'The results the figure shows, made first: the cells clustered as scanpy\'s defaults do and mapped (UMAP).',
      do: { action: 'cluster' },
      label: 'Cluster the cells',
    },
    {
      mode: 'celltypes',
      title: 'Label them',
      text: 'Each cell labeled from the panel\'s starter markers, its class, lineage and subtype where it leads the next label by the margin.',
      do: { action: 'label', markers: 'lung' },
      label: 'Label the cells',
    },
    {
      mode: 'spatial',
      title: 'Which cells are neighbors',
      text: 'The Delaunay graph of the instrument\'s cells, edges to 50 µm.',
      do: { action: 'graph', kind: 'delaunay' },
      label: 'Build the graph',
    },
    {
      mode: 'spatial',
      title: 'Which classes neighbor which',
      text: 'Neighborhood enrichment of the classes: each pair\'s edges against those of the labels dealt at random.',
      do: { action: 'ask', question: 'enrichment', segmentation: 'instrument', level: 0 },
      label: 'Ask',
    },
    {
      mode: 'spatial',
      title: 'Which genes are in a pattern',
      text: 'Moran\'s I of each gene over the same graph.',
      do: { action: 'ask', question: 'moran', segmentation: 'instrument' },
      label: 'Ask',
    },
    {
      ...LUNG_FIGURE_VIEW,
      title: 'A view of the tissue, made a figure',
      text: 'The cells filled by their class over the nuclear stain, 400 µm across. Make the figure: this view is saved, and a figure the width of a journal\'s two columns is laid out with four panels, lettered: the tissue as shown here, the cells\' map by cluster, the classes\' enrichment and the genes in a pattern.',
      do: { action: 'figure', name: LUNG_FIGURE_NAME, view: 'Lung classes' },
      label: 'Make the figure',
    },
    {
      mode: 'reports',
      title: 'The figure',
      text: 'The figure, previewed as it will print: A, the view of the tissue (2,298 cells filled by class, a scale bar of 50 µm and a legend); B, the cells\' map by cluster; C, the classes\' enrichment, a dot on each of the 7 pairs found; D, the 15 genes with the highest Moran\'s I. Write it as SVG, PDF or PNG: each file carries its manifest, so that SituCraft can read it back and say whether the project still draws it the same.',
      reports: { show: 'figure', figure: LUNG_FIGURE_NAME },
    },
    {
      mode: 'reports',
      title: 'Its methods',
      text: 'Its methods, written from the record of every step from the bundle to the figure: the data and how SituCraft read it; the cells clustered, mapped and labeled; the graph and both questions; the figure; the software. Each step\'s parameters and seed are said, and its 23 references are numbered as first cited. Copy it, or write it as Markdown or BibTeX, and read it against what you did.',
      reports: { show: 'methods', figure: LUNG_FIGURE_NAME },
    },
    {
      mode: 'reports',
      title: 'What made it',
      text: 'What made the figure: 11 steps from the bundle, each after those it was made from (the segmentation, the clustering and its map, the marker set and the labels, the graph, both answers, the saved view, the figure), each with its method and version, parameters, seed and software, and 22 references. Certify it and verify it in Results others can check.',
      reports: { show: 'record', figure: LUNG_FIGURE_NAME },
    },
  ],
};

const LUNG_CHECKED = {
  id: 'results-checked',
  title: 'Results others can check',
  question: 'How can a reader check the figure\'s results, with SituCraft or without it?',
  expect: 'The figure\'s certificate holds 11 steps from 1 bundle. Verified in SituCraft, its 7 steps SituCraft made are made again from the bundle, every file the same, and the figure is drawn again the same. The report traces 23 numbers to its certificate; the package holds 25 files, the view in 20 tiles, the dataset\'s name left out, and nothing left out was found in its 35 texts.',
  steps: [
    {
      mode: 'reports',
      title: 'The question',
      text: 'A figure says what was found; a certificate says what it rests on, so that anyone with the same bundle can make it again and compare. This example certifies the figure of A figure of results, and its methods (made here if it is not yet), verifies it, and writes it for a reader: a report, and a package that opens without SituCraft.',
    },
    {
      ...LUNG_FIGURE_VIEW,
      title: 'The figure',
      text: 'The figure of the Lung\'s results: its view of the tissue, the cells\' map, the classes\' enrichment and the genes in a pattern. Each step it rests on is made first if it is not yet: the clustering, the labels, the graph and both questions.',
      do: { action: 'figure', name: LUNG_FIGURE_NAME, view: 'Lung classes', prepare: true },
      label: 'Make the figure',
    },
    {
      mode: 'reports',
      title: 'Certify it, and verify it',
      text: 'The figure\'s record certified (a zip of the bundle\'s files by SHA-256, each step\'s job and files, the numbers each reports, fingerprinted), then verified here as situcraft verify does: each of the 7 steps SituCraft made is made again, in a SituCraft of its own, from the bundle found by its files\' SHA-256, and every file compared bit for bit; the figure is drawn again in a headless browser and its drawing compared.',
      do: { action: 'verify', figure: LUNG_FIGURE_NAME },
      label: 'Certify and verify',
    },
    {
      mode: 'reports',
      title: 'What verifying found',
      text: 'Verified: the 7 steps made again give the files certified, the figure draws the same, and what a person made (the marker set, the saved view) is as certified, by its digest. Anyone with the bundle can do the same with the certificate, in SituCraft (Verify a certificate…) or with situcraft verify on the command line, on another computer or processor.',
      reports: { show: 'record', figure: LUNG_FIGURE_NAME },
    },
    {
      mode: 'reports',
      title: 'A report',
      text: 'A report: one HTML file of the figure, the enrichment\'s numbers (23, each traced to its place in the certificate), the methods and their references, and the certificate itself. As it opens, in any browser and offline, it checks that the certificate is intact and that each number, figure and paragraph it shows is the certificate\'s.',
      do: { action: 'report', figure: LUNG_FIGURE_NAME, results: ['figure', 'enrichment'], title: 'The Lung\'s cell types and their neighbors' },
      label: 'Write the report',
    },
    {
      mode: 'reports',
      title: 'A package, the names left out',
      text: 'A package: the report, the view the figure shows drawn in advance as 20 tiles with a reader of its own, and the certificate, 25 files that open without SituCraft. It leaves out where files lay on this computer, people\'s names and their notes, and here the dataset\'s name too (written Sample 1); its 35 texts were searched for them before it was written.',
      do: { action: 'package', figure: LUNG_FIGURE_NAME, results: ['figure', 'enrichment'], title: 'The Lung\'s cell types and their neighbors', names: true },
      label: 'Write the package',
    },
    {
      mode: 'reports',
      title: 'What a reader does',
      text: 'Unzip the package and open index.html: the report checks itself, and the view opens in a page of its own, to move about and zoom into. To make the results again, a reader needs the bundle and SituCraft: Verify a certificate…, or situcraft verify --data <a folder holding the bundle> <the package>. A step whose job was given a name left out cannot be made again from a package; here none was.',
      reports: { show: 'figure', figure: LUNG_FIGURE_NAME },
    },
  ],
};

const LUNG_REMAKE = {
  id: 'remake-enrichment',
  title: 'An answer under another segmentation',
  question: 'How much does which cell types are neighbors in the Lung section depend on how its cells were drawn?',
  expect: 'The Lung classes\' enrichment over the Delaunay graph (7 of 10 pairs differ from chance), made again on a 3 µm nucleus expansion, its labels made again from the same clustering parameters and markers. The 10 numbers keep their order (rank correlation 0.976); the largest change is immune cells with immune cells, 1.44 to 1.94 times: there are few immune cells (249, and 227 under the expansion), and 44 and 22 of them carry another label, or none, under the other segmentation.',
  steps: [
    {
      mode: 'spatial',
      title: 'The question',
      text: 'Every spatial answer is of a segmentation\'s cells. Made again on another segmentation, its numbers say how much of the answer is the tissue\'s and how much the segmentation\'s.',
    },
    {
      mode: 'celltypes',
      title: 'Cluster the cells',
      text: 'scanpy\'s steps and defaults: 29 clusters of the 11,820 cells with at least 10 gene transcripts.',
      do: { action: 'cluster' },
      label: 'Cluster the cells',
    },
    {
      mode: 'celltypes',
      title: 'Label them from markers',
      text: 'The panel\'s lung starter set: 47% of the cells take a class.',
      do: { action: 'label', markers: 'lung' },
      label: 'Label the cells',
    },
    {
      mode: 'spatial',
      title: 'Which cells are neighbors',
      text: 'The Delaunay graph of the instrument\'s cells, edges to 50 µm.',
      do: { action: 'graph', kind: 'delaunay' },
      label: 'Build the graph',
    },
    {
      mode: 'spatial',
      title: 'Ask which classes are neighbors',
      text: 'Neighborhood enrichment of the four classes: 7 of the 10 pairs differ from chance. Stromal cells with stromal cells 3.99 times as often as chance; epithelial cells keep apart from endothelial (0.78 times) and stromal cells (0.79 times).',
      do: { action: 'ask', question: 'enrichment', ...CLASSES },
      label: 'Ask',
    },
    {
      mode: 'segmentation',
      title: 'Another segmentation',
      text: 'Each nucleus grown by 3 µm, not the instrument\'s 5: smaller cells, the same nuclei. 90.00% of the pixels and 95.45% of the transcripts are in the same cells as the instrument\'s.',
      do: { action: 'expand', distance: 3 },
      label: 'Expand the nuclei',
    },
    {
      mode: 'spatial',
      title: 'Make the answer again',
      text: 'In one step: the two segmentations compared, the labels made again on the expansion\'s cells from the same clustering parameters and markers, a Delaunay graph of the same definition, and the same question asked.',
      do: { action: 'remake-analysis', question: 'enrichment', ...CLASSES, to: 'expansion-3' },
      label: 'Make it again',
    },
    {
      mode: 'spatial',
      title: 'Read the two together',
      text: 'The 10 numbers under both keep their order (rank correlation 0.976). The largest change is immune cells with immune cells, 1.44 to 1.94 times: there are few immune cells, 249 under the instrument\'s and 227 under the expansion, and of them 44 and 22 carry another label, or none, under the other segmentation (the buttons below show them). A number that moves with the segmentation is the segmentation\'s as much as the tissue\'s.',
      spatial: { question: 'enrichment', ...CLASSES, to: 'expansion-3', choose: { change: 'pair:Immune|Immune' }, section: '.sp-remake' },
    },
  ],
};

// --- Cohorts' worked examples (wave 7) --------------------------------------------------------------
//
// A cohort's steps are in the Cohort view: the samples' fields set (sample-fields: each sample's
// name as a field, or values by sample name), a dataset's regions made its samples
// (region-samples), the design described (cohort-design: saved under a name, so that the
// comparisons made of it are found by it; refused: drafted and shown, not saved), the same steps on
// every sample (cohort-steps), the summary (summarize) and the comparisons (compare-abundance,
// compare-expression, compare-spatial); a step shows a section of the view (cohort: the samples,
// design, steps or summary, or a comparison of a design, a family and a result chosen, a label and
// gene).

const COHORT_GENE = {
  id: 'cohort-gene',
  note: 'The cohort is simulated: its effects, and every sample\'s truth, are known.',
  title: 'A gene changed in one cell type',
  simulated: true,
  question: 'Does the treated subjects\' expression of any gene change in one cell type, with each subject, not each cell, the unit?',
  expect: 'SIM033 is higher in the treated subjects\' epithelial-like cells: +0.94 log2, 1.9 times (0.59 to 1.30 log2), the only one of 424 tests below an adjusted p of 0.05. The true cells show +0.97 log2: the simulator asked 2.5 times (1.32 log2), and each subject\'s own level varies about it.',
  steps: [
    {
      mode: 'cohort',
      title: 'Eight subjects, two conditions',
      text: 'Each section of the simulated cohort is a sample, its subject and condition read in with it: four control subjects and four treated, on two slides. In the treated subjects\' epithelial-like cells the simulator raised one gene, SIM033, 2.5 times. SituCraft is not told which.',
      cohort: { show: 'samples' },
    },
    {
      mode: 'cohort',
      title: 'The design',
      text: 'Each subject is a replicate, and the treated subjects are compared with the controls: the subject, not the cell, is the unit.',
      do: { action: 'cohort-design', unit: 'subject', group: 'condition', reference: 'control', save: 'By condition' },
      label: 'Describe the design',
      cohort: { show: 'design' },
    },
    {
      mode: 'cohort',
      title: 'The same steps on every sample',
      text: 'Each section\'s cells are clustered and labeled from the same starter marker set, with a graph of them and their neighbor enrichment: every sample measured the same way.',
      do: { action: 'cohort-steps', markers: 'simulated', questions: ['enrichment'] },
      label: 'Run the steps',
      cohort: { show: 'steps' },
    },
    {
      mode: 'cohort',
      title: 'Summarize the samples',
      text: 'The summary counts each sample\'s cells by label and sums each label\'s cells\' transcripts gene by gene: the label\'s pseudobulk in that sample.',
      do: { action: 'summarize' },
      label: 'Summarize them',
      cohort: { show: 'summary' },
    },
    {
      mode: 'cohort',
      title: 'Compare the expression',
      text: 'Each label\'s pseudobulk is compared between the conditions, gene by gene: genes too little expressed left out, libraries normalized by TMM, each value weighted by voom, each gene tested by limma\'s moderated t. Each change is a log2 fold change with its interval.',
      do: { action: 'compare-expression' },
      label: 'Compare them',
      cohort: { show: 'expression', design: 'By condition' },
    },
    {
      mode: 'cohort',
      title: 'SIM033 in the epithelial-like cells',
      text: 'SIM033 is higher in the treated subjects\' epithelial-like cells: +0.94 log2 (1.9 times), its 95% interval 0.59 to 1.30, adjusted p 0.005. Each point is a subject\'s pseudobulk: the treated subjects\' all lie above the controls\'. The true cells show +0.97 log2, inside the interval.',
      cohort: { show: 'expression', design: 'By condition', label: 'epithelial-like', gene: 'SIM033' },
    },
    {
      mode: 'cohort',
      title: 'And nothing else',
      text: 'On the volcano, SIM033 stands apart. Of the 424 tests (each label\'s genes expressed enough to test, over four labels), it alone is below an adjusted p of 0.05. With four subjects a group an interval spans most of a log2 unit: the subjects\' own variation, which a test of cells would hide.',
      cohort: { show: 'expression', design: 'By condition', label: 'epithelial-like' },
    },
  ],
};

const COHORT_NULL = {
  id: 'cohort-null',
  note: 'The cohort is simulated: its effects, and every sample\'s truth, are known.',
  title: 'A comparison where nothing differs',
  simulated: true,
  question: 'What does a comparison find when the groups do not differ?',
  expect: 'Nothing below an adjusted p of 0.05: none of 14 tests of abundance (shares, densities, niches), 423 of expression or 10 of neighbor enrichment. By chance 11 genes\' raw p-values are below 0.05, fewer than the 21 chance alone gives on average, and the lowest (SIM015 in the epithelial-like cells, p 0.0006) is adjusted to 0.25 over the tests.',
  steps: [
    {
      mode: 'cohort',
      title: 'Two groups that do not differ',
      text: 'The null cohort is the standard cohort\'s design (four subjects in each condition, on two slides) without its effects: its sections differ from one another only by chance, as sections of different subjects do.',
      cohort: { show: 'samples' },
    },
    {
      mode: 'cohort',
      title: 'The design',
      text: 'The subjects compared by condition, each subject a replicate.',
      do: { action: 'cohort-design', unit: 'subject', group: 'condition', reference: 'control', save: 'By condition' },
      label: 'Describe the design',
      cohort: { show: 'design' },
    },
    {
      mode: 'cohort',
      title: 'The same steps, and the summary',
      text: 'Each section\'s cells labeled from the starter marker set, with their graph and neighbor enrichment; then each sample summarized, with niches found across the samples together.',
      do: { action: 'cohort-steps', markers: 'simulated', questions: ['enrichment'] },
      label: 'Run the steps',
      cohort: { show: 'steps' },
    },
    {
      mode: 'cohort',
      title: 'Summarize the samples',
      text: 'Each sample\'s cells by label and by niche, its pseudobulk, and its neighbor enrichment, in the cohort table.',
      do: { action: 'summarize' },
      label: 'Summarize them',
      cohort: { show: 'summary' },
    },
    {
      mode: 'cohort',
      title: 'Compare the abundances',
      text: 'Each label\'s share of the labeled cells and density, and each niche\'s share, between the conditions.',
      do: { action: 'compare-abundance', families: ['share', 'density', 'niche'] },
      label: 'Compare them',
      cohort: { show: 'abundance', design: 'By condition' },
    },
    {
      mode: 'cohort',
      title: 'No abundance found changed',
      text: 'None of the 14 tests is below an adjusted p of 0.05, nor any raw p below 0.05: each effect\'s interval holds 1, a ratio of no change.',
      cohort: { show: 'abundance', design: 'By condition', family: 'share' },
    },
    {
      mode: 'cohort',
      title: 'Compare the expression',
      text: 'Each label\'s genes between the conditions, by its pseudobulk.',
      do: { action: 'compare-expression' },
      label: 'Compare the expression',
      cohort: { show: 'expression', design: 'By condition' },
    },
    {
      mode: 'cohort',
      title: 'No gene found changed',
      text: 'Of 423 tests, 11 raw p-values are below 0.05, as chance gives some (21 on average), and none survives the adjustment over the tests: the lowest, SIM015 in the epithelial-like cells (p 0.0006), is adjusted to 0.25. A raw p below 0.05 among hundreds of tests is not a finding.',
      cohort: { show: 'expression', design: 'By condition', label: 'epithelial-like' },
    },
    {
      mode: 'cohort',
      title: 'Compare the arrangement',
      text: 'Each sample\'s neighbor enrichment of each pair of labels, compared between the conditions as log2 ratios.',
      do: { action: 'compare-spatial' },
      label: 'Compare the arrangement',
      cohort: { show: 'spatial', design: 'By condition' },
    },
    {
      mode: 'cohort',
      title: 'Nothing found, as it should be',
      text: 'None of the 10 pairs is below an adjusted p of 0.05 (the lowest 0.36). A family of tests here finds something in about 5% of such cohorts, as its level says: SituCraft measures that on thousands of them every week (see Error rates and scale).',
      cohort: { show: 'spatial', design: 'By condition', family: 'enrichment' },
    },
  ],
};

const COHORT_SHIFT = {
  id: 'cohort-shift',
  note: 'The cohort is simulated: its effects, and every sample\'s truth, are known.',
  title: 'A composition shift found',
  simulated: true,
  question: 'After treatment, are some cell types more common in the same subjects\' tissue?',
  expect: 'The endothelial-like cells\' odds among the labeled cells 2.19 times as high after (1.54 to 3.12, adjusted p 0.009), their density 1.80 times (1.39 to 2.33); the epithelial-like cells\' odds 0.48 times (0.27 to 0.84). The true cells show 2.26 and 0.47: the simulator made a cell four times as likely to be endothelial-like after, and more endothelial-like and immune-like cells leave less room for epithelial-like ones.',
  steps: [
    {
      mode: 'cohort',
      title: 'Six subjects, before and after',
      text: 'Each of six subjects was sampled before and after: twelve sections. After, the simulator made a cell four times as likely to be endothelial-like, and immune-like cells four times as likely where epithelial-like cells lead.',
      cohort: { show: 'samples' },
    },
    {
      mode: 'cohort',
      title: 'A paired design',
      text: 'Each subject is in both conditions, so the design pairs them: each subject is compared with itself, its own level entering the model.',
      do: { action: 'cohort-design', unit: 'subject', group: 'condition', reference: 'before', save: 'Paired' },
      label: 'Describe the design',
      cohort: { show: 'design' },
    },
    {
      mode: 'cohort',
      title: 'The same steps on every sample',
      text: 'Each section\'s cells labeled from the starter marker set, with their graph and neighbor enrichment.',
      do: { action: 'cohort-steps', markers: 'simulated', questions: ['enrichment'] },
      label: 'Run the steps',
      cohort: { show: 'steps' },
    },
    {
      mode: 'cohort',
      title: 'Summarize the samples',
      text: 'Each sample\'s cells by label, and its tissue.',
      do: { action: 'summarize' },
      label: 'Summarize them',
      cohort: { show: 'summary' },
    },
    {
      mode: 'cohort',
      title: 'Compare the abundances',
      text: 'Each label\'s share of the labeled cells by a quasi-binomial model of the design, and its density per mm² by a quasi-Poisson model.',
      do: { action: 'compare-abundance', families: ['share', 'density'] },
      label: 'Compare them',
      cohort: { show: 'abundance', design: 'Paired' },
    },
    {
      mode: 'cohort',
      title: 'More endothelial-like cells after',
      text: 'The endothelial-like cells\' odds among the labeled cells are 2.19 times as high after (1.54 to 3.12), adjusted p 0.009: found. The true cells show 2.26. The lines are the groups\' means; each point is a subject\'s section, and every subject\'s after lies above its before.',
      cohort: { show: 'abundance', design: 'Paired', family: 'share', pick: 'endothelial-like|after − before' },
    },
    {
      mode: 'cohort',
      title: 'Fewer epithelial-like cells',
      text: 'The epithelial-like cells\' odds fall to 0.48 times (0.27 to 0.84), adjusted p 0.04: the simulator did not lower them, but more endothelial-like and immune-like cells leave less room for them (the true cells show 0.47). A share is of all the labeled cells: one type\'s rise lowers the others\'.',
      cohort: { show: 'abundance', design: 'Paired', family: 'share', pick: 'epithelial-like|after − before' },
    },
    {
      mode: 'cohort',
      title: 'Per mm² of tissue',
      text: 'The endothelial-like cells\' density is 1.80 times as high after (1.39 to 2.33), adjusted p 0.009: per area of tissue, not a share, so not moved by the other types.',
      cohort: { show: 'abundance', design: 'Paired', family: 'density', pick: 'endothelial-like|after − before' },
    },
  ],
};

const COHORT_PAIRED = {
  id: 'cohort-paired',
  note: 'The cohort is simulated: its effects, and every sample\'s truth, are known.',
  title: 'Each subject compared with itself',
  simulated: true,
  question: 'Six subjects each sampled before and after: what does comparing each subject with itself gain over treating the twelve sections as separate units?',
  expect: 'SIM033 in the epithelial-like cells: +1.08 log2 either way, its interval 0.75 to 1.41 with the sections as units and 0.84 to 1.31 paired, 0.47 log2 wide instead of 0.66 (the true cells show +1.24). Paired, 22 genes are found against one: besides SIM033, the endothelial-like cells\' markers SIM025 to SIM030 in the epithelial-like and immune-like cells, transcripts of their more numerous endothelial-like neighbors read into their outlines.',
  steps: [
    {
      mode: 'cohort',
      title: 'Six subjects, before and after',
      text: 'Each subject\'s two sections share part of their variation: a subject\'s level of each gene, as one person\'s tissue does. A paired design removes it.',
      cohort: { show: 'samples' },
    },
    {
      mode: 'cohort',
      title: 'The same steps, and the summary',
      text: 'Each section\'s cells labeled from the starter marker set.',
      do: { action: 'cohort-steps', markers: 'simulated', questions: [] },
      label: 'Run the steps',
      cohort: { show: 'steps' },
    },
    {
      mode: 'cohort',
      title: 'Summarize the samples',
      text: 'Each label\'s pseudobulk in each sample: its cells\' transcripts summed, gene by gene.',
      do: { action: 'summarize' },
      label: 'Summarize them',
      cohort: { show: 'summary' },
    },
    {
      mode: 'cohort',
      title: 'Each section its own unit',
      text: 'First as if each section came from a different subject: a field, section, names each one, and is the replicate unit.',
      do: { action: 'sample-fields', name: 'section' },
      label: 'Name each section',
      cohort: { show: 'samples' },
    },
    {
      mode: 'cohort',
      title: 'Twelve sections as units',
      text: 'Six sections in each condition, compared as if unrelated.',
      do: { action: 'cohort-design', unit: 'section', group: 'condition', reference: 'before', save: 'Sections' },
      label: 'Describe it',
      cohort: { show: 'design' },
    },
    {
      mode: 'cohort',
      title: 'Compare the expression',
      text: 'Each label\'s genes, with the sections as units.',
      do: { action: 'compare-expression' },
      label: 'Compare them',
      cohort: { show: 'expression', design: 'Sections' },
    },
    {
      mode: 'cohort',
      title: 'SIM033, the sections as units',
      text: 'SIM033 in the epithelial-like cells: +1.08 log2, its interval 0.75 to 1.41 (0.66 wide). Found, the only gene; the subjects\' own levels are part of its variation.',
      cohort: { show: 'expression', design: 'Sections', label: 'epithelial-like', gene: 'SIM033' },
    },
    {
      mode: 'cohort',
      title: 'Each subject paired with itself',
      text: 'Now the subject is the unit, in both conditions: the design pairs each subject\'s sections.',
      do: { action: 'cohort-design', unit: 'subject', group: 'condition', reference: 'before', save: 'Paired' },
      label: 'Pair the subjects',
      cohort: { show: 'design' },
    },
    {
      mode: 'cohort',
      title: 'Compare again',
      text: 'The same pseudobulk, the same genes, the paired design.',
      do: { action: 'compare-expression' },
      label: 'Compare them',
      cohort: { show: 'expression', design: 'Paired' },
    },
    {
      mode: 'cohort',
      title: 'SIM033, paired',
      text: 'SIM033: +1.08 log2 again, its interval 0.84 to 1.31, 0.47 wide instead of 0.66: comparing each subject with itself takes the subjects\' own levels out of the variation. The true cells show +1.24.',
      cohort: { show: 'expression', design: 'Paired', label: 'epithelial-like', gene: 'SIM033' },
    },
    {
      mode: 'cohort',
      title: 'Not every gene found is the cells\' own',
      text: 'Paired, 22 genes are found. Besides SIM033, SIM025 to SIM030 are higher in the epithelial-like (and immune-like) cells after: they are the endothelial-like cells\' markers. After, there are more endothelial-like cells, and their transcripts reach the outlines of the cells beside them. A label\'s pseudobulk holds what its cells\' outlines hold: a change in its neighbors can read as a change in it. Check a gene found against the other types\' markers, and the segmentation.',
      cohort: { show: 'expression', design: 'Paired', label: 'epithelial-like', gene: 'SIM026' },
    },
  ],
};

// The Breast section's places: three regions in its stroma (beside the tumor and below it) and
// three in its tumor (wave 7: a teaching example of regions as pseudo-samples).
const BREAST_PLACES = [
  { name: 'Stroma upper left', place: 'stroma', rectangle: [5, 5, 245, 370] },
  { name: 'Stroma lower left', place: 'stroma', rectangle: [5, 380, 245, 740] },
  { name: 'Stroma below', place: 'stroma', rectangle: [730, 530, 1040, 740] },
  { name: 'Tumor 1', place: 'tumor', rectangle: [300, 50, 550, 300] },
  { name: 'Tumor 2', place: 'tumor', rectangle: [600, 50, 850, 300] },
  { name: 'Tumor 3', place: 'tumor', rectangle: [950, 50, 1200, 300] },
];

const BREAST_PSEUDO = {
  id: 'pseudo-samples',
  note: 'For teaching: its regions are pseudo-samples, not replicates, and every result says so.',
  title: 'Places in one section are not subjects',
  teaching: true,
  question: 'Can regions of one section stand in for samples, to try a cohort\'s comparisons on real data, and what may be said of what they find?',
  expect: 'Six regions of the Breast section, three of its stroma and three of its tumor, each given its own subject: refused. Marked pseudo-samples, for teaching, they find the stroma\'s cells 8.4 times as likely to be immune (1.6 to 44.5) and 0.04 times as likely to be epithelial: true of this section\'s places, and no evidence about patients, of whom one section is one.',
  steps: [
    {
      mode: 'viewer',
      title: 'One section, one patient',
      text: 'The Breast section is one patient\'s tissue: its tumor fills most of it, with stroma along its left edge and below. A cohort compares subjects; with one section there is one subject.',
      transcripts: { genes: [] },
      channels: GRAY_NUCLEI_ONLY,
    },
    {
      mode: 'viewer',
      title: 'Six places',
      text: 'Three regions of the stroma and three of the tumor.',
      do: { action: 'draw-regions', regions: BREAST_PLACES.map((r) => ({ name: r.name, cls: r.place === 'tumor' ? 'Tumor' : 'Stroma', rectangle: r.rectangle, layer: 'the stains' })) },
      label: 'Draw the regions',
      transcripts: { genes: [] },
      channels: GRAY_NUCLEI_ONLY,
    },
    {
      mode: 'cohort',
      title: 'The regions as samples',
      text: 'Each region becomes a sample of the section, in the Cohort view.',
      do: { action: 'region-samples', regions: BREAST_PLACES.map((r) => r.name) },
      label: 'Make them samples',
      cohort: { show: 'samples' },
    },
    {
      mode: 'cohort',
      title: 'Each its own subject',
      text: 'Each region given a subject of its own, and its place: stroma or tumor.',
      do: { action: 'sample-fields', values: Object.fromEntries(BREAST_PLACES.map((r) => [r.name, { subject: r.name, place: r.place }])) },
      label: 'Set their fields',
      cohort: { show: 'samples' },
    },
    {
      mode: 'cohort',
      title: 'Refused',
      text: 'The places compared, each region a subject: refused. Regions of one section of one tissue are not independent replicates; tested as if they were, any difference between places looks like a difference between patients.',
      do: { action: 'cohort-design', unit: 'subject', group: 'place', reference: 'tumor', refused: true },
      label: 'Try the design',
      cohort: { show: 'design' },
    },
    {
      mode: 'cohort',
      title: 'Pseudo-samples, for teaching',
      text: 'For teaching only, the regions can be marked pseudo-samples: the design is kept, says so first, and every comparison made of it says so above its results.',
      do: { action: 'cohort-design', unit: 'subject', group: 'place', reference: 'tumor', pseudoSamples: true, save: 'Pseudo-samples' },
      label: 'Mark them pseudo-samples',
      cohort: { show: 'design' },
    },
    {
      mode: 'cohort',
      title: 'The same steps on every sample',
      text: 'The section\'s cells clustered and labeled from the Breast panel\'s starter marker set, once for its six regions.',
      do: { action: 'cohort-steps', markers: 'breast', questions: [] },
      label: 'Run the steps',
      cohort: { show: 'steps' },
    },
    {
      mode: 'cohort',
      title: 'Summarize the regions',
      text: 'Each region\'s cells by label, and its tissue.',
      do: { action: 'summarize', niches: 0 },
      label: 'Summarize them',
      cohort: { show: 'summary' },
    },
    {
      mode: 'cohort',
      title: 'Compare the places',
      text: 'Each label\'s share and density, stroma against tumor.',
      do: { action: 'compare-abundance', families: ['share', 'density'] },
      label: 'Compare them',
      cohort: { show: 'abundance', design: 'Pseudo-samples' },
    },
    {
      mode: 'cohort',
      title: 'What they find',
      text: 'The stroma\'s cells are 8.4 times as likely to be immune (1.6 to 44.5) and 0.04 times as likely to be epithelial, adjusted p 0.03: this section\'s stroma holds more immune cells than its tumor. Adipocytes, in the stroma alone, cannot be compared. The banner above the results says what these are.',
      cohort: { show: 'abundance', design: 'Pseudo-samples', family: 'share', pick: 'Immune|stroma − tumor' },
    },
    {
      mode: 'cohort',
      title: 'What may be said',
      text: 'These p-values say this section\'s stroma and tumor differ by more than its regions vary: a fact about one patient\'s tissue. Whether tumors\' stroma holds more immune cells than tumors do, in patients, needs patients: sections of several, each a replicate.',
      cohort: { show: 'abundance', design: 'Pseudo-samples', family: 'density', pick: 'Immune|stroma − tumor' },
    },
  ],
};

export const WORKED = { 'xenium-breast-2fov': [BREAST_COMPARE, BREAST_REFINE, BREAST_LABELS, BREAST_NEIGHBORS, BREAST_EDGE, BREAST_PSEUDO], 'xenium-lung-2fov': [LUNG_PLANTED, LUNG_CELL_TYPES, LUNG_MORAN, LUNG_REMAKE, LUNG_FIGURE, LUNG_CHECKED], 'simulated-standard': [SIMULATED_TRUTH, SIMULATED_LABELS, SIMULATED_ALIGN, SIMULATED_REGIONS, SIMULATED_REVIEW, SIMULATED_NICHES], 'simulated-cohort': [COHORT_GENE], 'simulated-null-cohort': [COHORT_NULL], 'simulated-paired-cohort': [COHORT_SHIFT, COHORT_PAIRED] };

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
