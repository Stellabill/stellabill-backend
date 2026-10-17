import { parseErrorBodyText, normalizeErrorBody } from './client';

describe('parseErrorBodyText rejected input', () => {
  it('returns undefined for empty string', () => {
    expect(parseErrorBodyText('')).toBeUndefined();
  });

  it('returns undefined for undefined input', () => {
    expect(parseErrorBodyText(undefined)).toBeUndefined();
  });

  it('returns undefined for null input', () => {
    expect(parseErrorBodyText(null)).toBeUndefined();
  });

  it('returns undefined for malformed JSON', () => {
    expect(parseErrorBodyText('{ not json')).toBeUndefined();
  });

  it('returns undefined for JSON arrays', () => {
    expect(parseErrorBodyText('[1,2,3]')).toBeUndefined();
  });

  it('returns undefined for JSON null', () => {
    expect(parseErrorBodyText('null')).toBeUndefined();
  });

  it('returns undefined for JSON primitives', () => {
    expect(parseErrorBodyText('42')).toBeUndefined();
    expect(parseErrorBodyText('"tstring"')).toBeUndefined();
    expect(parseErrorBodyText('true')).toBeUndefined();
  });

  it('returns undefined for whitespace-only input', () => {
    expect(parseErrorBodyText('   ')).toBeUndefined();
    expect(parseErrorBodyText('\n\t')).toBeUndefined();
  });

  it('returns the parsed object for valid JSON object', () => {
    const body = parseErrorBodyText('{"message":"boom","code":"ERR"}');
    expect(body).toEqual({ message: 'boom', code: 'ERR' });
  });
});

describe('normalizeErrorBody', () => {
  it('returns the object for plain objects', () => {
    const obj = { message: 'x' };
    expect(normalizeErrorBody(obj)).toBe(obj);
  });

  it('returns undefined for null, arrays, and primitives', () => {
    expect(normalizeErrorBody(null)).toBeUndefined();
    expect(normalizeErrorBody([])).toBeUndefined();
    expect(normalizeErrorBody('str')).toBeUndefined();
    expect(normalizeErrorBody(42)).toBeUndefined();
  });
});
