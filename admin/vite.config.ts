/// <reference types="vite/client" />

/**
 * Vite 构建配置。
 * Admin 仅通过 REST/SSE 访问 Control Plane，禁止直连数据库。
 */
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

const rootDir = path.dirname(fileURLToPath(import.meta.url))

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(rootDir, 'src'),
    },
  },
  server: {
    port: 5173,
    proxy: {
      // 开发期将 /api 代理到本地 Control Plane
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
})
