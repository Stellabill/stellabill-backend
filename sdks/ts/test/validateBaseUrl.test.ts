import { describe, expect, it } from 'vitest';
import { StellarBillConfigError } from '../src/errors.js';
import { createStellarBillClient } from '../src/index.js';

describe('validateBaseUrl rejection paths via client creation', () => {
  const invalidInputs: [string, unknown, string][] = [
    ['undefined baseUrl', undefined as any, 'baseUrl is required'],
    ['null baseUrl', null as any, 'baseUrl is required'],
    ['non-string baseUrl (number)', 123 as any, 'baseUrl must be a non-empty string'],
    ['empty string', '', 'baseUrl must be a non-empty string'],
    ['whitespace only', '   ', 'baseUrl must be a non-empty string'],
    ['malformed URL', 'ht!tp://bad', 'baseUrl "ht!tp://bad" is not a valid URL'],
  ];

  for (const [caseName, input, expectedMessage] of invalidInputs) {
    it(`throws StellarBillConfigError for ${caseName}`, () => {
      expect(() =>
        // @ts-expect-error intentionally passing bad type
        createStellarBillClient({ baseUrl: input as any })
      ).toThrowError(new StellarBillConfigError(expectedMessage));
    });
  }

  it('normalizes WHATWG URL parser failures into the stable SDK config error', () => {
    expect(() =>
      createStellarBillClient({
        baseUrl: 'https://[::1',
        fetch: async () => new Response(),
      })
    ).toThrowError(
      new StellarBillConfigError('baseUrl "https://[::1" is not a valid URL')
    );
  });
});
