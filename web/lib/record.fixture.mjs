// The project the record's and the methods' tests read (wave 8, slices 3 and 4): two sections, each
// segmented, clustered, labeled from a marker set, with a graph; the first's neighborhood
// enrichment; a design, a cohort summary and an expression comparison of both; and the summary's
// manifest as the program keeps it.

import { createProject } from './project.js';

const SCANPY = 'Wolf FA, Angerer P, Theis FJ. SCANPY: large-scale single-cell gene expression data analysis. Genome Biol 2018;19:15';
const XENIUM = 'Janesick A, Shelansky R, Gottscho AD, et al. High resolution mapping of the tumor microenvironment using integrated single-cell, spatial and in situ analysis. Nat Commun. 2023;14:8353';

export const at = (n) => `2026-10-19T00:0${n}:00Z`;
const dataset = (id, name, n) => ({ id, name, software: 'xenium-2.0.0.10', version: '2.0.0', panel: 'Xenium Human Lung', cells: 100 + n, transcripts: 1000 + n, analysisUuid: `uuid-${id}`, validation: { fatal: 0, warnings: 1, metricsCompared: 5, metricsAgreeing: 5 }, added: at(0) });
const made = (kind, id, fields) => ({ kind, id, state: 'complete', created: at(1), registered: at(1), software: [{ name: 'situcraft', version: '0.8.0' }], ...fields });

