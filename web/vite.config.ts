import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/runs': 'http://localhost:8080',
      '/secrets': 'http://localhost:8080',
      '/billing': 'http://localhost:8080',
      '/webhooks': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
    },
  },
});

