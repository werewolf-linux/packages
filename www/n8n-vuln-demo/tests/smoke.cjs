// Check the installed server after its shell fixture starts it.
const path = require('node:path');
const root = process.env.N8N_PACKAGE_ROOT || process.cwd();
const version = require(path.join(root, 'node_modules/n8n/package.json')).version;
const url = `http://127.0.0.1:${process.env.N8N_PORT}`;

if (process.argv[2] === '--port') {
  const server = require('node:net').createServer();
  server.listen(0, '127.0.0.1', () => {
    console.log(server.address().port);
    server.close();
  });
} else (async () => {
  const deadline = Date.now() + 120000;
  while (true) {
    try {
      const ready = await fetch(`${url}/healthz/readiness`, {signal: AbortSignal.timeout(1000)});
      const settings = await fetch(`${url}/rest/settings`, {signal: AbortSignal.timeout(1000)});
      if (ready.ok && settings.ok && (await settings.json()).data.versionCli === version) break;
    } catch {}
    if (Date.now() > deadline) throw new Error('server did not become ready');
    await new Promise(resolve => setTimeout(resolve, 250));
  }
  console.log('n8n-build-startup: ok');
})().catch(error => {console.error(error); process.exitCode = 1;});
