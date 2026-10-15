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
// compare two segmentations, refine the cells, import the truth), or show a comparison in the
// Compare view (compare: its two segmentations, the view, the reviewer's rules, the difference
// shown, the side panel's tab). A Viewer step may draw a segmentation other than the instrument's
// (segmentation). Segmentations are named by what they are: 'instrument', 'expansion-5' (SituCraft's
// nucleus expansion by 5 µm), 'planted' (errors planted for practice), 'refined' (SituCraft's
// transcript-informed segmentation), 'truth' (a simulated tissue's true cells, imported).

const BREAST_COMPARE = {
  id: 'compare-expansion',
  title: 'The instrument\'s cells against a nucleus expansion',
  question: 'Where does growing each nucleus by 5 µm disagree with the cells the instrument drew from its boundary and interior stains, and which does the evidence favour there?',
  expect: 'About 3,600 cells the same in both. The expansion splits 263 of the instrument\'s cells and merges 128 pairs, and has no cell where the instrument drew 119 without a nucleus. Its spurious co-expression is higher (about 2.0 against 1.8): its cells take in more of their neighbours\' transcripts.',
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
      text: 'Ranked by contamination: differences whose cell in B expresses more pairs of mutually exclusive markers than in A. The metrics above say the expansion\'s spurious co-expression is higher than the instrument\'s: its cells reach into their neighbours.',
      compare: { a: 'instrument', b: 'expansion-5', view: 'side', map: false, rank: 'contamination', difference: 1, tab: 'queue' },
    },
    {
      mode: 'compare',
      title: 'Decide, with your reason',
      text: 'Promote, keep for exploration or reject either segmentation, with a reason and this comparison recorded; nothing is deleted. Here the evidence favours the instrument\'s: keep the expansion for exploration, or reject it.',
      compare: { a: 'instrument', b: 'expansion-5', tab: 'decide' },
    },
  ],
};

const LUNG_PLANTED = {
  id: 'planted-errors',
  title: 'Find the planted errors',
  simulated: true,
  question: 'Can you find a segmentation\'s errors with the comparison workspace, and which ranking finds which kind?',
  expect: 'SituCraft plants 60 errors in a copy of the instrument\'s segmentation (simulated): 20 merges of neighbouring cells of different lineages, 20 splits, 10 missed cells and 10 cells drawn 1 µm too wide. The comparison lists exactly those as differences, every other cell the same in both; ranked by contamination, the merges come first.',
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
      text: 'Only expanded and contracted cells: a cell drawn too wide takes its neighbours\' pixels and transcripts. The overlay shows how far its outline reaches.',
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
      text: 'Close up, each transcript is drawn by its probability of being in the cell it is in: the bright ones are those the model is least sure of, most of them where neighbouring cells express the same genes.',
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
      text: 'SituCraft simulated this tissue on this computer: cells of four types in domains, their nuclei, their transcripts drifted a little, a background, and an instrument that grows each nucleus by 5 µm. Every file is laid out as a Xenium bundle\'s, labelled simulated; its truth is in its truth folder.',
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

// The worked examples, by example dataset.
export const WORKED = { 'xenium-breast-2fov': [BREAST_COMPARE, BREAST_REFINE], 'xenium-lung-2fov': [LUNG_PLANTED], 'simulated-standard': [SIMULATED_TRUTH] };

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
