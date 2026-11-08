const { readFileSync } = require('node:fs')
const path = require('node:path')

const csp = readFileSync(path.join(__dirname, '../../deploy/Caddyfile'), 'utf8').match(/Content-Security-Policy "([^"]+)"/)[1]

// 使用实际部署策略验证构建产物，防止编辑器或 Worker 被 CSP 意外阻断。
async function enforceCSP(page, errors) {
  await page.exposeFunction('recordCSPViolation', text => errors.push(`CSP: ${text}`))
  await page.addInitScript(() => document.addEventListener('securitypolicyviolation', event => window.recordCSPViolation(`${event.violatedDirective}: ${event.blockedURI}`)))
  await page.route(url => !url.pathname.startsWith('/api/'), async route => {
    const response = await route.fetch()
    await route.fulfill({ response, headers: { ...response.headers(), 'content-security-policy': csp } })
  })
}
module.exports = { csp, enforceCSP }
