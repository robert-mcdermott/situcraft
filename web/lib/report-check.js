// The report's check of itself (wave 8, slice 7): run by the report, in any browser, offline, on
// the certificate it carries. The three functions are written into the report as they are here
// (Function.prototype.toString), so they hold everything they use: no import, nothing from outside
// (checkReportData calls numberText, written beside it).
//
// numberText(v): a number as the report's tables show it.
//
// checkReportData(input): input { zip (Uint8Array: the certificate), numbers: [{ pointer, value,
// text }] (each number a table shows: its place in certificate.json, its value, and its text as
// shown), figures: [{ id, svg (Uint8Array: the SVG shown) }], methods: [text] (the methods'
// paragraphs as shown) }. Each number is the certificate's when its value is the one at its place
// and its text that value's; each figure when its SVG's SHA-256 is the one certified. Answers {
// intact, fingerprint, problems: [text], numbers: { checked, same, differ: [pointer] }, figures: {
// checked, same, differ: [id] }, methods: { same } }.
//
// renderReportCheck(document, check): reads the report (its certificate, its numbers, figures and
// methods), checks them and says what it found at the top; makes the certificate a file to save.

// Whole numbers with commas; others to 4 significant digits, below 0.001 and from a million up as
// 3 significant digits and a power of ten.
export function numberText(v) {
  if (Number.isInteger(v)) return v.toLocaleString('en-US');
  if (!Number.isFinite(v)) return String(v);
  const a = Math.abs(v);
  if (a !== 0 && (a < 1e-3 || a >= 1e6)) return v.toExponential(2);
  return String(Number(v.toPrecision(4)));
}

export async function checkReportData(input) {
  const encoder = new TextEncoder();
  const decoder = new TextDecoder();

  // SHA-256 (FIPS 180-4), of bytes, as hex.
  const K = [
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da, 0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
  ];
  const sha256 = (bytes) => {
    const n = bytes.length;
    const padded = new Uint8Array(((n + 9 + 63) >> 6) << 6);
    padded.set(bytes);
    padded[n] = 0x80;
    const view = new DataView(padded.buffer);
    view.setUint32(padded.length - 8, Math.floor(n / 0x20000000), false);
    view.setUint32(padded.length - 4, (n << 3) >>> 0, false);
    const h = [0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19];
    const w = new Uint32Array(64);
    const rotr = (x, r) => (x >>> r) | (x << (32 - r));
    for (let off = 0; off < padded.length; off += 64) {
      for (let i = 0; i < 16; i += 1) w[i] = view.getUint32(off + 4 * i, false);
      for (let i = 16; i < 64; i += 1) {
        const s0 = rotr(w[i - 15], 7) ^ rotr(w[i - 15], 18) ^ (w[i - 15] >>> 3);
        const s1 = rotr(w[i - 2], 17) ^ rotr(w[i - 2], 19) ^ (w[i - 2] >>> 10);
        w[i] = (w[i - 16] + s0 + w[i - 7] + s1) >>> 0;
      }
      let [a, b, c, d, e, f, g, hh] = h;
      for (let i = 0; i < 64; i += 1) {
        const t1 = (hh + (rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25)) + ((e & f) ^ (~e & g)) + K[i] + w[i]) >>> 0;
        const t2 = ((rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22)) + ((a & b) ^ (a & c) ^ (b & c))) >>> 0;
        hh = g; g = f; f = e; e = (d + t1) >>> 0; d = c; c = b; b = a; a = (t1 + t2) >>> 0;
      }
      h[0] = (h[0] + a) >>> 0; h[1] = (h[1] + b) >>> 0; h[2] = (h[2] + c) >>> 0; h[3] = (h[3] + d) >>> 0;
      h[4] = (h[4] + e) >>> 0; h[5] = (h[5] + f) >>> 0; h[6] = (h[6] + g) >>> 0; h[7] = (h[7] + hh) >>> 0;
    }
    return h.map((x) => x.toString(16).padStart(8, '0')).join('');
  };

  // JSON with every object's keys sorted (as the certificate's fingerprint is written).
  const canonical = (v) => {
    if (Array.isArray(v)) return `[${v.map((x) => canonical(x === undefined ? null : x)).join(',')}]`;
    if (v && typeof v === 'object') return `{${Object.keys(v).filter((k) => v[k] !== undefined).sort().map((k) => `${JSON.stringify(k)}:${canonical(v[k])}`).join(',')}}`;
    return JSON.stringify(v ?? null);
  };

  // The zip's entries (its central directory; stored or deflated).
  const unzip = async (zip) => {
    const view = new DataView(zip.buffer, zip.byteOffset, zip.byteLength);
    let end = zip.length - 22;
    while (end >= 0 && view.getUint32(end, true) !== 0x06054b50) end -= 1;
    if (end < 0) throw new Error('the certificate it carries is not a zip');
    const files = new Map();
    let at = view.getUint32(end + 16, true);
    for (let i = 0; i < view.getUint16(end + 10, true); i += 1) {
      const method = view.getUint16(at + 10, true);
      const packed = view.getUint32(at + 20, true);
      const nameLength = view.getUint16(at + 28, true);
      const local = view.getUint32(at + 42, true);
      const name = decoder.decode(zip.subarray(at + 46, at + 46 + nameLength));
      const start = local + 30 + view.getUint16(local + 26, true) + view.getUint16(local + 28, true);
      let data = zip.subarray(start, start + packed);
      if (method === 8) data = new Uint8Array(await new Response(new Blob([data]).stream().pipeThrough(new DecompressionStream('deflate-raw'))).arrayBuffer());
      files.set(name, data);
      at += 46 + nameLength + view.getUint16(at + 30, true) + view.getUint16(at + 32, true);
    }
    return files;
  };

  // The value at a JSON pointer.
  const at = (doc, pointer) => pointer.split('/').slice(1).map((p) => p.replace(/~1/g, '/').replace(/~0/g, '~')).reduce((v, key) => (v === undefined || v === null ? undefined : v[key]), doc);

  const out = { intact: false, fingerprint: null, problems: [], numbers: { checked: 0, same: 0, differ: [] }, figures: { checked: 0, same: 0, differ: [] }, methods: { same: null } };
  let files;
  try {
    files = await unzip(input.zip);
  } catch (error) {
    out.problems.push(error.message);
    return out;
  }
  const json = files.get('certificate.json');
  if (!json) {
    out.problems.push('it carries no certificate.json');
    return out;
  }
  const certificate = JSON.parse(decoder.decode(json));
  out.fingerprint = certificate.fingerprint;
  const { fingerprint, ...rest } = certificate;
  if (sha256(encoder.encode(canonical(rest))) !== fingerprint) out.problems.push('the certificate is not as certified: its fingerprint does not match it');
  for (const named of [certificate.project, certificate.record, certificate.methods, ...(certificate.steps ?? []).map((s) => s.manifest).filter(Boolean)]) {
    const data = files.get(named.path);
    if (!data || sha256(data) !== named.sha256) out.problems.push(`${named.path} is not as certified`);
  }
  for (const n of input.numbers ?? []) {
    out.numbers.checked += 1;
    const certified = at(certificate, n.pointer);
    if (typeof certified === 'number' && canonical(certified) === canonical(n.value) && n.text === numberText(certified)) out.numbers.same += 1;
    else out.numbers.differ.push(n.pointer);
  }
  for (const f of input.figures ?? []) {
    out.figures.checked += 1;
    const step = (certificate.steps ?? []).find((s) => s.id === f.id);
    if (step?.drawing?.svg && step.drawing.svg === sha256(f.svg)) out.figures.same += 1;
    else out.figures.differ.push(f.id);
  }
  if (input.methods) {
    const md = decoder.decode(files.get(certificate.methods?.path) ?? new Uint8Array());
    const paragraphs = md.split('\n## References')[0].split('\n').filter((line) => line && !line.startsWith('#'));
    out.methods.same = paragraphs.length === input.methods.length && paragraphs.every((p, i) => p === input.methods[i]);
  }
  const are = (n, word) => `${n} of its ${word}s ${n === 1 ? 'is' : 'are'}`;
  if (out.numbers.differ.length) out.problems.push(`${are(out.numbers.differ.length, 'number')} not the certificate's`);
  if (out.figures.differ.length) out.problems.push(`${are(out.figures.differ.length, 'figure')} not the one certified`);
  if (out.methods.same === false) out.problems.push('its methods are not the certificate\'s');
  out.intact = out.problems.length === 0;
  return out;
}

