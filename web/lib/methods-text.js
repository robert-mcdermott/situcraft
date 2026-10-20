// The methods text (wave 8, slice 4; requirement E4): a methods section written from the record of
// a result, of a dataset or of the whole project (lib/record.js), every kind of step said as what
// was done, in paragraphs by topic (the data; images and regions; segmentation; quality and
// review; cell types; spatial analysis; the samples compared; figures; software), its references
// numbered as first cited. Only what the record and the project hold is said; a step made the same
// way for several sections or segmentations is said once, naming each. Deterministic: the same
// project and record give the same text.

import { REFERENCES, citationKeys, referenceText } from './references.js';
import { itemOf, numbers, referencesBibTeX } from './record.js';
import { filterSentence, joinList, methodsParagraph, segmentationSentence } from './methods.js';
import { describeCondition } from './filters.js';
import { describeImage, describePlacement } from './images.js';
import { describeCheck } from './transforms.js';
import { clusterName } from './clusterings.js';
import { reviewsOf } from './review-record.js';

export const TOPICS = [
  { id: 'data', heading: 'Data' },
  { id: 'tissue', heading: 'Images and regions' },
  { id: 'segmentation', heading: 'Segmentation' },
  { id: 'quality', heading: 'Quality control and review' },
  { id: 'cells', heading: 'Cell types' },
  { id: 'spatial', heading: 'Spatial analysis' },
  { id: 'cohort', heading: 'Samples compared' },
  { id: 'figures', heading: 'Figures' },
  { id: 'software', heading: 'Software' },
];

const TOPIC_OF = {
  dataset: 'data', image: 'tissue', transform: 'tissue', 'alignment-check': 'tissue', region: 'tissue',
  segmentation: 'segmentation', comparison: 'segmentation', filter: 'quality',
  clustering: 'cells', 'cluster-names': 'cells', embedding: 'cells', 'marker-set': 'cells', labels: 'cells', 'curated-labels': 'cells', 'consensus-labels': 'cells',
  graph: 'spatial', spatial: 'spatial',
  design: 'cohort', 'cohort-summary': 'cohort', abundance: 'cohort', expression: 'cohort', 'spatial-comparison': 'cohort',
  figure: 'figures',
};

// The kinds said once for several items made the same way (each a section's, or a segmentation's).
const SHARED = new Set(['clustering', 'embedding', 'labels', 'graph', 'spatial']);

const count = (n) => Number(n ?? 0).toLocaleString('en-US');
const fixed = (x, d = 2) => (Number.isFinite(x) ? x.toFixed(d) : '?');
const plural = (n, word, words = `${word}s`) => `${count(n)} ${n === 1 ? word : words}`;
const lowerFirst = (s) => (s ? s.charAt(0).toLowerCase() + s.slice(1) : s);
const upperFirst = (s) => (s ? s.charAt(0).toUpperCase() + s.slice(1) : s);
const article = (word) => `${/^[aeiou]/i.test(word) ? 'an' : 'a'} ${word}`;
const level = (l) => `${Math.round(100 * (l ?? 0.95))}%`;
// A reason quoted at a sentence's end: "…", closed by its own period or one added.
const quoted = (text) => `"${String(text ?? '').trim()}${/[.!?]$/.test(String(text ?? '').trim()) ? '' : '.'}"`;
// Decisions a person made (SituCraft's own, a dataset's only segmentation or an image's only
// alignment taken when it was added, are not said).
const madeBy = (d) => d.by && d.by !== 'SituCraft';
// Values as a range: "691", or "672 to 767" (each written by fmt).
const range = (values, fmt = count) => {
  const v = values.filter(Number.isFinite);
  if (!v.length) return '?';
  const lo = Math.min(...v);
  const hi = Math.max(...v);
  return lo === hi ? fmt(lo) : `${fmt(lo)} to ${fmt(hi)}`;
};
const canonical = (v) => JSON.stringify(v, (k, x) => (x && typeof x === 'object' && !Array.isArray(x) ? Object.fromEntries(Object.entries(x).sort(([a], [b]) => (a < b ? -1 : 1))) : x));
// A segmentation as a sentence names it.
const segName = (rec) => (!rec ? 'a segmentation' : /^instrument segmentation$/i.test(rec.name) ? 'the instrument\'s segmentation' : `"${rec.name}"`);
// A label set's name as a sentence says it: "the class labels of Human lung".
const labelsName = (name) => {
  const m = /^Labels by (.*): ([^:]+)$/.exec(name ?? '');
  return m ? `the ${m[2].toLowerCase()} labels of "${m[1]}"` : name ? `the labels "${name.replace(/^Labels by /, '')}"` : 'the labels';
};

