// The comparison workspace's logic: a comparison's differences (api_workspace.go, column by
// column) made ready, ranked for review, filtered by the reviewer's rules; and the warnings that
// say how far its measures can be trusted.

// The classes of differences (internal/compare), each with what it means, whether its cells are
// the same cell in both, and how much it disagrees (for ranking).
export const CLASS_INFO = {
  'one-to-one': { label: 'One-to-one', about: 'The same cell in both, its area within 25%', same: true, weight: 0 },
  expanded: { label: 'Expanded', about: 'The same cell, more than 25% larger in B', same: true, weight: 0.5 },
  contracted: { label: 'Contracted', about: 'The same cell, more than 25% smaller in B', same: true, weight: 0.5 },
  split: { label: 'Split', about: 'One cell of A is several of B', same: false, weight: 1 },
  merged: { label: 'Merged', about: 'Several cells of A are one of B', same: false, weight: 1 },
  rearranged: { label: 'Rearranged', about: 'Several cells of each overlap one another', same: false, weight: 1 },
  'only in A': { label: 'Only in A', about: 'A cell of A with no counterpart in B', same: false, weight: 1 },
  'only in B': { label: 'Only in B', about: 'A cell of B with no counterpart in A', same: false, weight: 1 },
  'assignment-only': { label: 'Assignment-only', about: 'Matched by their transcripts; one has no outline', same: false, weight: 0.75 },
};

// The classes shown at first: every one that differs.
export const DIFFERING = Object.keys(CLASS_INFO).filter((c) => c !== 'one-to-one');

// The orders a review queue can take.
export const RANKS = {
  reassigned: { label: 'Reassigned transcripts', about: 'Most gene transcripts whose cell changed first' },
  contamination: { label: 'Contamination', about: 'Most more pairs of mutually exclusive markers in one cell of B than of A first' },
  disagreement: { label: 'Disagreement', about: 'Splits, merges and cells in one only first, then by how little the cells overlap' },
  'low-counts': { label: 'Low counts', about: 'Fewest gene transcripts first (cells too sparse to type)' },
};

// prepare turns the program's columns into differences, with each cell's difference in each
// segmentation.
export function prepare(cols) {
  const n = cols.class.length;
  const groups = new Array(n);
  const ofA = new Int32Array(cols.idsA.length).fill(-1);
  const ofB = new Int32Array(cols.idsB.length).fill(-1);
  for (let i = 0; i < n; i += 1) {
    const g = {
      index: i,
      class: cols.classes[cols.class[i]],
      a: cols.a[i] ?? [],
      b: cols.b[i] ?? [],
      iou: cols.iou[i],
      jaccard: cols.jaccard[i],
      displacement: cols.displacement[i],
      changed: cols.changed[i],
      x: cols.x[i],
      y: cols.y[i],
      transcriptsA: cols.transcriptsA[i],
      transcriptsB: cols.transcriptsB[i],
      mixedA: cols.mixedA[i],
      mixedB: cols.mixedB[i],
    };
    for (const c of g.a) ofA[c] = i;
    for (const c of g.b) ofB[c] = i;
    groups[i] = g;
  }
  return { groups, ofA, ofB, idsA: cols.idsA, idsB: cols.idsB };
}

// score is a difference's place in a ranking: higher first.
function score(g, rank) {
  switch (rank) {
    case 'contamination':
      return (g.mixedB - g.mixedA) * 1e6 + g.mixedB * 1e3 + g.changed / 1e3;
    case 'disagreement': {
      const info = CLASS_INFO[g.class] ?? { weight: 1 };
      const overlap = info.same && g.a.length === 1 && g.b.length === 1 ? 1 - g.iou : 1;
      return info.weight * 1e3 + overlap + g.changed / 1e9;
    }
    case 'low-counts': {
      const counts = [g.a.length ? g.transcriptsA : Infinity, g.b.length ? g.transcriptsB : Infinity];
      return -Math.min(...counts);
    }
    default:
      return g.changed;
  }
}

// rank orders the differences a reviewer's rules keep: classes (a set of class names), at least
// minChanged reassigned transcripts. Ties keep the comparison's order, so the queue is the same
// every time.
export function rank(groups, { by = 'reassigned', classes = new Set(DIFFERING), minChanged = 0 } = {}) {
  const kept = groups.filter((g) => classes.has(g.class) && g.changed >= minChanged);
  const scores = new Map(kept.map((g) => [g, score(g, by)]));
  return kept.sort((p, q) => scores.get(q) - scores.get(p) || p.index - q.index);
}

// What the cells of a difference are called: their ids in each segmentation.
export function cellNames(prepared, g, max = 3) {
  const names = (ids, cells) => {
    const shown = cells.slice(0, max).map((c) => ids[c]);
    return cells.length > max ? `${shown.join(', ')} and ${cells.length - max} more` : shown.join(', ');
  };
  return { a: names(prepared.idsA, g.a) || '—', b: names(prepared.idsB, g.b) || '—' };
}

// warnings say which of a comparison's measures rest on little (its description).
export function warnings(d) {
  const out = [];
  const co = d.coexpression ?? {};
  if (!co.pairs) out.push('No spurious gene pairs were found, so the spurious co-expression is not measured.');
  else {
    if (co.nuclearCells < 1000) out.push(`The spurious co-expression rests on ${co.nuclearCells.toLocaleString('en-US')} nuclei with 50 gene transcripts or more: it varies with them.`);
    if (co.pairs < 20) out.push(`The spurious co-expression rests on ${co.pairs} gene pairs only.`);
  }
  for (const [side, m] of [['A', d.metricsA], ['B', d.metricsB]]) {
    if (m && m.coexpressionCells < 500) out.push(`${side}'s spurious co-expression is measured on ${m.coexpressionCells.toLocaleString('en-US')} cells of 50 gene transcripts or more.`);
    if (m?.emptyCells > 0) out.push(`${m.emptyCells.toLocaleString('en-US')} cells of ${side} have no outline: their areas, overlaps and boundaries are not measured.`);
  }
  const pairs = d.mecrPairs?.length ?? 0;
  if (pairs < 10) out.push(`The mutually exclusive co-expression uses ${pairs} pair${pairs === 1 ? '' : 's'} of lineage markers: the panel has few of them.`);
  if ((d.iou?.pairs ?? 0) < 50) out.push(`Only ${d.iou?.pairs ?? 0} cells are the same in both: their overlap and boundary measures vary.`);
  return out;
}
