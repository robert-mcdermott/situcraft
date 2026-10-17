// Downloads the external data of the validation suites into validation/cache/ (or $SITUCRAFT_DATA)
// and checks every file against the size and SHA-256 recorded in validation/sources.json. Data
// sets already present and intact are not downloaded again.
//
//   node validation/fetch.mjs [data set …] [--list] [--record]
//
// A data set published as an archive (a Xenium output bundle's zip) is checked by the publisher's
// MD5 and its size, extracted (each member's CRC-32 checked), each extracted file checked against
// sources.json, and the archive deleted, so a bundle costs only its extracted size on disk.
// --record writes the extracted files' sizes and SHA-256 into sources.json (the maintainer, once,
// when a data set is added). The data are not part of the repository or of the SituCraft program;
// sources.json records where each comes from and its license. Adapted from MaveScape 0.8.0
// validation/fetch.mjs.
//
// A fixture (sources.json's fixtures: data the validation suites use that are not examples) may
// take only some members of a large archive, read from it by HTTP range (its central directory,
// then each member, CRC-32 checked), beside files downloaded whole: the pathology fixture is 2.8 GB
// of a 9.9 GB bundle and its images.

import { createHash } from 'node:crypto';
import { createReadStream, createWriteStream } from 'node:fs';
import { mkdir, open, readFile, rename, rm, stat, writeFile } from 'node:fs/promises';
import { dirname, join, normalize, sep } from 'node:path';
import { Readable, Transform } from 'node:stream';
import { pipeline } from 'node:stream/promises';
import { fileURLToPath } from 'node:url';
import { createInflateRaw, crc32 } from 'node:zlib';

const here = dirname(fileURLToPath(import.meta.url));
const manifestPath = `${here}/sources.json`;
const manifest = JSON.parse(await readFile(manifestPath, 'utf8'));
const args = process.argv.slice(2);
const names = args.filter((a) => !a.startsWith('--'));
const record = args.includes('--record');
const dataRoot = process.env.SITUCRAFT_DATA || `${here}/cache`;
// The data sets, and the tools the checks use (axe-core): each an archive, or files under a base URL.
const sets = { ...manifest.datasets, ...(manifest.fixtures ?? {}), ...(manifest.tools ?? {}) };
const USER_AGENT = 'SituCraft-validation (+https://github.com/robert-mcdermott/situcraft)';

if (args.includes('--list')) {
  for (const [name, set] of Object.entries(sets)) {
    const bytes = set.archive?.size ?? set.files.reduce((sum, f) => sum + (f.size ?? 0), 0);
    console.log(`${name}: ${set.title}, ${(bytes / 1e6).toFixed(1)} MB to download\n  ${set.source}\n  ${set.license}`);
  }
  process.exit(0);
}

for (const name of names) {
  if (!sets[name]) {
    console.error(`Unknown data set ${name}. Data sets: ${Object.keys(sets).join(', ')}`);
    process.exit(2);
  }
}

async function hashFile(path, algorithms = ['sha256']) {
  const hashes = algorithms.map((a) => createHash(a));
  for await (const chunk of createReadStream(path)) for (const hash of hashes) hash.update(chunk);
  return hashes.map((hash) => hash.digest('hex'));
}

async function intact(root, files) {
  if (!files.length) return false;
  for (const file of files) {
    try {
      const info = await stat(join(root, file.path));
      if (info.size !== file.size) return false;
      const [digest] = await hashFile(join(root, file.path));
      if (digest !== file.sha256) return false;
    } catch {
      return false;
    }
  }
  return true;
}

// Streams a download to disk, retrying with a growing delay before giving up.
async function download(url, path) {
  let lastError;
  for (let attempt = 1; attempt <= 4; attempt += 1) {
    try {
      const response = await fetch(url, { headers: { 'User-Agent': USER_AGENT } });
      if (!response.ok || !response.body) throw new Error(`HTTP ${response.status}`);
      await mkdir(dirname(path), { recursive: true });
      await pipeline(Readable.fromWeb(response.body), createWriteStream(`${path}.part`));
      await rename(`${path}.part`, path);
      return;
    } catch (error) {
      lastError = error;
      await new Promise((resolve) => setTimeout(resolve, 2000 * attempt));
    }
  }
  throw new Error(`${url}: ${lastError.message}`);
}

