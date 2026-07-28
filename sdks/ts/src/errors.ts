/**
 * Typed error envelope returned by the Stellabill backend (see
 * `openapi/openapi.yaml` -> `components.schemas.Error`).
 */
export interface ApiErrorBody {
  error?: string;
  message?: string;
  code?: string;
}

/**
 * Base SDK error. Thrown by helpers like `assertOk` and `.throwOnError()`
 * when the API returns a non-2xx response.
 */
export class StellarBillError extends Error {
  public readonly status: number;
  public readonly body: ApiErrorBody | undefined;
  public readonly requestUrl: string;
  public readonly requestMethod: string;

  constructor(opts: {
    status: number;
    body: ApiErrorBody | undefined;
    requestUrl: string;
    requestMethod: string;
    message: string;
  }) {
    super(opts.message);
    this.name = 'StellarBillError';
    this.status = opts.status;
    this.body = opts.body;
    this.requestUrl = opts.requestUrl;
    this.requestMethod = opts.requestMethod;
    // Reset prototype chain for `instanceof` to work after transpilation
    Object.setPrototypeOf(this, StellarBillError.prototype);
  }

  /**
   * Best-effort text serialization for logging.
   */
  override toString(): string {
    const code = this.body?.code ?? 'unknown';
    return `StellarBillError [${this.requestMethod} ${this.requestUrl} -> ${this.status} (${code})]: ${this.message}`;
  }
}

/**
 * Error thrown when caller-configured inputs are invalid (e.g. missing
 * baseUrl, malformed token). These are programmer errors and never come
 * from the API itself.
 */
export class StellarBillConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'StellarBillConfigError';
    Object.setPrototypeOf(this, StellarBillConfigError.prototype);
  }
}
