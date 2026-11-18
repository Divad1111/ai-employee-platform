const { chromium } = require(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const { createServer } = require('node:http')
const { readFile } = require('node:fs/promises')
const path = require('node:path')
const assert = require('node:assert/strict')
const { csp } = require('./browserSecurity.cjs')

;(async () => {
  const root = path.join(__dirname, '../dist')
  const user = { id: 'export-user', username: 'export-test', roles: ['ADMIN'] }
  const registrations = [
    { id: 'mcp-workflow', name: 'workflow-mcp', server_type: 'builtin', transport: 'http', endpoint: '/mcp', status: 'active' },
    { id: 'remote', name: 'remote-mcp', server_type: 'custom', transport: 'http', endpoint: 'https://remote.example/mcp', status: 'active',
      config: { headers: { Authorization: 'Token synthetic-upstream-token' }, args: ['preserved-argument'] } },
    { id: 'mcp-collector', name: 'personal-work-collector', server_type: 'builtin', transport: 'http', endpoint: '/mcp/collector', status: 'active' },
  ]
  const tokens = []
  let exports = 0
  let exportError = false
  let hasExportPermission = true
  let serverUpdated = false
  const policies = { 'export-user': true }
  const collectorPolicies = {}
  const server = createServer(async (req, res) => {
    res.setHeader('Content-Security-Policy', csp)
    const url = new URL(req.url, 'http://localhost')
    const json = (data, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json' }); res.end(JSON.stringify(data)) }
    if (url.pathname.startsWith('/api/')) {
      if (url.pathname === '/api/setup/status') return json({ initialized: true, needs_setup: false })
      if (url.pathname === '/api/auth/login') {
        res.setHeader('Set-Cookie', 'aie_session=synthetic-session; HttpOnly; SameSite=Strict; Path=/')
        return json({ token: '', user })
      }
      if (!req.headers.cookie?.includes('aie_session=synthetic-session')) return json({ error: '未登录' }, 401)
      if (url.pathname === '/api/auth/me') return json({
        ...user,
        permissions: hasExportPermission ? [{ name: '*', scope: 'ALL' }] : [{ name: 'workflow.read', scope: 'ALL' }],
      })
      if (url.pathname === '/api/me/mcp-policy') return json({ user_id: user.id, can_write: !!policies[user.id], collector_can_write: !!collectorPolicies[user.id] })
      const policyUser = url.pathname.match(/^\/api\/users\/([^/]+)\/mcp-policy$/)?.[1]
      if (policyUser) {
        if (req.method === 'PUT') {
          assert.equal(user.roles[0], 'ADMIN')
          const chunks = []
          for await (const chunk of req) chunks.push(chunk)
          const input = JSON.parse(Buffer.concat(chunks))
          ;(input.service === 'personal-work-collector' ? collectorPolicies : policies)[policyUser] = input.can_write
        }
        return json({ user_id: policyUser, can_write: !!policies[policyUser], collector_can_write: !!collectorPolicies[policyUser] })
      }
      const detailUser = url.pathname.match(/^\/api\/users\/([^/]+)$/)?.[1]
      if (detailUser) return json({ id: detailUser, username: detailUser, roles: ['USER'], status: 'active' })
      if (url.pathname === '/api/mcp-servers') return json({ items: registrations })
      if (url.pathname === '/api/credentials') return json({ items: [
        { id: 'mine', credential_name: '我的 Jira', owner_type: 'USER', owner_id: user.id, status: 'active' },
        { id: 'other', credential_name: '其他人的凭证', owner_type: 'USER', owner_id: 'other-user', status: 'active' },
      ] })
      if (url.pathname === '/api/mcp-servers/remote' && req.method === 'PUT') {
        const chunks = []
        for await (const chunk of req) chunks.push(chunk)
        const data = JSON.parse(Buffer.concat(chunks))
        assert.equal(data.config.export_credential_id, 'mine')
        assert.deepEqual(data.config.args, ['preserved-argument'])
        serverUpdated = true
        return json({ server: { ...registrations[1], ...data } })
      }
      if (url.pathname === '/api/mcp-servers/export' || (url.pathname === '/api/me/mcp-tokens' && req.method === 'POST')) {
        assert.equal(req.method, 'POST')
        assert.equal(req.headers['x-aie-browser'], '1')
        assert.equal(req.headers.authorization, undefined)
        const chunks = []
        for await (const chunk of req) chunks.push(chunk)
        const input = JSON.parse(Buffer.concat(chunks))
        const personal = url.pathname === '/api/me/mcp-tokens'
        if (!personal && exportError) return json({ error: '存在多个本人凭证，请编辑服务选择导出凭证' }, 400)
        const selectedScope = personal ? input.services?.find(s => s.service === 'workflow-mcp')?.scope || input.scope : input.scope
        if (selectedScope === 'WRITE') assert.equal(policies[user.id], true)
        exports++
        const token = { id: `export-${exports}`, service: 'workflow-mcp', scope: selectedScope, label: personal ? input.label : `MCP 导出 (${input.format})`,
          created_at: new Date().toISOString(), expires_at: new Date(Date.now() + 90 * 86400000).toISOString() }
        tokens.unshift(token)
        const config = { mcpServers: {
          'workflow-mcp': { type: 'http', url: 'https://platform.example/mcp', headers: { Authorization: `Bearer synthetic-export-${exports}` } },
          'remote-mcp': { type: 'http', url: 'https://remote.example/mcp', headers: { Authorization: 'Token synthetic-upstream-token' } },
          'personal-work-collector': { type: 'http', url: 'https://platform.example/mcp/collector', headers: { Authorization: `Bearer synthetic-collector-${exports}` } },
        } }
        if (personal) delete config.mcpServers['remote-mcp']
        const content = input.format === 'json' ? JSON.stringify(config, null, 2)
          : '[mcp_servers."workflow-mcp"]\nurl = "https://platform.example/mcp"\n[mcp_servers."workflow-mcp".http_headers]\nAuthorization = "Bearer synthetic-export-2"\n'
        return json({ filename: input.format === 'json' ? 'mcp.json' : 'mcp.codex.toml', content, token, server_count: personal ? 2 : 3, warnings: [] })
      }
      if (url.pathname === '/api/me/mcp-tokens') return json({ items: tokens })
      if (url.pathname.startsWith('/api/me/mcp-tokens/') && req.method === 'DELETE') {
        const token = tokens.find(token => url.pathname.endsWith('/' + token.id))
        assert.ok(token)
        token.revoked_at = new Date().toISOString()
        res.writeHead(204)
        return res.end()
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
    const context = await browser.newContext({ acceptDownloads: true, viewport: { width: 1536, height: 1100 } })
    const page = await context.newPage()
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    const base = `http://127.0.0.1:${server.address().port}`
    await page.goto(base + '/login')
    await page.getByPlaceholder('请输入管理员账号').fill('export-test')
    await page.getByPlaceholder('请输入管理员密码').fill('synthetic-password')
    await page.getByRole('button', { name: '立即登录', exact: true }).click()
    await page.getByRole('button', { name: '退出登录', exact: true }).waitFor()
    await page.goto(base + '/mcp-servers')
    await page.getByRole('button', { name: '导出 MCP', exact: true }).waitFor()
    const headingBox = await page.getByRole('heading', { name: '已注册的 MCP 服务' }).boundingBox()
    const exportBox = await page.getByRole('button', { name: '导出 MCP', exact: true }).boundingBox()
    assert.ok(exportBox.x >= headingBox.x + headingBox.width, 'Export button must appear to the right of the registered-services heading')
    assert.equal(await page.getByLabel('MCP 导出格式').inputValue(), 'json')
    assert.equal(await page.getByLabel('内置 MCP 外部授权范围').inputValue(), 'READ')
    const download = async () => {
      const promise = page.waitForEvent('download')
      await page.getByRole('button', { name: '导出 MCP', exact: true }).click()
      const file = await promise
      const chunks = []
      for await (const chunk of await file.createReadStream()) chunks.push(chunk)
      return { name: file.suggestedFilename(), content: Buffer.concat(chunks).toString() }
    }
    const jsonFile = await download()
    assert.equal(jsonFile.name, 'mcp.json')
    const config = JSON.parse(jsonFile.content)
    assert.deepEqual(Object.keys(config.mcpServers), ['workflow-mcp', 'remote-mcp', 'personal-work-collector'])
    assert.notEqual(config.mcpServers['workflow-mcp'].headers.Authorization, config.mcpServers['personal-work-collector'].headers.Authorization)
    assert.match(config.mcpServers['workflow-mcp'].headers.Authorization, /^Bearer synthetic-export-/)
    assert.equal(config.mcpServers['remote-mcp'].headers.Authorization, 'Token synthetic-upstream-token')
    await page.getByLabel('MCP 导出格式').selectOption('codex')
    await page.getByLabel('内置 MCP 外部授权范围').selectOption('WRITE')
    const tomlFile = await download()
    assert.equal(tomlFile.name, 'mcp.codex.toml')
    assert.match(tomlFile.content, /\[mcp_servers\."workflow-mcp"\.http_headers\]/)
    assert.equal(tokens[0].scope, 'WRITE')
    assert.equal(await page.evaluate(() => JSON.stringify(localStorage).includes('synthetic-export')), false)
    if (process.env.MCP_EXPORT_SCREENSHOT) await page.screenshot({ path: process.env.MCP_EXPORT_SCREENSHOT, fullPage: true })
    await page.getByRole('button', { name: '外部授权', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '我的外部 MCP 授权' })
    await dialog.getByText('MCP 导出 (json) · 工作流', { exact: true }).waitFor()
    await dialog.getByRole('button', { name: '吊销', exact: true }).first().click()
    await dialog.getByText('已吊销', { exact: true }).waitFor()
    assert.equal(tokens.filter(token => token.revoked_at).length, 1)
    await page.getByTitle('关闭外部授权').click()
    exportError = true
    await page.getByRole('button', { name: '导出 MCP', exact: true }).click()
    await page.getByText('存在多个本人凭证，请编辑服务选择导出凭证', { exact: true }).waitFor()
    assert.equal(exports, 2)
    const remoteRow = page.getByRole('row').filter({ hasText: 'remote-mcp' })
    await remoteRow.getByRole('button', { name: '编辑', exact: true }).click()
    const select = page.getByLabel('导出使用的身份凭证')
    assert.equal(await select.getByRole('option', { name: '其他人的凭证', exact: true }).count(), 0)
    await select.selectOption('mine')
    await page.getByRole('button', { name: '保存修改', exact: true }).click()
    await page.getByText('MCP 服务 [remote-mcp] 更新成功', { exact: true }).waitFor()
    assert.equal(serverUpdated, true)
    hasExportPermission = false
    await page.reload()
    await page.getByRole('heading', { name: '已注册的 MCP 服务' }).waitFor()
    assert.equal(await page.getByRole('button', { name: '导出 MCP', exact: true }).count(), 0)
    // Ordinary users create their own read-only credentials from personal settings.
    user.roles = ['USER']
    policies[user.id] = false
    await page.goto(base + '/profile')
    await page.getByRole('heading', { name: '外部 MCP 授权', exact: true }).waitFor()
    await page.getByLabel('外部凭证名称').fill('我的 Cursor')
    assert.equal(await page.getByLabel('内置 MCP 外部授权范围').inputValue(), 'READ')
    assert.equal(await page.getByLabel('内置 MCP 外部授权范围').getByRole('option').count(), 1)
    assert.equal(await page.getByRole('button', { name: '授予工作流写权限', exact: true }).count(), 0)
    const personalDownload = page.waitForEvent('download')
    await page.getByRole('button', { name: '创建凭证并下载配置', exact: true }).click()
    const personalFile = await personalDownload
    const personalChunks = []
    for await (const chunk of await personalFile.createReadStream()) personalChunks.push(chunk)
    assert.deepEqual(Object.keys(JSON.parse(Buffer.concat(personalChunks)).mcpServers), ['workflow-mcp', 'personal-work-collector'])
    assert.equal(tokens[0].scope, 'READ')
    assert.equal(tokens[0].label, '我的 Cursor')
    if (process.env.MCP_SELF_SCREENSHOT) await page.screenshot({ path: process.env.MCP_SELF_SCREENSHOT, fullPage: true })
    // An administrator grants write access to that specific user.
    user.id = 'admin-user'
    user.roles = ['ADMIN']
    hasExportPermission = true
    await page.goto(base + '/users/export-user')
    await page.getByRole('button', { name: '授予工作流写权限', exact: true }).click()
    await page.getByRole('button', { name: '收回工作流写权限', exact: true }).waitFor()
    assert.equal(policies['export-user'], true)
    assert.equal(await page.getByRole('button', { name: '创建凭证并下载配置', exact: true }).count(), 0)
    user.id = 'export-user'
    user.roles = ['USER']
    hasExportPermission = false
    await page.goto(base + '/profile')
    await page.getByLabel('内置 MCP 外部授权范围').getByRole('option', { name: '工作流：读写（不含删除）', exact: true }).waitFor({ state: 'attached' })
    assert.equal(await page.getByLabel('采集器外部授权范围').getByRole('option').count(), 1)
    assert.equal(await page.getByLabel('内置 MCP 外部授权范围').inputValue(), 'READ')
    await page.getByLabel('内置 MCP 外部授权范围').selectOption('WRITE')
    const writeDownload = page.waitForEvent('download')
    await page.getByRole('button', { name: '创建凭证并下载配置', exact: true }).click()
    await writeDownload
    assert.equal(tokens[0].scope, 'WRITE')
    user.id = 'admin-user'
    user.roles = ['ADMIN']
    hasExportPermission = true
    await page.goto(base + '/users/export-user')
    await page.getByRole('button', { name: '收回工作流写权限', exact: true }).click()
    await page.getByRole('button', { name: '授予工作流写权限', exact: true }).waitFor()
    assert.equal(policies['export-user'], false)
    user.id = 'export-user'
    user.roles = ['USER']
    hasExportPermission = false
    await page.goto(base + '/profile')
    await page.getByRole('button', { name: '外部授权', exact: true }).click()
    await page.getByText('只读（写权限已收回）', { exact: true }).first().waitFor()
    await page.getByRole('dialog').getByRole('button', { name: '吊销', exact: true }).first().click()
    await page.getByRole('dialog').getByText('已吊销', { exact: true }).first().waitFor()
    assert.ok(tokens[0].revoked_at)
    assert.deepEqual(errors, [])
    console.log('MCP browser checks passed: all-server export, self-service read/write credentials, per-user admin grants/revocation and credential ownership.')
  } finally {
    await browser?.close()
    await new Promise(resolve => server.close(resolve))
  }
})().catch(error => { console.error(error); process.exitCode = 1 })