// The members of a zip archive, from its central directory (ZIP64 included).
async function zipEntries(handle, size) {
  const tailLength = Math.min(size, 65557);
  const tail = Buffer.alloc(tailLength);
  await handle.read(tail, 0, tailLength, size - tailLength);
  const eocd = tail.lastIndexOf(Buffer.from([0x50, 0x4b, 0x05, 0x06]));
  if (eocd < 0) throw new Error('not a zip archive (no end of central directory)');
  let count = tail.readUInt16LE(eocd + 10);
  let cdSize = tail.readUInt32LE(eocd + 12);
  let cdOffset = tail.readUInt32LE(eocd + 16);
  if (count === 0xffff || cdSize === 0xffffffff || cdOffset === 0xffffffff) {
    const locator = tail.lastIndexOf(Buffer.from([0x50, 0x4b, 0x06, 0x07]));
    if (locator < 0) throw new Error('ZIP64 archive without its locator');
    const recordOffset = Number(tail.readBigUInt64LE(locator + 8));
    const record64 = Buffer.alloc(56);
    await handle.read(record64, 0, 56, recordOffset);
    count = Number(record64.readBigUInt64LE(32));
    cdSize = Number(record64.readBigUInt64LE(40));
    cdOffset = Number(record64.readBigUInt64LE(48));
  }
  const cd = Buffer.alloc(cdSize);
  await handle.read(cd, 0, cdSize, cdOffset);
  const entries = [];
  let p = 0;
  for (let i = 0; i < count; i += 1) {
    if (cd.readUInt32LE(p) !== 0x02014b50) throw new Error('corrupt central directory');
    const method = cd.readUInt16LE(p + 10);
    const crc = cd.readUInt32LE(p + 16);
    let compressed = cd.readUInt32LE(p + 20);
    let uncompressed = cd.readUInt32LE(p + 24);
    const nameLength = cd.readUInt16LE(p + 28);
    const extraLength = cd.readUInt16LE(p + 30);
    const commentLength = cd.readUInt16LE(p + 32);
    let offset = cd.readUInt32LE(p + 42);
    const name = cd.toString('utf8', p + 46, p + 46 + nameLength);
    const extra = cd.subarray(p + 46 + nameLength, p + 46 + nameLength + extraLength);
    for (let q = 0; q + 4 <= extra.length;) {
      const id = extra.readUInt16LE(q);
      const length = extra.readUInt16LE(q + 2);
      if (id === 0x0001) {
        let r = q + 4;
        if (uncompressed === 0xffffffff) { uncompressed = Number(extra.readBigUInt64LE(r)); r += 8; }
        if (compressed === 0xffffffff) { compressed = Number(extra.readBigUInt64LE(r)); r += 8; }
        if (offset === 0xffffffff) { offset = Number(extra.readBigUInt64LE(r)); }
      }
      q += 4 + length;
    }
    entries.push({ name, method, crc, compressed, uncompressed, offset });
    p += 46 + nameLength + extraLength + commentLength;
  }
  return entries;
}

// A remote file read by HTTP range, as zipEntries reads a local one, each request retried.
function remoteFile(url) {
  async function ranged(start, end) {
    let lastError;
    for (let attempt = 1; attempt <= 4; attempt += 1) {
      try {
        const response = await fetch(url, { headers: { 'User-Agent': USER_AGENT, Range: `bytes=${start}-${end}` } });
        if (response.status !== 206 || !response.body) throw new Error(`HTTP ${response.status} for a range`);
        return response;
      } catch (error) {
        lastError = error;
        await new Promise((resolve) => setTimeout(resolve, 2000 * attempt));
      }
    }
    throw new Error(`${url}: ${lastError.message}`);
  }
  return {
    async size() {
      const response = await fetch(url, { method: 'HEAD', headers: { 'User-Agent': USER_AGENT } });
      if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
      return Number(response.headers.get('content-length'));
    },
    async read(buffer, offset, length, position) {
      const bytes = Buffer.from(await (await ranged(position, position + length - 1)).arrayBuffer());
      bytes.copy(buffer, offset);
      return { bytesRead: bytes.length };
    },
    async stream(start, end) {
      return Readable.fromWeb((await ranged(start, end)).body);
    },
  };
}

