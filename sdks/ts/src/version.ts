/**
 * SDK version. Keep in lock-step with `openapi/openapi.yaml`'s `info.version`.
 * Bumped by the release pipeline on each tagged release.
 */
export const SDK_VERSION = '0.2.0';

/**
 * The user-agent identifier sent on every request. Format:
 *   @stellabill/sdk/<version> node/<node-version>
 */
export function defaultUserAgent(nodeVersion: string = process.versions.node): string {
  return `@stellabill/sdk/${SDK_VERSION} node/${nodeVersion}`;
}
