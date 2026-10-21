// Tests the R and Python clients against a running SituCraft (wave 9, slice 7): builds SituCraft
// from source, starts it with remote control (its connection file in a temporary data folder, its
// examples made there), opens its page in headless Chrome, then runs each client's tests: the
// simulated tissue opened, clustered and asked about through the client, its tables as data frames
// with their columns' types, the tissue and the map written as files, an error carrying
// SituCraft's reason, results equal to the HTTP API's for the same actions, and what an action
// changed. Then the examples, each in a folder of its own: the Python notebook
// clients/examples/simulated-tissue.ipynb executed (nbclient), and the R script
// clients/examples/simulated-cohort.R. Each fails when one of its checks does. Adapted from
// MaveScape 0.8.0's clients/test-clients.mjs (the same author's).
//
//   node clients/test-clients.mjs [--python] [--r] [--require] [--update]
//
// --python, --r: that client alone (both when neither is named). --require: fail, rather than skip
// an example, when what it needs is missing (as CI runs it). --update: write the executed notebook,
// with its outputs, over clients/examples/simulated-tissue.ipynb.
//
// Needs Go, Chrome (CHROME=path), Python 3 for the Python client (PYTHON=path; pandas for its data
// frames, else those tests are skipped; nbclient, ipykernel and matplotlib too for the notebook)
// and R with curl, jsonlite and testthat for the R client. Exits with status 1 when a test or an
// example fails.

import { spawn } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { launch, sleep } from '../validation/cdp.mjs';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const PORT = 8853;
const only = ['--python', '--r'].filter((flag) => process.argv.includes(flag));
const run = (name) => !only.length || only.includes(`--${name}`);
const strict = process.argv.includes('--require');
const update = process.argv.includes('--update');
const NOTEBOOK = join(ROOT, 'clients/examples/simulated-tissue.ipynb');
const temp = mkdtempSync(join(tmpdir(), 'situcraft-clients-'));
const library = join(temp, 'library');
const out = join(temp, 'out');
mkdirSync(out);

// Executes a notebook with nbclient in a folder, then writes it with its outputs (each cell's
// timings and the kernel's details left out, so that it changes only when its outputs do). Exits
// with status 3 when nbclient, ipykernel, pandas or matplotlib is missing.
const EXECUTE = `
import sys
try:
    import ipykernel, matplotlib, nbformat, pandas
    from nbclient import NotebookClient
except ImportError as error:
    print(f"The notebook needs nbclient, ipykernel, pandas and matplotlib: {error}.")
    sys.exit(3)
source, folder, executed = sys.argv[1:4]
nb = nbformat.read(source, as_version=4)
NotebookClient(nb, timeout=900, kernel_name="python3", resources={"metadata": {"path": folder}}).execute()
for cell in nb.cells:
    cell.metadata.pop("execution", None)
nb.metadata["language_info"] = {"name": "python"}
nbformat.write(nb, executed)
`;
// An example that cannot run here: skipped, or a failure with --require.
function cannot(what, why) {
  console.log(`${strict ? 'FAIL' : 'Skipped'}: ${what}: ${why}`);
  return strict ? 1 : 0;
}

const exec = (command, args, options = {}) => new Promise((done) => {
  const child = spawn(command, args, { cwd: ROOT, stdio: 'inherit', ...options });
  child.on('exit', (code) => done(code ?? 1));
  child.on('error', (error) => { console.error(`${command}: ${error.message}`); done(1); });
});

async function startSituCraft() {
  const binary = join(temp, process.platform === 'win32' ? 'situcraft.exe' : 'situcraft');
  if (await exec('go', ['build', '-o', binary, '.'])) throw new Error('go build failed (is Go installed?)');
  const server = spawn(binary, ['--remote-control', '--window', 'none', '--port', String(PORT), '--data-dir', library, '--cache-dir', join(temp, 'cache'), '--examples-dir', join(temp, 'examples')], { cwd: temp, stdio: ['ignore', 'pipe', 'inherit'] });
  let output = '';
  server.stdout.on('data', (chunk) => { output += chunk; });
  for (let i = 0; i < 300; i += 1) {
    const url = /is running at (http:\/\/[\d.]+:\d+)/.exec(output)?.[1];
    const token = /X-SituCraft-Token: (\S+)/.exec(output)?.[1];
    if (url && token && existsSync(join(library, 'remote.json'))) {
      try {
        if ((await fetch(`${url}/api/info`)).ok) return { url, token, stop: () => server.kill() };
      } catch { /* starting */ }
    }
    await sleep(200);
  }
  server.kill();
  throw new Error(`SituCraft did not start:\n${output}`);
}

