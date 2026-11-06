// path: web/vite.config.ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/runs': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/secrets': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/billing': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/webhooks': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/healthz': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/version': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
});
