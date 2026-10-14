// A methods paragraph for a dataset, written from what the project records of it (its bundle's
// metadata and validation, its filters) and SituCraft's version, with the references it cites.
// Only what was done is said: nothing is written for a step that did not happen. Deterministic:
// the same inputs give the same text.

const count = (n) => Number(n).toLocaleString('en-US');

// The cell segmentation methods a run used, as { n: 3300, text: "by boundary stain
// (ATP1A1/CD45/E-Cadherin)" }, from the validation report's methods and counts.
function segmentationText(methods, counts) {
  const parts = [];
  (methods ?? []).forEach((m, i) => {
    const n = counts?.[i] ?? 0;
    if (n > 0) parts.push({ n, text: m.replace(/^Segmented /, '').replace(/\s+$/, '').replace(/(\d)µm/g, '$1 µm') });
  });
  return parts;
}

function joinList(parts) {
  if (parts.length <= 1) return parts.join('');
  return `${parts.slice(0, -1).join(', ')} and ${parts[parts.length - 1]}`;
}

// The paragraph and its references. inputs: { dataset (the project's record), report (its
// validation report, or null), filters (the dataset's filter versions to describe), program:
// { version, commit }, source: { title, attribution, license } for public data, or null }.
export function methodsParagraph({ dataset, report = null, filters = [], program, source = null }) {
  const refs = [];
  const cite = (key, text) => {
    let i = refs.findIndex((r) => r.key === key);
    if (i < 0) {
      refs.push({ key, text });
      i = refs.length - 1;
    }
    return `[${i + 1}]`;
  };
  const s = report?.summary ?? {};
  const sentences = [];
  const panel = dataset.panel ? `the ${dataset.panel} panel${s.panelGenes ? ` (${count(s.panelGenes)} genes)` : ''}` : 'its gene panel';
  const tissue = [dataset.organism, dataset.tissue].filter(Boolean).join(' ').toLowerCase();
  const software = dataset.software || s.software || '';
  sentences.push(`${tissue ? `A ${tissue} section` : 'The section'} was profiled on the 10x Genomics Xenium platform ${cite('xenium', 'Janesick A, Shelansky R, Gottscho AD, et al. High resolution mapping of the tumor microenvironment using integrated single-cell, spatial and in situ analysis. Nat Commun. 2023;14:8353. doi:10.1038/s41467-023-43458-x')} with ${panel}, and its output analyzed by Xenium Onboard Analysis${dataset.version ? ` ${dataset.version}` : ''}${software && !software.endsWith(dataset.version) ? ` (${software})` : ''}.`);
  if (source) sentences.push(`The data are ${source.title}, published by ${source.attribution} under ${source.license} ${cite('data', `${source.attribution}. ${source.title}. ${source.url ?? ''}`.trim())}.`);
  const seg = segmentationText(s.segmentationMethods, s.segmentationCounts);
  const cellsText = `${count(dataset.cells)} cells`;
  if (seg.length === 1 && seg[0].n === dataset.cells) sentences.push(`The instrument segmented ${cellsText}, all ${seg[0].text}.`);
  else if (seg.length) sentences.push(`The instrument segmented ${cellsText}: ${joinList(seg.map((p) => `${count(p.n)} ${p.text}`))}.`);
  else sentences.push(`The instrument's segmentation found ${cellsText}.`);
  if (s.q20GeneTranscripts) {
    sentences.push(`Of ${count(dataset.transcripts)} decoded transcripts, ${count(s.q20GeneTranscripts)} were of the panel's genes with a quality score (qv, Phred-scaled) of at least 20; only these were counted, as by Xenium Onboard Analysis${Number.isFinite(s.assignedFraction) && s.assignedFraction > 0 ? `, and ${(100 * s.assignedFraction).toFixed(1)}% of them were assigned to cells` : ''}.`);
  }
  const v = dataset.validation ?? {};
  const version = `SituCraft ${program.version}${program.commit ? ` (commit ${program.commit.slice(0, 7)})` : ''} ${cite('situcraft', `McDermott R. SituCraft, version ${program.version}. https://github.com/robert-mcdermott/situcraft`)}`;
  const checks = v.metricsCompared ? `, and recomputed ${count(v.metricsCompared)} of the values the run recorded from its data (${v.metricsAgreeing === v.metricsCompared ? 'all agreeing' : `${count(v.metricsAgreeing)} agreeing`})` : '';
  sentences.push(`The output bundle was read with ${version}, whose own readers decoded every file, including the JPEG 2000 morphology images (ITU-T T.800), checked the files' consistency (${count(v.fatal ?? 0)} fatal problems, ${count(v.warnings ?? 0)} warnings, ${count(v.info ?? 0)} notes)${checks}.`);
  for (const f of filters) {
    const conditions = (f.conditions ?? []).map((c) => c.text ?? '').filter(Boolean);
    const share = f.result.cells ? ` (${((100 * f.result.kept) / f.result.cells).toFixed(1)}%)` : '';
    sentences.push(`Cells were filtered with "${f.name}" (version ${f.version}${conditions.length ? `: ${joinList(conditions)}` : ''}), which kept ${count(f.result.kept)} of ${count(f.result.cells)} cells${share}; SituCraft records which cells under the SHA-256 checksum ${f.result.sha256.slice(0, 16)}….`);
  }
  return { text: sentences.join(' '), references: refs.map((r, i) => `[${i + 1}] ${r.text}`) };
}
