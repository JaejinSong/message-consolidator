import { defineConfig } from 'vite';
import tsconfigPaths from 'vite-tsconfig-paths';

export default defineConfig({
  root: './',
  plugins: [tsconfigPaths()],
  server: {
    host: true,
    port: 5173,
    proxy: {
      // 1. API 요청 처리
      '/api': {
        target: 'https://34.67.133.18.nip.io',
        changeOrigin: true,
        secure: false, // SSL 검증 무시
        cookieDomainRewrite: ""
      },
      // 2. 인증 요청 처리
      '/auth': {
        target: 'https://34.67.133.18.nip.io',
        changeOrigin: true,
        secure: false,
        cookieDomainRewrite: ""
      }
    }
  },
  build: {
    outDir: 'dist',
    rollupOptions: {
      input: {
        main: './index.html'
      }
    }
  }
});