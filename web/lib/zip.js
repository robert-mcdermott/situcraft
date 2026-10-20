// ZIP reader and writer, used for SituCraft's certificates (wave 8, slice 5). Adapted from
// MaveScape 0.8.0 web/lib/zip.js (itself after CytoWeave 0.8.0's and Proteoscope's, the same
// author's, Apache-2.0). Compression uses the
// platform's CompressionStream / DecompressionStream ('deflate-raw'), available in browsers,
// workers and Node 18+. The writer stores or deflates entries and writes sizes into the local
// headers (no data descriptors, no ZIP64); the reader works from the central directory, so
// archives written with data descriptors or ZIP64 records read too.

const CRC_TABLE = (() => {
  const table = new Uint32Array(256);
  for (let n = 0; n < 256; n += 1) {
    let c = n;
    for (let k = 0; k < 8; k += 1) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
    table[n] = c >>> 0;
  }
  return table;
})();

export function crc32(bytes) {
  let crc = 0xffffffff;
  for (let index = 0; index < bytes.length; index += 1) crc = CRC_TABLE[(crc ^ bytes[index]) & 0xff] ^ (crc >>> 8);
  return (crc ^ 0xffffffff) >>> 0;
}

async function deflateRaw(bytes) {
  const stream = new Blob([bytes]).stream().pipeThrough(new CompressionStream('deflate-raw'));
  return new Uint8Array(await new Response(stream).arrayBuffer());
}

// The DOS date and time of a ZIP entry; in UTC when asked, so that the same date gives the same
// bytes in every time zone.
function dosDateTime(date, utc = false) {
  const get = (local, universal) => (utc ? universal.call(date) : local.call(date));
  const year = Math.min(2107, Math.max(1980, get(date.getFullYear, date.getUTCFullYear)));
  const time = (get(date.getHours, date.getUTCHours) << 11) | (get(date.getMinutes, date.getUTCMinutes) << 5) | Math.floor(get(date.getSeconds, date.getUTCSeconds) / 2);
  const day = ((year - 1980) << 9) | ((get(date.getMonth, date.getUTCMonth) + 1) << 5) | get(date.getDate, date.getUTCDate);
  return { time, day };
}

// files: [{ name, data: Uint8Array | string, compress?: boolean }]. Returns the archive as a
// Uint8Array. options: { compress = true, date = new Date(), utc = false }.
export async function createZip(files, options = {}) {
  if (files.length > 0xffff) throw new Error('A ZIP archive without ZIP64 holds at most 65,535 files.');
  const encoder = new TextEncoder();
  const { time, day } = dosDateTime(options.date ?? new Date(), options.utc);
  const locals = [];
  const centrals = [];
  const seen = new Set();
  let offset = 0;
  for (const file of files) {
    if (seen.has(file.name)) throw new Error(`The archive would contain "${file.name}" twice.`);
    seen.add(file.name);
    const name = encoder.encode(file.name);
    const raw = typeof file.data === 'string' ? encoder.encode(file.data) : file.data;
    if (raw.length > 0xfffffffe) throw new Error(`${file.name} is larger than 4 GB, which this ZIP writer cannot store.`);
    const crc = crc32(raw);
    let method = 0;
    let body = raw;
    const compress = file.compress ?? options.compress ?? true;
    if (compress && typeof CompressionStream !== 'undefined' && raw.length > 256) {
      const deflated = await deflateRaw(raw);
      if (deflated.length < raw.length) {
        method = 8;
        body = deflated;
      }
    }
    if (offset + 30 + name.length + body.length > 0xfffffffe) throw new Error('The archive would exceed 4 GB, which this ZIP writer cannot store.');
    const local = new DataView(new ArrayBuffer(30));
    local.setUint32(0, 0x04034b50, true);
    local.setUint16(4, 20, true);
    local.setUint16(6, 0x0800, true);
    local.setUint16(8, method, true);
    local.setUint16(10, time, true);
    local.setUint16(12, day, true);
    local.setUint32(14, crc, true);
    local.setUint32(18, body.length, true);
    local.setUint32(22, raw.length, true);
    local.setUint16(26, name.length, true);
    local.setUint16(28, 0, true);
    locals.push(new Uint8Array(local.buffer), name, body);

    const central = new DataView(new ArrayBuffer(46));
    central.setUint32(0, 0x02014b50, true);
    central.setUint16(4, 20, true);
    central.setUint16(6, 20, true);
    central.setUint16(8, 0x0800, true);
    central.setUint16(10, method, true);
    central.setUint16(12, time, true);
    central.setUint16(14, day, true);
    central.setUint32(16, crc, true);
    central.setUint32(20, body.length, true);
    central.setUint32(24, raw.length, true);
    central.setUint16(28, name.length, true);
    central.setUint32(42, offset, true);
    centrals.push(new Uint8Array(central.buffer), name);
    offset += 30 + name.length + body.length;
  }
  const centralSize = centrals.reduce((total, part) => total + part.length, 0);
  const end = new DataView(new ArrayBuffer(22));
  end.setUint32(0, 0x06054b50, true);
  end.setUint16(8, files.length, true);
  end.setUint16(10, files.length, true);
  end.setUint32(12, centralSize, true);
  end.setUint32(16, offset, true);
  const parts = [...locals, ...centrals, new Uint8Array(end.buffer)];
  const total = parts.reduce((sum, part) => sum + part.length, 0);
  const archive = new Uint8Array(total);
  let cursor = 0;
  for (const part of parts) {
    archive.set(part, cursor);
    cursor += part.length;
  }
  return archive;
}

