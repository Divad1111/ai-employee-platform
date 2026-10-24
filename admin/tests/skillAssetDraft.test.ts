import { test } from 'node:test'
import assert from 'node:assert/strict'
import { decodeAsset, dirtyAsset, draftBlob, editAsset, validAssetPath } from '../src/pages/workflow/skillAssetDraft.ts'

test('viewing and reverting a BOM/CRLF file never changes its bytes', async () => {
  const bytes = new TextEncoder().encode('\uFEFFfirst\r\nsecond\r\n')
  const draft = { path: 'scripts/example.py', base: { path: 'scripts/example.py', sha256: 'baseline', size_bytes: bytes.length }, baseBytes: bytes, blob: new Blob([bytes]), ...decodeAsset(bytes), changed: false, deleted: false }
  assert.equal(dirtyAsset(draft), false)
  assert.deepEqual(new Uint8Array(await draftBlob(draft).arrayBuffer()), bytes)
  const changed = editAsset(draft, 'changed\n')
  assert.equal(dirtyAsset(changed), true)
  assert.deepEqual(new Uint8Array(await draftBlob(changed).arrayBuffer()), new TextEncoder().encode('\uFEFFchanged\r\n'))
  const reverted = editAsset(changed, draft.text!)
  assert.equal(dirtyAsset(reverted), false)
  assert.deepEqual(new Uint8Array(await draftBlob(reverted).arrayBuffer()), bytes)
})

test('empty file updates and new-then-delete are distinguishable', async () => {
  const bytes = new TextEncoder().encode('existing')
  const draft = { path: 'file', base: { path: 'file', sha256: '', size_bytes: bytes.length }, baseBytes: bytes, blob: new Blob([bytes]), ...decodeAsset(bytes), changed: false, deleted: false }
  assert.equal(dirtyAsset(editAsset(draft, '')), true)
  assert.equal((await draftBlob(editAsset(draft, '')).arrayBuffer()).byteLength, 0)
  assert.equal(dirtyAsset({ path: 'new', changed: true, deleted: true }), false)
  assert.equal(dirtyAsset({ ...draft, deleted: true }), true)
})

test('mixed line endings survive editing back to the original visible text', async () => {
  const bytes = new TextEncoder().encode('first\r\nsecond\n')
  const draft = { path: 'file', base: { path: 'file', sha256: '', size_bytes: bytes.length }, baseBytes: bytes, blob: new Blob([bytes]), ...decodeAsset(bytes), changed: false, deleted: false }
  const reverted = editAsset(editAsset(draft, 'other'), draft.text!)
  assert.equal(dirtyAsset(reverted), false)
  assert.deepEqual(new Uint8Array(await draftBlob(reverted).arrayBuffer()), bytes)
})

test('binary and invalid UTF-8 files are never decoded for text editing', () => {
  assert.equal(decodeAsset(new Uint8Array([0, 1, 2])).text, undefined)
  assert.equal(decodeAsset(new Uint8Array([0xff, 0xfe])).text, undefined)
  assert.equal(decodeAsset(new Uint8Array([])).text, '')
})

test('asset paths protect nested assets and reject unsafe or reserved names', () => {
  for (const path of ['scripts/run.py', '.config', 'assets/模板.xlsx', 'SKILL.md']) assert.equal(validAssetPath(path), true, path)
  for (const path of ['../bad', '/bad', 'a//bad', 'a\\bad', 'C:/bad', '.aie_content_hash', 'scripts/CON.py', 'skill.md']) assert.equal(validAssetPath(path), false, path)
})
