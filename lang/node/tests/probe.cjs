'use strict';
const assert = require('node:assert/strict');
const cp = require('node:child_process');
const fs = require('node:fs');
const { Worker } = require('node:worker_threads');
const { DatabaseSync } = require('node:sqlite');

function denied(fn) {
  try { fn(); } catch (e) {
    if (e.code === 'ERR_ACCESS_DENIED') return true;
    throw e;
  }
  return false;
}

async function main() {
  switch (process.argv[2]) {
    case 'report': {
      const exec = !denied(() => {
        const result = cp.spawnSync(process.execPath, ['--version']);
        if (result.error) throw result.error;
        assert.equal(result.status, 0);
      });
      const native = !denied(() => assert.equal(require(process.argv[3]).value, 42));
      const sqlite = !denied(() => new DatabaseSync(':memory:', { allowExtension: true }).close());
      console.log(JSON.stringify({ exec, native, sqlite, inspector: process.features.inspector }));
      break;
    }
    case 'exec-paths': {
      for (const fn of [
        () => cp.spawn('/bin/true'),
        () => cp.spawnSync('/bin/true'),
        () => cp.exec('true'),
        () => cp.execSync('true'),
        () => cp.execFile('/bin/true'),
        () => cp.execFileSync('/bin/true'),
        () => cp.fork(process.argv[3]),
        () => process.binding('spawn_sync').spawn({
          file: '/bin/true', args: ['/bin/true'],
          stdio: [{ type: 'ignore' }, { type: 'ignore' }, { type: 'ignore' }],
        }),
      ]) assert(denied(fn), 'child-process entrypoint bypassed the floor');
      console.log('blocked');
      break;
    }
    case 'hide-stderr':
      process.stderr.write = () => true;
      assert(denied(() => cp.spawnSync('/denied-node-tool')));
      console.log('blocked');
      break;
    case 'data-import':
      await import('data:text/javascript,export default 42');
      console.log('ran');
      break;
    case 'worker-eval': {
      const w = new Worker('require("node:worker_threads").parentPort.postMessage(42)', { eval: true });
      const value = await new Promise((resolve, reject) => {
        w.once('message', resolve); w.once('error', reject);
      });
      assert.equal(value, 42);
      await w.terminate();
      console.log('ran');
      break;
    }
    case 'worker-options': {
      const w = new Worker(process.argv[3], { env: { NODE_OPTIONS: '--require=/missing-werewolf-hook' } });
      const value = await new Promise((resolve, reject) => {
        w.once('message', resolve); w.once('error', reject);
      });
      assert.equal(value, 42);
      await w.terminate();
      console.log('ran');
      break;
    }
    case 'data': {
      fs.writeFileSync(process.argv[3], 'data');
      assert.equal(fs.readFileSync(process.argv[3], 'utf8'), 'data');
      const db = new DatabaseSync(':memory:');
      assert.equal(db.prepare('SELECT 42 AS n').get().n, 42);
      db.close();
      console.log('ran');
      break;
    }
    case 'openssl-env':
      assert.equal(process.env.OPENSSL_CONF, undefined);
      assert.equal(process.env.OPENSSL_MODULES, undefined);
      console.log('cleared');
      break;
    case 'ffi':
      assert(denied(() => require('node:ffi')));
      console.log('blocked');
      break;
    case 'wasi':
      assert(denied(() => new (require('node:wasi').WASI)({version: 'preview1'})));
      console.log('blocked');
      break;
    default: throw new Error('Unknown probe action');
  }
}
main().catch(e => { console.error(e); process.exitCode = 1; });