export async function renderReportCheck(doc, check) {
  const box = doc.getElementById('report-check');
  const carried = doc.getElementById('situcraft-certificate');
  if (!box || !carried) return null;
  try {
    const binary = atob(carried.textContent.trim());
    const zip = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) zip[i] = binary.charCodeAt(i);
    const link = doc.getElementById('certificate-link');
    if (link) link.href = URL.createObjectURL(new Blob([zip], { type: 'application/zip' }));
    // A value that is not JSON is not the certificate's.
    const value = (text) => {
      try {
        return JSON.parse(text);
      } catch {
        return null;
      }
    };
    const numbers = [...doc.querySelectorAll('[data-pointer]')].map((el) => ({ pointer: el.getAttribute('data-pointer'), value: value(el.getAttribute('data-value')), text: el.textContent }));
    const figures = [...doc.querySelectorAll('img[data-figure]')].map((img) => ({ id: img.getAttribute('data-figure'), svg: Uint8Array.from(atob(img.getAttribute('src').replace(/^data:image\/svg\+xml;base64,/, '')), (c) => c.charCodeAt(0)) }));
    const methods = [...doc.querySelectorAll('#methods .methods-paragraph')].map((p) => p.textContent);
    const result = await check({ zip, numbers, figures, methods: methods.length ? methods : null });
    const n = (k, word) => `${k.toLocaleString('en-US')} ${word}${k === 1 ? '' : 's'}`;
    box.className = `check ${result.intact ? 'ok' : 'bad'}`;
    box.textContent = result.intact
      ? `Checked here, offline: the certificate this report carries is intact (fingerprint ${result.fingerprint.slice(0, 16)}…), and its ${n(result.numbers.same, 'number')}, ${n(result.figures.same, 'figure')} and methods are the certificate's.`
      : `Checked here, offline: ${result.problems.join('; ')}.`;
    for (const pointer of result.numbers.differ) {
      for (const el of doc.querySelectorAll(`[data-pointer="${pointer}"]`)) el.classList.add('differs');
    }
    doc.documentElement.dataset.checked = result.intact ? 'intact' : 'differs';
    return result;
  } catch (error) {
    box.className = 'check bad';
    box.textContent = `The report could not check its certificate: ${error.message}.`;
    doc.documentElement.dataset.checked = 'error';
    return null;
  }
}
