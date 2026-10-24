'use strict';
const assert = require('node:assert/strict');
const fs = require('node:fs');
const zlib = require('node:zlib');
const https = require('node:https');
const tls = require('node:tls');

async function main() {
  for (const name of ['brotli', 'zstd']) assert.equal(process.versions[name], undefined);
  for (const name of ['BrotliCompress', 'BrotliDecompress', 'ZstdCompress', 'ZstdDecompress',
                     'createBrotliCompress', 'createBrotliDecompress',
                     'createZstdCompress', 'createZstdDecompress',
                     'brotliCompress', 'brotliCompressSync', 'brotliDecompress', 'brotliDecompressSync',
                     'zstdCompress', 'zstdCompressSync', 'zstdDecompress', 'zstdDecompressSync']) {
    assert.equal(zlib[name], undefined, name);
  }
  const binding = process.binding('zlib');
  for (const name of ['BrotliEncoder', 'BrotliDecoder', 'ZstdCompress', 'ZstdDecompress']) {
    assert.equal(binding[name], undefined, name);
  }
  assert.throws(() => new CompressionStream('brotli'), TypeError);
  assert.throws(() => new DecompressionStream('brotli'), TypeError);
  const plain = Buffer.from('retained gzip and deflate');
  assert.deepEqual(zlib.gunzipSync(zlib.gzipSync(plain)), plain);
  assert.deepEqual(zlib.inflateSync(zlib.deflateSync(plain)), plain);

  const cert = fs.readFileSync(process.argv[2]);
  // Local trust for this process; certificate verification stays enabled.
  tls.setDefaultCACertificates([cert.toString()]);
  const server = https.createServer({cert, key: fs.readFileSync(process.argv[3])}, (req, res) => {
    assert.equal(req.headers['accept-encoding'], 'gzip, deflate');
    const coding = req.url.slice(1);
    const body = coding === 'gzip' ? zlib.gzipSync(plain) :
      coding === 'deflate' ? zlib.deflateSync(plain) : Buffer.from('unsupported encoding');
    res.writeHead(200, {'content-encoding': coding, 'content-length': body.length});
    res.end(body);
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  try {
    const url = `https://localhost:${server.address().port}`;
    for (const coding of ['gzip', 'deflate']) {
      const response = await fetch(`${url}/${coding}`);
      assert.equal(await response.text(), plain.toString());
    }
    for (const coding of ['br', 'zstd']) {
      await assert.rejects(async () => {
        const response = await fetch(`${url}/${coding}`);
        await response.arrayBuffer();
      });
    }
  } finally {
    server.closeAllConnections();
    await new Promise(resolve => server.close(resolve));
  }
  console.log('PASS: omitted codecs, retained gzip/deflate, HTTPS negotiation and rejected responses');
}
main().catch(error => { console.error(error); process.exitCode = 1; });

require('node:assert/strict').throws(() => require('node:ffi'), { code: 'ERR_UNKNOWN_BUILTIN_MODULE' });
