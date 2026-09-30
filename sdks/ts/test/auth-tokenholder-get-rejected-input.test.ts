// (issue #828, TokenHolder.get() at sdks/ts/src/auth.ts:17)
//
// Focused regression guard for the `TokenHolder.get()` read path and the
// get()/hasToken() distinction. The line number in the issue title is stale;
// this suite pins the behaviour by content: `get()` is a raw read that never
// validates, sanitises, coerces, or throws, while `hasToken()` is the
// presence check that rejects empty strings and non-string values.
import { describe, expect, it } from 'vitest';

import { TokenHolder } from '../src/index.js';

describe('TokenHolder.get() rejected-input boundary (issue #828)', () => {
  it('returns undefined when constructed with no argument', () => {
    const holder = new TokenHolder();
    expect(holder.get()).toBeUndefined();
  });

  it('returns undefined when explicitly constructed with undefined', () => {
    const holder = new TokenHolder(undefined);
    expect(holder.get()).toBeUndefined();
  });

  it('returns the exact token it was constructed with', () => {
    const holder = new TokenHolder('tok');
    expect(holder.get()).toBe('tok');
  });

  it('performs no sanitisation: surrounding whitespace is returned verbatim', () => {
    const holder = new TokenHolder('  spaced  ');
    expect(holder.get()).toBe('  spaced  ');
  });

  it('echoes the empty string while hasToken() reports false, then tracks both transitions', () => {
    const holder = new TokenHolder('');
    expect(holder.get()).toBe('');
    expect(holder.hasToken()).toBe(false);

    holder.set('t');
    expect(holder.get()).toBe('t');
    expect(holder.hasToken()).toBe(true);

    holder.set(undefined);
    expect(holder.get()).toBeUndefined();
    expect(holder.hasToken()).toBe(false);
  });

  it('returns non-string rejected input verbatim without validation or throwing, while hasToken() is false', () => {
    const holder = new TokenHolder(123 as unknown as string);

    let value: unknown = 'sentinel';
    expect(() => {
      value = holder.get();
    }).not.toThrow();

    expect(value).toBe(123);
    expect(holder.get()).toBe(123 as unknown as string);
    expect(holder.hasToken()).toBe(false);
  });

  it('is deterministic across repeated reads, including after a set() round-trip', () => {
    const holder = new TokenHolder('stable');
    expect(holder.get()).toBe(holder.get());
    expect(holder.get()).toBe('stable');

    holder.set('rotated');
    expect(holder.get()).toBe(holder.get());
    expect(holder.get()).toBe('rotated');

    holder.set('stable');
    expect(holder.get()).toBe(holder.get());
    expect(holder.get()).toBe('stable');
  });
});
