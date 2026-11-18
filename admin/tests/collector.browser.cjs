const { chromium } = require(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const { createServer } = require('node:http')
const { readFile, mkdir } = require('node:fs/promises')
const path = require('node:path')
const assert = require('node:assert/strict')
const { csp } = require('./browserSecurity.cjs')

;(async () => {
  const root = path.join(__dirname, '../dist')
  const user = { id: 'alice', username: 'alice', display_name: '测试用户', roles: ['ADMIN'] }
  const targets = []
  const rules = []
  const runs = []
  let savedProfile
  let profile = {
    display_name: '测试用户', aliases: ['Alice'], timezone: 'Asia/Shanghai',
    scopes: 'offline_access docx:document:readonly', granted_scopes: 'docx:document:readonly',
    authorized: true, needs_reauthorization: false, feishu_name: '测试用户', feishu_open_id: 'ou_test',
    has_refresh_token: true, platform_app_available: true, has_app_secret: true,
    redirect_uri: 'https://platform.example/api/collector/oauth/callback',
    capabilities: [{ name: '云文档读取', status: 'granted' }, { name: '云文档编辑', status: 'not_granted' }],
  }
  const server = createServer(async (req, res) => {
    res.setHeader('Content-Security-Policy', csp)
    const url = new URL(req.url, 'http://localhost')
    const json = (value, status = 200) => {
      res.writeHead(status, { 'Content-Type': 'application/json' })
      res.end(JSON.stringify(value))
    }
    const body = async () => {
      const chunks = []
      for await (const chunk of req) chunks.push(chunk)
      return JSON.parse(Buffer.concat(chunks) || '{}')
    }
    if (url.pathname.startsWith('/api/')) {
      if (url.pathname === '/api/setup/status') return json({ initialized: true, needs_setup: false })
      if (url.pathname === '/api/auth/login') {
        res.setHeader('Set-Cookie', 'aie_session=synthetic; HttpOnly; SameSite=Strict; Path=/')
        return json({ token: '', user })
      }
      if (!req.headers.cookie?.includes('aie_session=synthetic')) return json({ error: '未登录' }, 401)
      if (url.pathname === '/api/auth/me') return json({ ...user, permissions: [{ name: '*', scope: 'ALL' }] })
      if (url.pathname === '/api/users') return json({ items: [user, { id: 'bob', username: 'bob', display_name: '另一位用户' }] })
      if (url.pathname === '/api/collector/profile') {
        if (req.method === 'PUT') {
          savedProfile = await body()
          assert.equal(savedProfile.granted_scopes, undefined)
          assert.equal(savedProfile.secret_ref, undefined)
          profile = { ...profile, ...savedProfile }
          delete profile.credentials
        }
        return json(profile)
      }
      if (url.pathname === '/api/collector/discover') return json({
        type: 'sheet', sheets: [{ sheet_id: 'tab1', title: '研发计划' }],
        sample_rows: [['单号', '标题', '开发人', '状态', '截止日期']],
      })
      if (url.pathname === '/api/collector/preview') return json({
        count: 2, changes: 2, status: 'success', preview: true, events: [],
        samples: [{ id: 'p1', title: '完善采集规则', status: '进行中', due_date: '2026-11-20' }],
      })
      if (url.pathname === '/api/collector/targets' && req.method === 'POST') {
        const target = await body()
        assert.equal(target.config.identity, 'user')
        target.id = 'target-1'; target.version = 1
        targets.push(target)
        return json(target)
      }
      if (url.pathname === '/api/collector/targets/target-1/scan') {
        const run = { id: 'run1', target_id: 'target-1', target_name: targets[0].name, source: 'sheet', status: 'queued', count: 0, changes: 0, started_at: new Date().toISOString(), events: [], samples: [] }
        runs.unshift(run)
        return json(run, 202)
      }
      if (url.pathname === '/api/collector/rules' && req.method === 'POST') {
        const rule = await body(); rule.id = 'rule1'; rules.push(rule); return json(rule)
      }
      if (url.pathname === '/api/collector/rules/preview') return json({ items: [] })
      if (url.pathname === '/api/collector/items') return json({ items: [] })
      if (url.pathname.startsWith('/api/collector/records/')) {
        const kind = url.pathname.split('/').pop()
        const rows = url.searchParams.get('owner_id') === 'bob' ? [] : ({ target: targets, rule: rules, run: runs, delivery: [] }[kind] || [])
        return json({ items: rows, total: rows.length })
      }
      return json({ items: [] })
    }
    try {
      const requested = path.resolve(root, '.' + url.pathname)
      const filename = requested.startsWith(root + path.sep) && path.extname(requested) ? requested : path.join(root, 'index.html')
      res.setHeader('Content-Type', { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css' }[path.extname(filename)] || 'application/octet-stream')
      res.end(await readFile(filename))
    } catch { res.writeHead(404); res.end() }
  })
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  let browser
  try {
    browser = await chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL || 'chrome', headless: true })
    const page = await browser.newPage({ viewport: { width: 1536, height: 1100 } })
    const errors = []
    page.on('pageerror', e => errors.push(e.message))
    const base = `http://127.0.0.1:${server.address().port}`
    await page.goto(base + '/login')
    await page.getByPlaceholder('请输入管理员账号').fill('alice')
    await page.getByPlaceholder('请输入管理员密码').fill('synthetic')
    await page.getByRole('button', { name: '立即登录', exact: true }).click()
    await page.getByRole('button', { name: '退出登录', exact: true }).waitFor()
    await page.goto(base + '/mcp-servers/personal-work-collector')
    await page.getByRole('heading', { name: '工作采集中心', exact: true }).waitFor()
    await page.getByRole('button', { name: '接入与身份', exact: true }).click()
    await page.getByText('范围已授权', { exact: true }).waitFor()
    const secret = page.getByLabel('个人应用 App Secret', { exact: true })
    assert.equal(await secret.inputValue(), '')
    await secret.fill('synthetic-personal-secret')
    await page.getByRole('button', { name: '保存接入设置', exact: true }).click()
    await page.getByText('个人接入设置已保存。', { exact: true }).waitFor()
    assert.equal(savedProfile.credentials.app_secret, 'synthetic-personal-secret')
    assert.equal(await secret.inputValue(), '')
    await page.getByRole('button', { name: '＋ 添加扫描目标', exact: true }).click()
    const editor = page.getByRole('dialog', { name: '配置扫描目标' })
    await editor.getByLabel('名称', { exact: true }).fill('研发周计划')
    await editor.getByLabel('资源链接', { exact: true }).fill('https://test.feishu.cn/sheets/test')
    await editor.getByRole('button', { name: '读取资源结构', exact: true }).click()
    await editor.getByLabel('研发计划 tab1').check()
    await editor.getByRole('button', { name: '预览本次扫描', exact: true }).click()
    await editor.getByText('找到 2 条记录', { exact: true }).waitFor()
    assert.equal(targets.length, 0, 'Preview must not save a target')
    if (process.env.COLLECTOR_SCREENSHOT_DIR) {
      await mkdir(process.env.COLLECTOR_SCREENSHOT_DIR, { recursive: true })
      await page.screenshot({ path: path.join(process.env.COLLECTOR_SCREENSHOT_DIR, 'collector-target.png'), animations: 'disabled' })
    }
    await editor.getByRole('button', { name: '保存扫描目标', exact: true }).click()
    await editor.waitFor({ state: 'hidden' })
    assert.equal(targets[0].enabled, false)
    await page.getByRole('button', { name: '扫描目标', exact: true }).click()
    await page.getByRole('button', { name: '立即扫描', exact: true }).click()
    await page.getByText('已加入扫描队列，运行结果会自动刷新。', { exact: true }).waitFor()
    await page.getByText('排队中 · 0 条', { exact: true }).waitFor()
    await page.getByRole('button', { name: '规则与通知', exact: true }).click()
    await page.getByRole('button', { name: '＋ 新建规则', exact: true }).click()
    const rule = page.getByRole('dialog', { name: '编辑通知规则' })
    await rule.getByLabel('名称', { exact: true }).fill('状态变化时提醒')
    await rule.getByRole('button', { name: '＋ 添加条件', exact: true }).click()
    await rule.getByLabel('命中后').selectOption('batched')
    await rule.getByRole('button', { name: '保存规则', exact: true }).click()
    await rule.waitFor({ state: 'hidden' })
    assert.equal(rules[0].conditions[0].field, 'status')
    assert.equal(rules[0].action, 'batched')
    await page.getByLabel('采集配置所属用户').selectOption('bob')
    await page.getByText('先定义一条关注规则', { exact: true }).waitFor()
    assert.equal(await page.getByText('状态变化时提醒', { exact: true }).count(), 0)
    await page.getByLabel('采集配置所属用户').selectOption('')
    await page.getByRole('button', { name: '概览', exact: true }).click()
    await page.getByText('研发周计划', { exact: true }).waitFor()
    assert.equal(await page.locator('.col-tabs .active').innerText(), '概览')
    if (process.env.COLLECTOR_SCREENSHOT_DIR) await page.screenshot({ path: path.join(process.env.COLLECTOR_SCREENSHOT_DIR, 'collector-overview.png'), fullPage: true, animations: 'disabled' })
    await page.setViewportSize({ width: 390, height: 844 })
    assert.ok(await page.locator('.collector-page').evaluate(el => el.getBoundingClientRect().width) < 400)
    assert.deepEqual(errors, [])
    console.log('Collector browser tests passed: personal credentials, target discovery/preview/save, queued scans, rules, owner isolation, responsive layout.')
  } finally {
    if (browser) await browser.close()
    await new Promise(resolve => server.close(resolve))
  }
})().catch(err => { console.error(err); process.exit(1) })
