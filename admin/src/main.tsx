/**
 * Admin 前端入口。
 * 挂载根组件并启用路由。
 */
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { AppRouter } from './router'
import './styles.css'

const rootEl = document.getElementById('root')
if (!rootEl) {
  throw new Error('未找到 #root 挂载点')
}

createRoot(rootEl).render(
  <StrictMode>
    <AppRouter />
  </StrictMode>,
)
