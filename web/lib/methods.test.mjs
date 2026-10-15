import { test } from 'node:test';
import assert from 'node:assert/strict';
import { methodsParagraph } from './methods.js';

const dataset = {
  name: 'xenium-breast-2fov', panel: 'Xenium Human Breast Gene Expression', organism: 'Human', tissue: 'Breast',
  version: '2.0.0', software: 'xenium-2.0.0.10', cells: 7275, transcripts: 1113950,
  validation: { fatal: 0, warnings: 0, info: 3, metricsCompared: 22, metricsAgreeing: 22 },
};
const report = { summary: { panelGenes: 280, q20GeneTranscripts: 1035543, assignedFraction: 0.865653, segmentationMethods: ['Segmented by boundary stain (ATP1A1/CD45/E-Cadherin)', 'Segmented by interior stain (18S)', 'Segmented by nucleus expansion of 5.0µm', 'Segmented by nuclear stain (DAPI)'], segmentationCounts: [3300, 3913, 62, 0] } };
const program = { version: '0.1.0', commit: '956e0951234abcd' };

test('the methods paragraph says what was done, citing it', () => {
  const { text, references } = methodsParagraph({ dataset, report, program, filters: [] });
  assert.match(text, /^A human breast section was profiled on the 10x Genomics Xenium platform \[1\] with the Xenium Human Breast Gene Expression panel \(280 genes\), and its output analyzed by Xenium Onboard Analysis 2.0.0 \(xenium-2.0.0.10\)\./);
  assert.match(text, /The instrument segmented 7,275 cells: 3,300 by boundary stain \(ATP1A1\/CD45\/E-Cadherin\), 3,913 by interior stain \(18S\) and 62 by nucleus expansion of 5.0 µm\./);
  assert.match(text, /1,035,543 were of the panel's genes with a quality score \(qv, Phred-scaled\) of at least 20; only these were counted, as by Xenium Onboard Analysis, and 86.6% of them were assigned to cells\./);
  assert.match(text, /SituCraft 0.1.0 \(commit 956e095\) \[2\]/);
  assert.match(text, /0 fatal problems, 0 warnings, 3 notes\), and recomputed 22 of the values the run recorded from its data \(all agreeing\)\./);
  assert.equal(references.length, 2);
  assert.match(references[0], /^\[1\] Janesick A/);
  assert.match(references[1], /^\[2\] McDermott R\. SituCraft, version 0\.1\.0/);
  // The same inputs, the same text.
  assert.equal(methodsParagraph({ dataset, report, program }).text, text);
});

