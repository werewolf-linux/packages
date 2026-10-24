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
