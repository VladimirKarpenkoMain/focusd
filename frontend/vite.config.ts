import { defineConfig } from 'vite';

// Wails ждёт итоговую сборку в frontend/dist.
export default defineConfig({
  base: './',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2020',
  },
  server: {
    port: 34115,
    strictPort: true,
  },
});