test('filters and public data are described when there are some, and nothing else is', () => {
  const filters = [{ name: 'Enough signal', version: 2, conditions: [{ text: 'Gene transcripts ≥ 30' }, { text: 'Genes detected ≥ 10' }], result: { cells: 7275, kept: 6520, sha256: '3e961211f175321bda59a815a0d8ff8e2aea10854b8ce268e04bc0b9f222aebe' } }];
  const source = { title: 'Xenium V1 human Breast, 2 FOVs', attribution: '10x Genomics', license: 'CC BY 4.0', url: 'https://www.10xgenomics.com/' };
  const { text, references } = methodsParagraph({ dataset, report, program, filters, source });
  assert.match(text, /The data are Xenium V1 human Breast, 2 FOVs, published by 10x Genomics under CC BY 4.0 \[2\]\./);
  assert.match(text, /Cells were filtered with "Enough signal" \(version 2: Gene transcripts ≥ 30 and Genes detected ≥ 10\), which kept 6,520 of 7,275 cells \(89.6%\); SituCraft records which cells under the SHA-256 checksum 3e961211f175321b…\./);
  assert.equal(references.length, 3);
  // Without a report: no segmentation or quality sentence beyond what the record has.
  const bare = methodsParagraph({ dataset: { ...dataset, validation: {} }, program: { version: '0.1.0' } });
  assert.match(bare.text, /The instrument's segmentation found 7,275 cells\./);
  assert.doesNotMatch(bare.text, /quality score|recomputed|commit/);
  // One method for every cell: said once.
  const one = methodsParagraph({ dataset: { ...dataset, cells: 11898 }, report: { summary: { segmentationMethods: ['Segmented by nucleus expansion of 5.0µm'], segmentationCounts: [11898] } }, program });
  assert.match(one.text, /The instrument segmented 11,898 cells, all by nucleus expansion of 5\.0 µm\./);
});

test('a default segmentation other than the instrument\'s is described, with why it was chosen', () => {
  const record = { source: 'situcraft', method: { name: 'SituCraft nucleus expansion', version: '1', citation: 'Okabe A, Boots B, Sugihara K, Chiu SN. Spatial Tessellations. 2000' }, parameters: { distance_um: 5 }, summary: { cells: 7312, assignedFraction: 0.9012 } };
  const segmentation = { record, decision: { rationale: 'Fewer merged cells' }, comparison: { other: 'the instrument\'s segmentation', citation: 'Jones DC, et al. 2025; Hartman A, Satija R. 2024' } };
  const { text, references } = methodsParagraph({ dataset, report, program, segmentation });
  assert.match(text, /Cells were segmented again with SituCraft's nucleus expansion \(version 1\): each nucleus grew by up to 5 µm, every pixel going to the nearest nucleus within that distance \[3\], giving 7,312 cells, 90\.1% of the gene transcripts of quality 20 or more assigned to them\./);
  assert.match(text, /It was chosen as the dataset's segmentation after comparing it with the instrument's segmentation cell by cell .* \[4\]\): "Fewer merged cells"\./);
  assert.equal(references.length, 4);
  // The instrument's own, the default, adds nothing.
  assert.equal(methodsParagraph({ dataset, report, program, segmentation: { record: { source: 'instrument' } } }).text, methodsParagraph({ dataset, report, program }).text);
});

test('a simulated dataset is said to be simulated, and no instrument or platform is implied', () => {
  const simulated = {
    name: 'sim-std', panel: 'SituCraft simulated panel', organism: 'Simulated', tissue: 'Simulated', region: 'simulated tissue, seed 20261014',
    version: '2.0.0', software: 'xenium-2.0.0 layout, situcraft-simulate 0.3.0', cells: 2925, transcripts: 440796,
    validation: { support: 'simulated', fatal: 0, warnings: 0, info: 2, metricsCompared: 16, metricsAgreeing: 16 },
  };
  const r = { support: 'simulated', summary: { q20GeneTranscripts: 397000, assignedFraction: 0.98, segmentationMethods: ['Segmented by nucleus expansion of 5.0µm'], segmentationCounts: [2925] } };
  const { text, references } = methodsParagraph({ dataset: simulated, report: r, program });
  assert.match(text, /^The data were simulated \(situcraft-simulate 0\.3\.0, simulated tissue, seed 20261014\): a tissue whose every cell, nucleus and transcript is known/);
  assert.match(text, /The simulated instrument segmented 2,925 cells, all by nucleus expansion of 5\.0 µm\./);
  assert.match(text, /only these were counted, as Xenium Onboard Analysis counts them/);
  assert.match(text, /including the morphology image, checked/);
  assert.doesNotMatch(text, /profiled|Xenium platform|JPEG 2000|analyzed by Xenium/);
  assert.equal(references.length, 1);
  // The record alone (no report) says the same.
  assert.match(methodsParagraph({ dataset: simulated, program }).text, /^The data were simulated/);
});

test('a refined default segmentation is described with its schedule, seed and summary', () => {
  const record = {
    source: 'situcraft', name: 'Transcript-informed segmentation',
    method: { name: 'SituCraft transcript-informed segmentation', version: '1', citation: 'Jones DC, et al. Cell simulation as cell segmentation. Nat Methods 2025;22(6):1331–1342. doi:10.1038/s41592-025-02697-0' },
    parameters: { stages: [{ voxel_um: 1, layers: 1, sweeps: 600 }], seed: 20250, samples: 500 },
    summary: { cells: 11890, assignedFraction: 0.912 },
  };
  const { text, references } = methodsParagraph({ dataset, report, program, segmentation: { record, decision: null } });
  assert.match(text, /Cells were segmented again with SituCraft's transcript-informed segmentation \(version 1\), after Jones et al\. \[3\]: starting from the instrument's cells and their nuclei, each cell's extent was sampled from its posterior given the transcripts/);
  assert.match(text, /at 1 µm voxels in one layer for 600 sweeps, seed 20250, and the summary over the last 500 sweeps taken, each transcript in its most probable cell, giving 11,890 cells, 91\.2% of the gene transcripts of quality 20 or more assigned to them\./);
  assert.match(references[2], /^\[3\] Jones DC, et al\. Cell simulation as cell segmentation/);
});