// Takes the named members of a remote zip archive into root, each under its name less strip, each
// one's CRC-32 and size checked (a member is fetched again, up to three times, when its transfer
// breaks).
async function extractRemote(remote, size, names, strip, root) {
  const entries = await zipEntries(remote, size);
  const wanted = new Set(names);
  const found = entries.filter((e) => wanted.has(e.name.startsWith(strip) ? e.name.slice(strip.length) : e.name));
  if (found.length !== wanted.size) throw new Error(`the archive lacks ${[...wanted].filter((n) => !found.some((e) => e.name === strip + n)).join(', ')}`);
  for (const entry of found) {
    const name = entry.name.slice(strip.length);
    const relative = normalize(name);
    if (name.startsWith('/') || relative.startsWith('..') || relative.split(sep).includes('..')) throw new Error(`unsafe member name ${entry.name}`);
    if (entry.method !== 0 && entry.method !== 8) throw new Error(`${entry.name}: compression method ${entry.method} is not read`);
    const local = Buffer.alloc(30);
    await remote.read(local, 0, 30, entry.offset);
    if (local.readUInt32LE(0) !== 0x04034b50) throw new Error(`${entry.name}: corrupt local header`);
    const start = entry.offset + 30 + local.readUInt16LE(26) + local.readUInt16LE(28);
    const target = join(root, relative);
    await mkdir(dirname(target), { recursive: true });
    console.log(`  … ${name}: ${(entry.compressed / 1e6).toFixed(0)} MB`);
    for (let attempt = 1; ; attempt += 1) {
      let crc = 0;
      let written = 0;
      const check = new Transform({
        transform(chunk, _, done) {
          crc = crc32(chunk, crc);
          written += chunk.length;
          done(null, chunk);
        },
      });
      try {
        const source = entry.compressed > 0 ? await remote.stream(start, start + entry.compressed - 1) : Readable.from([]);
        const stages = entry.method === 8 ? [source, createInflateRaw(), check] : [source, check];
        await pipeline(...stages, createWriteStream(`${target}.part`));
        if (written !== entry.uncompressed || (crc >>> 0) !== entry.crc) throw new Error(`${entry.name}: size or CRC-32 does not match the archive's record`);
        break;
      } catch (error) {
        if (attempt >= 4) throw error;
        await new Promise((resolve) => setTimeout(resolve, 2000 * attempt));
      }
    }
    await rename(`${target}.part`, target);
  }
}

// Extracts an archive into root: members only inside it (no absolute paths or ".."), each one's
// CRC-32 and size checked.
async function extract(archive, root) {
  const handle = await open(archive, 'r');
  try {
    const { size } = await handle.stat();
    const entries = await zipEntries(handle, size);
    for (const entry of entries) {
      const relative = normalize(entry.name);
      if (entry.name.startsWith('/') || relative.startsWith('..') || relative.split(sep).includes('..')) throw new Error(`unsafe member name ${entry.name}`);
      const target = join(root, relative);
      if (entry.name.endsWith('/')) {
        await mkdir(target, { recursive: true });
        continue;
      }
      if (entry.method !== 0 && entry.method !== 8) throw new Error(`${entry.name}: compression method ${entry.method} is not read`);
      const local = Buffer.alloc(30);
      await handle.read(local, 0, 30, entry.offset);
      if (local.readUInt32LE(0) !== 0x04034b50) throw new Error(`${entry.name}: corrupt local header`);
      const start = entry.offset + 30 + local.readUInt16LE(26) + local.readUInt16LE(28);
      await mkdir(dirname(target), { recursive: true });
      let crc = 0;
      let written = 0;
      const check = new Transform({
        transform(chunk, _, done) {
          crc = crc32(chunk, crc);
          written += chunk.length;
          done(null, chunk);
        },
      });
      const source = entry.compressed > 0 ? createReadStream(null, { fd: handle.fd, start, end: start + entry.compressed - 1, autoClose: false }) : Readable.from([]);
      const stages = entry.method === 8 ? [source, createInflateRaw(), check] : [source, check];
      await pipeline(...stages, createWriteStream(`${target}.part`));
      if (written !== entry.uncompressed || (crc >>> 0) !== entry.crc) throw new Error(`${entry.name}: size or CRC-32 does not match the archive's record`);
      await rename(`${target}.part`, target);
    }
    return entries.filter((e) => !e.name.endsWith('/')).map((e) => normalize(e.name).split(sep).join('/'));
  } finally {
    await handle.close();
  }
}

