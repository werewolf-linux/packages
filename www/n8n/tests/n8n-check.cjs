const { createRequire } = require('node:module');
const appRequire = createRequire('/usr/lib/n8n/node_modules/n8n-core/package.json');
for (const name of ['@sentry/node-native', '@sentry/profiling-node', '@parcel/watcher']) {
  let missing = false;
  try { appRequire.resolve(name); } catch (error) {
    if (error.code !== 'MODULE_NOT_FOUND') throw error;
    missing = true;
  }
  if (!missing) throw new Error(`optional native package remains: ${name}`);
}

const sqlite = require('/usr/lib/n8n/node_modules/sqlite3');
const ivm = require('/usr/lib/n8n/node_modules/isolated-vm');

const isolate = new ivm.Isolate({ memoryLimit: 16 });
isolate.dispose();
const db = new sqlite.Database(':memory:');
db.get('select 42 as value', (err, row) => {
  if (err || row.value !== 42) process.exit(1);
  db.close();
});

async function checkPdf() {
  const assert = require('node:assert/strict');
  const { pathToFileURL } = require('node:url');
  const req = createRequire('/usr/lib/n8n/node_modules/n8n-nodes-base/package.json');
  const { createCanvas } = req('@napi-rs/canvas');
  const canvas = createCanvas(32, 32);
  const context = canvas.getContext('2d');
  context.fillStyle = '#ff0000';
  context.fillRect(0, 0, 32, 32);
  assert.deepEqual(Array.from(context.getImageData(0, 0, 1, 1).data), [255, 0, 0, 255]);
  assert.equal(canvas.toBuffer('image/png').subarray(1, 4).toString(), 'PNG');

  // Small valid PDF with text: exercise the same parser used by Extract From File.
  const content = 'BT /F1 18 Tf 20 100 Td (Invoice 12345) Tj ET';
  const objects = [
    '<< /Type /Catalog /Pages 2 0 R >>',
    '<< /Type /Pages /Kids [3 0 R] /Count 1 >>',
    '<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>',
    '<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>',
    `<< /Length ${content.length} >>\nstream\n${content}\nendstream`,
  ];
  let pdf = '%PDF-1.4\n';
  const offsets = [0];
  for (const [i, object] of objects.entries()) {
    offsets.push(Buffer.byteLength(pdf));
    pdf += `${i + 1} 0 obj\n${object}\nendobj\n`;
  }
  const xref = Buffer.byteLength(pdf);
  pdf += `xref\n0 6\n0000000000 65535 f \n`;
  for (const offset of offsets.slice(1)) pdf += `${String(offset).padStart(10, '0')} 00000 n \n`;
  pdf += `trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`;
  const { getDocument } = await import(pathToFileURL(req.resolve('pdfjs-dist/legacy/build/pdf.mjs')).href);
  const loadingTask = getDocument({ data: new Uint8Array(Buffer.from(pdf)), useSystemFonts: true });
  try {
    const document = await loadingTask.promise;
    const page = await document.getPage(1);
    const text = await page.getTextContent();
    assert.match(text.items.map(item => item.str).join(' '), /Invoice 12345/);
  } finally {
    await loadingTask.destroy();
  }
  console.log('PASS: n8n native canvas and PDF text extraction');
}
checkPdf().catch(error => { console.error(error); process.exitCode = 1; });