// Lists the entries of a ZIP archive from its central directory: [{ name, method, crc, size,
// compressedSize, localOffset, encrypted }]. Directory entries are left out.
export function listZip(archive) {
  const view = new DataView(archive.buffer, archive.byteOffset, archive.byteLength);
  const end = findEndOfCentralDirectory(view);
  if (end < 0) throw new Error('Not a ZIP archive (no end-of-central-directory record).');
  let count = view.getUint16(end + 10, true);
  let offset = view.getUint32(end + 16, true);
  // ZIP64: the locator sits just before the classic record and points at the 64-bit one.
  const locator = end - 20;
  if ((count === 0xffff || offset === 0xffffffff) && locator >= 0 && view.getUint32(locator, true) === 0x07064b50) {
    const record = Number(view.getBigUint64(locator + 8, true));
    if (record + 56 <= view.byteLength && view.getUint32(record, true) === 0x06064b50) {
      count = Number(view.getBigUint64(record + 32, true));
      offset = Number(view.getBigUint64(record + 48, true));
    }
  }
  const utf8 = new TextDecoder('utf-8', { fatal: true });
  const entries = [];
  for (let index = 0; index < count; index += 1) {
    if (offset + 46 > view.byteLength || view.getUint32(offset, true) !== 0x02014b50) throw new Error('The ZIP central directory is damaged.');
    const flags = view.getUint16(offset + 8, true);
    const method = view.getUint16(offset + 10, true);
    const crc = view.getUint32(offset + 16, true);
    let compressedSize = view.getUint32(offset + 20, true);
    let size = view.getUint32(offset + 24, true);
    const nameLength = view.getUint16(offset + 28, true);
    const extraLength = view.getUint16(offset + 30, true);
    const commentLength = view.getUint16(offset + 32, true);
    let localOffset = view.getUint32(offset + 42, true);
    const nameBytes = archive.subarray(offset + 46, offset + 46 + nameLength);
    // Bit 11 marks UTF-8 names, but many zippers write UTF-8 without setting it; names that are
    // not valid UTF-8 are read byte for byte.
    let name;
    try {
      name = utf8.decode(nameBytes);
    } catch {
      name = String.fromCharCode(...nameBytes);
    }
    const zip64 = zip64Fields(view, offset + 46 + nameLength, extraLength, { size, compressedSize, localOffset });
    ({ size, compressedSize, localOffset } = zip64);
    offset += 46 + nameLength + extraLength + commentLength;
    if (name.endsWith('/')) continue;
    entries.push({ name, method, crc, size, compressedSize, localOffset, encrypted: Boolean(flags & 1) });
  }
  return entries;
}

function findEndOfCentralDirectory(view) {
  const last = view.byteLength - 22;
  const first = Math.max(0, last - 0xffff);
  for (let offset = last; offset >= first; offset -= 1) {
    if (view.getUint32(offset, true) === 0x06054b50) return offset;
  }
  return -1;
}

function zip64Fields(view, start, length, values) {
  const result = { ...values };
  let cursor = start;
  while (cursor + 4 <= start + length) {
    const id = view.getUint16(cursor, true);
    const size = view.getUint16(cursor + 2, true);
    if (id === 0x0001) {
      let field = cursor + 4;
      for (const key of ['size', 'compressedSize', 'localOffset']) {
        if (result[key] !== 0xffffffff || field + 8 > cursor + 4 + size) continue;
        result[key] = Number(view.getBigUint64(field, true));
        field += 8;
      }
    }
    cursor += 4 + size;
  }
  return result;
}

// Returns the uncompressed bytes of one entry from listZip(), checking its CRC.
export async function readZipEntry(archive, entry) {
  if (entry.encrypted) throw new Error(`${entry.name} is encrypted.`);
  const view = new DataView(archive.buffer, archive.byteOffset, archive.byteLength);
  if (entry.localOffset + 30 > view.byteLength || view.getUint32(entry.localOffset, true) !== 0x04034b50) throw new Error(`The ZIP entry ${entry.name} is damaged.`);
  const start = entry.localOffset + 30 + view.getUint16(entry.localOffset + 26, true) + view.getUint16(entry.localOffset + 28, true);
  if (start + entry.compressedSize > view.byteLength) throw new Error(`The ZIP entry ${entry.name} is truncated.`);
  let data = archive.subarray(start, start + entry.compressedSize);
  if (entry.method === 8) {
    const stream = new Blob([data]).stream().pipeThrough(new DecompressionStream('deflate-raw'));
    data = new Uint8Array(await new Response(stream).arrayBuffer());
  } else if (entry.method !== 0) {
    throw new Error(`${entry.name} uses an unsupported ZIP compression method (${entry.method}).`);
  }
  if (crc32(data) !== entry.crc) throw new Error(`CRC mismatch for ${entry.name}; the archive is damaged.`);
  return data;
}

// Reads every file of a ZIP archive into a Map of name → bytes.
export async function readZip(archive) {
  const files = new Map();
  for (const entry of listZip(archive)) files.set(entry.name, await readZipEntry(archive, entry));
  return files;
}

export function isZip(bytes) {
  return bytes.length >= 4 && bytes[0] === 0x50 && bytes[1] === 0x4b && (bytes[2] === 3 || bytes[2] === 5) && (bytes[3] === 4 || bytes[3] === 6);
}
