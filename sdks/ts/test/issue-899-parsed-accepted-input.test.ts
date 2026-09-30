import { describe, expect, it } from 'vitest';
import { normalizeErrorBody } from '../src/client.js';

describe('issue 899: parsed accepted input', () => {
  it('keeps a parsed error object and rejects arrays or null', () => {
    expect(normalizeErrorBody({ message: 'bad request' })).toEqual({ message: 'bad request' });
    expect(normalizeErrorBody(['bad'])).toBeUndefined();
    expect(normalizeErrorBody(null)).toBeUndefined();
  });
});
