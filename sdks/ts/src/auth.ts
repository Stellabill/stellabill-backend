/**
 * Holds a bearer token that is injected on every outbound request via the
 * `Authorization: Bearer <token>` header defined by the `bearerAuth`
 * security scheme in `openapi/openapi.yaml`.
 *
 * Mutable so callers can rotate tokens without recreating the client
 * (e.g. after a 401 / refresh).
 */
export class TokenHolder {
  #token: string | undefined;

  constructor(initialToken?: string) {
    this.#token = initialToken;
  }

  get(): string | undefined {
    return this.#token;
  }

  set(token: string | undefined): void {
    this.#token = token;
  }

  hasToken(): boolean {
    return typeof this.#token === 'string' && this.#token.length > 0;
  }
}

/**
 * Validate the shape of a bearer token. We only enforce non-empty + no
 * whitespace; the server is the source of truth for token validity.
 */
export function sanitizeToken(token: string | undefined): string | undefined {
  if (token === undefined) return undefined;
  if (typeof token !== 'string') return undefined;
  const trimmed = token.trim();
  if (trimmed.length === 0) return undefined;
  if (/\s/.test(trimmed)) return undefined;
  return trimmed;
}
