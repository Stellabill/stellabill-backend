import { createStellarBillClient } from '../src/client.js';
import { StellarBillConfigError } from '../src/errors.js';

const duration = process.env.FUZZ_TIME ?? '60s';
const requestedSeed = Number(process.env.SDK_FUZZ_SEED ?? Date.now());
const seed = Number.isFinite(requestedSeed) ? requestedSeed >>> 0 : 1;
const durationMatch = /^(\d+(?:\.\d+)?)(ms|s|m|h)$/.exec(duration);

if (!durationMatch) {
  throw new Error(`Unsupported FUZZ_TIME value: ${duration}`);
}

const durationScale: Record<string, number> = { ms: 1, s: 1_000, m: 60_000, h: 3_600_000 };
const durationUnit = durationScale[durationMatch[2]!];
if (durationUnit === undefined) {
  throw new Error(`Unsupported FUZZ_TIME unit: ${durationMatch[2]}`);
}
const durationMs = Number(durationMatch[1]) * durationUnit;
const random = createRandom(seed);
const schemes = ['http', 'https', 'HtTp', 'ftp', 'file', 'javascript', 'data', 'ws'];
const knownInputs = [
  '',
  ' ',
  'http://',
  'https://api.example.com',
  'http://localhost:8080///',
  'ftp://api.example.com',
  'file:///tmp/api',
  'javascript:alert(1)',
  'https://example.com:65536',
  '\u0000https://api.example.com',
];

function createRandom(initialSeed: number): () => number {
  let state = initialSeed;
  return () => {
    state = (state + 0x6d2b79f5) | 0;
    let value = state;
    value = Math.imul(value ^ (value >>> 15), value | 1);
    value ^= value + Math.imul(value ^ (value >>> 7), value | 61);
    return ((value ^ (value >>> 14)) >>> 0) / 4_294_967_296;
  };
}

function pick<T>(values: T[]): T {
  return values[Math.floor(random() * values.length)]!;
}

function randomAscii(): string {
  const length = Math.floor(random() * 256);
  let value = '';
  for (let index = 0; index < length; index += 1) {
    value += String.fromCharCode(Math.floor(random() * 128));
  }
  return value;
}

function nextInput(): string {
  switch (Math.floor(random() * 5)) {
    case 0:
      return pick(knownInputs);
    case 1:
      return `${pick(schemes)}://${randomAscii()}`;
    case 2:
      return `${pick(schemes)}:${randomAscii()}`;
    case 3:
      return `${pick(['http', 'https'])}://${pick(['localhost', '127.0.0.1', 'api.example.com'])}:${Math.floor(random() * 70_000)}${randomAscii()}`;
    default:
      return randomAscii();
  }
}

function shouldAccept(value: string): boolean {
  if (value.trim().length === 0) return false;
  try {
    const protocol = new URL(value).protocol;
    return protocol === 'http:' || protocol === 'https:';
  } catch {
    return false;
  }
}

function checkInput(value: string, caseNumber: number): void {
  const expected = shouldAccept(value);
  let accepted = false;
  try {
    createStellarBillClient({ baseUrl: value, fetch: async () => new Response(null) });
    accepted = true;
  } catch (error) {
    if (!(error instanceof StellarBillConfigError)) throw error;
  }
  if (accepted !== expected) {
    throw new Error(
      `baseUrl fuzz mismatch (seed=${seed}, case=${caseNumber}, accepted=${accepted}, input=${JSON.stringify(value)})`,
    );
  }
}

const originalWarn = console.warn;
console.warn = () => {};
try {
  let cases = 0;
  for (const input of knownInputs) {
    checkInput(input, cases);
    cases += 1;
  }

  const deadline = Date.now() + durationMs;
  while (Date.now() < deadline) {
    checkInput(nextInput(), cases);
    cases += 1;
  }
  console.log(`baseUrl fuzz passed: ${cases} cases, seed=${seed}, duration=${duration}`);
} finally {
  console.warn = originalWarn;
}