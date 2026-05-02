import { defineConfig } from 'vitest/config';
import path from 'path';

export default defineConfig({
  test: {
    // Why: Default to node — happy-dom boot ~1.5s per file × 16 non-DOM files = wasted ~24s cumulative.
    // The 6 DOM-needing files override per-file via `// @vitest-environment happy-dom` directive.
    environment: 'node',
    globals: true,
    setupFiles: ['./src/tests/setup.ts'],
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  define: {
    'import.meta.env.VITE_API_BASE_URL': JSON.stringify('/api'),
  },
});
