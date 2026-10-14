import assert from 'node:assert/strict';
import test from 'node:test';
import { PROJECT_FORMAT, PROJECT_VERSION, createProject, isEmptyProject, parseProject, rename, serializeProject } from './project.js';

test('a new project is an empty, versioned document with a valid id', () => {
  const project = createProject('Lung 2-FOV', { now: '2026-10-14T12:00:00.000Z' });
  assert.equal(project.format, PROJECT_FORMAT);
  assert.equal(project.version, PROJECT_VERSION);
  assert.match(project.id, /^[A-Za-z0-9_-]{1,80}$/);
  assert.equal(project.created, '2026-10-14T12:00:00.000Z');
  assert.ok(isEmptyProject(project));
  assert.notEqual(createProject().id, createProject().id);
});

test('a project survives saving and reopening byte for byte', () => {
  const project = createProject('Round trip', { now: '2026-10-14T12:00:00.000Z' });
  const text = serializeProject(project);
  const back = parseProject(text);
  assert.deepEqual(back, project);
  assert.equal(serializeProject(back), text);
});

test('fields a later version of the envelope added are filled in when an older document opens', () => {
  const old = JSON.stringify({ format: PROJECT_FORMAT, version: 1, id: 'p-old', name: 'Old', created: '2026-10-01T00:00:00Z' });
  const project = parseProject(old);
  assert.deepEqual(project.datasets, []);
  assert.deepEqual(project.artifacts, []);
  assert.deepEqual(project.decisions, []);
  assert.deepEqual(project.views, []);
  assert.equal(project.name, 'Old');
});

test('other documents are refused with a plain message', () => {
  assert.throws(() => parseProject('{not json'), /not valid JSON/);
  assert.throws(() => parseProject(JSON.stringify({ format: 'mavescape-workspace', version: 1, id: 'x' })), /not a SituCraft project/);
  assert.throws(() => parseProject(JSON.stringify({ format: PROJECT_FORMAT, version: 99, id: 'x' })), /newer SituCraft/);
  assert.throws(() => parseProject(JSON.stringify({ format: PROJECT_FORMAT, version: 1, id: '../etc' })), /no valid id/);
  assert.throws(() => parseProject(JSON.stringify({ format: PROJECT_FORMAT, version: 1, id: 'p-1', datasets: {} })), /datasets is not a list/);
});

test('rename returns a new value and leaves the original alone', () => {
  const project = createProject('A');
  const renamed = rename(project, '  B  ', '2026-10-14T13:00:00.000Z');
  assert.equal(renamed.name, 'B');
  assert.equal(renamed.modified, '2026-10-14T13:00:00.000Z');
  assert.equal(project.name, 'A');
  assert.equal(rename(project, '   '), project);
  assert.equal(rename(project, 'A'), project);
});

test('a project with a dataset, decision or saved view is worth saving', () => {
  const empty = createProject();
  assert.ok(isEmptyProject(empty));
  for (const field of ['datasets', 'artifacts', 'decisions', 'views']) {
    assert.equal(isEmptyProject({ ...empty, [field]: [{ id: 'x' }] }), false, field);
  }
});

// Projects saved by earlier versions (validation/projects) open with this one, keeping every field,
// and what they refer to is there: each view's dataset and filter, each filter's dataset and
// previous version. (The viewer suite reopens them against the data.)
test('projects saved by earlier versions open', async () => {
  const { readdirSync, readFileSync } = await import('node:fs');
  const { join, dirname } = await import('node:path');
  const { fileURLToPath } = await import('node:url');
  const dir = join(dirname(fileURLToPath(import.meta.url)), '..', '..', 'validation', 'projects');
  const files = readdirSync(dir).filter((f) => f.endsWith('.situcraft.json'));
  assert.ok(files.length >= 1);
  for (const file of files) {
    const text = readFileSync(join(dir, file), 'utf8');
    const project = parseProject(text);
    assert.deepEqual(JSON.parse(serializeProject(project)), JSON.parse(text), `${file} keeps every field`);
    const datasets = new Set(project.datasets.map((d) => d.id));
    const filters = new Map(project.artifacts.filter((a) => a.kind === 'filter').map((f) => [f.id, f]));
    for (const f of filters.values()) {
      assert.ok(datasets.has(f.dataset), `${file}: filter ${f.name}'s dataset`);
      if (f.parent) assert.equal(filters.get(f.parent)?.version, f.version - 1, `${file}: filter ${f.name}'s previous version`);
      assert.match(f.result.sha256, /^[0-9a-f]{64}$/);
    }
    for (const v of project.views) {
      assert.ok(datasets.has(v.dataset), `${file}: view ${v.name}'s dataset`);
      assert.ok(v.camera.umPerCss > 0, `${file}: view ${v.name}'s zoom`);
      if (v.qc?.filter) assert.ok(filters.has(v.qc.filter), `${file}: view ${v.name}'s filter`);
    }
  }
});
