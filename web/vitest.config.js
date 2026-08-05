import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

function getApiProxyTarget() {
  return process.env.VITE_API_PROXY_TARGET?.trim() || 'http://127.0.0.1:8080';
}

export default defineConfig(({ command }) => ({
  base: './',
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
    },
  },
  server: command === 'serve' ? {
    proxy: {
      '/api': {
        target: getApiProxyTarget(),
        changeOrigin: true,
      },
    },
  } : undefined,
  // CI runners under load can take longer than the 5s default for tests that
  // exercise a modal focus sequence (RankingPage uses a setTimeout helper to
  // wait for the next task to set initial focus; React act() + happy-dom can
  // easily exceed 5s when many modal helpers stack in a single test).
  test: {
    testTimeout: 15_000,
    hookTimeout: 15_000,
  },
}));