// Two samples, each segmented, clustered, labeled by a marker set, with a graph; the first's
// neighborhood enrichment; a design, a cohort summary and an expression comparison of both.
export function project() {
  const p = createProject('Record', { id: 'p-record', now: at(0) });
  p.datasets.push(dataset('d-1', 'S01', 1), dataset('d-2', 'S02', 2));
  for (const k of [1, 2]) {
    p.artifacts.push(
      made('segmentation', `seg-${k}`, { name: 'Instrument segmentation', dataset: `d-${k}`, source: 'instrument', method: { name: 'Xenium cell segmentation', version: 'xenium-2.0.0.10', citation: XENIUM }, parameters: null, software: [{ name: 'Xenium Onboard Analysis', version: 'xenium-2.0.0.10' }, { name: 'situcraft', version: '0.8.0' }], parents: [{ kind: 'bundle', id: `uuid-d-${k}` }] }),
      made('clustering', `clu-${k}`, { name: 'Clusters, resolution 1', dataset: `d-${k}`, segmentation: `seg-${k}`, created: at(2), method: { name: 'SituCraft clustering', version: '1', citation: `${SCANPY}; Traag VA, Waltman L, van Eck NJ. From Louvain to Leiden. Sci Rep 2019;9:5233` }, parameters: { minTranscripts: 10, components: 50, resolution: 1, neighbors: 15, seed: 20250 }, summary: { cells: 90 + k, clusters: 4 + k } }),
      made('labels', `lab-${k}`, { name: 'Labels by Lung', dataset: `d-${k}`, segmentation: `seg-${k}`, clustering: `clu-${k}`, markerSet: 'ms-1', created: at(4), method: { name: 'SituCraft labels from markers', version: '1', citation: `Tirosh I, et al. Dissecting the multicellular ecosystem. Science 2016;352:189–196; ${SCANPY}` }, parameters: { margin: 0.1, minimum: 0, state: 0.25, bins: 25, controls: 50, lineage: true, seed: 20250, markers: { name: 'Lung', checksum: 'a'.repeat(64) } }, summary: { cells: 90 + k, levels: ['Class'], labeled: [80 + k], states: 0 } }),
      made('graph', `gra-${k}`, { name: 'Delaunay graph', dataset: `d-${k}`, segmentation: `seg-${k}`, created: at(5), method: { name: 'SituCraft cell graph', version: '1', citation: 'Lee DT, Schachter BJ. Two algorithms for constructing a Delaunay triangulation. Int J Comput Inf Sci 1980;9:219–242' }, parameters: { kind: 'delaunay', longest: 50 }, summary: { edges: 300 + k, meanDegree: 5.8, choice: {} } }),
    );
  }
  p.artifacts.push(
    { kind: 'marker-set', id: 'ms-1', family: 'ms-1', version: 1, parent: null, name: 'Lung', set: { levels: ['Class'], labels: [{ name: 'T cell' }, { name: 'B cell' }], references: [{ doi: '10.1038/s41586-020-2922-4', citation: 'Travaglini KJ, et al. A molecular cell atlas of the human lung. Nature 2020;587:619–625.' }, { doi: '10.9999/atlas', citation: 'Doe J. An atlas of our own. J Atlases 2021;1:1.' }] }, checksum: 'b'.repeat(64), source: { starter: 'lung' }, by: 'the user', created: at(3) },
    made('spatial', 'spa-1', { name: 'Neighborhood enrichment', dataset: 'd-1', segmentation: 'seg-1', graph: 'gra-1', question: 'enrichment', created: at(6), method: { name: 'SituCraft spatial analysis', version: '1', citation: 'Schapiro D, Jackson HW, Raghuraman S, et al. histoCAT. Nat Methods 2017;14:873–876; Roe R. Unpublished permutations. 2025' }, parameters: { permutations: 1000, seed: 7, weight: 'edges' }, summary: { question: 'enrichment', labels: { id: 'lab-1', name: 'Labels by Lung: class', count: 2 }, significant: 2 } }),
    { kind: 'design', id: 'des-1', name: 'By condition', version: 1, parent: null, created: at(7), spec: { unit: 'subject', group: 'condition', reference: 'control' }, samples: [{ id: 's-d-1', name: 'S01', dataset: 'd-1', region: null }, { id: 's-d-2', name: 'S02', dataset: 'd-2', region: null }], excluded: [], result: { names: ['Intercept', 'condition[T.treated]'], df: 0, words: ['Compares condition: treated against control.'] } },
    made('cohort-summary', 'coh-1', { name: 'Summary of 2 samples', markerSet: 'ms-1', labelsFrom: 'ms-1', level: 0, samples: ['s-d-1', 's-d-2'], created: at(8), method: { name: 'SituCraft cohort summary', version: '2', citation: 'Crowell HL, Soneson C, Germain P-L, et al. muscat. Nat Commun 2020;11:6077' }, parameters: { closing: 10, labels: { name: 'Lung', level: 0, names: ['T cell', 'B cell'] }, niches: { neighbors: 10, radius: 0, niches: 6, starts: 10, seeds: 4, seed: 20250 } } }),
    made('expression', 'exp-1', { name: 'Expression compared: treated − control', summary: 'coh-1', design: { name: 'By condition', version: 1 }, created: at(9), method: { name: 'SituCraft differential expression', version: '1', citation: 'Smyth GK. Linear models and empirical Bayes methods. Stat Appl Genet Mol Biol 2004;3:3; Benjamini Y, Hochberg Y. Controlling the false discovery rate. J R Stat Soc B 1995;57:289–300' }, parameters: { level: 0.95, minCells: 10, design: { name: 'By condition', version: 1, contrasts: [{ name: 'treated − control', vector: [0, 1] }] } }, summaryOf: { summary: 'coh-1', tests: 50, found: [{ label: 'T cell', gene: 'CD3E', contrast: 'treated − control', logFC: 1.2, padj: 0.012 }] } }),
  );
  p.decisions.push({ kind: 'promote-segmentation', dataset: 'd-1', artifact: 'seg-1', rationale: 'The only one.', by: 'SituCraft', time: at(1) }, { kind: 'promote-segmentation', dataset: 'd-2', artifact: 'seg-2', rationale: 'The only one.', by: 'SituCraft', time: at(1) });
  return p;
}

// The cohort summary's manifest, as the program keeps it: the samples' segmentations its parents,
// and its job's inputs (each sample's segmentation, labels, graph and analyses).
export const SUMMARY_MANIFEST = { id: 'coh-1', lineage: { parents: [{ kind: 'segmentation', id: 'seg-1' }, { kind: 'segmentation', id: 'seg-2' }], job: { kind: 'cohort', spec: { samples: [{ sample: 's-d-1', segmentation: 'seg-1', labels: { id: 'lab-1', codes: 'AAAA' }, graph: 'gra-1', analyses: ['spa-1'] }, { sample: 's-d-2', segmentation: 'seg-2', labels: { id: 'lab-2' }, graph: 'gra-2' }] } } } };
