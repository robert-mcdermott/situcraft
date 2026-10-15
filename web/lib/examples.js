// The example datasets as the page knows them (api_examples.go, validation/sources.json): which
// example a dataset is, by its experiment.xenium's SHA-256 wherever its bundle is, and the
// attribution its methods paragraph cites.

export function exampleFor(dataset, examples) {
  const manifest = (dataset?.fingerprint ?? []).find((f) => f.path === 'experiment.xenium' || f.path.endsWith('/experiment.xenium'))?.sha256;
  if (!manifest) return null;
  return (examples ?? []).find((e) => e.manifestSha256 === manifest) ?? null;
}

// The source of an example's data, as methodsParagraph (lib/methods.js) cites it.
export function exampleSource(example) {
  // A generated example is SituCraft's own simulation, which the methods paragraph says it is.
  if (!example || example.generated) return null;
  return { title: example.title, attribution: example.publisher, license: example.license.replace(/\s*\(.*\)$/, ''), url: example.page };
}
