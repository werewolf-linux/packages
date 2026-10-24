// n8n-core catches missing native Sentry profiling modules; the watcher is
// only used by N8N_DEV_RELOAD. Keep workflow dependencies and their backends.
const fs = require('node:fs');
const path = require('node:path');
const omitted = new Set(['@sentry/node-native', '@sentry/node-native-stacktrace', '@sentry/profiling-node', '@parcel/watcher']);
const consumers = {
  '@sentry/node-native': 'n8n-core',
  '@sentry/node-native-stacktrace': '@sentry/node-native',
  '@sentry/profiling-node': 'n8n-core',
  '@parcel/watcher': 'n8n',
};
// Stop an update that introduces a new consumer instead of breaking its feature.
const lock = JSON.parse(fs.readFileSync('package-lock.json', 'utf8'));
for (const [location, pkg] of Object.entries(lock.packages)) {
  const owner = location.split('node_modules/').at(-1);
  for (const dep of Object.keys({ ...pkg.dependencies, ...pkg.optionalDependencies })) {
    const expected = consumers[dep] || (/^@parcel\/watcher-/.test(dep) ? '@parcel/watcher' : null);
    if (expected && owner !== expected) throw new Error(`review new consumer of ${dep}: ${owner}`);
  }
}

function prune(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (!entry.isDirectory()) continue;
    const name = path.join(dir, entry.name);
    const manifest = path.join(name, 'package.json');
    const parent = path.basename(dir);
    const candidate = (parent === '@sentry' && ['node-native', 'node-native-stacktrace', 'profiling-node'].includes(entry.name)) ||
      (parent === '@parcel' && (entry.name === 'watcher' || entry.name.startsWith('watcher-')));
    if (candidate && fs.existsSync(manifest)) {
      const pkg = JSON.parse(fs.readFileSync(manifest, 'utf8'));
      if (omitted.has(pkg.name) || /^@parcel\/watcher-/.test(pkg.name)) {
        fs.rmSync(name, { recursive: true });
        continue;
      }
    }
    prune(name);
  }
}
prune('node_modules');
