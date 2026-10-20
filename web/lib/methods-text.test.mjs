import assert from 'node:assert/strict';
import test from 'node:test';
import { SUMMARY_MANIFEST, at, project } from './record.fixture.mjs';
import { resultRecord, scopeResults } from './record.js';
import { methodsBibTeX, methodsMarkdown, methodsPlain, methodsText } from './methods-text.js';

// The citations in a text, in the order they appear: [1, 3–5] gives 1, 3, 4, 5.
function cited(text) {
  const out = [];
  for (const m of text.matchAll(/\[([\d,–\s]+)\]/g)) {
    for (const part of m[1].split(/,\s*/)) {
      const [a, b] = part.split('–').map(Number);
      for (let n = a; n <= (b ?? a); n += 1) out.push(n);
    }
  }
  return out;
}

// References numbered as first cited: each first appearance the next number, every one cited.
function numberedAsCited(m) {
  const firsts = [...new Set(cited(m.text))];
  return firsts.every((n, i) => n === i + 1) && firsts.length === m.references.length;
}

const projectRecord = (p, manifests = new Map()) => resultRecord(p, scopeResults(p), { manifests, program: { version: '0.8.0' }, subject: { id: p.id, kind: 'project', name: p.name } });

test('a result\'s methods: each of its steps said as what was done, by topic, its references numbered as first cited', () => {
  const p = project();
  const m = methodsText(p, resultRecord(p, 'spa-1', { program: { version: '0.8.0', commit: 'abcdef1234' } }));
  assert.equal(m.title, 'Methods: Neighborhood enrichment');
  assert.deepEqual(m.paragraphs.map((x) => x.topic), ['data', 'cells', 'spatial', 'software']);
  const [data, cells, spatial, software] = m.paragraphs.map((x) => x.text);
  assert.match(data, /^The section was profiled on the 10x Genomics Xenium platform \[1\] with the Xenium Human Lung panel, and its output analyzed by Xenium Onboard Analysis 2\.0\.0 \(xenium-2\.0\.0\.10\)\./);
  assert.match(data, /read with SituCraft 0\.8\.0 \(commit abcdef1\) \[2\].*\(0 fatal problems, 1 warning, 0 notes\)/);
  assert.match(cells, /^The cells were clustered as scanpy's defaults do \[2–4\]: the cells with at least 10 gene transcripts, .* 50 principal components; a graph of each cell's 15 nearest neighbors; and Leiden communities of modularity at a resolution of 1 \(seed 20250\), .* This gave 5 clusters of 91 cells\./);
  assert.match(cells, /Cell types were defined by the marker set "Lung" \(version 1: 2 labels; one of SituCraft's starter sets\) \[5, 6\]\./);
  assert.match(cells, /The cells were labeled from the marker set's markers \[2, 3, 5–7\]: .* \(the genes cut into 25 bins by mean expression, 50 controls drawn .*; seed 20250\), a cell taking a level's best label when it scored above 0 and led the next by 0\.1\. The cells labeled: 89\.0% at the class level\./);
  assert.match(spatial, /^A graph of the cells was built: cells whose centroids are neighbors in the Delaunay triangulation .* edges longer than 50 µm left out \[2, 8\]; 301 edges, a mean degree of 5\.8\./);
  assert.match(spatial, /The neighborhood enrichment of the class labels of "Lung" was measured over the cells' graph \[2, 9, 10\]: .* from 1,000 permutations of the labels \(seed 7\), .*: 2 of 3 pairs differed from chance/);
  assert.match(software, /^The steps were run with Xenium Onboard Analysis \(xenium-2\.0\.0\.10\) and SituCraft \(0\.8\.0\) \[2\]; this text was written from the project's record of each step by SituCraft 0\.8\.0\.$/);
  // Numbered as first cited in this text; a marker set's atlas the registry does not hold, and a
  // method's citation, kept as written.
  assert.ok(numberedAsCited(m), JSON.stringify([...new Set(cited(m.text))]));
  assert.deepEqual(m.references.map((r) => r.key), ['janesick2023', 'situcraft', 'wolf2018', 'traag2019', 'travaglini2020', null, 'tirosh2016', 'lee1980', 'schapiro2017', null]);
  assert.equal(m.references[5].doi, '10.9999/atlas');
  // The same project and record, the same text; written as Markdown, plain text and BibTeX.
  assert.deepEqual(methodsText(p, resultRecord(p, 'spa-1', { program: { version: '0.8.0', commit: 'abcdef1234' } })), m);
  const md = methodsMarkdown(m);
  assert.match(md, /^# Methods: Neighborhood enrichment\n\n## Data\n\nThe section/);
  assert.match(md, /\n## References\n\n1\. Janesick A, .* doi:10\.1038\/s41467-023-43458-x\n2\. McDermott R\. SituCraft, version 0\.8\.0/);
  assert.match(methodsPlain(m), /\n\n\[1\] Janesick A/);
  const bib = methodsBibTeX(m);
  assert.equal([...bib.matchAll(/^@\w+\{/gm)].length, m.references.length);
  assert.match(bib, /@software\{situcraft,[\s\S]*version = \{0\.8\.0\}/);
});

test('a project\'s methods: the same steps of each section said once, the samples\' design, summary and comparison', () => {
  const p = project();
  const m = methodsText(p, projectRecord(p, new Map([['coh-1', SUMMARY_MANIFEST]])), { scope: 'project' });
  assert.equal(m.title, 'Methods: Record');
  assert.deepEqual(m.paragraphs.map((x) => x.topic), ['data', 'cells', 'spatial', 'cohort', 'software']);
  const text = (topic) => m.paragraphs.find((x) => x.topic === topic).text;
  assert.match(text('data'), /^The 2 sections \(S01 and S02\) were profiled on the 10x Genomics Xenium platform \[1\] .*: 101 to 102 cells and 1,001 to 1,002 decoded transcripts a section, as the instrument segmented them\. Each output bundle was read with SituCraft 0\.8\.0 \[2\], .* 0 fatal problems and 2 warnings in all, and 10 of the 10 values the runs recorded recomputed from their data alike\./);
  assert.match(text('cells'), /The cells of each of S01 and S02 were clustered .* This gave 5 clusters of 91 cells \(S01\) and 6 clusters of 92 cells \(S02\)\./);
  assert.match(text('cells'), /The cells labeled: 89\.0% at the class level \(S01\) and 89\.1% at the class level \(S02\)\./);
  assert.match(text('spatial'), /A graph of the cells of each of S01 and S02 was built: .*; 301 edges, a mean degree of 5\.8 \(S01\) and 302 edges, a mean degree of 5\.8 \(S02\)\./);
  assert.match(text('spatial'), /The neighborhood enrichment of the class labels of "Lung" \(the cells of S01\)/);
  assert.match(text('cohort'), /^The samples \(2 samples: S01 and S02\) were compared by the design "By condition" \(version 1\), which reads: Compares condition: treated against control\. Each sample was summarized \[2, 11\]: .* labeled by "Lung" \(T cell and B cell\); .* \(its outline closed by 10 µm\); .* Niches were found across the samples together: each cell's 10 nearest labeled cells .* into 6 niches \(10 starts, 4 seeds compared; seed 20250\)\./);
  assert.match(text('cohort'), /Each label's pseudobulk was compared between the groups \[2, 12, 13\], with the subject as the unit: .* each label's rows with at least 10 of its cells; .* with its 95% interval and moderated t; .* Of 50 tests, 1 had an adjusted p below 0\.05: CD3E \(T cell; treated − control; log2 fold change \+1\.20, adjusted p 0\.012\)\./);
  assert.ok(numberedAsCited(m));
});

test('choices and reviews: a segmentation a person chose said with why, SituCraft\'s own not; a dataset\'s reviews', () => {
  const p = project();
  // An expansion of S01's cells, compared with the instrument's and chosen by a person.
  p.artifacts.push(
    { kind: 'segmentation', id: 'seg-x', name: 'Nucleus expansion, 3 µm', dataset: 'd-1', source: 'situcraft', state: 'complete', created: at(2), registered: at(2), method: { name: 'SituCraft nucleus expansion', version: '1', citation: 'Okabe A, Boots B, Sugihara K, Chiu SN. Spatial Tessellations. Wiley, 2000; distances: Felzenszwalb PF, Huttenlocher DP. Theory Comput. 2012;8:415' }, parameters: { distance_um: 3 }, software: [{ name: 'situcraft', version: '0.8.0' }], parents: [{ kind: 'bundle', id: 'uuid-d-1' }, { kind: 'segmentation', id: 'seg-1' }], summary: { cells: 101, assignedFraction: 0.94 } },
    { kind: 'comparison', id: 'cmp-1', name: 'Instrument segmentation and Nucleus expansion, 3 µm', dataset: 'd-1', a: 'seg-1', b: 'seg-x', state: 'complete', created: at(3), registered: at(3), method: { name: 'SituCraft segmentation comparison', version: '2', citation: 'Spurious co-expression: Jones DC, Elz AE, Hadadianpour A, et al. Cell simulation as cell segmentation. Nat Methods. 2025;22:1331–1342' }, parameters: { within: 0.5, seed: 20250 }, summary: { pixelAgreement: 0.9, medianIoU: 0.967 } },
  );
  p.decisions.push({ kind: 'promote-segmentation', dataset: 'd-1', artifact: 'seg-x', rationale: 'Fewer merged cells', comparison: 'cmp-1', by: 'Ann', time: at(4) });
  p.reviews = [{ id: 'rev-1', dataset: 'd-1', action: 'accept', by: 'Ann', time: at(5) }, { id: 'rev-2', dataset: 'd-1', action: 'exclude', by: 'Ann', time: at(6) }];
  const m = methodsText(p, resultRecord(p, scopeResults(p, 'd-1'), { program: { version: '0.8.0' }, subject: { id: 'd-1', kind: 'dataset', name: 'S01' } }), { scope: 'dataset' });
  const seg = m.paragraphs.find((x) => x.topic === 'segmentation').text;
  assert.match(seg, /^Cells were segmented again with SituCraft's nucleus expansion \(version 1\): each nucleus grew by up to 3 µm, every pixel going to the nearest nucleus within that distance \[\d+–\d+\], giving 101 cells, 94\.0% of the gene transcripts of quality 20 or more assigned to them\. It was chosen as the dataset's segmentation after comparing it with the instrument's segmentation cell by cell \[\d+\]: "Fewer merged cells\."/);
  assert.match(seg, /The instrument's segmentation and "Nucleus expansion, 3 µm" were compared cell by cell \[[\d, –]+\]: cells corresponding when they share a nucleus or half of one lies within the other, .*; 90\.0% of their pixels agreed, and the median intersection over union of corresponding cells was 0\.97\./);
  // SituCraft's own choice of the instrument's segmentation is not said.
  assert.doesNotMatch(m.text, /The only one/);
  assert.match(m.paragraphs.find((x) => x.topic === 'quality').text, /^The cells and regions were reviewed by Ann: 2 reviews \(1 accepted and 1 excluded from analyses\)/);
  // A result's methods say no review.
  assert.doesNotMatch(methodsText(p, resultRecord(p, 'clu-1')).text, /reviewed by/);
  assert.ok(numberedAsCited(m));
});
