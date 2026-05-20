import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      // Trace APIs → serve-web.py :8081
      // serve-web.py is the sole authority for trace data (ClickHouse agentshield.spans).
      // Prefix match covers /api/v1/traces, /api/v1/traces/<id>, /api/v1/traces/by-agent.
      '/api/v1/traces': 'http://localhost:8081',
      // Family group aggregation → serve-web.py :8081
      // serve-web.py aggregates Go :8080 /api/v1/family-groups + /api/v1/agents into a single response.
      '/api/v1/family-groups-with-agents': 'http://localhost:8081',
      // All other API → management-server Go backend :8080
      // Includes /api/v1/audit/events, /api/v1/alerts, /api/v1/policies, /api/v1/agents, etc.
      '/api': 'http://localhost:8080',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
});
