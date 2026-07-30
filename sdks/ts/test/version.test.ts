import { describe, expect, it } from 'vitest';

import { SDK_VERSION, defaultUserAgent } from '../src/index.js';

describe('SDK versioning', () => {
  it('SDK_VERSION matches semver', () => {
    expect(SDK_VERSION).toMatch(/^\d+\.\d+\.\d+$/);
  });
  it('defaultUserAgent includes the SDK version', () => {
    const ua = defaultUserAgent('20.10.0');
    expect(ua).toContain(`@stellabill/sdk/${SDK_VERSION}`);
    expect(ua).toContain('node/20.10.0');
  });
  it('defaultUserAgent uses process.versions.node by default', () => {
    const ua = defaultUserAgent();
    expect(ua).toMatch(/node\/\d+\.\d+\.\d+/);
  });
});
