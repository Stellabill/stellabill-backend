// Replays the recorded Pact request corpus against a running Prism mock
// server and fails if any request cannot be resolved by the OpenAPI spec.
import fs from 'node:fs';
import path from 'node:path';

const PRISM = process.env.PRISM_URL || 'http://127.0.0.1:4010';

function walk(dir) {
  const out = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, entry.name);
    if (entry.isDirectory()) out.push(...walk(p));
    else if (entry.name.endsWith('.json')) out.push(p);
  }
  return out;
}

function extractRequests(pact) {
  const reqs = [];
  const interactions = pact.interactions || pact.pacts || pact.contracts || [];
  for (const it of interactions) {
    const req = it.request || it;
    if (req && req.method && req.path) {
      reqs.push({ method: String(req.method).toUpperCase(), path: req.path, description: it.description || '' });
    }
  }
  return reqs;
}

let total = 0;
let unresolved = 0;
const files = walk(process.argv[2] || 'tests/pact');
if (files.length === 0) {
  console.error('::error::No Pact JSON files found under tests/pact');
  process.exit(1);
}

for (const file of files) {
  const pact = JSON.parse(fs.readFileSync(file, 'utf8'));
  for (const req of extractRequests(pact)) {
    total += 1;
    const url = `${PRISM}${req.path}`;
    try {
      const res = await fetch(url, {
        method: req.method,
        headers: { 'Content-Type': 'application/json', Prefer: 'code=200, example=first' },
      });
      const text = await res.text();
      const notResolved = text.includes('NO_ROUTE_MATCHED') || text.includes('No route matched');
      if (res.status === 404 && notResolved) {
        console.error(`FAIL ${req.method} ${req.path} - not resolvable by OpenAPI spec (${req.description})`);
        unresolved += 1;
      } else {
        console.log(`OK ${res.status} ${req.method} ${req.path} (${req.description})`);
      }
    } catch (e) {
      console.error(`FAIL ${req.method} ${req.path} - ${e.message}`);
      unresolved += 1;
    }
  }
}

console.log(`Replayed ${total} requests, ${unresolved} unresolved.`);
if (unresolved > 0) process.exit(1);
