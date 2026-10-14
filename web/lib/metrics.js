// The run's metrics (metrics_summary.csv) explained: what each means, and how SituCraft recomputes
// it from the bundle's data, or why it does not (internal/xenium/validate.go, research.md §1.3b).
// Metrics a bundle leaves empty are not shown.

export const METRIC_GROUPS = [
  { id: 'transcripts', label: 'Transcripts and their quality' },
  { id: 'cells', label: 'Cells' },
  { id: 'specificity', label: 'Negative controls (specificity)' },
  { id: 'segmentation', label: 'Segmentation' },
];

// name → { group, label, unit, format ('count', 'share', 'number'), help, how (SituCraft's
// recomputation) or why (why it is not recomputed) }.
export const METRICS = {
  total_high_quality_decoded_transcripts: {
    group: 'transcripts', label: 'Gene transcripts of quality 20 or more', format: 'count',
    help: 'Decoded transcripts of the panel\'s genes whose quality (a Phred score) is at least 20, the analyses\' input.',
    how: 'Transcripts of genes (not controls or codewords) with qv ≥ 20 in transcripts.parquet.',
  },
  fraction_transcripts_decoded_q20: {
    group: 'transcripts', label: 'Share of gene transcripts of quality 20 or more', format: 'share',
    help: 'How much of what was decoded is reliable. A low share points to poor imaging or chemistry.',
    how: 'Gene transcripts with qv ≥ 20 ÷ all gene transcripts in transcripts.parquet.',
  },
  decoded_transcripts_per_100um2: {
    group: 'transcripts', label: 'Gene transcripts per 100 µm² of cell', format: 'number',
    help: 'Transcript density within cells: how much each cell area yields.',
    how: 'Gene transcripts with qv ≥ 20 ÷ the cells\' total area × 100 (per cell area, not per region area).',
  },
  nuclear_transcripts_per_100um2: {
    group: 'transcripts', label: 'Gene transcripts per 100 µm² of nucleus', format: 'number',
    help: 'Transcript density within nuclei, where segmentation is most certain.',
    how: 'Gene transcripts with qv ≥ 20 overlapping a nucleus ÷ the nucleus mask\'s area (cells.zarr.zip masks/0, its pixels × 0.2125² µm²) × 100.',
  },
  thickness_transcripts_high_quality: {
    group: 'transcripts', label: 'Thickness of the transcript layer', unit: 'µm', format: 'number',
    help: 'The spread in depth (z) of the high-quality transcripts: roughly how thick the section reads.',
    why: 'Its exact definition is not in the bundle, and the percentile ranges SituCraft tried do not reproduce it; it is shown as recorded.',
  },
  region_area: {
    group: 'cells', label: 'Region area', unit: 'µm²', format: 'number',
    help: 'The area of the region the run imaged, from which densities per region are computed.',
    why: 'It comes from the region chosen on the slide, which the bundle does not describe; it is shown as recorded.',
  },
  num_cells_detected: {
    group: 'cells', label: 'Cells', format: 'count',
    help: 'Cells the instrument\'s segmentation found.',
    how: 'The rows of cells.parquet.',
  },
  cells_per_100um2: {
    group: 'cells', label: 'Cells per 100 µm² of region', format: 'number',
    help: 'Cell density over the imaged region.',
    how: 'Cells ÷ the region area × 100.',
  },
  total_cell_area: {
    group: 'cells', label: 'Total cell area', unit: 'µm²', format: 'number',
    help: 'The area all cells cover.',
    how: 'The sum of cells.parquet\'s cell_area (the cell mask\'s pixels give the same).',
  },
  fraction_transcripts_assigned: {
    group: 'cells', label: 'Share of gene transcripts in cells', format: 'share',
    help: 'How much of the signal segmentation captures. A low share means boundaries miss cytoplasm, or much signal lies outside cells.',
    how: 'Gene transcripts with qv ≥ 20 assigned to a cell ÷ all gene transcripts with qv ≥ 20.',
  },
  median_transcripts_per_cell: {
    group: 'cells', label: 'Median gene transcripts per cell', format: 'number',
    help: 'The typical cell\'s gene transcripts: with the genes per cell, the depth available to tell cell types apart.',
    how: 'The median of cells.parquet\'s transcript_counts.',
  },
  median_genes_per_cell: {
    group: 'cells', label: 'Median genes per cell', format: 'number',
    help: 'The typical number of the panel\'s genes detected in a cell.',
    how: 'For each cell, its genes with at least one transcript of qv ≥ 20 (from the matrix); the median over cells.',
  },
  fraction_empty_cells: {
    group: 'cells', label: 'Share of cells with no gene transcript', format: 'share',
    help: 'Cells without a single gene transcript are segmentation artifacts or very low-quality cells.',
    how: 'Cells whose transcript_counts is 0 ÷ cells.',
  },
  negative_control_probe_counts_per_control_per_cell: {
    group: 'specificity', label: 'Negative control probes per control and cell', format: 'number',
    help: 'Probes that target nothing in the tissue: their counts estimate how often a probe detects something that is not there.',
    how: 'The sum of cells.parquet\'s control_probe_counts ÷ the panel\'s negative control probes ÷ cells.',
  },
  estimated_number_of_false_positive_transcripts_per_cell: {
    group: 'specificity', label: 'Estimated false-positive transcripts per cell', format: 'number',
    help: 'What a cell would collect from all the panel\'s genes at the negative controls\' rate.',
    how: 'Negative control probes per control and cell × the panel\'s genes.',
  },
  adjusted_negative_control_probe_rate: {
    group: 'specificity', label: 'Adjusted negative control probe rate', format: 'number',
    help: 'Negative control probe detections relative to the genes\' detections.',
    why: 'The adjustment it applies is not described in the bundle and simple rates do not reproduce it; it is shown as recorded. The negative-control map shows where controls are detected.',
  },
  adjusted_negative_control_codeword_rate: {
    group: 'specificity', label: 'Adjusted negative control codeword rate', format: 'number',
    help: 'Detections of codewords no probe uses (decoding errors) relative to the genes\' detections.',
    why: 'As for the probe rate, its adjustment is not described in the bundle; it is shown as recorded.',
  },
  segmented_cell_boundary_count: {
    group: 'segmentation', label: 'Cells segmented by a boundary stain', format: 'count',
    help: 'Multimodal segmentation first uses a membrane (boundary) stain.',
    how: 'Cells whose method in cells.zarr.zip is the boundary stain.',
  },
  segmented_cell_interior_count: {
    group: 'segmentation', label: 'Cells segmented by an interior stain', format: 'count',
    help: 'Where no boundary is found, the interior (RNA and protein) stains.',
    how: 'Cells whose method is the interior stain.',
  },
  segmented_cell_nuc_expansion_count: {
    group: 'segmentation', label: 'Cells by nucleus expansion', format: 'count',
    help: 'Where neither stain finds the cell, its nucleus grown by a few µm, the least certain method.',
    how: 'Cells whose method is nucleus expansion.',
  },
  segmented_cell_boundary_frac: {
    group: 'segmentation', label: 'Share segmented by a boundary stain', format: 'share',
    help: 'The boundary-stain cells\' share.', how: 'Boundary-stain cells ÷ cells.',
  },
  segmented_cell_interior_frac: {
    group: 'segmentation', label: 'Share segmented by an interior stain', format: 'share',
    help: 'The interior-stain cells\' share.', how: 'Interior-stain cells ÷ cells.',
  },
  segmented_cell_nuc_expansion_frac: {
    group: 'segmentation', label: 'Share by nucleus expansion', format: 'share',
    help: 'The nucleus-expansion cells\' share: the higher, the less the boundaries follow the cells.', how: 'Nucleus-expansion cells ÷ cells.',
  },
  segmented_cell_stain_frac: {
    group: 'segmentation', label: 'Share segmented by a stain', format: 'share',
    help: 'Cells found by a boundary or interior stain rather than by expanding a nucleus.', how: '(Boundary-stain + interior-stain cells) ÷ cells.',
  },
};