let failures = 0;
let recorded = false;
for (const [name, set] of Object.entries(sets)) {
  if (names.length && !names.includes(name)) continue;
  const root = `${dataRoot}/${name}`;
  try {
    if (await intact(root, set.files)) {
      console.log(`✓ ${name}: already present (${set.title})`);
      continue;
    }
    // Files under a base URL (a tool such as axe-core): each downloaded and checked.
    if (set.base) {
      await mkdir(root, { recursive: true });
      for (const file of set.files) {
        const target = join(root, file.path);
        await mkdir(dirname(target), { recursive: true });
        await download(new URL(file.path, set.base).href, `${target}.part`);
        const info = await stat(`${target}.part`);
        const [digest] = await hashFile(`${target}.part`);
        if (record) {
          file.size = info.size;
          file.sha256 = digest;
          recorded = true;
        } else if (info.size !== file.size || digest !== file.sha256) {
          await rm(`${target}.part`, { force: true });
          throw new Error(`${file.path} does not match its record (got ${info.size} bytes, SHA-256 ${digest})`);
        }
        await rename(`${target}.part`, target);
      }
      console.log(`✓ ${name}: downloaded ${set.files.length} file${set.files.length === 1 ? '' : 's'}${record ? ', recorded in sources.json' : ''} (${set.title})`);
      continue;
    }
    // Members of a remote archive, and files downloaded whole (a fixture).
    if (set.members || set.downloads) {
      await mkdir(root, { recursive: true });
      if (set.members) {
        const remote = remoteFile(set.members.url);
        const size = await remote.size();
        if (size !== set.members.size) throw new Error(`the archive is ${size} bytes, not the ${set.members.size} recorded: the publisher has changed it`);
        console.log(`… ${name}: ${set.members.names.length} members of ${new URL(set.members.url).pathname.split('/').pop()} (${(set.members.size / 1e9).toFixed(1)} GB), by range`);
        await extractRemote(remote, size, set.members.names, set.members.strip ?? '', join(root, set.members.into ?? ''));
      }
      for (const file of set.downloads ?? []) {
        console.log(`  … ${file.path}`);
        await download(file.url, join(root, file.path));
      }
      const files = [];
      for (const path of [...(set.members?.names ?? []).map((n) => `${set.members.into ?? ''}${n}`), ...(set.downloads ?? []).map((d) => d.path)].sort()) {
        const info = await stat(join(root, path));
        const [digest] = await hashFile(join(root, path));
        files.push({ path, size: info.size, sha256: digest });
      }
      if (record) {
        set.files = files;
        recorded = true;
        console.log(`✓ ${name}: ${files.length} files; their sizes and SHA-256 recorded in sources.json`);
        continue;
      }
      if (!(await intact(root, set.files))) throw new Error(set.files.length ? 'the files do not match sources.json' : 'sources.json records no files for this data set yet (the maintainer runs --record once)');
      console.log(`✓ ${name}: ${files.length} files (${set.title})`);
      continue;
    }
    if (!set.archive) throw new Error('no archive to download');
    const archive = `${dataRoot}/${name}.download.zip`;
    console.log(`… ${name}: downloading ${(set.archive.size / 1e6).toFixed(0)} MB from ${new URL(set.archive.url).host}`);
    await download(set.archive.url, archive);
    const { size } = await stat(archive);
    const [md5, sha256] = await hashFile(archive, ['md5', 'sha256']);
    if (size !== set.archive.size || md5 !== set.archive.md5) {
      await rm(archive, { force: true });
      throw new Error(`the archive does not match its record (got ${size} bytes, MD5 ${md5}): the publisher has changed it`);
    }
    await rm(root, { recursive: true, force: true });
    const members = await extract(archive, root);
    await rm(archive, { force: true });
    const files = [];
    for (const path of members.sort()) {
      const info = await stat(join(root, path));
      const [digest] = await hashFile(join(root, path));
      files.push({ path, size: info.size, sha256: digest });
    }
    if (record) {
      set.archive.sha256 = sha256;
      set.files = files;
      recorded = true;
      console.log(`✓ ${name}: extracted ${files.length} files; their sizes and SHA-256 recorded in sources.json`);
      continue;
    }
    if (!(await intact(root, set.files))) {
      throw new Error(set.files.length ? 'the extracted files do not match sources.json' : 'sources.json records no files for this data set yet (the maintainer runs --record once)');
    }
    console.log(`✓ ${name}: downloaded and extracted ${files.length} files (${set.title})`);
  } catch (error) {
    failures += 1;
    console.error(`✗ ${name}: ${error.message}`);
  }
}
if (recorded) await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
if (failures) process.exit(1);
