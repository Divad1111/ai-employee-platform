// Offline regression: a refreshed browser must recover its identity before
// loading personal settings. API fixtures never touch a real user account.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const assert = require('node:assert/strict')
const { readFileSync } = require('node:fs')
const path = require('node:path')
const http = require('node:http')

;(async () => {
  let browser
  const dist = path.join(__dirname, '../dist')
  const server = http.createServer((req, res) => {
    const pathname = new URL(req.url, 'http://localhost').pathname
    const asset = pathname.startsWith('/assets/')
    const file = asset ? path.join(dist, pathname) : path.join(dist, 'index.html')
    res.setHeader('Content-Type', asset ? pathname.endsWith('.css') ? 'text/css' : 'text/javascript' : 'text/html')
    res.end(readFileSync(file))
  })
  try {
    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
    const base = `http://127.0.0.1:${server.address().port}`
    browser = await chromium.launch(process.env.PLAYWRIGHT_EXECUTABLE_PATH
      ? { executablePath: process.env.PLAYWRIGHT_EXECUTABLE_PATH, headless: true, args: ['--no-sandbox'] }
      : { channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome', headless: true })
    for (const role of ['USER', 'VIEWER', 'SUPER_ADMIN']) {
      const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } })
      const page = await context.newPage()
      const errors = [], requests = [], patches = []
      const admin = role === 'SUPER_ADMIN'
      const me = { id: 'profile-owner', username: 'alice', roles: [role] }
      const baseLimit = admin ? 50_000_000 : role === 'VIEWER' ? 1_000_000 : 5_000_000
      const defaultQuota = { period_key: '2026-11', source_role: admin ? 'ADMIN' : role, role_token_limit: baseLimit,
        token_limit: baseLimit, tokens_used: 12345, requests_used: 2, remaining: baseLimit - 12345,
        unlimited: false, has_exception: false, exception_token_limit: 0, extra_token_limit: 0 }
      let myQuota = { ...defaultQuota }
      let policies = admin ? ['VIEWER', 'USER', 'OPERATOR', 'ADMIN'].map(name => ({ id: name, resource_type: 'ROLE', resource_id: name,
        period_type: 'MONTHLY', token_limit: name === 'ADMIN' ? 50_000_000 : name === 'VIEWER' ? 1_000_000 : 5_000_000, enabled: true })) : []
      await require('./browserSecurity.cjs').enforceCSP(page, errors)
      page.on('pageerror', error => errors.push(error.message))
      await page.addInitScript(() => localStorage.setItem('aie_browser_session', '1'))
      await page.route(url => url.pathname.startsWith('/api/'), async route => {
        const request = route.request()
        const pathname = new URL(request.url()).pathname
        requests.push(pathname)
        if (request.method() === 'PATCH') {
          patches.push({ path: pathname, body: request.postDataJSON() })
          return route.fulfill({ json: {} })
        }
        if (pathname === '/api/auth/me') {
          // Hydration occurs asynchronously, with no in-memory login identity.
          await new Promise(resolve => setTimeout(resolve, 30))
          return route.fulfill({ json: { ...me, permissions: admin ? [{ name: '*', scope: 'ALL' }] : role === 'USER' ? [{ name: 'user.read', scope: 'OWN' }, { name: 'user.update', scope: 'OWN' }, { name: 'quota.read', scope: 'OWN' }] : [] } })
        }
        if (pathname === '/api/quotas/me') return route.fulfill({ json: myQuota })
        if (pathname === '/api/quotas') return route.fulfill({ json: { items: policies } })
        if (pathname === '/api/users/profile-owner' || pathname === '/api/users/other-user') {
          return route.fulfill({ json: { ...(pathname.endsWith('other-user') ? { id: 'other-user', username: 'bob', roles: ['USER'] } : me), display_name: '', status: 'active', email: '', employees: [], workstations: [] } })
        }
        return route.fulfill({ json: { items: [] } })
      })
      await page.goto(base + '/profile')
      await page.getByRole('heading', { name: '个人设置', exact: true }).waitFor()
      await page.getByRole('button', { name: '修改密码', exact: true }).waitFor()
      assert.equal(await page.locator('.sidebar .user-meta .name').innerText(), 'alice')
      assert.equal(await page.locator('.nav-group a[href="/profile"]').count(), 0)
      const settings = page.locator('.user-profile-link')
      assert.equal(await settings.getAttribute('href'), '/profile')
      assert.equal(await page.locator('.nav-group').getByRole('link', { name: '用户管理', exact: true }).count(), admin ? 1 : 0)
      assert(!requests.includes('/api/users/undefined'))
      assert(!requests.includes('/api/users'))
      const gear = page.getByRole('link', { name: '打开个人设置', exact: true })
      const gearBox = await gear.boundingBox(), nameBox = await page.locator('.user-meta').boundingBox()
      assert(gearBox.x >= nameBox.x + nameBox.width, 'settings gear must sit to the right of user name')
      assert(gearBox.y + gearBox.height <= 1000, 'settings gear must stay inside sidebar viewport')
      await page.reload()
      await page.getByRole('button', { name: '修改密码', exact: true }).waitFor()

      await page.goto(base + '/quotas')
      const summary = page.getByRole('region', { name: '我的配额' })
      await summary.waitFor()
      assert.equal(await summary.locator('.quota-summary-item strong').nth(0).innerText(), baseLimit.toLocaleString('zh-CN'))
      assert.equal(await summary.locator('.quota-summary-item strong').nth(1).innerText(), '12,345')
      assert.equal(await summary.locator('.quota-summary-item strong').nth(2).innerText(), (baseLimit - 12345).toLocaleString('zh-CN'))
      if (admin) {
        const userPreset = page.getByRole('row').filter({ hasText: '普通用户角色默认月度 Token' })
        assert.equal(await userPreset.locator('input').inputValue(), '5000000')
        policies = []
        await page.getByRole('button', { name: '刷新', exact: true }).click()
        await page.getByText('暂无角色预设配额', { exact: true }).waitFor()
        assert.equal(await page.getByRole('row').filter({ hasText: '普通用户角色默认月度 Token' }).count(), 0, 'do not invent presets for an empty API result')
      } else {
        assert.equal(await page.getByRole('heading', { name: '角色预设配额' }).count(), 0)
        assert.equal(await page.getByRole('heading', { name: '新增 / 更新策略' }).count(), 0)
        assert(!requests.includes('/api/users'))
        assert(!requests.includes('/api/roles'))
        if (role === 'USER') {
          assert((await summary.innerText()).includes('普通用户'))
          if (process.env.PROFILE_UI_SCREENSHOT) await page.screenshot({ path: path.join(path.dirname(process.env.PROFILE_UI_SCREENSHOT), 'quota-user-desktop.png'), fullPage: true })
        }
        myQuota = { ...defaultQuota, has_exception: true, exception_token_limit: 7_000_000, extra_token_limit: 250_000,
          token_limit: 7_250_000, remaining: 7_237_655 }
        await page.getByRole('button', { name: '刷新', exact: true }).click()
        await summary.getByText('7,250,000', { exact: true }).waitFor()
        assert((await summary.innerText()).includes('7,000,000（替换角色额度）'))
        assert((await summary.innerText()).includes('250,000'))
        myQuota = { ...myQuota, unlimited: true, token_limit: 0, exception_token_limit: 0, remaining: 0 }
        await page.getByRole('button', { name: '刷新', exact: true }).click()
        await summary.locator('.quota-summary-item strong').nth(0).filter({ hasText: /^不限$/ }).waitFor()
        await page.setViewportSize({ width: 390, height: 844 })
        assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), 'quota page mobile overflow')
        await page.setViewportSize({ width: 1440, height: 1000 })
      }
      await gear.click()
      await page.getByRole('heading', { name: '个人设置', exact: true }).waitFor()
      assert.equal(await page.locator('.topbar-breadcrumb .current').innerText(), '个人设置')
      if (!admin) {
        assert.equal(await page.getByRole('link', { name: '← 返回用户列表' }).count(), 0)
        await page.setViewportSize({ width: 390, height: 844 })
        await settings.click()
        const overflow = await page.evaluate(() => ({ width: window.innerWidth, actual: document.documentElement.scrollWidth,
          elements: [...document.querySelectorAll('body *')].filter(el => el.getBoundingClientRect().right > window.innerWidth).slice(0, 8).map(el => ({ tag: el.tagName, class: el.className, right: el.getBoundingClientRect().right })) }))
        assert(overflow.actual <= overflow.width, `mobile overflow: ${JSON.stringify(overflow)}`)
        if (process.env.PROFILE_UI_SCREENSHOT && role === 'USER') {
          await page.setViewportSize({ width: 1440, height: 1000 })
          await page.screenshot({ path: process.env.PROFILE_UI_SCREENSHOT, fullPage: true })
        }
      }
      await page.getByLabel('新密码', { exact: true }).fill('OfflineFixture123!')
      await page.getByLabel('确认新密码', { exact: true }).fill('DifferentFixture123!')
      await page.getByRole('button', { name: '修改密码', exact: true }).click()
      await page.getByRole('alert').waitFor()
      assert.equal(patches.length, 0)
      await page.getByLabel('确认新密码', { exact: true }).fill('OfflineFixture123!')
      await page.getByRole('button', { name: '修改密码', exact: true }).click()
      await page.getByRole('button', { name: '立即登录', exact: true }).waitFor()
      assert.deepEqual(patches, [{ path: '/api/users/profile-owner', body: { password: 'OfflineFixture123!' } }])
      assert.equal(await page.evaluate(() => localStorage.getItem('aie_browser_session')), null)
      if (admin) {
        await page.goto(base + '/users/other-user')
        await page.getByRole('button', { name: '重置密码', exact: true }).waitFor()
        assert.equal(await page.getByRole('heading', { name: 'bob', exact: true }).count(), 1)
      }
      assert.deepEqual(errors, [])
      await context.close()
    }
    console.log('PASS: sidebar settings gear, personal settings after reload, own quota/usage, exception/bonus/unlimited quota, real admin role presets, no fake fallback, mobile layout, password confirmation, self logout, and admin reset form')
  } finally {
    if (browser) await browser.close()
    await new Promise(resolve => server.close(resolve))
  }
})().catch(error => { console.error(error); process.exitCode = 1 })