// The methods text of a record: { title, subject, paragraphs: [{ topic, heading, text }], text,
// references: [{ n, key, text, doi?, url? }], program }. options: { reports (dataset id → its
// validation report: its panel's genes, the transcripts counted, the instrument's segmentation
// methods), sources (dataset id → { title, attribution, license, url } for public data), scope
// ('result', 'dataset' or 'project': for a dataset or the project, its reviews are said too) }.
export function methodsText(project, record, { reports = {}, sources = {}, scope = 'result' } = {}) {
  const program = record.program ?? { version: '' };
  // References numbered as first cited in this text.
  const list = [];
  const numberOf = (ref) => {
    let i = list.findIndex((x) => (ref.key ? x.key === ref.key : !x.key && x.text === ref.text));
    if (i < 0) {
      list.push(ref);
      i = list.length - 1;
    }
    return i + 1;
  };
  const brackets = (ns) => {
    const sorted = [...new Set(ns)].sort((a, b) => a - b);
    return sorted.length ? ` [${numbers(sorted)}]` : '';
  };
  const fromRecord = (n) => {
    const r = record.references[n - 1];
    return numberOf({ key: r.key, text: r.text, ...(r.doi ? { doi: r.doi } : {}), ...(r.url ? { url: r.url } : {}) });
  };
  const registry = (key) => (key === 'situcraft'
    ? { key, text: record.references.find((r) => r.key === 'situcraft')?.text ?? `McDermott R. SituCraft, version ${program.version}. https://github.com/robert-mcdermott/situcraft`, url: 'https://github.com/robert-mcdermott/situcraft' }
    : { key, text: referenceText(REFERENCES[key]), ...(REFERENCES[key].doi ? { doi: REFERENCES[key].doi } : {}) });
  // A group's references (the record's numbers), or a citation's (as written), cited here.
  const cite = (g) => brackets(g.flatMap((s) => s.references.map(fromRecord)));
  const citeText = (citation) => {
    const { keys, unknown } = citationKeys(citation ?? '');
    return brackets([...keys.map((k) => numberOf(registry(k))), ...unknown.map((text) => numberOf({ key: null, text }))]);
  };
  // methods.js's numbering (a dataset's paragraph): Xenium's paper, SituCraft, the data.
  const legacy = (key, text) => `[${numberOf(key === 'xenium' ? registry('janesick2023') : key === 'situcraft' ? registry('situcraft') : { key: null, text })}]`;

  const rec = (s) => itemOf(project, s.id)?.record ?? {};
  const byId = (id) => (id ? itemOf(project, id)?.record ?? null : null);
  const datasets = record.steps.filter((s) => s.kind === 'dataset');
  const several = datasets.length > 1;
  // A group's items named by what tells them apart: their sections, their segmentations, or both
  // (one item: its section when there are several, its segmentation when not the instrument's).
  const segOf = (s) => {
    const r = rec(s);
    const seg = r.segmentation ? byId(r.segmentation) : null;
    return seg ? segName(seg) : '';
  };
  // The regions an item's cells were chosen in (a graph's, or an analysis's), named.
  const regionOf = (s) => joinList(((rec(s).summary ?? {}).choice?.regions ?? []).map((x) => x.name));
  const parts = (g) => {
    const differ = (fn) => new Set(g.map(fn)).size > 1;
    if (g.length === 1) return { sections: several, segs: /^"/.test(segOf(g[0])), regions: Boolean(regionOf(g[0])) };
    return { sections: differ((x) => x.dataset), segs: differ(segOf), regions: differ(regionOf) };
  };
  const labelsOf = (g) => {
    const { sections, segs, regions } = parts(g);
    return g.map((x) => [sections ? x.dataset : '', segs ? segOf(x) : '', regions ? regionOf(x) : ''].filter(Boolean).join(', '));
  };
  const labelOf = (x, g) => labelsOf(g)[g.indexOf(x)];
  // Whom a group's sentence is of: "The cells", "The cells of S01", "The cells under "Nucleus
  // expansion"", "The cells in Tumor", "The cells of each of S01 and S02", "The cells under each
  // of …", "The cells in each of …".
  const cellsOf = (g) => {
    const { sections, segs, regions } = parts(g);
    if (g.length === 1) return `The cells${sections ? ` of ${g[0].dataset}` : ''}${segs ? ` under ${segOf(g[0])}` : ''}${regions ? ` in ${regionOf(g[0])}` : ''}`;
    if (sections && !segs && !regions) return g.length === datasets.length && g.length > 2 ? 'The cells of each section' : `The cells of each of ${joinList(g.map((x) => x.dataset))}`;
    if (segs && !sections && !regions) return `The cells${several ? ` of ${g[0].dataset}` : ''} under each of ${joinList(g.map(segOf))}`;
    if (regions && !sections && !segs) return `The cells${several ? ` of ${g[0].dataset}` : ''} in each of ${joinList(g.map(regionOf))}`;
    return `The cells of each of ${joinList(labelsOf(g))}`;
  };
  // A group's results: each item's (named, for a few), or their range (for many sections or
  // regions; ranged a text, or a function of the unit, "a section").
  const results = (g, fn, ranged) => {
    if (g.length === 1) return fn(g[0]);
    if (g.length > 3 && ranged) {
      const { sections, regions } = parts(g);
      const unit = sections ? 'a section' : regions ? 'a region' : 'each';
      return typeof ranged === 'function' ? ranged(unit) : `${ranged} ${unit}`;
    }
    return joinList(g.map((x) => `${fn(x)} (${labelOf(x, g)})`));
  };
  const seedOf = (g) => {
    const seeds = [...new Set(g.flatMap((x) => x.seeds))];
    return seeds.length ? `seed ${seeds.join(', ')}` : '';
  };

  // A cohort's result named when the record holds several of its kind.
  const kinds = new Map();
  for (const x of record.steps) kinds.set(x.kind, (kinds.get(x.kind) ?? 0) + 1);
  const forThe = (x) => (kinds.get(x.kind) > 1 ? `For "${x.name}", ` : '');
  const opening = (x, sentence) => (forThe(x) ? `${forThe(x)}${lowerFirst(sentence)}` : sentence);
  const out = new Map(TOPICS.map((t) => [t.id, []]));
  const say = (topic, sentence) => sentence && out.get(topic).push(sentence);

  for (const g of textGroups(record, rec)) {
    const s = g[0];
    const r = rec(s);
    const topic = TOPIC_OF[s.kind];
    if (!topic) continue;
    const of = (x) => (several && x.dataset ? ` of ${x.dataset}` : '');
    switch (s.kind) {
      case 'dataset': {
        if (g.length === 1) {
          say(topic, methodsParagraph({ dataset: r, report: reports[r.id] ?? null, program, source: sources[r.id] ?? null, cite: legacy }).text);
          break;
        }
        const ds = g.map(rec);
        const simulated = r.validation?.support === 'simulated';
        const names = joinList(ds.map((x) => x.name));
        const sizes = `${range(ds.map((x) => x.cells))} cells and ${range(ds.map((x) => x.transcripts))} decoded transcripts a section`;
        if (simulated) say(topic, `The ${count(g.length)} sections (${names}) were simulated by SituCraft (situcraft simulate ${s.method?.version ?? ''}): tissues whose every cell, nucleus and transcript is known, written in the Xenium output bundle's format with their true segmentations alongside; ${sizes}.`);
        else say(topic, `The ${count(g.length)} sections (${names}) were profiled on the 10x Genomics Xenium platform ${legacy('xenium')} with the ${r.panel || 'same'} panel, and their outputs analyzed by Xenium Onboard Analysis${r.version ? ` ${r.version}` : ''}${r.software && !r.software.endsWith(r.version) ? ` (${r.software})` : ''}: ${sizes}, as the instrument segmented them.`);
        const v = ds.map((x) => x.validation ?? {});
        const compared = v.reduce((a, x) => a + (x.metricsCompared ?? 0), 0);
        const agreeing = v.reduce((a, x) => a + (x.metricsAgreeing ?? 0), 0);
        say(topic, `Each output bundle was read with SituCraft ${program.version}${program.commit ? ` (commit ${program.commit.slice(0, 7)})` : ''} ${legacy('situcraft')}, whose own readers decoded every file, and checked: ${plural(v.reduce((a, x) => a + (x.fatal ?? 0), 0), 'fatal problem')} and ${plural(v.reduce((a, x) => a + (x.warnings ?? 0), 0), 'warning')} in all${compared ? `, and ${count(agreeing)} of the ${count(compared)} values the runs recorded recomputed from their data alike` : ''}.`);
        break;
      }
      case 'segmentation': {
        for (const x of g) {
          const xr = rec(x);
          if (xr.source === 'instrument') continue;
          if (xr.source === 'planted') {
            say(topic, `Errors were planted in a copy of the instrument's segmentation${of(x)} for practice ("${xr.name}").`);
            continue;
          }
          const sentence = segmentationSentence(xr, cite([x]));
          say(topic, several ? `For ${x.dataset}, ${lowerFirst(sentence)}` : sentence);
          for (const d of x.decisions.filter((y) => y.kind === 'promote-segmentation' && madeBy(y))) {
            const cmp = byId(d.comparison);
            const other = cmp ? byId(cmp.a === x.id ? cmp.b : cmp.a) : null;
            say(topic, `It was chosen as ${several ? `${x.dataset}'s` : 'the dataset\'s'} segmentation${cmp ? ` after comparing it with ${segName(other)} cell by cell${citeText(cmp.method?.citation)}` : ''}: ${quoted(d.rationale)}`);
          }
        }
        break;
      }
      case 'comparison': {
        for (const x of g) {
          const xr = rec(x);
          const p = xr.parameters ?? {};
          const sm = xr.summary ?? {};
          const within = p.within === undefined || p.within === 0.5 ? 'half' : `${Math.round(100 * p.within)}%`;
          say(topic, `${upperFirst(segName(byId(xr.a)))} and ${segName(byId(xr.b))}${of(x)} were compared cell by cell${cite([x])}: cells corresponding when they share a nucleus or ${within} of one lies within the other, and the spurious co-expression and the mutually exclusive co-expression rate measuring transcripts given to the wrong cell; ${Number.isFinite(sm.pixelAgreement) ? `${(100 * sm.pixelAgreement).toFixed(1)}% of their pixels agreed` : 'their pixels were compared'}${Number.isFinite(sm.medianIoU) ? `, and the median intersection over union of corresponding cells was ${fixed(sm.medianIoU)}` : ''}.`);
        }
        break;
      }
      case 'filter': {
        for (const x of g) {
          const xr = rec(x);
          const methods = reports[xr.dataset]?.summary?.segmentationMethods ?? [];
          const described = { ...xr, conditions: (xr.conditions ?? []).map((c) => ({ ...c, text: c.text ?? describeCondition(c, methods) })) };
          const sentence = filterSentence(described);
          say(topic, several ? `For ${x.dataset}, ${lowerFirst(sentence)}` : sentence);
        }
        break;
      }
      case 'image': {
        for (const x of g) {
          const xr = rec(x);
          const pixel = xr.summary?.pixelSize ? `, ${xr.summary.pixelSize} µm a pixel` : '';
          say(topic, `${upperFirst(xr.name)}, a post-Xenium image${of(x)} (${describeImage(xr)}${pixel}), was read where it lies${cite([x])}.`);
        }
        break;
      }
      case 'transform': {
        for (const x of g) say(topic, transformSentence(rec(x), byId(rec(x).image), cite([x]), x.decisions));
        break;
      }
      case 'alignment-check': {
        for (const x of g) {
          const xr = rec(x);
          say(topic, `The alignment "${byId(xr.transform)?.name ?? xr.transform}" was checked over the tissue${cite([x])}: patches${xr.summary?.patchUm ? ` of ${Math.round(xr.summary.patchUm)} µm` : ''} of the Xenium image's nuclear stain and of the image, drawn as the alignment places it, matched by phase correlation; it was ${describeCheck(xr)}.`);
        }
        break;
      }
      case 'region': {
        const byDataset = new Map();
        for (const x of g) {
          const key = x.dataset ?? '';
          if (!byDataset.has(key)) byDataset.set(key, []);
          byDataset.get(key).push(x);
        }
        for (const [dataset, xs] of byDataset) {
          const versions = xs.map((x) => rec(x).versions?.at(-1) ?? {});
          const who = [...new Set(versions.map((v) => v.by).filter(Boolean))];
          say(topic, `${xs.length === 1 ? 'A region was' : `${count(xs.length)} regions were`} drawn on ${several && dataset ? dataset : 'the tissue'} in SituCraft${who.length ? ` by ${joinList(who)}` : ''}: ${joinList(versions.map((v) => `${v.name}${v.cls ? ` (${v.cls})` : ''}`))}.`);
        }
        break;
      }
      case 'clustering': {
        const p = s.parameters ?? {};
        const sm = g.map((x) => rec(x).summary ?? {});
        const gave = results(g, (x) => `${count(rec(x).summary?.clusters)} clusters of ${count(rec(x).summary?.cells)} cells`, `${range(sm.map((x) => x.clusters))} clusters of ${range(sm.map((x) => x.cells))} cells`);
        say(topic, `${cellsOf(g)} were clustered as scanpy's defaults do${cite(g)}: the cells with at least ${count(p.minTranscripts)} gene transcripts, each scaled to the median total and log-transformed, and each gene scaled; ${count(p.components)} principal components; a graph of each cell's ${count(p.neighbors)} nearest neighbors; and Leiden communities of modularity at a resolution of ${p.resolution} (${seedOf(g)}), each cluster's markers found by the rank-sum test. This gave ${gave}.`);
        break;
      }
      case 'cluster-names': {
        for (const x of g) {
          const xr = rec(x);
          const named = (xr.names ?? []).map((n, k) => (n ? `${clusterName(k)} "${n}"` : null)).filter(Boolean);
          say(topic, `The clusters of ${byId(xr.clustering)?.name ?? 'a clustering'} were named by ${xr.by ?? 'hand'}: ${joinList(named)}.`);
        }
        break;
      }
      case 'embedding': {
        const p = s.parameters ?? {};
        const three = (x) => fixed(x, 3);
        const two = (x) => fixed(x, 2);
        const sm = g.map((x) => rec(x).summary ?? {});
        const scores = results(g, (x) => `a trustworthiness of ${three(rec(x).summary?.trustworthiness)}, each cell's neighborhoods kept with a precision of ${two(rec(x).summary?.precision)} and a recall of ${two(rec(x).summary?.recall)}`, `a trustworthiness of ${range(sm.map((x) => x.trustworthiness), three)}`);
        say(topic, `${cellsOf(g)} were mapped by UMAP from a spectral start${cite(g)} (a minimum distance of ${p.minDist}, a spread of ${p.spread}, ${p.epochs ? `${count(p.epochs)} epochs` : 'umap-learn\'s number of epochs'}, ${p.negativeRate} negative samples a positive one; ${seedOf(g)}): ${scores}.`);
        break;
      }
      case 'marker-set': {
        for (const x of g) {
          const xr = rec(x);
          const levels = xr.set?.levels ?? [];
          say(topic, `Cell types were defined by the marker set "${xr.name}" (version ${xr.version}: ${plural((xr.set?.labels ?? []).length, 'label')}${levels.length > 1 ? ` at ${count(levels.length)} levels, ${joinList(levels.map((l) => l.toLowerCase()))}` : ''}${xr.source?.starter ? '; one of SituCraft\'s starter sets' : ''})${cite([x])}.`);
        }
        break;
      }
      case 'labels': {
        if (r.reference) {
          for (const x of g) say(topic, referenceLabelsSentence(rec(x), cellsOf([x]), cite([x])));
          break;
        }
        if (r.source) {
          for (const x of g) say(topic, importedLabelsSentence(rec(x), cellsOf([x]), cite([x])));
          break;
        }
        const p = s.parameters ?? {};
        const levelsOf = (x) => {
          const sm = rec(x).summary ?? {};
          return (sm.levels ?? []).map((l, i) => `${((100 * (sm.labeled?.[i] ?? 0)) / (sm.cells || 1)).toFixed(1)}% at the ${l.toLowerCase()} level`);
        };
        const sm = g.map((x) => rec(x).summary ?? {});
        const firstLevel = (sm[0].levels ?? ['first'])[0].toLowerCase();
        const labeled = results(g, (x) => joinList(levelsOf(x)), (unit) => `${range(sm.map((x) => (100 * (x.labeled?.[0] ?? 0)) / (x.cells || 1)), (v) => `${v.toFixed(1)}%`)} ${unit} at the ${firstLevel} level`);
        say(topic, `${cellsOf(g)} were labeled from the marker set's markers${cite(g)}: each label's markers scored against control genes of similar expression, as scanpy's score_genes does (the genes cut into ${p.bins} bins by mean expression, ${p.controls} controls drawn from the bin of each of a label's markers${p.lineage ? ', a label\'s lineage\'s markers left out of its controls' : ''}; ${seedOf(g)}), a cell taking a level's best label when it scored above ${p.minimum} and led the next by ${p.margin}${sm.some((x) => x.states) ? `, and a state when its markers scored ${p.state} or more` : ''}. The cells labeled: ${labeled}.`);
        break;
      }
      case 'curated-labels': {
        for (const x of g) {
          const xr = rec(x);
          const edits = xr.edits ?? [];
          const tally = (kind) => edits.filter((e) => e.kind === kind).reduce((a, e) => a + (kind === 'cluster' ? e.clusters?.length ?? 0 : e.cells?.length ?? 0), 0);
          const kinds = [[tally('accept'), 'cell\'s label accepted', 'cells\' labels accepted'], [tally('cells'), 'cell relabeled', 'cells relabeled'], [tally('cluster'), 'cluster relabeled', 'clusters relabeled']].filter(([n]) => n).map(([n, one, many]) => plural(n, one, many));
          say(topic, `The label set "${xr.base?.name ?? 'its base'}" was reviewed by ${xr.by ?? 'hand'}, its edits kept as the curated set "${xr.name}": ${plural(edits.length, 'edit')}${kinds.length ? ` (${joinList(kinds)})` : ''}, each with its reason.`);
        }
        break;
      }
      case 'consensus-labels': {
        for (const x of g) {
          const xr = rec(x);
          const rule = xr.rule?.kind === 'majority' ? `a majority of ${xr.rule.min}` : xr.rule?.kind === 'unanimous' ? 'unanimity' : 'the sets in order';
          say(topic, `The consensus label set "${xr.name}" was made of ${joinList((xr.members ?? []).map((m) => `"${m.name}"`))} by ${rule}.`);
        }
        break;
      }
      case 'graph': {
        const p = s.parameters ?? {};
        const words = {
          delaunay: `cells whose centroids are neighbors in the Delaunay triangulation (found with exact geometric predicates) were joined${p.longest > 0 ? `, edges longer than ${p.longest} µm left out` : ''}`,
          knn: `each cell was joined to its ${p.k} nearest cells (by centroid)`,
          radius: `cells within ${p.radius} µm of each other (by centroid) were joined`,
          contact: `cells whose outlines touch${p.gap > 0 ? ` or come within ${p.gap} µm` : ''} were joined`,
        }[p.kind] ?? 'cells were joined';
        const sm = g.map((x) => rec(x).summary ?? {});
        const choice = sm[0].choice ?? {};
        const over = choice.filter && g.every((x) => rec(x).summary?.choice?.filter?.id === choice.filter.id) ? `the cells "${choice.filter.name}" keeps` : '';
        const excluded = sm.map((x) => x.choice?.excluded ?? 0);
        const degree = (x) => `${count(rec(x).summary?.edges)} edges, a mean degree of ${fixed(rec(x).summary?.meanDegree, 1)}`;
        say(topic, `A graph of ${lowerFirst(cellsOf(g))} was built${over ? `, of ${over}` : ''}: ${words}${cite(g)}${excluded.some((n) => n > 0) ? `, the ${range(excluded)} cells a review excluded left out` : ''}; ${results(g, degree, `${range(sm.map((x) => x.edges))} edges`)}.`);
        break;
      }
      case 'spatial': {
        say(topic, spatialSentence(g.map(rec), g, cite(g), cellsOf(g), results, seedOf(g)));
        break;
      }
      case 'design': {
        const xr = rec(s);
        say(topic, `The samples (${plural((xr.samples ?? []).length, 'sample')}: ${joinList((xr.samples ?? []).map((x) => x.name))}) were compared by the design "${xr.name}" (version ${xr.version})${(xr.result?.words ?? []).length ? `, which reads: ${xr.result.words.join(' ')}` : '.'}`);
        break;
      }
      case 'cohort-summary': {
        const p = s.parameters ?? {};
        const labels = p.labels ?? {};
        const n = p.niches;
        say(topic, opening(s, `Each sample was summarized${cite([s])}: its cells chosen (its section's, or its region's; never those a review excluded), labeled by ${labels.name ? `"${labels.name}"` : 'the cohort\'s label set'}${(labels.names ?? []).length ? ` (${joinList(labels.names)})` : ''}; each label's cells counted, with their share of the labeled cells and their density per mm² of the sample's tissue (its outline closed by ${p.closing ?? '?'} µm); its quality; and its pseudobulk, each label's cells' gene transcripts summed gene by gene.${n ? ` Niches were found across the samples together: each cell's ${n.neighbors ? `${n.neighbors} nearest labeled cells` : `labeled cells within ${n.radius} µm`} described by their labels' shares, and the cells grouped by k-means into ${n.niches} niches (${n.starts} starts, ${n.seeds} seeds compared; seed ${n.seed}).` : ''}`));
        break;
      }
      case 'abundance': {
        const p = s.parameters ?? {};
        const families = (p.families ?? []).map((f) => ({
          share: 'a label\'s share of the labeled cells by a quasi-binomial model of the design (its effect an odds ratio)',
          density: 'its density by a quasi-Poisson model with the tissue\'s area as offset (a density ratio)',
          niche: 'a niche\'s share of the cells by a quasi-binomial model',
        }[f] ?? f));
        say(topic, opening(s, `The labels' abundance was compared between the groups${cite([s])}, ${unitWords(project, s)}: ${joinList(families)}; each contrast's effect with its ${level(p.level)} interval, and p-values adjusted by Benjamini and Hochberg's procedure over each family's tests. ${foundWords(r.summaryOf, (f) => `the ${f.family} of ${f.label} (${f.contrast}; ${f.family === 'density' ? 'density' : 'odds'} ratio ${fixed(f.ratio)})`)}`));
        break;
      }
      case 'expression': {
        const p = s.parameters ?? {};
        say(topic, opening(s, `Each label's pseudobulk was compared between the groups${cite([s])}, ${unitWords(project, s)}: the genes every panel has; each label's rows with at least ${p.minCells ?? '?'} of its cells; genes filtered by expression; TMM's library sizes and voom's weights; each gene's weighted linear model of the design, its variance moderated; each contrast's log2 fold change with its ${level(p.level)} interval and moderated t; and p-values adjusted by Benjamini and Hochberg's procedure over every label, gene and contrast tested. ${foundWords(r.summaryOf, (f) => `${f.gene} (${f.label}; ${f.contrast}; log2 fold change ${f.logFC >= 0 ? '+' : ''}${fixed(f.logFC)}, adjusted p ${Number(f.padj).toPrecision(2)})`)}`));
        break;
      }
      case 'spatial-comparison': {
        const p = s.parameters ?? {};
        const families = (p.families ?? []).map((f) => ({ enrichment: 'neighbor enrichment', nearest: 'median nearest distances', moran: 'Moran\'s I', niche: 'niches\' shares' }[f] ?? f));
        say(topic, opening(s, `Each sample's spatial statistics (${joinList(families)}) were compared between the groups${cite([s])}, ${unitWords(project, s)}: a unit's value the mean of its samples', ratios on the log2 scale, a family's statistics fitted together in a linear model of the design with their variances moderated${(p.families ?? []).includes('niche') ? ' (a niche\'s share by abundance\'s quasi-binomial model)' : ''}, each contrast's estimate with its ${level(p.level)} interval and moderated t, and p-values adjusted by Benjamini and Hochberg's procedure over each family's tests. ${foundWords(r.summaryOf, (f) => `${f.family} of ${String(f.key ?? f.label ?? '').replace('|', ' and ')} (${f.contrast}${Number.isFinite(f.ratio) ? `; ratio ${fixed(f.ratio)}` : ''})`)}`));
        break;
      }
      case 'figure': {
        const figs = g.map(rec);
        say(topic, `${figs.length === 1 ? `The figure "${figs[0].name}" was` : `The figures ${joinList(figs.map((f) => `"${f.name}"`))} were`} drawn by SituCraft${cite(g)} from the project's saved views and results (${plural(figs.reduce((a, f) => a + (f.panels ?? []).length, 0), 'panel')}), each file written carrying a manifest of what each panel was drawn from.`);
        break;
      }
      default:
    }
  }

  // Reviews, for a dataset or the project.
  if (scope !== 'result') {
    const words = { accept: 'accepted', relabel: 'relabeled', ambiguous: 'ambiguous', exclude: 'excluded from analyses', include: 'included again', concern: 'segmentation concerns', resolve: 'concerns resolved', comment: 'comments' };
    for (const d of datasets) {
      const reviews = reviewsOf(project, d.id).filter((x) => !x.derived);
      if (!reviews.length) continue;
      const by = [...new Set(reviews.map((x) => x.by).filter(Boolean))];
      const tally = new Map();
      for (const x of reviews) tally.set(x.action, (tally.get(x.action) ?? 0) + 1);
      say('quality', `${several ? `${d.name}'s cells` : 'The cells'} and regions were reviewed by ${joinList(by.length ? by : ['hand'])}: ${plural(reviews.length, 'review')} (${joinList([...tally].map(([a, n]) => `${count(n)} ${words[a] ?? a}`))}), each kept with the label set, segmentation and alignment in use and its reason.`);
    }
  }

  // The software, each with its versions.
  const versions = new Map();
  for (const x of record.software) {
    const name = x.name === 'situcraft' ? 'SituCraft' : x.name;
    if (!versions.has(name)) versions.set(name, []);
    if (x.version && !versions.get(name).includes(x.version)) versions.get(name).push(x.version);
  }
  if (versions.size) {
    const ran = [...versions].map(([name, v]) => `${name}${v.length ? ` (${joinList(v)})` : ''}${name === 'SituCraft' ? ` ${legacy('situcraft')}` : ''}`);
    say('software', `The steps were run with ${joinList(ran)}; this text was written from the project's record of each step${program.version ? ` by SituCraft ${program.version}` : ''}.`);
  }

  const paragraphs = TOPICS.map((t) => ({ topic: t.id, heading: t.heading, text: out.get(t.id).join(' ') })).filter((p) => p.text);
  return {
    title: `Methods: ${record.result.name}`,
    subject: record.result,
    paragraphs,
    text: paragraphs.map((p) => p.text).join('\n\n'),
    references: list.map((r, i) => ({ n: i + 1, ...r })),
    program,
  };
}

// The record's steps as the text says them: the steps of a kind said once for several items
// (SHARED) made by the same method with the same parameters, none made from another, said where the
// first of them is; the datasets' when their platform and software are the same; every other step
// on its own.
function textGroups(record, rec) {
  const key = (s) => {
    const r = rec(s);
    if (s.kind === 'dataset') return canonical([s.kind, r.validation?.support === 'simulated', r.panel, r.software, r.version]);
    if (s.kind === 'region') return canonical([s.kind, r.dataset]);
    if (!SHARED.has(s.kind)) return null;
    return canonical([s.kind, s.method, s.parameters && { ...s.parameters, seed: undefined }, Boolean(r.reference), Boolean(r.source), r.question ?? null]);
  };
  const groups = [];
  const open = new Map();
  for (const s of record.steps) {
    const k = key(s);
    const g = k ? open.get(k) : null;
    if (g && !s.inputs.some((i) => g.some((x) => x.n === i))) g.push(s);
    else {
      groups.push([s]);
      if (k) open.set(k, groups.at(-1));
    }
  }
  return groups;
}

// --- Sentences ----------------------------------------------------------------------------------

// The unit a comparison of samples replicates by, from its design.
function unitWords(project, s) {
  const d = project.artifacts.find((a) => a.kind === 'design' && a.name === s.parameters?.design?.name && a.version === s.parameters?.design?.version);
  return `with the ${d?.spec?.unit ?? 'sample'} as the unit`;
}

// What a comparison found: its tests, and those whose adjusted p is below 0.05 (up to three named).
function foundWords(summaryOf, name) {
  if (!summaryOf) return '';
  const found = summaryOf.found ?? [];
  const tests = Number.isFinite(summaryOf.tests) ? `Of ${plural(summaryOf.tests, 'test')}, ` : '';
  if (!found.length) return `${tests}${tests ? 'none' : 'None'} had an adjusted p below 0.05.`;
  const shown = found.slice(0, 3).map(name);
  return `${tests}${count(found.length)} had an adjusted p below 0.05: ${joinList(shown)}${found.length > 3 ? `, and ${count(found.length - 3)} more` : ''}.`;
}

// How an image was placed: by Xenium Explorer's matrix or keypoints, by landmarks, or
// automatically; and its choice.
function transformSentence(r, image, cite, decisions) {
  const s = r.summary ?? {};
  const name = upperFirst(image?.name ?? 'the image');
  const placement = describePlacement(s);
  const fit = Number.isFinite(s.rms) && s.method !== 'automatic' ? `residuals ${fixed(s.rms)} µm root-mean-square${Number.isFinite(s.looRms) && s.looRms > 0 ? `, ${fixed(s.looRms)} µm each left out` : ''}` : '';
  const parts = [fit, placement].filter(Boolean).join('; ');
  let sentence;
  if (s.method === 'matrix') sentence = `${name} was placed by Xenium Explorer's transformation matrix supplied with it${cite}${parts ? ` (${parts})` : ''}.`;
  else if (s.method === 'keypoints') sentence = `${name} was placed by Xenium Explorer's ${count(s.landmarks)} keypoints, ${article(s.model ?? 'similarity')} fitted to them by least squares${cite}${parts ? ` (${parts})` : ''}.`;
  else if (s.method === 'landmarks') sentence = `${name} was placed by ${count(s.landmarks)} landmarks${s.by ? ` placed by ${s.by}` : ''}, ${article(s.model ?? 'similarity')} fitted to them by least squares${cite}${parts ? ` (${parts})` : ''}.`;
  else if (s.method === 'automatic') {
    const how = String(r.method?.name ?? '').replace(/^[^:]*:\s*/, '').replace(/\ba (affine)\b/, 'an $1');
    const deformed = s.deformed ? `, deformed by at most ${fixed(s.maxDisplacement, 1)} µm (each fifth of the matches predicted ${fixed(s.heldOut)} µm off, against ${fixed(s.modelHeldOut)} µm by the ${s.model} alone)` : '';
    sentence = `${name} was aligned automatically by the nuclei${s.channel ? ` (its ${s.channel.split(',')[0]})` : ''}: ${how}${cite}; the ${count(s.kept)} local matches kept of ${count(s.matches)} lay ${fixed(s.rms)} µm from the fit (root-mean-square)${deformed}.`;
  } else sentence = `${name} was placed by ${r.name}${cite}.`;
  const chosen = decisions.filter((d) => d.kind === 'use-transform').at(-1);
  return chosen && madeBy(chosen) ? `${sentence} This alignment was chosen to place it, by ${chosen.by}: ${quoted(chosen.rationale)}` : sentence;
}

function referenceLabelsSentence(r, cells, cite) {
  const ref = r.reference ?? {};
  const p = r.parameters ?? {};
  const sm = r.summary ?? {};
  const labeled = sm.labeled?.[0] ?? (sm.cells ?? 0) - (sm.unlabeled ?? 0);
  return `${cells} were labeled from the reference ${ref.file}${ref.labelColumn ? ` (its ${ref.labelColumn} labels${ref.sha256 ? `; SHA-256 ${ref.sha256.slice(0, 16)}…` : ''})` : ''}${cite}: each cell's expression correlated with each label's profile over the genes telling each pair of labels apart${p.markers ? ` (${p.markers} a pair)` : ''}, and fine-tuned among the labels within ${p.tune} of its best${p.margin > 0 ? `, a cell labeled when its best led the next by ${p.margin}` : ''}; ${Number.isFinite(sm.reference?.shared) ? `${count(sm.reference.shared)} of the panel's genes were shared, and ` : ''}${count(labeled)} of ${count(sm.cells)} cells labeled.`;
}

function importedLabelsSentence(r, cells, cite) {
  const src = r.source ?? {};
  const sm = r.summary ?? {};
  const labeled = sm.labeled?.[0] ?? (sm.cells ?? 0) - (sm.unlabeled ?? 0);
  if (src.format === 'instrument') return `${cells} were labeled by the instrument's ${String(src.title ?? 'graph-based').toLowerCase()} clusters, as Xenium Onboard Analysis recorded them${cite}: ${count(labeled)} of ${count(sm.cells)} cells.`;
  return `${cells} were labeled from the column ${src.column} of ${src.file}${src.sha256 ? ` (SHA-256 ${src.sha256.slice(0, 16)}…${src.rows ? `, ${count(src.rows)} rows` : ''})` : ''}, matched to the cells by ${src.idColumn}${cite}: ${count(labeled)} of ${count(sm.cells)} cells labeled.`;
}

// A spatial analysis (or the same one of several sections or segmentations) in a sentence: what
// it asked, of which labels over which graph, how it was tested, and what it found.
function spatialSentence(records, g, cite, cells, results, seed) {
  const r = records[0];
  const p = r.parameters ?? {};
  const sm = r.summary ?? {};
  const labels = labelsName(sm.labels?.name);
  const of = g.length === 1 && cells === 'The cells' ? '' : ` (${lowerFirst(cells)})`;
  const seeded = seed ? ` (${seed})` : '';
  const within = sm.strata?.length ? ', the labels dealt within regions' : '';
  // Each item's finding, or (for many sections) their range.
  const found = (fn, ranged = null) => results(g, (x) => fn(records[g.indexOf(x)].summary ?? {}), ranged && ranged(records.map((x) => x.summary ?? {})));
  const sig = (xs) => range(xs.map((x) => x.significant));
  switch (r.question ?? sm.question) {
    case 'enrichment': {
      const k = sm.labels?.count;
      return `The neighborhood enrichment of ${labels}${of} was measured over the cells' graph${cite}: for each pair of labels, the edges between their cells against those expected, ${p.weight === 'boundary' ? 'each weighted by the boundary shared' : 'each edge counting one'}, with a z-score and a p-value from ${count(p.permutations)} permutations of the labels${within}${seeded}, the p-values adjusted by Benjamini and Hochberg's procedure: ${found((x) => `${count(x.significant)}${k ? ` of ${count((k * (k + 1)) / 2)}` : ''} pairs`, (xs) => `${sig(xs)}${k ? ` of ${count((k * (k + 1)) / 2)}` : ''} pairs`)} differed from chance (adjusted p below 0.05).`;
    }
    case 'cooccurrence': return `The co-occurrence of ${labels}${of} was measured${cite}: how much likelier a label's cells are within each of ${count((p.distances ?? []).length)} distances (${range(p.distances ?? [])} µm) of another label's cells than of any cell.`;
    case 'distances': return `The nearest distances between the cells of ${labels}${of} were measured${cite}: each label's cells' distance to the nearest cell of each label, to ${p.reach} µm, against ${count(p.permutations)} permutations of the labels${seeded}, the p-values adjusted by Benjamini and Hochberg's procedure: ${found((x) => `${count(x.significant)} pairs`, (xs) => `${sig(xs)} pairs`)} differed from chance.`;
    case 'ripley': return `Ripley's K and L of ${labels}${of} were measured${cite}, with Ripley's isotropic edge correction, at ${count((p.radii ?? []).length)} radii to ${Math.max(0, ...(p.radii ?? []))} µm, against envelopes of ${count(p.permutations)} permutations of the labels${seeded}.`;
    case 'regions': return `The cells of ${labels}${of} were compared between regions${cite}: each label's density per mm² of a region's tissue and its share of the region's labeled cells, each with its ${level(p.level)} interval, the p-values adjusted by Benjamini and Hochberg's procedure: ${found((x) => `${count(x.significant)} differences`, (xs) => `${sig(xs)} differences`)} found.`;
    case 'boundary': return `The density of each of ${labels}${of} was measured across ${sm.edge?.name ? `the edge of ${sm.edge.name}` : 'a region\'s edge'}${cite}: in bands of ${p.step} µm from ${p.reach} µm inside to ${p.reach} µm outside, each band's own tissue its denominator, the margin within ${p.margin} µm of the edge, each with its ${level(p.level)} interval: ${found((x) => `${count(x.significant)} labels`, (xs) => `${sig(xs)} labels`)} differed between inside, margin and around.`;
    case 'niches': return `Cellular neighborhoods${of} were found from ${labels}${cite}: each cell's ${p.neighbors ? `${p.neighbors} nearest labeled cells` : `labeled cells within ${p.radius} µm`} described by their labels' shares, and the cells grouped by k-means from k-means++ starts into ${p.niches} niches (${p.starts} starts${seed ? `; ${seed}` : ''}); ${found((x) => `the niches of ${p.seeds} further seeds agreed with them by an adjusted Rand index of ${fixed(x.agreementMean)} on average (${fixed(x.agreementMin)} at least)`)}.`;
    case 'moran': return `Spatially variable genes${of} were found by Moran's I of each gene over the cells' graph${cite}, with ${p.weights === 'binary' ? 'binary' : 'row-standardized'} weights, each tested by ${count(p.permutations)} permutations${seeded}, the ${p.local} leading genes' local Moran's I by ${count(p.localPermutations)} permutations, the p-values adjusted by Benjamini and Hochberg's procedure: ${found((x) => `${count(x.significant)} of ${count(x.genes)} genes`, (xs) => `${sig(xs)} of ${range(xs.map((x) => x.genes))} genes`)} were in a spatial pattern.`;
    default: return `${r.name}${cite}.`;
  }
}

// --- Written ------------------------------------------------------------------------------------

// The text as Markdown: its title, a heading a paragraph, the references.
export function methodsMarkdown(m) {
  const lines = [`# ${m.title}`, ''];
  for (const p of m.paragraphs) lines.push(`## ${p.heading}`, '', p.text, '');
  if (m.references.length) {
    lines.push('## References', '');
    for (const r of m.references) lines.push(`${r.n}. ${r.text}${r.doi && !r.text.includes(r.doi) ? ` doi:${r.doi}` : ''}`);
    lines.push('');
  }
  return lines.join('\n');
}

// The text as plain text, to paste: the paragraphs, then the references as [n] text.
export function methodsPlain(m) {
  return `${m.paragraphs.map((p) => p.text).join('\n\n')}\n\n${m.references.map((r) => `[${r.n}] ${r.text}${r.doi && !r.text.includes(r.doi) ? ` doi:${r.doi}` : ''}`).join('\n')}\n`;
}

// The text's references as BibTeX.
export function methodsBibTeX(m) {
  return referencesBibTeX(m.references, m.program?.version ?? '');
}
