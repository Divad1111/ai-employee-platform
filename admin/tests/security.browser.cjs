const { chromium } = require(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const { createServer } = require('node:http')
const { readFile } = require('node:fs/promises')
const path = require('node:path')
const assert = require('node:assert/strict')
const { csp } = require('./browserSecurity.cjs')

;(async () => {
  const root = path.join(__dirname, '../dist')
  const secret = 'synthetic-http-only-cookie'
  const zip = Buffer.from('504b0506000000000000000000000000000000000000', 'hex')
  let loggedIn = false
  let imports = 0
  const server = createServer(async (req, res) => {
    res.setHeader('Content-Security-Policy', csp)
    const url = new URL(req.url, 'http://localhost')
    const json = (data, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(data)) }
    if (url.pathname.startsWith('/api/')) {
      assert.equal(req.headers.authorization, undefined, 'Browser leaked a bearer token')
      if (url.pathname === '/api/setup/status') return json({ initialized: true, needs_setup: false })
      if (url.pathname === '/api/auth/login') {
        assert.equal(req.headers['x-aie-browser'], '1')
        loggedIn = true
        res.setHeader('Set-Cookie', `aie_session=${secret}; HttpOnly; SameSite=Strict; Path=/`)
        return json({ token: '', user: { id: 'test', username: 'test', roles: ['SUPER_ADMIN'] } })
      }
      if (!loggedIn || !req.headers.cookie?.includes(`aie_session=${secret}`)) return json({ error: '未登录' }, 401)
      if (url.pathname === '/api/workflow-mcp/import') {
        // 与服务端一致：Cookie 写请求必须带浏览器校验头。
        if (req.headers['x-aie-browser'] !== '1') return json({ error: '浏览器请求来源校验失败' }, 403)
        assert.equal(req.method, 'POST')
        assert.equal(req.headers.origin, `http://${req.headers.host}`)
        assert.match(req.headers['content-type'], /^multipart\/form-data; boundary=/)
        const chunks = []
        for await (const chunk of req) chunks.push(chunk)
        const form = await new Request(`http://${req.headers.host}${req.url}`, {
          method: req.method, headers: req.headers, body: Buffer.concat(chunks),
        }).formData()
        const file = form.get('file')
        assert.equal(file.name, 'data.zip')
        assert.deepEqual(Buffer.from(await file.arrayBuffer()), zip)
        imports++
        return json({ status: 'ok', stats: { workflows: 0, skills: 0, knowledge: 0, errors: 0 } })
      }
      if (url.pathname === '/api/auth/logout') {
        assert.equal(req.headers['x-aie-browser'], '1')
        loggedIn = false
        res.setHeader('Set-Cookie', 'aie_session=; HttpOnly; SameSite=Strict; Path=/; Max-Age=0')
        return json({ status: 'ok' })
      }
      if (url.pathname === '/api/auth/me') return json({ id: 'test', username: 'test', roles: ['SUPER_ADMIN'], permissions: [{ name: '*', scope: 'ALL' }] })
      return json({ items: [] })
    }
    try {
      const requested = path.resolve(root, '.' + url.pathname)
      const filename = requested.startsWith(root + path.sep) && path.extname(requested) ? requested : path.join(root, 'index.html')
      const types = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.ttf': 'font/ttf' }
      res.setHeader('Content-Type', types[path.extname(filename)] || 'application/octet-stream')
      res.end(await readFile(filename))
    } catch { res.writeHead(404); res.end() }
  })
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  let browser
  try {
    browser = await chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome', headless: true })
    const context = await browser.newContext()
    const page = await context.newPage()
    const base = `http://127.0.0.1:${server.address().port}`
    await page.goto(base + '/login')
    await page.getByPlaceholder('请输入管理员账号').fill('test')
    await page.getByPlaceholder('请输入管理员密码').fill('test-password')
    await page.getByRole('button', { name: '立即登录', exact: true }).click()
    await page.getByRole('button', { name: '退出登录', exact: true }).waitFor()
    const cookies = await context.cookies()
    assert.equal(cookies.find(cookie => cookie.name === 'aie_session').httpOnly, true)
    assert.equal(await page.evaluate(() => document.cookie.includes('aie_session')), false)
    assert.equal(await page.evaluate(() => JSON.stringify(localStorage).includes('synthetic-http-only-cookie')), false)
    await page.reload()
    await page.getByRole('button', { name: '退出登录', exact: true }).waitFor()
    await page.goto(base + '/mcp-servers/workflow-mcp')
    const upload = async () => {
      const response = page.waitForResponse(res => res.url() === base + '/api/workflow-mcp/import' && res.request().method() === 'POST')
      await page.locator('input[type=file]').setInputFiles({ name: 'data.zip', mimeType: 'application/zip', buffer: zip })
      assert.equal((await response).status(), 200)
      await page.getByRole('button', { name: '导入 data.zip', exact: true }).waitFor()
    }
    await upload()
    await page.getByText('导入完成：工作流 0 / 技能包 0 / 知识 0', { exact: true }).waitFor()
    assert.equal(imports, 1)
    assert.equal(await page.locator('input[type=file]').inputValue(), '')
    // 同一文件可再次选择；导入成功后仍保留 Cookie 会话。
    await upload()
    await page.getByText('导入完成：工作流 0 / 技能包 0 / 知识 0', { exact: true }).waitFor()
    assert.equal(imports, 2)
    assert.equal(await page.evaluate(() => localStorage.getItem('aie_browser_session')), '1')
    await page.evaluate(() => { const script = document.createElement('script'); script.textContent = 'window.injectedScriptRan=true'; document.head.append(script) })
    assert.equal(await page.evaluate(() => window.injectedScriptRan), undefined, 'CSP allowed inline scripts')
    await page.getByRole('button', { name: '退出登录', exact: true }).click()
    await page.getByRole('button', { name: '立即登录', exact: true }).waitFor()
    assert.equal((await context.cookies()).some(cookie => cookie.name === 'aie_session'), false)
    console.log('PASS: cookie login, reload, multipart ZIP import with browser header, repeat import, no JS token access, logout, and enforced CSP')
  } finally { await browser?.close(); await new Promise(resolve => server.close(resolve)) }
})().catch(error => { console.error(error); process.exitCode = 1 })
