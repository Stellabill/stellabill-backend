import { defineConfig } from 'tsup';

export default defineConfig({
  entry: ['src/index.ts'],
  format: ['esm', 'cjs'],
  dts: true,
  splitting: false,
  sourcemap: true,
  clean: true,
  target: 'node20',
  outExtension({ format }) {
    return { js: format === 'cjs' ? '.cjs' : '.js' };
  },
  // Keep openapi-fetch as a peer-style dependency so we don't bundle it
  // (peer-deps tree stays clean and consumers share a single copy).
  external: ['openapi-fetch'],
});
