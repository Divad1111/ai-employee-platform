import { test } from 'node:test'
import assert from 'node:assert/strict'
import { rebuildSavedIndex, savedIndexMessage } from '../src/api/savedIndex.ts'

test('a committed save waits for knowledge reindex and reports the result', async () => {
  let called = 0
  const result = await rebuildSavedIndex({ revision: 3 }, async () => { called++; return { reindexed: 7 } })
  assert.equal(called, 1)
  assert.equal(result.revision, 3)
  assert.equal(result.reindexed, 7)
  assert.match(savedIndexMessage('已保存', result), /已自动重建 7 篇/)
})

test('index failure preserves a successful save and explains index-only retry', async () => {
  const result = await rebuildSavedIndex({ revision: 4, changed: true }, async () => { throw new Error('index unavailable') })
  assert.equal(result.revision, 4)
  assert.equal(result.changed, true)
  assert.equal(result.index_warning, 'index unavailable')
  assert.match(savedIndexMessage('已保存', result), /无需重复保存/)
})
