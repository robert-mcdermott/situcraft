// Guided tours of the example datasets: a few steps through the Data view, the Viewer and QC, each
// opening a view of the tissue and saying what it shows. A step is data: the view it opens (a
// saved view's shape, lib/views.js, kept out of the project; its channels name only what they
// change) and its text. The genes are among
// those the section's transcripts show most (quality 20 or more), and each zoomed step looks where
// its genes are found together (the 300 µm square where their shares of the section's transcripts
// are highest together).

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

export function tourFor(example) {
  return TOURS[example] ?? null;
}

// The view a step opens on a dataset (null for a Data view step). Its id marks it as a tour's: it
// is applied, never kept. A step without a camera shows the whole section.
export function stepView(step, dataset, index) {
  if (step.mode === 'data') return null;
  const view = {
    id: `tour-${index}`,
    name: step.title,
    dataset,
    mode: step.mode,
    segmentation: 'instrument',
    camera: step.camera ? { ...step.camera } : null,
    channels: (step.channels ?? []).map((c) => ({ ...c })),
    transcripts: { visible: true, all: false, genes: [...(step.transcripts?.genes ?? [])] },
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
