// Run against a local Vite dev server. PLAYWRIGHT_MODULE_PATH can point to an
// existing Playwright installation; this test uses isolated in-memory API data.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const assert = require('node:assert/strict')
const { createHash } = require('node:crypto')
const { mkdir } = require('node:fs/promises')
const path = require('node:path')
const { spawn } = require('node:child_process')
const sha = bytes => createHash('sha256').update(bytes).digest('hex')

;(async () => {
  let server
  if (process.env.ASSET_UI_START_SERVER === '1') {
    process.env.ASSET_UI_URL = 'http://127.0.0.1:19091'
    server = spawn(process.execPath, [path.join(__dirname, '../node_modules/vite/bin/vite.js'), ...(process.env.ASSET_UI_PREVIEW === '1' ? ['preview'] : []), '--host', '127.0.0.1', '--port', '19091', '--strictPort'], { cwd: path.join(__dirname, '..'), windowsHide: true, stdio: 'ignore' })
    for (let i = 0; i < 100; i++) {
      if (await fetch(process.env.ASSET_UI_URL).then(r => r.ok).catch(() => false)) break
      await new Promise(resolve => setTimeout(resolve, 100))
    }
  }
  const browser = await chromium.launch({ channel: 'chrome', headless: true })
  const page = await browser.newPage({ viewport: { width: 1440, height: 1050 } })
  const errors = []
  const consoleErrors = []
  page.on('pageerror', error => errors.push(error.message))
  page.on('console', message => { if (message.type() === 'error') consoleErrors.push(message.text()) })
  page.on('dialog', dialog => dialog.accept())
  let revision = 1
  const files = new Map([
    ['SKILL.md', Buffer.from('---\nname: asset-demo\nid: asset.demo\nversion: 1.0.0\ncustom: preserved\n---\n# Asset guide\n')],
    ['scripts/run.py', Buffer.from('print("original")\n')],
    ['references/guide.md', Buffer.from('# Reference\nKeep this untouched.\n')],
    ['assets/logo.png', Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l9sAAAAASUVORK5CYII=', 'base64')],
    ['assets/raw.bin', Buffer.from([0, 255, 1])],
  ])
  const indexes = () => [...files].map(([path, bytes]) => ({ path, sha256: sha(bytes), size_bytes: bytes.length }))
  const skill = () => ({ id: 'asset.demo', name: '资产管理验收', cursor_name: 'asset-demo', version: '1.0.0', description: '', revision, content_hash: sha(Buffer.concat([...files.values()])) })
  const commits = []
  let reindexes = 0
  let failIndex = false
  await page.addInitScript(() => localStorage.setItem('aie_admin_token', 'isolated-ui-test'))
  await page.route(url => url.pathname.startsWith('/api/'), async route => {
    const req = route.request(); const url = new URL(req.url()); const suffix = url.pathname.replace(/^\/api/, '')
    const json = (body, status = 200) => route.fulfill({ status, json: body })
    if (suffix === '/auth/me') return json({ roles: ['SUPER_ADMIN'], permissions: [{ name: '*', scope: 'ALL' }], username: 'ui-test' })
    if (suffix === '/workflow-mcp/skills') return json({ items: [skill()] })
    if (suffix === '/workflow-mcp/knowledge/reindex') {
      reindexes++
      return failIndex ? json({ error: 'index temporarily unavailable' }, 500) : json({ reindexed: 4 })
    }
    if (suffix.endsWith('/files')) return json({ skill: skill(), files: indexes() })
    if (suffix.endsWith('/file')) {
      const name = url.searchParams.get('path'); const bytes = files.get(name)
      return bytes ? route.fulfill({ status: 200, body: bytes, headers: { 'Content-Type': name.endsWith('.png') ? 'image/png' : 'application/octet-stream', 'X-Skill-Revision': String(revision) } }) : json({ error: 'missing' }, 404)
    }
    if (suffix.endsWith('/assets')) {
      const form = await new Request(req.url(), { method: 'PATCH', headers: req.headers(), body: req.postDataBuffer() }).formData()
      const manifest = JSON.parse(form.get('manifest'))
      if (manifest.revision !== revision) return json({ error: '资源冲突' }, 409)
      commits.push(manifest)
      for (const entry of manifest.writes) files.set(entry.path, Buffer.from(await form.get(entry.part).arrayBuffer()))
      for (const name of manifest.deletes) files.delete(name)
      revision++
      return json({ changed: true, revision, content_hash: skill().content_hash, skill: skill(), files: indexes().filter(f => manifest.writes.some(op => op.path === f.path)) })
    }
    return json({ items: [] })
  })
  const edit = async text => {
    const input = page.locator('.skill-assets-content .monaco-editor textarea').first()
    await input.waitFor({ state: 'attached' })
    await waitFor(() => input.isEditable())
    await input.focus()
    await page.keyboard.press('Control+A'); await page.keyboard.insertText(text)
  }
  const select = name => page.locator('.skill-tree-file').filter({ hasText: name }).click()
  const waitFor = async predicate => { for (let i = 0; i < 100; i++) { if (await predicate()) return; await page.waitForTimeout(50) } throw new Error('Condition timed out') }
  try {
    await page.goto((process.env.ASSET_UI_URL || 'http://127.0.0.1:19090') + '/mcp-servers/workflow-mcp')
    await page.locator('.tab-nav button').filter({ hasText: '技能包' }).click()
    await page.getByRole('button', { name: '编辑', exact: true }).click()
    await page.locator('.skill-tree-file').filter({ hasText: 'SKILL.md' }).waitFor()
    await waitFor(() => page.locator('.skill-assets-content .monaco-editor').count())
    assert.equal(await page.getByRole('button', { name: '保存变更', exact: true }).isDisabled(), true)
    assert.equal(await page.locator('.skill-tree-file').count(), 5)
    await select('run.py'); await edit('print("updated")\n')
    await waitFor(() => page.getByRole('button', { name: '保存变更', exact: true }).isEnabled())
    await select('guide.md'); await waitFor(() => page.locator('.skill-assets-content .monaco-editor').count())
    await select('run.py'); await page.getByRole('button', { name: '查看差异', exact: true }).click()
    await page.getByRole('button', { name: '继续编辑', exact: true }).click()
    await page.getByRole('button', { name: '保存变更', exact: true }).click()
    await waitFor(() => commits.length === 1)
    assert.deepEqual(commits[0].writes.map(f => f.path), ['scripts/run.py'])
    assert.equal(files.get('references/guide.md').toString(), '# Reference\nKeep this untouched.\n')
    await waitFor(() => page.getByRole('button', { name: '保存变更', exact: true }).isDisabled())
    assert.equal(reindexes, 1)
    await page.getByText(/已自动重建 4 篇知识文档索引/).waitFor()
    await edit('temporary change'); await edit('print("updated")\n')
    assert.equal(await page.getByRole('button', { name: '保存变更', exact: true }).isDisabled(), true)
    await select('logo.png'); await page.getByRole('img', { name: 'assets/logo.png' }).waitFor()
    const replaceChooser = page.waitForEvent('filechooser')
    await page.getByRole('button', { name: '替换文件', exact: true }).click()
    await (await replaceChooser).setFiles({ name: 'different-local-name.png', mimeType: 'image/png', buffer: files.get('assets/logo.png') })
    await waitFor(() => page.getByRole('button', { name: '新建文件', exact: true }).isEnabled())
    assert.equal(await page.getByRole('button', { name: '保存变更', exact: true }).isDisabled(), true)
    assert.equal(await page.locator('.skill-tree-file').filter({ hasText: 'different-local-name.png' }).count(), 0)
    await select('raw.bin'); await page.getByText('此文件支持下载和上传替换。', { exact: false }).waitFor()
    await page.getByRole('button', { name: '新建文件', exact: true }).click()
    await page.getByLabel('新文件路径', { exact: true }).fill('scripts/new.py')
    await page.getByRole('button', { name: '创建草稿', exact: true }).click(); await edit('new content\n')
    await select('raw.bin'); await page.getByRole('button', { name: '删除文件', exact: true }).click()
    await page.locator('.skill-assets input[type=file]').first().setInputFiles({ name: 'replacement.bin', mimeType: 'application/octet-stream', buffer: Buffer.from([0, 2, 255]) })
    await waitFor(() => page.locator('.skill-tree-file').filter({ hasText: 'replacement.bin' }).count())
    await page.getByRole('button', { name: '保存变更', exact: true }).click()
    await waitFor(() => commits.length === 2)
    assert.deepEqual(commits[1].writes.map(f => f.path).sort(), ['replacement.bin', 'scripts/new.py'])
    assert.deepEqual(commits[1].deletes, ['assets/raw.bin'])
    assert.deepEqual(files.get('replacement.bin'), Buffer.from([0, 2, 255]))
    await waitFor(() => page.getByRole('button', { name: '保存变更', exact: true }).isDisabled())
    // A concurrent change to another file is rebased without replacing it.
    await select('run.py'); await edit('print("local draft")\n')
    files.set('references/guide.md', Buffer.from('remote reference')); revision++
    await page.getByRole('button', { name: '保存变更', exact: true }).click()
    await page.getByText(/本地草稿已保留/).first().waitFor()
    assert.equal(reindexes, 2, 'a rejected save must not rebuild the index')
    await page.getByRole('button', { name: '合并最新版本并保留草稿', exact: true }).click()
    await waitFor(() => page.getByRole('button', { name: '保存变更', exact: true }).isEnabled())
    failIndex = true
    await page.getByRole('button', { name: '保存变更', exact: true }).click()
    await waitFor(() => commits.length === 3)
    await page.getByText(/无需重复保存/).waitFor()
    assert.equal(reindexes, 3)
    assert.equal(files.get('references/guide.md').toString(), 'remote reference')
    // Same-file conflicts require an explicit choice before rebasing.
    await waitFor(() => page.getByRole('button', { name: '保存变更', exact: true }).isDisabled())
    await edit('print("second local")\n')
    files.set('scripts/run.py', Buffer.from('print("remote")\n')); revision++
    await page.getByRole('button', { name: '保存变更', exact: true }).click()
    await page.getByLabel('采用服务端', { exact: true }).check()
    await page.getByRole('button', { name: '合并最新版本并保留草稿', exact: true }).click()
    await waitFor(() => page.getByRole('button', { name: '保存变更', exact: true }).isDisabled())
    if (process.env.ASSET_UI_SCREENSHOT_DIR) {
      await mkdir(process.env.ASSET_UI_SCREENSHOT_DIR, { recursive: true })
      await page.screenshot({ path: path.join(process.env.ASSET_UI_SCREENSHOT_DIR, 'skill-assets-desktop.png'), fullPage: true })
      await page.setViewportSize({ width: 720, height: 1000 })
      await page.screenshot({ path: path.join(process.env.ASSET_UI_SCREENSHOT_DIR, 'skill-assets-narrow.png'), fullPage: true })
    }
    assert.deepEqual(errors, [])
    console.log('PASS: file tree, lazy edit, diff, no-op/revert, image/binary, upload/add/delete, incremental payload, conflicts, automatic reindex and index failure after successful save')
  } catch (error) {
    console.error('UI diagnostics:', page.url(), errors, consoleErrors, (await page.locator('body').innerText()).slice(0, 2500))
    if (process.env.ASSET_UI_SCREENSHOT_DIR) await page.screenshot({ path: path.join(process.env.ASSET_UI_SCREENSHOT_DIR, 'skill-assets-failure.png'), fullPage: true })
    throw error
  } finally { await browser.close(); server?.kill() }
})().catch(error => { console.error(error); process.exitCode = 1 })
