// Isolated browser regression for every backup timestamp surface, including
// UTC-to-local conversion across midnight. No business data is modified.
const { chromium } = require(process.env.PLAYWRIGHT_MODULE_PATH || 'playwright')
const assert = require('node:assert/strict')
const path = require('node:path')
const { spawn } = require('node:child_process')

;(async () => {
  let server
  const base = process.env.BACKUP_UI_URL || 'http://127.0.0.1:19092'
  let browser
  try {
    if (!process.env.BACKUP_UI_URL) {
      server = spawn(process.execPath, [path.join(__dirname, '../node_modules/vite/bin/vite.js'), 'preview', '--host', '127.0.0.1', '--port', '19092', '--strictPort'], { cwd: path.join(__dirname, '..'), windowsHide: true, stdio: 'ignore' })
      for (let i = 0; i < 100; i++) {
        if (await fetch(base).then(r => r.ok).catch(() => false)) break
        await new Promise(resolve => setTimeout(resolve, 100))
      }
    }
    browser = await chromium.launch({ channel: 'chrome', headless: true })
    const page = await browser.newPage({ viewport: { width: 1440, height: 1050 }, timezoneId: 'Asia/Shanghai' })
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    const stamp = '2026-10-23T16:30:45Z'
    const shown = '2026/10/24 00:30:45'
    const run = { id: 'run-1', backup_id: 'snapshot-1', scope: 'FULL', status: 'SUCCESS', trigger_type: 'MANUAL', created_at: stamp, updated_at: stamp, duration_ms: 1000, artifact_size: 1024, file_count: 3, checksum: 'abc', destinations: [], manifest: {} }
    await page.addInitScript(() => localStorage.setItem('aie_admin_token', 'isolated-ui-test'))
    await page.route(url => url.pathname.startsWith('/api/'), route => {
      const suffix = new URL(route.request().url()).pathname.replace(/^\/api/, '')
      const fixtures = {
        '/auth/me': { roles: ['SUPER_ADMIN'], permissions: [{ name: '*', scope: 'ALL' }], username: 'ui-test' },
        '/backups/overview': { total_runs: 1, success_runs: 1, failed_runs: 0, partial_runs: 0, total_policies: 1, active_policies: 1, total_destinations: 1, total_artifact_size: 1024, last_success_run_at: stamp, next_scheduled_at: stamp },
        '/backups/runs': { items: [run], total: 1 },
        '/backups/policies': { items: [{ id: 'policy-1', name: '每日备份', enabled: true, scope: 'FULL', schedule_type: 'CRON', cron_expression: '0 2 * * *', timezone: 'Asia/Shanghai', retention_keep_last: 7, compression_algorithm: 'zstd', compression_level: 3, encryption_enabled: false, destination_ids: [], last_run_at: stamp, next_run_at: stamp, last_run_status: 'SUCCESS' }] },
        '/backups/destinations': { items: [{ id: 'dest-1', name: '本地', type: 'LOCAL', enabled: true, config: { path: '/data/backups' }, last_test_at: stamp, last_test_status: 'SUCCESS' }] },
        '/backups/restore-jobs': { items: [{ id: 'restore-1', backup_run_id: run.id, status: 'SUCCESS', started_at: stamp, completed_at: stamp }] },
      }
      return route.fulfill({ json: fixtures[suffix] || { items: [] } })
    })
    const tab = name => page.locator('.tab-nav button').filter({ hasText: name }).click()
    const expectStamp = async locator => {
      await locator.waitFor()
      assert.ok((await locator.innerText()).includes(shown), await locator.innerText())
    }
    await page.goto(base + '/backups')
    // Summary and recent history contain a full date; midnight stays on Oct 24.
    await page.getByText(`最近一次成功备份生成于 ${shown}`, { exact: false }).waitFor()
    await expectStamp(page.locator('tbody tr').first())
    await page.getByRole('button', { name: '详情', exact: true }).first().click()
    await expectStamp(page.locator('.modal-body'))
    await page.getByRole('button', { name: '关闭', exact: true }).click()
    await tab('备份策略')
    await expectStamp(page.locator('tbody tr').first())
    assert.equal((await page.locator('tbody tr').first().innerText()).split(shown).length - 1, 2)
    await tab('存储目标')
    await expectStamp(page.getByText('最后连通测试:', { exact: false }))
    await tab('备份历史记录')
    await expectStamp(page.locator('tbody tr').first())
    await page.getByRole('button', { name: '详情', exact: true }).click()
    await expectStamp(page.locator('.modal-body'))
    await page.getByRole('button', { name: '关闭', exact: true }).click()
    await page.getByRole('button', { name: '恢复', exact: true }).click()
    await expectStamp(page.locator('.modal-body'))
    await page.locator('.modal-close-btn').click()
    await page.getByRole('button', { name: '删除', exact: true }).click()
    await expectStamp(page.getByRole('dialog'))
    await page.getByRole('button', { name: '取消', exact: true }).click()
    await tab('容灾恢复任务')
    await expectStamp(page.locator('tbody tr').first())
    assert.equal((await page.locator('tbody tr').first().innerText()).split(shown).length - 1, 2)
    assert.deepEqual(errors, [])
    console.log('PASS: full backup dates in summary, recent/history lists, details, policies, destinations, restore/delete confirmations and restore jobs; local midnight conversion')
  } finally {
    await browser?.close()
    server?.kill()
  }
})().catch(error => { console.error(error); process.exitCode = 1 })