let failed = 0;
const server = await startSituCraft();
let browser;
try {
  browser = await launch({ width: 1400, height: 900, scale: 1, args: (process.env.SITUCRAFT_CHROME_ARGS ?? '').split(' ').filter(Boolean) });
  await browser.goto(`${server.url}/`);
  for (let i = 0; i < 100 && !(await browser.eval('Boolean(window.situcraft?.remote)')); i += 1) await sleep(200);
  if (!(await browser.eval('Boolean(window.situcraft?.remote)'))) throw new Error('The page did not start remote control.');
  const env = { ...process.env, SITUCRAFT_TEST_URL: server.url, SITUCRAFT_TEST_TOKEN: server.token, SITUCRAFT_TEST_DIR: out, SITUCRAFT_DATA_DIR: library };
  for (const key of ['SITUCRAFT_URL', 'SITUCRAFT_TOKEN']) delete env[key];
  if (run('python')) {
    console.log('\n--- Python client ---');
    const python = process.env.PYTHON ?? (process.platform === 'win32' ? 'python' : 'python3');
    const pythonEnv = { ...env, PYTHONPATH: join(ROOT, 'clients/python/src') };
    const code = await exec(python, ['-m', 'unittest', 'discover', '-s', 'clients/python/tests', '-v'], { env: pythonEnv });
    if (code) failed += 1;
    console.log('\n--- The Python notebook (clients/examples/simulated-tissue.ipynb) ---');
    const folder = join(out, 'notebook');
    mkdirSync(folder);
    const executed = join(folder, 'simulated-tissue.ipynb');
    let notebook = await exec(python, ['-c', EXECUTE, NOTEBOOK, folder, executed], { env: pythonEnv });
    if (notebook === 3) notebook = cannot('the notebook', 'install nbclient, ipykernel, pandas and matplotlib');
    else if (!notebook) {
      console.log('The notebook ran to its end, every check passing.');
      if (update) {
        copyFileSync(executed, NOTEBOOK);
        console.log('Wrote it, with its outputs, to clients/examples/simulated-tissue.ipynb.');
      }
    }
    if (notebook) failed += 1;
  }
  if (run('r')) {
    console.log('\n--- R client ---');
    const rlib = join(temp, 'rlib');
    mkdirSync(rlib);
    let code = await exec('R', ['CMD', 'INSTALL', '--no-test-load', `--library=${rlib}`, 'clients/r'], { env });
    if (!code) {
      const script = `.libPaths(c(${JSON.stringify(rlib)}, .libPaths())); library(testthat); results <- as.data.frame(test_dir("clients/r/tests/testthat", package = "situcraft", load_package = "installed", reporter = "summary", stop_on_failure = FALSE)); if (sum(results$failed) + sum(results$error) > 0) quit(status = 1)`;
      code = await exec('Rscript', ['-e', script], { env });
    }
    if (code) failed += 1;
    console.log('\n--- The R script (clients/examples/simulated-cohort.R) ---');
    let example;
    if (!existsSync(join(rlib, 'situcraft'))) example = cannot('the R script', 'the R client did not install');
    else {
      const folder = join(out, 'r-example');
      mkdirSync(folder);
      example = await exec('Rscript', [join(ROOT, 'clients/examples/simulated-cohort.R')], { cwd: folder, env: { ...env, R_LIBS: rlib } });
      if (!example) console.log('The R script ran to its end, every check passing.');
    }
    if (example) failed += 1;
  }
} catch (error) {
  console.error(error);
  failed += 1;
} finally {
  await browser?.close?.();
  server.stop();
  rmSync(temp, { recursive: true, force: true });
}
console.log(failed ? `\n${failed} of the clients' test suites and examples failed.` : '\nThe clients\' tests and examples passed.');
process.exit(failed ? 1 : 0);
