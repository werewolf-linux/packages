const sqlite = require('/usr/lib/n8n/node_modules/sqlite3');
const ivm = require('/usr/lib/n8n/node_modules/isolated-vm');

const isolate = new ivm.Isolate({ memoryLimit: 16 });
isolate.dispose();
const db = new sqlite.Database(':memory:');
db.get('select 42 as value', (err, row) => {
  if (err || row.value !== 42) process.exit(1);
  db.close();
});
