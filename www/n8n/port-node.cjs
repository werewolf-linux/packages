// Permit the hardened runtime's Node major without changing application code.
const fs = require('node:fs');
const path = 'node_modules/n8n/package.json';
const pkg = JSON.parse(fs.readFileSync(path, 'utf8'));
if (!pkg.engines || !pkg.engines.node) throw new Error('missing Node version guard');
pkg.engines.node += ' || >=26.0.0 <27.0.0';
fs.writeFileSync(path, JSON.stringify(pkg, null, 2) + '\n');

// Some integrations bundle binaries for every platform. Only glibc binaries
// for the build architecture belong in this package or its ELF dependencies.
const paths = require('node:path');
const machine = { arm64: 183, x64: 62 }[process.arch];
if (!machine) throw new Error('unsupported build architecture');
const lock = JSON.parse(fs.readFileSync('package-lock.json', 'utf8'));
const packages = new Map(Object.keys(lock.packages).map(location => [location, location.split('node_modules/').at(-1)]));
// Match upstream's production cleanup, preserving first-party source maps and
// runtime data such as PDF fonts, WASM, workflow nodes and editor assets.
// https://github.com/n8n-io/n8n/blob/n8n%402.44.7/scripts/build-n8n.mjs
const unused = {
  resolve: ['test'],
  'import-in-the-middle': ['test'],
  'github-from-package': ['example'],
  tedious: ['benchmarks'],
  // We rebuild this addon; its prebuild must not shadow the compiled binding.
  'isolated-vm': ['prebuilds'],
};
function prune(dir, owner = '') {
  owner = packages.get(dir) || owner;
  const firstParty = owner === 'n8n' || owner.startsWith('n8n-') || owner.startsWith('@n8n/');
  for (const entry of fs.readdirSync(dir, {withFileTypes: true})) {
    const name = paths.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (packages.has(dir) && unused[owner]?.includes(entry.name)) fs.rmSync(name, {recursive: true});
      else prune(name, owner);
      continue;
    }
    if (!entry.isFile()) continue;
    // Upstream also omits declaration artifacts; ordinary .ts and .md files
    // stay because AI features read node definitions and knowledge at runtime.
    if (/\.d\.ts(?:\.map)?$/.test(name) || (!firstParty && name.endsWith('.map')) ||
        (owner === 'agent-browser' && entry.name.startsWith('agent-browser-') && entry.name !== `agent-browser-linux-${process.arch}`)) {
      fs.unlinkSync(name);
      continue;
    }
    const fd = fs.openSync(name, 'r');
    const header = Buffer.alloc(64);
    const length = fs.readSync(fd, header);
    fs.closeSync(fd);
    const elf = length >= 20 && header.subarray(0, 4).equals(Buffer.from([127,69,76,70]));
    const foreign = elf && (header[5] !== 1 || header.readUInt16LE(18) !== machine);
    const musl = elf && (name.includes('musl') || fs.readFileSync(name).includes(Buffer.from('libc.musl-')));
    if (foreign || musl || /\.(dll|dylib|exe)$/.test(name) || (name.endsWith('.node') && !elf)) fs.unlinkSync(name);
  }
}
prune('node_modules');

// GCC 16 no longer supplies fixed-width integers through <memory>. Both
// pinned isolated-vm releases use uint32_t without including its own header.
const timerPath = 'node_modules/isolated-vm/src/lib/timer.h';
const timer = fs.readFileSync(timerPath, 'utf8');
if (!timer.startsWith('#pragma once\n')) throw new Error('unexpected isolated-vm timer header');
fs.writeFileSync(timerPath, timer.replace('#pragma once\n', '#pragma once\n#include <cstdint>\n'));