// The rows to show: every recorded metric with a value, explained, with SituCraft's recomputation
// when it made one. recorded: [{ name, text, value }]; checks: [{ name, recorded, computed,
// agrees, exact }] (internal/xenium Summary.Metrics).
export function metricRows(recorded, checks) {
  const check = new Map((checks ?? []).filter((c) => c.source !== 'experiment.xenium').map((c) => [c.name, c]));
  const rows = [];
  for (const r of recorded ?? []) {
    if (r.value === undefined || r.value === null) continue;
    const info = METRICS[r.name];
    const c = check.get(r.name);
    rows.push({
      name: r.name,
      group: info?.group ?? 'other',
      label: info?.label ?? r.name.replace(/_/g, ' '),
      unit: info?.unit ?? '',
      format: info?.format ?? 'number',
      help: info?.help ?? '',
      how: info?.how ?? '',
      why: info?.why ?? (c ? '' : 'SituCraft does not recompute it.'),
      recorded: r.value,
      computed: c ? c.computed : null,
      agrees: c ? c.agrees : null,
    });
  }
  return rows;
}

// A metric's value as text.
export function formatMetric(value, format) {
  if (value === null || value === undefined || !Number.isFinite(value)) return '—';
  if (format === 'count') return Math.round(value).toLocaleString('en-US');
  if (format === 'share') return `${(100 * value).toFixed(value < 0.001 ? 4 : 2)}%`;
  const abs = Math.abs(value);
  if (abs !== 0 && (abs < 0.001 || abs >= 1e6)) return value.toExponential(3);
  return value.toLocaleString('en-US', { maximumFractionDigits: abs < 1 ? 5 : 2 });
}
